package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// Performance analysis (V2) turns the telemetry V1 already collects into a
// short list of explained findings. Like the health score, it is a set of
// fixed, documented thresholds over recent history: no learning, no
// prediction, and nothing is reported that wasn't measured.

// AnalysisWindow is how much recent history the analysis looks at.
const AnalysisWindow = 5 * time.Minute

// Thresholds for the analysis rules. They are documented in the README; keep
// the two in sync.
const (
	// Sustained checks (CPU, memory, GPU, disk I/O, bottleneck) need at least
	// this much history so a freshly started agent doesn't judge a spike.
	minAnalysisCoverage = time.Minute

	cpuAvgWarning  = 85.0 // % average over the window
	cpuAvgCritical = 95.0

	memAvgWarning  = 85.0 // % of RAM, average over the window
	memAvgCritical = 95.0

	gpuAvgInfo    = 85.0 // % average; heavy GPU use is normal while gaming
	gpuAvgWarning = 97.0

	// Possible CPU bottleneck: the CPU is near its limit while the GPU is
	// clearly under-used but not idle (an idle GPU has no work to be starved of).
	bottleneckCPUMin = 80.0
	bottleneckGPUMin = 15.0
	bottleneckGPUMax = 60.0
	bottleneckGap    = 25.0

	diskIOInfo    = 50 << 20  // bytes/s, read+write average over the window
	diskIOWarning = 200 << 20 // bytes/s

	procCPUWarning     = 25.0 // % of the whole machine (100% = every core busy)
	procCPUCritical    = 50.0
	procMemWarning     = 20.0 // % of RAM
	procMemCritical    = 35.0
	maxProcessFindings = 3 // per metric
)

// Analysis statuses. The non-ok values mirror the highest finding severity.
const (
	StatusOK         = "ok"
	StatusCollecting = "collecting"
)

// Finding is one detected issue, with the evidence behind it.
type Finding struct {
	ID             string          `json:"id"`
	Rule           string          `json:"rule"`
	Category       string          `json:"category"` // cpu, memory, gpu, disk, process
	Target         string          `json:"target,omitempty"`
	Severity       models.Severity `json:"severity"`
	Title          string          `json:"title"`
	Explanation    string          `json:"explanation"`
	Evidence       string          `json:"evidence"`
	Value          float64         `json:"value"`
	Threshold      float64         `json:"threshold"`
	Unit           string          `json:"unit"`
	Recommendation string          `json:"recommendation"`
}

// PerformanceReport is the result of one analysis pass.
type PerformanceReport struct {
	GeneratedAt time.Time `json:"generatedAt"`
	// Status is "ok", "collecting" (not enough history yet, nothing found),
	// or the highest finding severity: "info", "warning" or "critical".
	Status  string `json:"status"`
	Summary string `json:"summary"`
	// WindowSeconds is how much history was actually available (up to 5 min).
	WindowSeconds   float64       `json:"windowSeconds"`
	Samples         int           `json:"samples"`
	Metrics         SeriesSummary `json:"metrics"`
	Findings        []Finding     `json:"findings"`
	Recommendations []string      `json:"recommendations"`
	// Notes explain checks that were skipped because data was missing.
	Notes []string `json:"notes"`
}

// PerformanceInput is everything the analysis looks at.
type PerformanceInput struct {
	Now      time.Time
	Points   []models.HistoryPoint // trailing window, oldest first
	Snapshot *models.Snapshot      // latest sample; nil if none yet
	// Processes is nil when the process list couldn't be read.
	Processes []models.Process
	DiskFree  config.Threshold
}

// AnalyzePerformance runs every rule and returns findings sorted most severe first.
func AnalyzePerformance(in PerformanceInput) PerformanceReport {
	r := PerformanceReport{
		GeneratedAt: in.Now, Metrics: Summarize(in.Points), Samples: len(in.Points),
		Findings: []Finding{}, Recommendations: []string{}, Notes: []string{},
	}
	span := seriesSpan(in.Points)
	r.WindowSeconds = span.Seconds()
	window := describeWindow(span)
	m := r.Metrics

	if span >= minAnalysisCoverage {
		r.add(cpuFinding(m.CPU, window))
		r.add(memoryFinding(m.Memory, in.Snapshot, window))
		if m.GPU.Available {
			r.add(gpuFinding(m.GPU, window))
			r.add(bottleneckFinding(m.CPU, m.GPU, window))
		} else {
			r.note("GPU utilization is not available on this system, so the GPU and CPU/GPU bottleneck checks were skipped.")
		}
		if m.DiskRead.Available || m.DiskWrite.Available {
			r.add(diskIOFinding(m.DiskRead, m.DiskWrite, window))
		} else {
			r.note("Disk throughput counters are unavailable, so the disk activity check was skipped.")
		}
	} else {
		r.note(fmt.Sprintf("Only %d seconds of history so far. The CPU, memory, GPU, disk activity and bottleneck checks start once PC Sentinel has 1 minute of data.",
			int(span.Seconds())))
	}

	if in.Processes == nil {
		r.note("The process list could not be read, so per-process checks were skipped.")
	} else {
		var totalMem uint64
		if in.Snapshot != nil {
			totalMem = in.Snapshot.Memory.TotalBytes
		}
		for _, f := range processFindings(in.Processes, totalMem) {
			r.add(&f)
		}
	}
	if in.Snapshot != nil {
		for _, d := range in.Snapshot.Disks {
			r.add(diskSpaceFinding(d, in.DiskFree))
		}
	}

	sort.SliceStable(r.Findings, func(i, j int) bool {
		return r.Findings[i].Severity.Rank() > r.Findings[j].Severity.Rank()
	})
	seen := map[string]bool{}
	for _, f := range r.Findings {
		if !seen[f.Recommendation] {
			seen[f.Recommendation] = true
			r.Recommendations = append(r.Recommendations, f.Recommendation)
		}
	}
	r.Status, r.Summary = summarizeFindings(r.Findings, m, span, window)
	return r
}

func (r *PerformanceReport) add(f *Finding) {
	if f != nil {
		r.Findings = append(r.Findings, *f)
	}
}

func (r *PerformanceReport) note(s string) { r.Notes = append(r.Notes, s) }

// level returns the severity for value against ascending thresholds, or "" if
// below all of them.
func level(value float64, levels ...Level) (models.Severity, float64) {
	var sev models.Severity
	var th float64
	for _, l := range levels {
		if value >= l.Threshold {
			sev, th = l.Severity, l.Threshold
		}
	}
	return sev, th
}

func cpuFinding(cpu Stat, window string) *Finding {
	sev, th := level(cpu.Avg, Level{models.SeverityWarning, cpuAvgWarning}, Level{models.SeverityCritical, cpuAvgCritical})
	if sev == "" {
		return nil
	}
	return &Finding{
		ID: "high_cpu", Rule: "high_cpu", Category: "cpu", Severity: sev, Title: "Sustained high CPU usage",
		Value: cpu.Avg, Threshold: th, Unit: "%",
		Evidence: fmt.Sprintf("CPU averaged %s over the %s (peak %s).", pct(cpu.Avg), window, pct(cpu.Peak)),
		Explanation: "The processor has been close to fully busy for most of this period, so programs have to wait for CPU time. " +
			"This is a common cause of a sluggish, stuttering or slow-to-respond PC.",
		Recommendation: "Check the Processes page (sort by CPU) and close programs you don't need right now. " +
			"If the load comes from something you are using, reduce what it is doing (fewer browser tabs, pausing background tasks, lower CPU-heavy settings).",
	}
}

func memoryFinding(mem Stat, snap *models.Snapshot, window string) *Finding {
	sev, th := level(mem.Avg, Level{models.SeverityWarning, memAvgWarning}, Level{models.SeverityCritical, memAvgCritical})
	if sev == "" {
		return nil
	}
	evidence := fmt.Sprintf("Memory use averaged %s over the %s (currently %s", pct(mem.Avg), window, pct(mem.Current))
	if snap != nil && snap.Memory.TotalBytes > 0 {
		evidence += fmt.Sprintf(", %s of %s in use", bytesText(snap.Memory.UsedBytes), bytesText(snap.Memory.TotalBytes))
	}
	evidence += ")."
	return &Finding{
		ID: "high_memory", Rule: "high_memory", Category: "memory", Severity: sev, Title: "High memory usage",
		Value: mem.Avg, Threshold: th, Unit: "%", Evidence: evidence,
		Explanation: "When RAM is nearly full, Windows moves data out to the page file on disk, which is far slower than RAM. " +
			"Switching between apps and opening new ones gets slow.",
		Recommendation: "Close memory-heavy programs you aren't using (browsers with many tabs are a common cause). " +
			"If memory is regularly this full during your normal work, the PC would benefit from more RAM.",
	}
}

func gpuFinding(gpu Stat, window string) *Finding {
	sev, th := level(gpu.Avg, Level{models.SeverityInfo, gpuAvgInfo}, Level{models.SeverityWarning, gpuAvgWarning})
	if sev == "" {
		return nil
	}
	return &Finding{
		ID: "high_gpu", Rule: "high_gpu", Category: "gpu", Severity: sev, Title: "High GPU usage",
		Value: gpu.Avg, Threshold: th, Unit: "%",
		Evidence: fmt.Sprintf("GPU usage averaged %s over the %s (peak %s).", pct(gpu.Avg), window, pct(gpu.Peak)),
		Explanation: "The graphics card is working at or near its limit. While gaming or rendering this is expected and means " +
			"the GPU is the limiting component (GPU-bound), not that something is wrong. Outside those tasks it is unusual.",
		Recommendation: "If this happens in a game and performance is too low, lower GPU-heavy settings such as resolution, render scale, " +
			"shadows or ray tracing. If nothing graphics-heavy is running, look for an unexpected program on the Processes page.",
	}
}

func bottleneckFinding(cpu, gpu Stat, window string) *Finding {
	if cpu.Avg < bottleneckCPUMin || gpu.Avg < bottleneckGPUMin || gpu.Avg > bottleneckGPUMax || cpu.Avg-gpu.Avg < bottleneckGap {
		return nil
	}
	return &Finding{
		ID: "cpu_bottleneck", Rule: "cpu_bottleneck", Category: "cpu", Severity: models.SeverityWarning,
		Title: "Possible CPU bottleneck", Value: cpu.Avg, Threshold: bottleneckCPUMin, Unit: "%",
		Evidence: fmt.Sprintf("CPU averaged %s over the %s while GPU usage averaged %s.", pct(cpu.Avg), window, pct(gpu.Avg)),
		Explanation: "The processor is close to its limit while the graphics card still has headroom. " +
			"A CPU-heavy workload may be limiting how much work reaches the GPU.",
		Recommendation: "Close unnecessary CPU-heavy processes, or lower CPU-intensive settings in the application or game " +
			"(for example view distance, crowd or physics detail, or cap the frame rate).",
	}
}

func diskIOFinding(read, write Stat, window string) *Finding {
	total := read.Avg + write.Avg
	sev, th := level(total, Level{models.SeverityInfo, diskIOInfo}, Level{models.SeverityWarning, diskIOWarning})
	if sev == "" {
		return nil
	}
	return &Finding{
		ID: "high_disk_io", Rule: "high_disk_io", Category: "disk", Severity: sev, Title: "High disk activity",
		Value: total, Threshold: th, Unit: "B/s",
		Evidence: fmt.Sprintf("Disk throughput averaged %s over the %s (read %s, write %s).",
			rateText(total), window, rateText(read.Avg), rateText(write.Avg)),
		Explanation: "Sustained heavy disk activity can slow program launches and file access, especially on hard drives. " +
			"PC Sentinel measures throughput, not how busy the drive is, so this shows heavy use rather than proving a bottleneck.",
		Recommendation: "Look for large downloads, game updates, backups, antivirus scans or file indexing running in the background, " +
			"and let them finish or schedule them for when you aren't using the PC.",
	}
}

func diskSpaceFinding(d models.DiskStats, th config.Threshold) *Finding {
	if d.TotalBytes == 0 { // no capacity reported: nothing to judge
		return nil
	}
	var sev models.Severity
	var limit float64
	switch {
	case d.FreePercent <= th.Critical:
		sev, limit = models.SeverityCritical, th.Critical
	case d.FreePercent <= th.Warning:
		sev, limit = models.SeverityWarning, th.Warning
	default:
		return nil
	}
	return &Finding{
		ID: "low_disk_space:" + d.Mountpoint, Rule: "low_disk_space", Category: "disk", Target: d.Mountpoint, Severity: sev,
		Title: "Low free space on " + d.Mountpoint, Value: d.FreePercent, Threshold: limit, Unit: "%",
		Evidence: fmt.Sprintf("%s has %.1f%% free (%s of %s).", d.Mountpoint, d.FreePercent, bytesText(d.FreeBytes), bytesText(d.TotalBytes)),
		Explanation: "Windows needs free space for the page file, updates and temporary files. " +
			"A nearly full drive can slow the PC down and make updates fail.",
		Recommendation: fmt.Sprintf("Free up space on %s: empty the Recycle Bin, run Storage Sense or Disk Cleanup, "+
			"and uninstall programs or move large files you no longer need.", d.Mountpoint),
	}
}

// processGroup aggregates processes that share an executable name, so a
// browser split across 30 processes is judged as one program.
type processGroup struct {
	name     string
	count    int
	cpu      float64
	hasCPU   bool
	memBytes uint64
	memPct   float64
}

// idleProcess reports pseudo-processes whose "CPU use" is really idle time.
func idleProcess(p models.Process) bool {
	n := strings.ToLower(p.Name)
	return p.PID == 0 || n == "system idle process" || n == "idle"
}

func groupProcesses(procs []models.Process) []processGroup {
	idx := map[string]int{}
	var groups []processGroup
	for _, p := range procs {
		if idleProcess(p) {
			continue
		}
		key := strings.ToLower(p.Name)
		i, ok := idx[key]
		if !ok {
			i = len(groups)
			idx[key] = i
			groups = append(groups, processGroup{name: p.Name})
		}
		g := &groups[i]
		g.count++
		if p.CPUPercent != nil {
			g.cpu += *p.CPUPercent
			g.hasCPU = true
		}
		g.memBytes += p.MemoryBytes
		g.memPct += p.MemoryPct
	}
	return groups
}

// TopProcesses returns up to n process groups ordered by CPU or memory,
// the same grouping the analysis uses. It is exported for the AI context.
func TopProcesses(procs []models.Process, byCPU bool, n int) []ProcessUsage {
	groups := groupProcesses(procs)
	sort.SliceStable(groups, func(i, j int) bool {
		if byCPU {
			return groups[i].cpu > groups[j].cpu
		}
		return groups[i].memBytes > groups[j].memBytes
	})
	out := []ProcessUsage{}
	for _, g := range groups {
		if len(out) == n {
			break
		}
		if byCPU && !g.hasCPU {
			continue
		}
		u := ProcessUsage{Name: g.name, Processes: g.count, MemoryBytes: g.memBytes, MemoryPercent: round1(g.memPct)}
		if g.hasCPU {
			c := round1(g.cpu)
			u.CPUPercent = &c
		}
		out = append(out, u)
	}
	return out
}

// ProcessUsage is a program's combined usage across its processes.
type ProcessUsage struct {
	Name          string   `json:"name"`
	Processes     int      `json:"processes"`
	CPUPercent    *float64 `json:"cpuPercent"`
	MemoryBytes   uint64   `json:"memoryBytes"`
	MemoryPercent float64  `json:"memoryPercent"`
}

func processFindings(procs []models.Process, totalMem uint64) []Finding {
	groups := groupProcesses(procs)
	var out []Finding

	sort.SliceStable(groups, func(i, j int) bool { return groups[i].cpu > groups[j].cpu })
	for i, g := range groups {
		if i == maxProcessFindings {
			break
		}
		sev, th := level(g.cpu, Level{models.SeverityWarning, procCPUWarning}, Level{models.SeverityCritical, procCPUCritical})
		if !g.hasCPU || sev == "" {
			break
		}
		out = append(out, Finding{
			ID: "process_cpu:" + strings.ToLower(g.name), Rule: "process_cpu", Category: "process", Target: g.name,
			Severity: sev, Title: g.name + " is using a lot of CPU", Value: g.cpu, Threshold: th, Unit: "%",
			Evidence: fmt.Sprintf("%s%s was using %s of total CPU when this analysis ran.", g.name, countText(g.count), pct(g.cpu)),
			Explanation: "One program is taking a large share of the processor, leaving less for everything else. " +
				"This is a point-in-time reading, so a short burst (such as a program starting up) can also show here.",
			Recommendation: fmt.Sprintf("If you don't need %s right now, close it or check whether it is stuck on a task; "+
				"if you do need it, reduce what it is doing.", g.name),
		})
	}

	sort.SliceStable(groups, func(i, j int) bool { return groups[i].memBytes > groups[j].memBytes })
	for i, g := range groups {
		if i == maxProcessFindings {
			break
		}
		share := g.memPct
		if totalMem > 0 {
			share = float64(g.memBytes) / float64(totalMem) * 100
		}
		sev, th := level(share, Level{models.SeverityWarning, procMemWarning}, Level{models.SeverityCritical, procMemCritical})
		if sev == "" {
			break
		}
		out = append(out, Finding{
			ID: "process_memory:" + strings.ToLower(g.name), Rule: "process_memory", Category: "process", Target: g.name,
			Severity: sev, Title: g.name + " is using a lot of memory", Value: share, Threshold: th, Unit: "%",
			Evidence: fmt.Sprintf("%s%s is using %s (%s of RAM).", g.name, countText(g.count), bytesText(g.memBytes), pct(share)),
			Explanation: "One program is holding a large share of the PC's memory, which leaves less for other programs " +
				"and makes Windows more likely to fall back on the slower page file.",
			Recommendation: fmt.Sprintf("If you don't need %s right now, close it or restart it to release memory "+
				"(for browsers, closing unused tabs usually has the same effect).", g.name),
		})
	}
	return out
}

func summarizeFindings(findings []Finding, m SeriesSummary, span time.Duration, window string) (string, string) {
	if len(findings) == 0 {
		if span < minAnalysisCoverage {
			return StatusCollecting, "Collecting data. PC Sentinel needs about a minute of history before it can judge sustained load."
		}
		s := fmt.Sprintf("No performance problems detected over the %s. CPU averaged %s and memory %s", window, pct(m.CPU.Avg), pct(m.Memory.Avg))
		if m.GPU.Available {
			s += fmt.Sprintf(", GPU %s", pct(m.GPU.Avg))
		}
		return StatusOK, s + "."
	}
	top := findings[0]
	issues := "1 issue"
	if len(findings) > 1 {
		issues = fmt.Sprintf("%d issues", len(findings))
	}
	return string(top.Severity), fmt.Sprintf("%s found. Most important: %s. %s", issues, strings.ToLower(top.Title[:1])+top.Title[1:], top.Evidence)
}

func seriesSpan(pts []models.HistoryPoint) time.Duration {
	if len(pts) < 2 {
		return 0
	}
	return pts[len(pts)-1].Timestamp.Sub(pts[0].Timestamp)
}

// describeWindow renders the covered span as "last 5 minutes" etc.
func describeWindow(span time.Duration) string {
	mins := int(span.Round(time.Minute).Minutes())
	switch {
	case mins <= 1:
		return "last minute"
	default:
		return fmt.Sprintf("last %d minutes", mins)
	}
}

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v) }

func countText(n int) string {
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf(" (%d processes)", n)
}

func bytesText(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	v, suffix := float64(b), []string{"KB", "MB", "GB", "TB"}
	i := -1
	for v >= unit && i < len(suffix)-1 {
		v /= unit
		i++
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, suffix[i])
	}
	return fmt.Sprintf("%.1f %s", v, suffix[i])
}

func rateText(bps float64) string { return bytesText(uint64(max(bps, 0))) + "/s" }

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }
