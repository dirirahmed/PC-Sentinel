package analyzer

import (
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func category(t *testing.T, r HealthReport, key string) CategoryScore {
	t.Helper()
	for _, c := range r.Categories {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("category %q missing", key)
	return CategoryScore{}
}

func TestScoringRuleBoundaries(t *testing.T) {
	tests := []struct {
		name string
		rule scoringRule
		in   float64
		want float64
	}{
		{"at good", cpuRule, 50, 100},
		{"below good", cpuRule, 5, 100},
		{"at bad", cpuRule, 95, 40},
		{"beyond bad clamps", cpuRule, 100, 40},
		{"midpoint", scoringRule{good: 0, bad: 100, floor: 0}, 50, 50},
		{"higher-is-better at good", diskFreeRule, 20, 100},
		{"higher-is-better at bad", diskFreeRule, 5, 25},
		{"higher-is-better beyond bad", diskFreeRule, 0, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rule.score(tt.in); got != tt.want {
				t.Errorf("score(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestHealthyIdleSystem(t *testing.T) {
	r := ScoreHealth(HealthInput{
		CPUAvgPercent: 12, MemoryPercent: 45,
		DiskFree:      []DiskFree{{"C:", 60}, {"D:", 35}},
		GPUAvgPercent: f(3), CPUTempC: f(48), GPUTempC: f(41),
	})
	if r.Overall != 100 || r.Grade != "good" {
		t.Fatalf("overall = %d (%s), want 100 (good)", r.Overall, r.Grade)
	}
	if !strings.Contains(r.Summary, "normal") {
		t.Errorf("unexpected summary %q", r.Summary)
	}
}

func TestSustainedCPUReducesCPUScore(t *testing.T) {
	r := ScoreHealth(HealthInput{CPUAvgPercent: 95, MemoryPercent: 40, DiskFree: []DiskFree{{"C:", 50}}})
	if c := category(t, r, "cpu"); c.Score != 40 || c.Grade != "poor" {
		t.Errorf("cpu = %d (%s), want 40 (poor)", c.Score, c.Grade)
	}
	if !strings.HasPrefix(r.Summary, "CPU") {
		t.Errorf("summary should blame CPU: %q", r.Summary)
	}
}

func TestLowDiskUsesWorstDrive(t *testing.T) {
	r := ScoreHealth(HealthInput{CPUAvgPercent: 10, MemoryPercent: 30,
		DiskFree: []DiskFree{{"C:", 80}, {"E:", 3}}})
	d := category(t, r, "disk")
	if d.Score != 25 {
		t.Errorf("disk score = %d, want 25", d.Score)
	}
	if !strings.Contains(d.Reasons[0], "E:") {
		t.Errorf("reason should name worst drive: %q", d.Reasons[0])
	}
	// Averaging alone would give ~85; the cap keeps a critical disk visible.
	if r.Overall != 25+overallCapMargin {
		t.Errorf("overall = %d, want capped at %d", r.Overall, 25+overallCapMargin)
	}
}

func TestUnavailableCategoriesAreExcluded(t *testing.T) {
	r := ScoreHealth(HealthInput{CPUAvgPercent: 10, MemoryPercent: 30, DiskFree: []DiskFree{{"C:", 50}}})
	for _, key := range []string{"gpu", "temperature"} {
		c := category(t, r, key)
		if c.Available || c.Grade != "unavailable" {
			t.Errorf("%s should be unavailable, got %+v", key, c)
		}
	}
	if r.Overall != 100 {
		t.Errorf("missing sensors must not lower the score; overall = %d", r.Overall)
	}
}

func TestTemperatureUsesHottestSensor(t *testing.T) {
	r := ScoreHealth(HealthInput{CPUTempC: f(60), GPUTempC: f(95), DiskFree: []DiskFree{{"C:", 50}}})
	c := category(t, r, "temperature")
	if c.Score != 25 {
		t.Errorf("temperature score = %d, want 25 (GPU at 95°C)", c.Score)
	}
}

func TestGrades(t *testing.T) {
	for score, want := range map[int]string{100: "good", 85: "good", 84: "fair", 65: "fair", 64: "poor", 40: "poor", 39: "critical", 0: "critical"} {
		if got := Grade(score); got != want {
			t.Errorf("Grade(%d) = %s, want %s", score, got, want)
		}
	}
}
