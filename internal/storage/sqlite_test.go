package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

func open(t *testing.T) *Store {
	t.Helper()
	// A space in the path checks that the DSN is escaped properly.
	s, err := Open(filepath.Join(t.TempDir(), "with space", "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func fp(v float64) *float64 { return &v }

var base = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func TestSamplesRoundTripAndBucketing(t *testing.T) {
	s, ctx := open(t), context.Background()
	for i := 0; i < 6; i++ {
		p := models.HistoryPoint{
			Timestamp: base.Add(time.Duration(i*10) * time.Second),
			CPU:       float64(10 * (i + 1)), CPUMax: float64(10*(i+1) + 5),
			Memory: 50, MemoryUsed: 8 << 30, NetRx: 100, NetTx: 50,
		}
		if i%2 == 0 {
			p.GPU, p.GPUMax = fp(20), fp(40)
		}
		if err := s.InsertSample(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	// 60-second buckets: all six samples fall into one bucket.
	pts, err := s.QuerySamples(ctx, base, base.Add(time.Hour), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 {
		t.Fatalf("got %d buckets, want 1", len(pts))
	}
	p := pts[0]
	if p.CPU != 35 || p.CPUMax != 65 {
		t.Errorf("cpu avg/max = %v/%v, want 35/65", p.CPU, p.CPUMax)
	}
	if p.GPU == nil || *p.GPU != 20 || *p.GPUMax != 40 {
		t.Errorf("gpu = %v/%v", p.GPU, p.GPUMax)
	}
	if p.CPUTemp != nil {
		t.Error("never-recorded metric must stay nil")
	}
	if p.MemoryUsed != 8<<30 {
		t.Errorf("memory used = %d", p.MemoryUsed)
	}
	// 10-second buckets keep every sample.
	pts, _ = s.QuerySamples(ctx, base, base.Add(time.Hour), 10*time.Second)
	if len(pts) != 6 {
		t.Errorf("got %d points, want 6", len(pts))
	}
}

func TestPruneRemovesOnlyOldData(t *testing.T) {
	s, ctx := open(t), context.Background()
	old, recent := base.AddDate(0, 0, -10), base
	for _, ts := range []time.Time{old, recent} {
		s.InsertSample(ctx, models.HistoryPoint{Timestamp: ts, CPU: 1})
		s.InsertDiskUsage(ctx, ts, []models.DiskStats{{Mountpoint: "C:", UsedPercent: 50, FreeBytes: 1}})
	}
	resolved := old.Add(time.Minute)
	s.SaveAlert(ctx, models.Alert{ID: "old", Rule: "r", Component: "c", Severity: models.SeverityWarning, StartedAt: old, UpdatedAt: old, ResolvedAt: &resolved})
	s.SaveAlert(ctx, models.Alert{ID: "stillopen", Rule: "r", Component: "c", Severity: models.SeverityWarning, StartedAt: old, UpdatedAt: old})

	n, err := s.Prune(ctx, base.AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("pruned %d rows, want 3 (sample, disk row, resolved alert)", n)
	}
	pts, _ := s.QuerySamples(ctx, old.Add(-time.Hour), recent.Add(time.Hour), time.Second)
	if len(pts) != 1 || !pts[0].Timestamp.Equal(recent) {
		t.Errorf("remaining samples = %+v", pts)
	}
	alerts, _ := s.ListAlerts(ctx, old.Add(-time.Hour), 10)
	if len(alerts) != 1 || alerts[0].ID != "stillopen" {
		t.Errorf("open alerts must survive pruning: %+v", alerts)
	}
}

func TestAlertUpsertAndDanglingClose(t *testing.T) {
	s, ctx := open(t), context.Background()
	a := models.Alert{ID: "cpu@1", Rule: "cpu_usage", Component: "cpu", Severity: models.SeverityWarning,
		Title: "CPU usage", Reason: "high", Value: 92, Threshold: 90, StartedAt: base, UpdatedAt: base}
	if err := s.SaveAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Severity, a.Threshold, a.UpdatedAt = models.SeverityCritical, 97, base.Add(time.Minute)
	if err := s.SaveAlert(ctx, a); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListAlerts(ctx, base.Add(-time.Hour), 10)
	if len(list) != 1 || list[0].Severity != models.SeverityCritical || list[0].ResolvedAt != nil {
		t.Fatalf("upsert failed: %+v", list)
	}
	n, err := s.CloseDanglingAlerts(ctx, base.Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("closed %d, err %v", n, err)
	}
	list, _ = s.ListAlerts(ctx, base.Add(-time.Hour), 10)
	if list[0].ResolvedAt == nil {
		t.Error("alert should be resolved after restart cleanup")
	}
}

func TestDiskUsageHistory(t *testing.T) {
	s, ctx := open(t), context.Background()
	s.InsertDiskUsage(ctx, base, []models.DiskStats{{Mountpoint: "C:", UsedPercent: 40, FreeBytes: 600}, {Mountpoint: "D:", UsedPercent: 90, FreeBytes: 10}})
	s.InsertDiskUsage(ctx, base.Add(time.Minute), []models.DiskStats{{Mountpoint: "C:", UsedPercent: 42, FreeBytes: 580}})
	pts, err := s.QueryDiskUsage(ctx, base, base.Add(time.Hour), time.Minute)
	if err != nil || len(pts) != 3 {
		t.Fatalf("points=%+v err=%v", pts, err)
	}
}

func TestOpenFailsCleanlyOnCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	os.WriteFile(path, []byte("this is not a sqlite database, just some text padding it out......................."), 0o644)
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("expected error for corrupt database")
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec("PRAGMA user_version = 99")
	s.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("expected schema version error")
	}
}
