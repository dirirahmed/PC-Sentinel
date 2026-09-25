// Package api exposes telemetry over a small local JSON HTTP API and serves
// the built frontend.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/analyzer"
	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/dirirahmed/pc-sentinel/internal/storage"
)

// Source is the read side of the monitor.
type Source interface {
	Snapshot() (models.Snapshot, bool)
	Health() analyzer.HealthReport
	ActiveAlerts() []models.Alert
	AlertHistory(ctx context.Context, since time.Time, limit int) ([]models.Alert, error)
	Series(ctx context.Context, period time.Duration) ([]models.HistoryPoint, time.Duration, error)
	DiskHistory(ctx context.Context, period time.Duration) ([]models.DiskUsagePoint, error)
	Processes(ctx context.Context) ([]models.Process, error)
	Host() models.HostInfo
	Agent() models.AgentStats
	Reconfigure()
}

type Server struct {
	src    Source
	cfg    *config.Store
	boot   config.Config // settings the HTTP listener was started with
	static fs.FS
	log    *slog.Logger
	ai     Assistant // optional (V2); nil or disabled when no API key is set
}

// New creates the API server. static may be nil (API-only mode).
func New(src Source, cfg *config.Store, static fs.FS, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{src: src, cfg: cfg, boot: cfg.Get(), static: static, log: log}
}

var ranges = map[string]time.Duration{
	"5m": 5 * time.Minute, "15m": 15 * time.Minute, "30m": 30 * time.Minute,
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour,
}

const processNote = "Read-only view. Terminating or modifying processes is intentionally not supported."

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/system", s.handleSystem)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/cpu", s.handleCPU)
	mux.HandleFunc("GET /api/memory", s.handleMemory)
	mux.HandleFunc("GET /api/gpu", s.handleGPU)
	mux.HandleFunc("GET /api/disks", s.handleDisks)
	mux.HandleFunc("GET /api/network", s.handleNetwork)
	mux.HandleFunc("GET /api/processes", s.handleProcesses)
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("GET /api/analysis", s.handleAnalysis)
	mux.HandleFunc("POST /api/ai/ask", s.handleAsk)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "unknown endpoint")
	})
	mux.Handle("/", s.staticHandler())
	return s.recoverer(s.localOnly(securityHeaders(mux)))
}

func (s *Server) snapshot(w http.ResponseWriter) (models.Snapshot, bool) {
	snap, ok := s.src.Snapshot()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "first sample not collected yet; retry shortly")
	}
	return snap, ok
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshot(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, SystemResponse{
		Host: s.src.Host(), Timestamp: snap.Timestamp, Uptime: snap.UptimeSeconds, Health: s.src.Health(),
		CPU: snap.CPU, Memory: snap.Memory, GPU: snap.GPU, Disks: snap.Disks, DiskIO: snap.DiskIO,
		Network: snap.Network, ActiveAlerts: s.src.ActiveAlerts(), Collectors: snap.Collectors, Agent: s.src.Agent(),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.snapshot(w); ok {
		writeJSON(w, http.StatusOK, s.src.Health())
	}
}

// componentSummary pulls history for ?range= and picks one metric's stats.
// History is optional context: if it fails the current values still return.
func (s *Server) componentSummary(r *http.Request, pick func(analyzer.SeriesSummary) analyzer.Stat) (string, *analyzer.Stat) {
	name, period, err := parseRange(r, "15m")
	if err != nil {
		return "", nil
	}
	pts, _, err := s.src.Series(r.Context(), period)
	if err != nil {
		return name, nil
	}
	st := pick(analyzer.Summarize(pts))
	return name, &st
}

func (s *Server) handleCPU(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshot(w)
	if !ok {
		return
	}
	rng, sum := s.componentSummary(r, func(x analyzer.SeriesSummary) analyzer.Stat { return x.CPU })
	writeJSON(w, http.StatusOK, ComponentResponse[models.CPUStats]{snap.Timestamp, snap.CPU, rng, sum})
}

func (s *Server) handleMemory(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshot(w)
	if !ok {
		return
	}
	rng, sum := s.componentSummary(r, func(x analyzer.SeriesSummary) analyzer.Stat { return x.Memory })
	writeJSON(w, http.StatusOK, ComponentResponse[models.MemoryStats]{snap.Timestamp, snap.Memory, rng, sum})
}

func (s *Server) handleGPU(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshot(w)
	if !ok {
		return
	}
	rng, sum := s.componentSummary(r, func(x analyzer.SeriesSummary) analyzer.Stat { return x.GPU })
	writeJSON(w, http.StatusOK, ComponentResponse[models.GPUReport]{snap.Timestamp, snap.GPU, rng, sum})
}

func (s *Server) handleDisks(w http.ResponseWriter, r *http.Request) {
	if snap, ok := s.snapshot(w); ok {
		writeJSON(w, http.StatusOK, DisksResponse{snap.Timestamp, snap.Disks, snap.DiskIO})
	}
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	snap, ok := s.snapshot(w)
	if !ok {
		return
	}
	resp := NetworkResponse{Timestamp: snap.Timestamp, Current: snap.Network}
	name, period, err := parseRange(r, "15m")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp.Range = name
	if pts, _, err := s.src.Series(r.Context(), period); err == nil {
		sum := analyzer.Summarize(pts)
		resp.Download, resp.Upload = sum.NetRx, sum.NetTx
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	procs, err := s.src.Processes(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "process list unavailable: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ProcessesResponse{Timestamp: time.Now().UTC(), Count: len(procs), Processes: procs, Note: processNote})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	name, period, err := parseRange(r, "15m")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pts, bucket, err := s.src.Series(r.Context(), period)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrUnavailable) {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "history unavailable: "+err.Error())
		return
	}
	resp := MetricsResponse{Range: name, BucketSeconds: bucket.Seconds(), Points: pts, Summary: analyzer.Summarize(pts)}
	if r.URL.Query().Get("disks") == "1" {
		// Disk fill level changes slowly and is only kept in SQLite.
		d, err := s.src.DiskHistory(r.Context(), max(period, time.Hour))
		if err != nil {
			resp.DisksError = err.Error()
		}
		resp.Disks = d
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = n
	}
	resp := AlertsResponse{Active: s.src.ActiveAlerts(), History: []models.Alert{}}
	days := s.cfg.Get().RetentionDays
	hist, err := s.src.AlertHistory(r.Context(), time.Now().AddDate(0, 0, -days), limit)
	if err != nil {
		resp.HistoryError = err.Error()
	} else {
		resp.History = hist
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsResponse(s.cfg.Get()))
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	// Requiring JSON blocks cross-site form posts, which can't set this type.
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	next := s.cfg.Get() // fields omitted from the body keep their current values
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&next); err != nil {
		writeError(w, http.StatusBadRequest, "invalid settings JSON: "+err.Error())
		return
	}
	if err := next.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.cfg.Update(next); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.src.Reconfigure()
	s.log.Info("settings updated")
	writeJSON(w, http.StatusOK, s.settingsResponse(next))
}

func (s *Server) settingsResponse(c config.Config) SettingsResponse {
	resp := SettingsResponse{Config: c}
	if c.Port != s.boot.Port {
		resp.RestartRequired = append(resp.RestartRequired, "port")
	}
	if c.ListenAddress != s.boot.ListenAddress {
		resp.RestartRequired = append(resp.RestartRequired, "listenAddress")
	}
	return resp
}

func parseRange(r *http.Request, def string) (string, time.Duration, error) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = def
	}
	d, ok := ranges[name]
	if !ok {
		return "", 0, fmt.Errorf("unsupported range %q (use 5m, 15m, 30m, 1h, 6h, 24h or 7d)", name)
	}
	return name, d, nil
}

// staticHandler serves the embedded single-page app, falling back to
// index.html for client-side routes.
func (s *Server) staticHandler() http.Handler {
	if s.static == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotFound, "frontend not bundled; run the Vite dev server or build web/")
		})
	}
	files := http.FileServerFS(s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name != "" {
			if f, err := s.static.Open(name); err == nil {
				f.Close()
				if strings.HasPrefix(name, "assets/") {
					// Vite fingerprints asset filenames, so they never change.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.static, "index.html")
		if err != nil {
			http.Error(w, "frontend not built: run `npm run build` in web/ and rebuild", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}

// localOnly rejects requests whose Host header isn't a loopback name. Binding
// to 127.0.0.1 stops remote access; this also stops DNS-rebinding pages in
// the user's own browser from reading telemetry.
func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		allowed := host == "localhost" || host == s.boot.ListenAddress
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			allowed = true
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "host not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("handler panic", "path", r.URL.Path, "panic", rec)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
