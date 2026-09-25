package api

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dirirahmed/pc-sentinel/internal/ai"
	"github.com/dirirahmed/pc-sentinel/internal/analyzer"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// V2: performance analysis and the optional AI assistant.

// Assistant answers questions about the PC. *ai.Client implements it; a nil
// Assistant means the feature is off.
type Assistant interface {
	Enabled() bool
	Model() string
	Ask(ctx context.Context, q ai.Question) (ai.Answer, error)
}

// WithAssistant enables POST /api/ai/ask. Monitoring works without it.
func (s *Server) WithAssistant(a Assistant) *Server {
	s.ai = a
	return s
}

const (
	maxQuestionChars   = 1000
	maxHistoryTurns    = 8
	maxHistoryChars    = 4000 // per turn
	topProcessesForAI  = 5
	processListTimeout = 10 * time.Second
)

// analysisData is one consistent read of everything the analysis and the
// AI context need.
type analysisData struct {
	snap   models.Snapshot
	report analyzer.PerformanceReport
	procs  []models.Process // nil when unavailable
}

func (s *Server) collectAnalysis(ctx context.Context) (analysisData, bool) {
	snap, ok := s.src.Snapshot()
	if !ok {
		return analysisData{}, false
	}
	// Both reads degrade gracefully: missing history or processes turn into
	// "check skipped" notes rather than errors.
	pts, _, err := s.src.Series(ctx, analyzer.AnalysisWindow)
	if err != nil {
		pts = nil
	}
	pctx, cancel := context.WithTimeout(ctx, processListTimeout)
	defer cancel()
	procs, err := s.src.Processes(pctx)
	if err != nil {
		procs = nil
	} else if procs == nil {
		procs = []models.Process{}
	}
	report := analyzer.AnalyzePerformance(analyzer.PerformanceInput{
		Now: time.Now().UTC(), Points: pts, Snapshot: &snap, Processes: procs,
		DiskFree: s.cfg.Get().Thresholds.DiskFreePercent,
	})
	return analysisData{snap: snap, report: report, procs: procs}, true
}

func (s *Server) aiStatus() AIStatus {
	if s.ai == nil || !s.ai.Enabled() {
		return AIStatus{Enabled: false, Reason: "Set the " + ai.EnvAPIKey + " environment variable and restart PC Sentinel to enable Ask Sentinel."}
	}
	return AIStatus{Enabled: true, Model: s.ai.Model()}
}

func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	d, ok := s.collectAnalysis(r.Context())
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "first sample not collected yet; retry shortly")
		return
	}
	writeJSON(w, http.StatusOK, AnalysisResponse{Analysis: d.report, AI: s.aiStatus()})
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil || !s.ai.Enabled() {
		writeError(w, http.StatusServiceUnavailable, ai.ErrDisabled.Error())
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	var req AskRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request JSON: "+err.Error())
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		writeError(w, http.StatusBadRequest, "question must not be empty")
		return
	}
	if utf8.RuneCountInString(question) > maxQuestionChars {
		writeError(w, http.StatusBadRequest, "question is too long (max 1000 characters)")
		return
	}

	d, ok := s.collectAnalysis(r.Context())
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "first sample not collected yet; retry shortly")
		return
	}
	ans, err := s.ai.Ask(r.Context(), ai.Question{Text: question, History: sanitizeHistory(req.History), Data: s.aiContext(d)})
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, ai.ErrDisabled):
			status = http.StatusServiceUnavailable
		case errors.Is(err, context.DeadlineExceeded):
			status = http.StatusGatewayTimeout
		}
		s.log.Warn("ask sentinel failed", "err", err)
		writeError(w, status, "AI request failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, AskResponse{Answer: ans.Text, Model: ans.Model, GeneratedAt: time.Now().UTC(), Analysis: d.report})
}

// sanitizeHistory keeps the most recent well-formed turns, starting with a
// user turn as the Messages API expects.
func sanitizeHistory(in []ai.Turn) []ai.Turn {
	out := []ai.Turn{}
	for _, t := range in {
		t.Content = strings.TrimSpace(t.Content)
		if (t.Role != "user" && t.Role != "assistant") || t.Content == "" {
			continue
		}
		if r := []rune(t.Content); len(r) > maxHistoryChars {
			t.Content = string(r[:maxHistoryChars])
		}
		out = append(out, t)
	}
	if len(out) > maxHistoryTurns {
		out = out[len(out)-maxHistoryTurns:]
	}
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	// The new question is a user turn, so history must end with the assistant.
	if len(out) > 0 && out[len(out)-1].Role == "user" {
		out = out[:len(out)-1]
	}
	return out
}

// aiContext picks what the model may see. Hostname, executable paths and
// anything else that identifies the user are deliberately left out.
func (s *Server) aiContext(d analysisData) ai.Context {
	snap := d.snap
	host := s.src.Host()
	health := s.src.Health()
	c := ai.Context{
		System: ai.SystemInfo{
			OS:       strings.TrimSpace(host.Platform + " " + host.PlatformVersion),
			CPUModel: snap.CPU.Model, PhysicalCores: snap.CPU.PhysicalCores, LogicalCores: snap.CPU.LogicalCores,
			CPUNowPercent: snap.CPU.UsagePercent, CPUTemperatureC: snap.CPU.TemperatureC,
			MemoryTotalBytes: snap.Memory.TotalBytes, MemoryUsedPercent: snap.Memory.UsedPercent,
			GPUs: []ai.GPU{}, Drives: []ai.Drive{}, UptimeSeconds: snap.UptimeSeconds,
		},
		Analysis:     d.report,
		Health:       ai.HealthInfo{Score: health.Overall, Grade: health.Grade, Summary: health.Summary},
		ActiveAlerts: []ai.AlertInfo{},
	}
	for _, v := range snap.CPU.PerCorePercent {
		if c.System.BusiestCorePercent == nil || v > *c.System.BusiestCorePercent {
			core := v
			c.System.BusiestCorePercent = &core
		}
	}
	if !snap.GPU.Available {
		c.System.GPUUnavailable = snap.GPU.Reason
	}
	for _, g := range snap.GPU.Adapters {
		c.System.GPUs = append(c.System.GPUs, ai.GPU{Name: g.Name, UsagePercent: g.UsagePercent,
			MemoryUsedBytes: g.MemoryUsedBytes, MemoryTotalBytes: g.MemoryTotalBytes, TemperatureC: g.TemperatureC})
	}
	for _, dk := range snap.Disks {
		c.System.Drives = append(c.System.Drives, ai.Drive{Mountpoint: dk.Mountpoint, TotalBytes: dk.TotalBytes, FreePercent: dk.FreePercent})
	}
	for _, a := range s.src.ActiveAlerts() {
		c.ActiveAlerts = append(c.ActiveAlerts, ai.AlertInfo{Severity: string(a.Severity), Title: a.Title, Reason: a.Reason})
	}
	if d.procs != nil {
		c.TopByCPU = analyzer.TopProcesses(d.procs, true, topProcessesForAI)
		c.TopByMemory = analyzer.TopProcesses(d.procs, false, topProcessesForAI)
	}
	return c
}
