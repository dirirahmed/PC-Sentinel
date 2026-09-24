package collector

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	processCacheTTL    = 1500 * time.Millisecond
	processWarmupDelay = 500 * time.Millisecond
	processStaleAfter  = 30 * time.Second
)

// ProcessSampler lists processes on demand. Enumerating every process is the
// most expensive thing PC Sentinel does, so it only runs while someone is
// looking at the process page, and results are cached briefly so several
// open tabs don't multiply the cost.
type ProcessSampler struct {
	mu       sync.Mutex
	prev     map[procKey]float64 // CPU seconds per process at prevTime
	prevTime time.Time
	cached   []models.Process
	cachedAt time.Time
	numCPU   int
}

// procKey includes creation time so a recycled PID isn't mistaken for the
// process that previously held it.
type procKey struct {
	pid     int32
	created int64
}

type rawProcess struct {
	key        procKey
	name, path string
	status     string
	cpuSeconds float64
	hasCPU     bool
	rss        uint64
}

func NewProcessSampler() *ProcessSampler {
	return &ProcessSampler{numCPU: max(runtime.NumCPU(), 1)}
}

func (s *ProcessSampler) Sample(ctx context.Context) ([]models.Process, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.cached != nil && now.Sub(s.cachedAt) < processCacheTTL {
		return s.cached, nil
	}
	// CPU% needs two readings. If the baseline is missing or stale (nobody
	// viewed processes for a while), take a quick one first.
	if s.prev == nil || now.Sub(s.prevTime) > processStaleAfter {
		raw, err := readProcesses(ctx)
		if err != nil {
			return nil, err
		}
		s.remember(raw, now)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(processWarmupDelay):
		}
		now = time.Now()
	}
	raw, err := readProcesses(ctx)
	if err != nil {
		return nil, err
	}
	var totalMem uint64
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		totalMem = vm.Total
	}
	s.cached = buildProcessList(raw, s.prev, now.Sub(s.prevTime).Seconds(), s.numCPU, totalMem)
	s.cachedAt = now
	s.remember(raw, now)
	return s.cached, nil
}

func (s *ProcessSampler) remember(raw []rawProcess, now time.Time) {
	s.prev = make(map[procKey]float64, len(raw))
	for _, r := range raw {
		if r.hasCPU {
			s.prev[r.key] = r.cpuSeconds
		}
	}
	s.prevTime = now
}

// buildProcessList converts raw readings into API rows. CPU% is normalized
// to the whole machine (100% = every logical core busy), like Task Manager.
func buildProcessList(raw []rawProcess, prev map[procKey]float64, elapsed float64, numCPU int, totalMem uint64) []models.Process {
	out := make([]models.Process, 0, len(raw))
	for _, r := range raw {
		p := models.Process{
			PID: r.key.pid, Name: r.name, Path: r.path, Status: r.status, MemoryBytes: r.rss,
		}
		if totalMem > 0 {
			p.MemoryPct = float64(r.rss) / float64(totalMem) * 100
		}
		if before, ok := prev[r.key]; ok && r.hasCPU && elapsed > 0 && r.cpuSeconds >= before {
			p.CPUPercent = ptr(clampPercent((r.cpuSeconds - before) / elapsed / float64(numCPU) * 100))
		}
		out = append(out, p)
	}
	return out
}

// readProcesses gathers what it can for each process. Any field can fail
// (access denied for system processes, or the process exiting mid-read), so
// every lookup is independent and failures just leave the field empty.
func readProcesses(ctx context.Context) ([]rawProcess, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil && len(procs) == 0 {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	out := make([]rawProcess, 0, len(procs))
	for _, p := range procs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		name, err := p.NameWithContext(ctx)
		if errors.Is(err, process.ErrorProcessNotRunning) {
			continue
		}
		if name == "" {
			name = fmt.Sprintf("PID %d", p.Pid)
		}
		created, _ := p.CreateTimeWithContext(ctx)
		r := rawProcess{key: procKey{p.Pid, created}, name: name}
		if t, err := p.TimesWithContext(ctx); err == nil {
			r.cpuSeconds, r.hasCPU = t.User+t.System, true
		}
		if m, err := p.MemoryInfoWithContext(ctx); err == nil {
			r.rss = m.RSS
		}
		if exe, err := p.ExeWithContext(ctx); err == nil {
			r.path = exe
		}
		if st, err := p.StatusWithContext(ctx); err == nil && len(st) > 0 {
			r.status = st[0]
		}
		out = append(out, r)
	}
	return out, nil
}
