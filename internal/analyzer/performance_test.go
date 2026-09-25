package analyzer

import (
	"strings"
	"testing"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// series builds 5 minutes of 2-second samples with constant CPU, memory and
// (optionally) GPU values.
func series(cpu, mem float64, gpu *float64) []models.HistoryPoint {
	var pts []models.HistoryPoint
	for i := 0; i <= 150; i++ {
		p := models.HistoryPoint{Timestamp: t0.Add(time.Duration(i) * 2 * time.Second), CPU: cpu, CPUMax: cpu, Memory: mem}
		if gpu != nil {
			g := *gpu
			p.GPU, p.GPUMax = &g, &g
		}
		pts = append(pts, p)
	}
	return pts
}

func analyze(pts []models.HistoryPoint, procs []models.Process, snap *models.Snapshot) PerformanceReport {
	if procs == nil {
		procs = []models.Process{}
	}
	return AnalyzePerformance(PerformanceInput{
		Now: t0.Add(5 * time.Minute), Points: pts, Snapshot: snap, Processes: procs,
		DiskFree: config.Default().Thresholds.DiskFreePercent,
	})
}

func findRule(r PerformanceReport, rule string) *Finding {
	for i := range r.Findings {
		if r.Findings[i].Rule == rule {
			return &r.Findings[i]
		}
	}
	return nil
}

func TestIdleSystemHasNoFindings(t *testing.T) {
	r := analyze(series(12, 40, f(5)), nil, nil)
	if len(r.Findings) != 0 || r.Status != StatusOK {
		t.Fatalf("expected ok with no findings, got %s %+v", r.Status, r.Findings)
	}
	if !strings.Contains(r.Summary, "No performance problems") || !strings.Contains(r.Summary, "CPU averaged 12%") {
		t.Errorf("summary should quote the measured averages: %q", r.Summary)
	}
	if len(r.Recommendations) != 0 {
		t.Errorf("no findings means no recommendations, got %v", r.Recommendations)
	}
}

func TestHighCPUDetectionAndSeverity(t *testing.T) {
	cases := []struct {
		cpu  float64
		want models.Severity
	}{{84, ""}, {85, models.SeverityWarning}, {94.9, models.SeverityWarning}, {95, models.SeverityCritical}}
	for _, c := range cases {
		r := analyze(series(c.cpu, 30, nil), nil, nil)
		got := findRule(r, "high_cpu")
		if c.want == "" {
			if got != nil {
				t.Errorf("cpu %v: unexpected finding %+v", c.cpu, got)
			}
			continue
		}
		if got == nil || got.Severity != c.want {
			t.Fatalf("cpu %v: want %s finding, got %+v", c.cpu, c.want, got)
		}
		if !strings.Contains(got.Evidence, "last 5 minutes") || got.Recommendation == "" || got.Explanation == "" {
			t.Errorf("finding must carry evidence, explanation and a recommendation: %+v", got)
		}
	}
}

func TestHighMemoryDetection(t *testing.T) {
	snap := &models.Snapshot{Memory: models.MemoryStats{TotalBytes: 16 << 30, UsedBytes: 15 << 30, UsedPercent: 93.75}}
	r := analyze(series(10, 92, nil), nil, snap)
	m := findRule(r, "high_memory")
	if m == nil || m.Severity != models.SeverityWarning {
		t.Fatalf("expected memory warning, got %+v", r.Findings)
	}
	if !strings.Contains(m.Evidence, "92%") || !strings.Contains(m.Evidence, "15.0 GB of 16.0 GB") {
		t.Errorf("evidence should quote real values: %q", m.Evidence)
	}
	if r := analyze(series(10, 96, nil), nil, nil); findRule(r, "high_memory").Severity != models.SeverityCritical {
		t.Error("96% memory should be critical")
	}
}

func TestSustainedChecksNeedAMinuteOfHistory(t *testing.T) {
	short := series(99, 99, f(10))[:20] // 38 seconds
	r := analyze(short, nil, nil)
	if findRule(r, "high_cpu") != nil || findRule(r, "high_memory") != nil {
		t.Fatalf("must not judge sustained load from 38 s of data: %+v", r.Findings)
	}
	if r.Status != StatusCollecting || len(r.Notes) == 0 {
		t.Errorf("expected collecting status with a note, got %s %v", r.Status, r.Notes)
	}
}

func TestProcessDetectionGroupsByName(t *testing.T) {
	procs := []models.Process{
		{PID: 0, Name: "System Idle Process", CPUPercent: f(90)}, // idle time, never a finding
		{PID: 10, Name: "chrome.exe", CPUPercent: f(15), MemoryBytes: 2 << 30},
		{PID: 11, Name: "chrome.exe", CPUPercent: f(15), MemoryBytes: 2 << 30},
		{PID: 12, Name: "game.exe", CPUPercent: f(55), MemoryBytes: 1 << 30},
		{PID: 13, Name: "notepad.exe", CPUPercent: f(1), MemoryBytes: 50 << 20},
		{PID: 14, Name: "protected.exe", CPUPercent: nil, MemoryBytes: 10 << 20},
	}
	snap := &models.Snapshot{Memory: models.MemoryStats{TotalBytes: 16 << 30}}
	r := analyze(series(20, 40, nil), procs, snap)

	byTarget := map[string]Finding{}
	for _, f := range r.Findings {
		byTarget[f.Rule+":"+f.Target] = f
	}
	if g, ok := byTarget["process_cpu:game.exe"]; !ok || g.Severity != models.SeverityCritical {
		t.Errorf("game.exe at 55%% CPU should be critical: %+v", r.Findings)
	}
	c, ok := byTarget["process_cpu:chrome.exe"]
	if !ok || c.Severity != models.SeverityWarning || !strings.Contains(c.Evidence, "(2 processes)") || c.Value != 30 {
		t.Errorf("chrome.exe processes should be combined to 30%% CPU: %+v", c)
	}
	if m, ok := byTarget["process_memory:chrome.exe"]; !ok || m.Severity != models.SeverityWarning || m.Value != 25 {
		t.Errorf("chrome.exe at 4 GB of 16 GB should be a memory warning: %+v", m)
	}
	for key := range byTarget {
		if strings.Contains(key, "Idle") || strings.Contains(key, "notepad") || strings.Contains(key, "protected") {
			t.Errorf("unexpected finding %s", key)
		}
	}
	// Most severe first.
	if r.Findings[0].Severity != models.SeverityCritical || r.Status != "critical" {
		t.Errorf("findings must be sorted most severe first; status %s, first %+v", r.Status, r.Findings[0])
	}
}

func TestMissingProcessListIsNotedNotGuessed(t *testing.T) {
	r := AnalyzePerformance(PerformanceInput{Now: t0, Points: series(10, 30, nil), Processes: nil})
	if findRule(r, "process_cpu") != nil || len(r.Notes) == 0 || !strings.Contains(strings.Join(r.Notes, " "), "process") {
		t.Errorf("expected a note about the missing process list, got %v", r.Notes)
	}
}

func TestCPUBottleneckDetection(t *testing.T) {
	r := analyze(series(91, 40, f(48)), nil, nil)
	b := findRule(r, "cpu_bottleneck")
	if b == nil || b.Severity != models.SeverityWarning {
		t.Fatalf("expected possible CPU bottleneck, got %+v", r.Findings)
	}
	want := "CPU averaged 91% over the last 5 minutes while GPU usage averaged 48%."
	if b.Evidence != want {
		t.Errorf("evidence = %q, want %q", b.Evidence, want)
	}
}

func TestNoBottleneckWhenGPUIsBusyIdleOrUnmeasured(t *testing.T) {
	cases := map[string]PerformanceReport{
		"gpu busy":       analyze(series(90, 40, f(95)), nil, nil),
		"gpu idle":       analyze(series(90, 40, f(3)), nil, nil),
		"small gap":      analyze(series(82, 40, f(60)), nil, nil),
		"cpu not high":   analyze(series(70, 40, f(30)), nil, nil),
		"gpu unmeasured": analyze(series(95, 40, nil), nil, nil),
	}
	for name, r := range cases {
		if findRule(r, "cpu_bottleneck") != nil {
			t.Errorf("%s: unexpected bottleneck finding", name)
		}
	}
	if r := cases["gpu unmeasured"]; !strings.Contains(strings.Join(r.Notes, " "), "GPU") {
		t.Errorf("skipped GPU checks must be explained, notes = %v", r.Notes)
	}
	if g := findRule(cases["gpu busy"], "high_gpu"); g == nil || g.Severity != models.SeverityInfo {
		t.Errorf("95%% GPU should be an info finding (normal while gaming), got %+v", g)
	}
}

func TestDiskFindings(t *testing.T) {
	pts := series(10, 30, nil)
	for i := range pts {
		r, w := float64(180<<20), float64(40<<20)
		pts[i].DiskRead, pts[i].DiskWrite = &r, &w
	}
	snap := &models.Snapshot{Disks: []models.DiskStats{
		{Mountpoint: "C:", FreePercent: 4, FreeBytes: 10 << 30, TotalBytes: 250 << 30},
		{Mountpoint: "D:", FreePercent: 60, TotalBytes: 1 << 40},
		{Mountpoint: "X:", FreePercent: 0, TotalBytes: 0}, // no capacity reported
	}}
	r := analyze(pts, nil, snap)
	if io := findRule(r, "high_disk_io"); io == nil || io.Severity != models.SeverityWarning || !strings.Contains(io.Evidence, "220 MB/s") {
		t.Errorf("expected disk I/O warning quoting 220 MB/s, got %+v", io)
	}
	if d := findRule(r, "low_disk_space"); d == nil || d.Severity != models.SeverityCritical || d.Target != "C:" {
		t.Errorf("expected critical low space on C:, got %+v", d)
	}
}

func TestRecommendationsAreDeduplicatedAndOrdered(t *testing.T) {
	r := analyze(series(96, 96, f(40)), nil, nil)
	if len(r.Findings) < 3 {
		t.Fatalf("expected cpu, memory and bottleneck findings, got %+v", r.Findings)
	}
	if len(r.Recommendations) != len(r.Findings) {
		t.Errorf("one recommendation per distinct finding, got %d for %d", len(r.Recommendations), len(r.Findings))
	}
	if r.Recommendations[0] != r.Findings[0].Recommendation {
		t.Error("recommendations should follow finding severity order")
	}
	if !strings.HasPrefix(r.Summary, "3 issues found") {
		t.Errorf("summary = %q", r.Summary)
	}
}

func TestTopProcesses(t *testing.T) {
	procs := []models.Process{
		{PID: 1, Name: "a.exe", CPUPercent: f(5), MemoryBytes: 100},
		{PID: 2, Name: "b.exe", CPUPercent: f(20), MemoryBytes: 10},
		{PID: 3, Name: "a.exe", CPUPercent: f(1), MemoryBytes: 100},
		{PID: 4, Name: "c.exe", MemoryBytes: 500},
	}
	cpu := TopProcesses(procs, true, 5)
	if len(cpu) != 2 || cpu[0].Name != "b.exe" || cpu[1].Processes != 2 || *cpu[1].CPUPercent != 6 {
		t.Errorf("by CPU = %+v", cpu)
	}
	mem := TopProcesses(procs, false, 1)
	if len(mem) != 1 || mem[0].Name != "c.exe" || mem[0].CPUPercent != nil {
		t.Errorf("by memory = %+v", mem)
	}
}
