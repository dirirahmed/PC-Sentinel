package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/dirirahmed/pc-sentinel/internal/storage"
)

type fakeCollector struct {
	now  time.Time
	cpu  float64
	free float64
}

func (f *fakeCollector) Collect(context.Context) models.Snapshot {
	f.now = f.now.Add(2 * time.Second)
	return models.Snapshot{
		Timestamp: f.now,
		CPU:       models.CPUStats{UsagePercent: f.cpu},
		Memory:    models.MemoryStats{UsedPercent: 40},
		Disks:     []models.DiskStats{{Mountpoint: "C:", FreePercent: f.free}},
		GPU:       models.GPUReport{Adapters: []models.GPUStats{}},
	}
}

type fakeStore struct {
	mu      sync.Mutex
	samples []models.HistoryPoint
	alerts  []models.Alert
	disks   int
	fail    error
}

func (s *fakeStore) InsertSample(_ context.Context, p models.HistoryPoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.samples = append(s.samples, p)
	return nil
}
func (s *fakeStore) QuerySamples(context.Context, time.Time, time.Time, time.Duration) ([]models.HistoryPoint, error) {
	return s.samples, nil
}
func (s *fakeStore) InsertDiskUsage(context.Context, time.Time, []models.DiskStats) error {
	s.disks++
	return s.fail
}
func (s *fakeStore) QueryDiskUsage(context.Context, time.Time, time.Time, time.Duration) ([]models.DiskUsagePoint, error) {
	return nil, nil
}
func (s *fakeStore) SaveAlert(_ context.Context, a models.Alert) error {
	if s.fail != nil {
		return s.fail
	}
	s.alerts = append(s.alerts, a)
	return nil
}
func (s *fakeStore) ListAlerts(context.Context, time.Time, int) ([]models.Alert, error) {
	return s.alerts, nil
}
func (s *fakeStore) Prune(context.Context, time.Time) (int64, error) { return 0, s.fail }

func newTestMonitor(store Store) (*Monitor, *fakeCollector) {
	cfg := config.Default()
	cfg.Thresholds.CPUUsage.SustainSeconds = 0
	col := &fakeCollector{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), cpu: 20, free: 50}
	m := New(Options{Config: config.NewStore("", cfg), Collector: col, Store: store})
	return m, col
}

func TestTickPublishesSnapshotAndHealth(t *testing.T) {
	m, col := newTestMonitor(&fakeStore{})
	if _, ok := m.Snapshot(); ok {
		t.Fatal("no snapshot expected before first tick")
	}
	col.free = 4
	m.tick(context.Background())
	snap, ok := m.Snapshot()
	if !ok {
		t.Fatal("snapshot missing after tick")
	}
	if snap.Disks[0].SpaceStatus != models.SpaceCritical {
		t.Errorf("space status = %q, want critical", snap.Disks[0].SpaceStatus)
	}
	if h := m.Health(); h.Overall == 0 || len(h.Categories) == 0 {
		t.Errorf("health not computed: %+v", h)
	}
	if len(m.ActiveAlerts()) != 1 {
		t.Errorf("expected low-disk alert, got %+v", m.ActiveAlerts())
	}
}

func TestActiveAlertsNeverNil(t *testing.T) {
	m, _ := newTestMonitor(nil)
	m.tick(context.Background())
	if a := m.ActiveAlerts(); a == nil {
		t.Fatal("ActiveAlerts must return an empty slice, not nil (JSON null breaks clients)")
	}
}

func TestAlertsArePersisted(t *testing.T) {
	store := &fakeStore{}
	m, col := newTestMonitor(store)
	col.cpu = 99
	m.tick(context.Background())
	col.cpu = 10
	m.tick(context.Background())
	if len(store.alerts) != 2 || store.alerts[0].ResolvedAt != nil || store.alerts[1].ResolvedAt == nil {
		t.Fatalf("expected open then resolve to be saved, got %+v", store.alerts)
	}
}

func TestHistoryIsDownsampledBeforeStorage(t *testing.T) {
	store := &fakeStore{}
	m, col := newTestMonitor(store)
	// 2s samples, 10s history interval -> about one stored row per 5 ticks.
	for i := 0; i < 16; i++ {
		col.cpu = float64(i * 10)
		m.tick(context.Background())
	}
	if len(store.samples) != 2 {
		t.Fatalf("stored %d rows, want 2", len(store.samples))
	}
	if first := store.samples[0]; first.CPU != 25 || first.CPUMax != 50 {
		t.Errorf("first bucket avg/max = %v/%v, want 25/50", first.CPU, first.CPUMax)
	}
}

func TestStorageFailureDoesNotStopMonitoring(t *testing.T) {
	store := &fakeStore{fail: errors.New("database is locked")}
	m, _ := newTestMonitor(store)
	for i := 0; i < 6; i++ {
		m.tick(context.Background())
	}
	if _, ok := m.Snapshot(); !ok {
		t.Fatal("monitoring stopped after storage errors")
	}
	a := m.Agent()
	if a.StorageAvailable || a.StorageError != "database is locked" {
		t.Errorf("agent storage status = %v %q", a.StorageAvailable, a.StorageError)
	}
	store.fail = nil
	for i := 0; i < 6; i++ {
		m.tick(context.Background())
	}
	if !m.Agent().StorageAvailable {
		t.Error("storage should report recovered")
	}
}

func TestNoStoreServesLiveHistoryOnly(t *testing.T) {
	m, _ := newTestMonitor(nil)
	m.tick(context.Background())
	if _, _, err := m.Series(context.Background(), 15*time.Minute); err != nil {
		t.Errorf("live series should work without storage: %v", err)
	}
	if _, _, err := m.Series(context.Background(), 24*time.Hour); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("long range without storage: err = %v", err)
	}
	if _, err := m.AlertHistory(context.Background(), time.Time{}, 10); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("alert history without storage: err = %v", err)
	}
}

func TestRingTrimsByAge(t *testing.T) {
	var r ring
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2000; i++ {
		r.add(models.HistoryPoint{Timestamp: start.Add(time.Duration(i) * 2 * time.Second), CPU: 50})
	}
	if span := r.points[len(r.points)-1].Timestamp.Sub(r.points[0].Timestamp); span > liveWindow {
		t.Errorf("ring spans %v, want <= %v", span, liveWindow)
	}
	now := r.points[len(r.points)-1].Timestamp
	if avg, ok := r.cpuAverage(now, 15*time.Minute, 0.9); !ok || avg != 50 {
		t.Errorf("15m average = %v, %v", avg, ok)
	}
}

func TestCPUAverageRequiresCoverage(t *testing.T) {
	var r ring
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ { // one minute of data
		r.add(models.HistoryPoint{Timestamp: start.Add(time.Duration(i) * 2 * time.Second), CPU: 90})
	}
	if _, ok := r.cpuAverage(start.Add(time.Minute), 15*time.Minute, 0.9); ok {
		t.Error("a 15-minute average must not be reported from one minute of data")
	}
}
