// Package monitor runs the collection loop and owns the live state that the
// API reads: latest snapshot, recent history, health and active alerts.
package monitor

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/analyzer"
	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/dirirahmed/pc-sentinel/internal/storage"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	healthWindow       = 5 * time.Minute
	diskUsageInterval  = time.Minute
	pruneInterval      = time.Hour
	maxSeriesPoints    = 300
	storageCallTimeout = 5 * time.Second
)

type Collector interface {
	Collect(ctx context.Context) models.Snapshot
}

type ProcessLister interface {
	Sample(ctx context.Context) ([]models.Process, error)
}

// Store is the subset of storage.Store the monitor uses; nil disables history.
type Store interface {
	InsertSample(ctx context.Context, p models.HistoryPoint) error
	QuerySamples(ctx context.Context, from, to time.Time, bucket time.Duration) ([]models.HistoryPoint, error)
	InsertDiskUsage(ctx context.Context, at time.Time, disks []models.DiskStats) error
	QueryDiskUsage(ctx context.Context, from, to time.Time, bucket time.Duration) ([]models.DiskUsagePoint, error)
	SaveAlert(ctx context.Context, a models.Alert) error
	ListAlerts(ctx context.Context, since time.Time, limit int) ([]models.Alert, error)
	Prune(ctx context.Context, before time.Time) (int64, error)
}

type Monitor struct {
	cfg      *config.Store
	col      Collector
	procs    ProcessLister
	store    Store
	host     models.HostInfo
	log      *slog.Logger
	started  time.Time
	reconfig chan struct{}

	// Owned by the loop goroutine.
	engine        *analyzer.AlertEngine
	pending       bucket
	lastDiskWrite time.Time
	lastPrune     time.Time
	self          *selfUsage

	mu         sync.RWMutex
	latest     *models.Snapshot
	health     analyzer.HealthReport
	live       ring
	active     []models.Alert
	agent      models.AgentStats
	storageErr string
	collectSum float64
	collectN   int
}

type Options struct {
	Config    *config.Store
	Collector Collector
	Processes ProcessLister
	Store     Store // may be nil when the database couldn't be opened
	StoreErr  error
	Host      models.HostInfo
	Logger    *slog.Logger
}

func New(o Options) *Monitor {
	m := &Monitor{
		cfg: o.Config, col: o.Collector, procs: o.Processes, store: o.Store, host: o.Host,
		log: o.Logger, started: time.Now(), reconfig: make(chan struct{}, 1),
		engine: analyzer.NewAlertEngine(), self: newSelfUsage(),
		active: []models.Alert{},
	}
	if m.log == nil {
		m.log = slog.Default()
	}
	if o.Store == nil {
		m.storageErr = "history database not open"
		if o.StoreErr != nil {
			m.storageErr = o.StoreErr.Error()
		}
	}
	return m
}

// Run collects until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	// The first pass only establishes counter baselines (CPU, disk, network
	// rates are deltas), so it isn't published.
	m.col.Collect(ctx)
	interval := m.interval()
	select {
	case <-ctx.Done():
		return
	case <-time.After(min(interval, time.Second)):
	}
	m.tick(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.flush(context.Background())
			return
		case <-ticker.C:
			m.tick(ctx)
		case <-m.reconfig:
			if next := m.interval(); next != interval {
				interval = next
				ticker.Reset(interval)
				m.log.Info("sample interval changed", "interval", interval)
			}
		}
	}
}

// Reconfigure tells the loop to pick up changed settings.
func (m *Monitor) Reconfigure() {
	select {
	case m.reconfig <- struct{}{}:
	default:
	}
}

func (m *Monitor) interval() time.Duration {
	return time.Duration(m.cfg.Get().SampleIntervalSeconds) * time.Second
}

func (m *Monitor) tick(parent context.Context) {
	cfg := m.cfg.Get()
	ctx, cancel := context.WithTimeout(parent, max(m.interval(), 5*time.Second))
	defer cancel()

	snap := m.col.Collect(ctx)
	for i := range snap.Disks {
		snap.Disks[i].SpaceStatus = analyzer.DiskSpaceStatus(snap.Disks[i].FreePercent, cfg.Thresholds.DiskFreePercent)
	}
	now := snap.Timestamp
	point := pointFromSnapshot(snap)

	m.mu.Lock()
	m.live.add(point)
	cpuAvg, _ := m.live.cpuAverage(now, healthWindow, 0)
	gpuAvg := m.live.gpuAverage(now, healthWindow)
	var sustained *float64
	if avg, ok := m.live.cpuAverage(now, time.Duration(cfg.Thresholds.SustainedCPU.WindowMinutes)*time.Minute, 0.9); ok {
		sustained = &avg
	}
	m.mu.Unlock()

	health := analyzer.ScoreHealth(healthInput(snap, cpuAvg, gpuAvg))
	events := m.engine.Evaluate(now, analyzer.BuildObservations(snap, cfg.Thresholds, sustained),
		time.Duration(cfg.AlertCooldownSeconds)*time.Second)
	for _, ev := range events {
		m.log.Info("alert "+string(ev.Type), "rule", ev.Alert.Rule, "severity", ev.Alert.Severity, "reason", ev.Alert.Reason)
		m.persist(parent, func(ctx context.Context) error { return m.store.SaveAlert(ctx, ev.Alert) })
	}

	m.pending.add(point)
	if m.pending.due(now, time.Duration(cfg.HistoryIntervalSeconds)*time.Second) {
		if p, ok := m.pending.flush(); ok {
			m.persist(parent, func(ctx context.Context) error { return m.store.InsertSample(ctx, p) })
		}
	}
	if now.Sub(m.lastDiskWrite) >= diskUsageInterval {
		m.lastDiskWrite = now
		m.persist(parent, func(ctx context.Context) error { return m.store.InsertDiskUsage(ctx, now, snap.Disks) })
	}
	if now.Sub(m.lastPrune) >= pruneInterval {
		m.lastPrune = now
		m.prune(parent, now, cfg.RetentionDays)
	}

	agent := m.self.sample(now)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latest = &snap
	m.health = health
	m.active = m.engine.Active()
	m.collectSum += snap.CollectionMillis
	m.collectN++
	agent.LastCollectionMillis = snap.CollectionMillis
	agent.AvgCollectionMillis = m.collectSum / float64(m.collectN)
	m.agent = agent
}

func healthInput(s models.Snapshot, cpuAvg float64, gpuAvg *float64) analyzer.HealthInput {
	in := analyzer.HealthInput{
		CPUAvgPercent: cpuAvg, MemoryPercent: s.Memory.UsedPercent, GPUAvgPercent: gpuAvg,
		CPUTempC: s.CPU.TemperatureC, WindowMinutes: int(healthWindow.Minutes()),
	}
	for _, d := range s.Disks {
		in.DiskFree = append(in.DiskFree, analyzer.DiskFree{Mountpoint: d.Mountpoint, FreePercent: d.FreePercent})
	}
	for _, g := range s.GPU.Adapters {
		if g.TemperatureC != nil && (in.GPUTempC == nil || *g.TemperatureC > *in.GPUTempC) {
			in.GPUTempC = g.TemperatureC
		}
	}
	return in
}

// persist runs a storage write, recording (not propagating) failures: a
// locked or corrupt database degrades history but never stops monitoring.
func (m *Monitor) persist(parent context.Context, fn func(context.Context) error) {
	if m.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), storageCallTimeout)
	defer cancel()
	err := fn(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case err != nil && m.storageErr != err.Error():
		m.log.Error("storage write failed", "err", err)
		m.storageErr = err.Error()
	case err == nil && m.storageErr != "":
		m.log.Info("storage recovered")
		m.storageErr = ""
	}
}

func (m *Monitor) prune(parent context.Context, now time.Time, days int) {
	m.persist(parent, func(ctx context.Context) error {
		n, err := m.store.Prune(ctx, now.AddDate(0, 0, -days))
		if err == nil && n > 0 {
			m.log.Info("pruned old history", "rows", n, "retentionDays", days)
		}
		return err
	})
}

func (m *Monitor) flush(ctx context.Context) {
	if p, ok := m.pending.flush(); ok {
		m.persist(ctx, func(ctx context.Context) error { return m.store.InsertSample(ctx, p) })
	}
}

// ---- read side, used by the API ----

func (m *Monitor) Snapshot() (models.Snapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.latest == nil {
		return models.Snapshot{}, false
	}
	return *m.latest, true
}

func (m *Monitor) Health() analyzer.HealthReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.health
}

func (m *Monitor) ActiveAlerts() []models.Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Copy into a non-nil slice so the API renders [] rather than null.
	out := make([]models.Alert, len(m.active))
	copy(out, m.active)
	return out
}

func (m *Monitor) Host() models.HostInfo { return m.host }

func (m *Monitor) Agent() models.AgentStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a := m.agent
	a.PID = os.Getpid()
	a.Goroutines = runtime.NumGoroutine()
	a.UptimeSeconds = time.Since(m.started).Seconds()
	a.StorageAvailable = m.store != nil && m.storageErr == ""
	a.StorageError = m.storageErr
	return a
}

func (m *Monitor) Processes(ctx context.Context) ([]models.Process, error) {
	return m.procs.Sample(ctx)
}

// Series returns metric history for the trailing period. Short periods come
// from memory at full resolution; longer ones from SQLite, bucketed so the
// response stays around maxSeriesPoints points.
func (m *Monitor) Series(ctx context.Context, period time.Duration) ([]models.HistoryPoint, time.Duration, error) {
	now := time.Now()
	if period <= liveWindow {
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.live.since(now.Add(-period)), m.interval(), nil
	}
	if m.store == nil {
		return nil, 0, storage.ErrUnavailable
	}
	b := m.bucketFor(period)
	pts, err := m.store.QuerySamples(ctx, now.Add(-period), now, b)
	return pts, b, err
}

func (m *Monitor) DiskHistory(ctx context.Context, period time.Duration) ([]models.DiskUsagePoint, error) {
	if m.store == nil {
		return nil, storage.ErrUnavailable
	}
	now := time.Now()
	return m.store.QueryDiskUsage(ctx, now.Add(-period), now, max(m.bucketFor(period), diskUsageInterval))
}

func (m *Monitor) AlertHistory(ctx context.Context, since time.Time, limit int) ([]models.Alert, error) {
	if m.store == nil {
		return nil, storage.ErrUnavailable
	}
	return m.store.ListAlerts(ctx, since, limit)
}

func (m *Monitor) bucketFor(period time.Duration) time.Duration {
	hist := time.Duration(m.cfg.Get().HistoryIntervalSeconds) * time.Second
	return max(hist, (period / maxSeriesPoints).Round(time.Second))
}

// selfUsage measures PC Sentinel's own CPU and memory footprint. CPU time
// has coarse (10-16 ms) OS granularity, so the since-start average is the
// more meaningful figure; the per-interval value is often 0.
type selfUsage struct {
	proc      *process.Process
	prevCPU   float64
	prevTime  time.Time
	startCPU  float64
	startTime time.Time
}

func newSelfUsage() *selfUsage {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return &selfUsage{}
	}
	return &selfUsage{proc: p}
}

func (s *selfUsage) sample(now time.Time) models.AgentStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	a := models.AgentStats{GoHeapBytes: ms.HeapAlloc}
	if s.proc == nil {
		return a
	}
	if mi, err := s.proc.MemoryInfo(); err == nil {
		a.MemoryRSSBytes = mi.RSS
	}
	t, err := s.proc.Times()
	if err != nil {
		return a
	}
	cpu := t.User + t.System
	cores := float64(runtime.NumCPU())
	pct := func(dCPU, dWall float64) float64 {
		if dWall <= 0 {
			return 0
		}
		return max(0, dCPU/dWall/cores*100)
	}
	if s.startTime.IsZero() {
		s.startCPU, s.startTime = cpu, now
	} else {
		a.CPUPercent = pct(cpu-s.prevCPU, now.Sub(s.prevTime).Seconds())
		a.AvgCPUPercent = pct(cpu-s.startCPU, now.Sub(s.startTime).Seconds())
	}
	s.prevCPU, s.prevTime = cpu, now
	return a
}
