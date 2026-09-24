package analyzer

import (
	"testing"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func cpuObs(v float64) Observation {
	return Observation{
		Key: "cpu_usage", Rule: "cpu_usage", Component: "cpu", Title: "CPU usage", Unit: "%", Value: v,
		Levels:  []Level{{models.SeverityWarning, 90}, {models.SeverityCritical, 97}},
		Sustain: 60 * time.Second, Hysteresis: 10,
	}
}

// run feeds one value per 2-second tick and returns all events.
func run(e *AlertEngine, start time.Time, values []float64, cooldown time.Duration) ([]Event, time.Time) {
	var events []Event
	now := start
	for _, v := range values {
		events = append(events, e.Evaluate(now, []Observation{cpuObs(v)}, cooldown)...)
		now = now.Add(2 * time.Second)
	}
	return events, now
}

func repeat(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestAlertRequiresSustainedBreach(t *testing.T) {
	e := NewAlertEngine()
	// 58 seconds above threshold is not enough...
	events, _ := run(e, t0, repeat(95, 30), 0)
	if len(events) != 0 {
		t.Fatalf("alert fired before sustain period: %+v", events)
	}
	// ...but the 31st sample at 60 seconds is.
	events, _ = run(e, t0.Add(60*time.Second), []float64{95}, 0)
	if len(events) != 1 || events[0].Type != EventOpened || events[0].Alert.Severity != models.SeverityWarning {
		t.Fatalf("expected warning to open, got %+v", events)
	}
}

func TestShortSpikeResetsSustainTimer(t *testing.T) {
	e := NewAlertEngine()
	vals := append(repeat(95, 20), 50) // dip below threshold
	vals = append(vals, repeat(95, 20)...)
	if events, _ := run(e, t0, vals, 0); len(events) != 0 {
		t.Fatalf("interrupted breach must not alert: %+v", events)
	}
}

func TestEscalationAndHysteresis(t *testing.T) {
	e := NewAlertEngine()
	events, now := run(e, t0, repeat(99, 31), 0)
	// Warning and critical are both sustained at the same moment -> opens as critical.
	if len(events) != 1 || events[0].Alert.Severity != models.SeverityCritical {
		t.Fatalf("expected critical open, got %+v", events)
	}
	// 85 is below the warning line but inside the 10-point hysteresis band.
	events, now = run(e, now, repeat(85, 5), 0)
	if len(events) != 0 {
		t.Fatalf("alert resolved inside hysteresis band: %+v", events)
	}
	events, _ = run(e, now, []float64{79}, 0)
	if len(events) != 1 || events[0].Type != EventResolved || events[0].Alert.ResolvedAt == nil {
		t.Fatalf("expected resolution, got %+v", events)
	}
	if len(e.Active()) != 0 {
		t.Fatal("no alerts should remain active")
	}
}

func TestWarningEscalatesToCritical(t *testing.T) {
	e := NewAlertEngine()
	events, now := run(e, t0, repeat(92, 31), 0)
	if len(events) != 1 || events[0].Alert.Severity != models.SeverityWarning {
		t.Fatalf("expected warning, got %+v", events)
	}
	id := events[0].Alert.ID
	events, _ = run(e, now, repeat(98, 31), 0)
	if len(events) != 1 || events[0].Type != EventEscalated || events[0].Alert.Severity != models.SeverityCritical {
		t.Fatalf("expected escalation, got %+v", events)
	}
	if events[0].Alert.ID != id {
		t.Error("escalation must update the same alert, not open a new one")
	}
}

func TestCooldownSuppressesReopen(t *testing.T) {
	e := NewAlertEngine()
	cooldown := 5 * time.Minute
	_, now := run(e, t0, repeat(95, 31), cooldown)
	_, now = run(e, now, []float64{10}, cooldown) // resolve
	events, now := run(e, now, repeat(95, 40), cooldown)
	if len(events) != 0 {
		t.Fatalf("re-opened during cooldown: %+v", events)
	}
	events, _ = run(e, now.Add(cooldown), repeat(95, 31), cooldown)
	if len(events) != 1 || events[0].Type != EventOpened {
		t.Fatalf("expected re-open after cooldown, got %+v", events)
	}
}

func TestDisappearedTargetResolves(t *testing.T) {
	e := NewAlertEngine()
	disk := Observation{Key: "disk_free:E:", Rule: "disk_free", Title: "Free space on E:", Value: 2, Below: true,
		Levels: []Level{{models.SeverityWarning, 10}, {models.SeverityCritical, 5}}, Hysteresis: 1}
	if ev := e.Evaluate(t0, []Observation{disk}, 0); len(ev) != 1 || ev[0].Alert.Severity != models.SeverityCritical {
		t.Fatalf("disk alert should open immediately as critical, got %+v", ev)
	}
	ev := e.Evaluate(t0.Add(2*time.Second), nil, 0) // drive unplugged
	if len(ev) != 1 || ev[0].Type != EventResolved {
		t.Fatalf("expected resolution for missing drive, got %+v", ev)
	}
}

func TestBuildObservationsSkipsUnavailableSensors(t *testing.T) {
	snap := models.Snapshot{
		CPU:    models.CPUStats{UsagePercent: 50},
		Memory: models.MemoryStats{UsedPercent: 40},
		Disks:  []models.DiskStats{{Mountpoint: "C:", FreePercent: 40}},
		GPU:    models.GPUReport{Adapters: []models.GPUStats{{Name: "iGPU"}}},
	}
	obs := BuildObservations(snap, config.Default().Thresholds, nil)
	keys := map[string]bool{}
	for _, o := range obs {
		keys[o.Key] = true
	}
	for _, want := range []string{"cpu_usage", "memory_usage", "disk_free:C:"} {
		if !keys[want] {
			t.Errorf("missing observation %s", want)
		}
	}
	for _, unwanted := range []string{"cpu_temperature", "gpu_temperature:iGPU", "sustained_cpu"} {
		if keys[unwanted] {
			t.Errorf("observation %s should be skipped when data is unavailable", unwanted)
		}
	}
}

func TestDiskSpaceStatus(t *testing.T) {
	th := config.Threshold{Warning: 10, Critical: 5}
	cases := map[float64]string{50: models.SpaceOK, 10.1: models.SpaceOK, 10: models.SpaceWarning, 5: models.SpaceCritical, 0: models.SpaceCritical}
	for free, want := range cases {
		if got := DiskSpaceStatus(free, th); got != want {
			t.Errorf("DiskSpaceStatus(%v) = %s, want %s", free, got, want)
		}
	}
}
