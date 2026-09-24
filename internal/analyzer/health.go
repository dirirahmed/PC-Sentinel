// Package analyzer turns raw telemetry into health scores and alerts using
// fixed, documented rules. There is no learning or prediction involved.
package analyzer

import (
	"fmt"
	"math"
)

// HealthInput is what the scorer needs; nil pointers mean "not measurable".
type HealthInput struct {
	CPUAvgPercent float64 // average over the scoring window
	MemoryPercent float64 // current
	DiskFree      []DiskFree
	GPUAvgPercent *float64 // average over the scoring window
	CPUTempC      *float64
	GPUTempC      *float64 // hottest adapter
	WindowMinutes int
}

type DiskFree struct {
	Mountpoint  string
	FreePercent float64
}

type CategoryScore struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Available bool     `json:"available"`
	Score     int      `json:"score"`
	Grade     string   `json:"grade"`
	Weight    float64  `json:"weight"`
	Reasons   []string `json:"reasons"`
}

type HealthReport struct {
	Overall    int             `json:"overall"`
	Grade      string          `json:"grade"`
	Categories []CategoryScore `json:"categories"`
	Summary    string          `json:"summary"`
}

// scoringRule maps a measurement onto 0-100: full marks at or better than
// good, falling linearly to floor at bad.
type scoringRule struct {
	good, bad, floor float64
}

func (r scoringRule) score(v float64) float64 {
	lowerIsBetter := r.good < r.bad
	var frac float64
	if lowerIsBetter {
		frac = (v - r.good) / (r.bad - r.good)
	} else {
		frac = (r.good - v) / (r.good - r.bad)
	}
	frac = math.Max(0, math.Min(1, frac))
	return 100 - frac*(100-r.floor)
}

// The rules and weights below are documented in the README. They are
// judgement calls for a typical desktop, not a scientific model.
var (
	cpuRule         = scoringRule{good: 50, bad: 95, floor: 40}
	memoryRule      = scoringRule{good: 70, bad: 95, floor: 35}
	diskFreeRule    = scoringRule{good: 20, bad: 5, floor: 25}
	gpuRule         = scoringRule{good: 70, bad: 98, floor: 60}
	temperatureRule = scoringRule{good: 70, bad: 95, floor: 25}

	categoryWeights = map[string]float64{
		"cpu": 0.25, "memory": 0.25, "disk": 0.20, "temperature": 0.20, "gpu": 0.10,
	}
)

// overallCapMargin stops a single critical category from being averaged
// away: overall can't exceed the worst category by more than this.
const overallCapMargin = 25

func Grade(score int) string {
	switch {
	case score >= 85:
		return "good"
	case score >= 65:
		return "fair"
	case score >= 40:
		return "poor"
	}
	return "critical"
}

func ScoreHealth(in HealthInput) HealthReport {
	window := in.WindowMinutes
	if window <= 0 {
		window = 5
	}
	cats := []CategoryScore{
		scored("cpu", "CPU", cpuRule.score(in.CPUAvgPercent),
			fmt.Sprintf("%d-minute average utilization %.0f%% (full score at ≤%.0f%%)", window, in.CPUAvgPercent, cpuRule.good)),
		scored("memory", "Memory", memoryRule.score(in.MemoryPercent),
			fmt.Sprintf("%.0f%% of RAM in use (full score at ≤%.0f%%)", in.MemoryPercent, memoryRule.good)),
		diskCategory(in.DiskFree),
		gpuCategory(in.GPUAvgPercent, window),
		temperatureCategory(in.CPUTempC, in.GPUTempC),
	}

	var weighted, weightSum float64
	worst := 100
	for _, c := range cats {
		if !c.Available {
			continue
		}
		weighted += float64(c.Score) * c.Weight
		weightSum += c.Weight
		worst = min(worst, c.Score)
	}
	overall := 100
	if weightSum > 0 {
		overall = int(math.Round(weighted / weightSum))
	}
	overall = min(overall, worst+overallCapMargin)

	report := HealthReport{Overall: overall, Grade: Grade(overall), Categories: cats}
	report.Summary = summarize(cats, report.Grade)
	return report
}

func scored(key, label string, score float64, reasons ...string) CategoryScore {
	s := int(math.Round(score))
	return CategoryScore{Key: key, Label: label, Available: true, Score: s, Grade: Grade(s),
		Weight: categoryWeights[key], Reasons: reasons}
}

func unavailable(key, label, reason string) CategoryScore {
	return CategoryScore{Key: key, Label: label, Grade: "unavailable", Weight: categoryWeights[key], Reasons: []string{reason}}
}

func diskCategory(disks []DiskFree) CategoryScore {
	if len(disks) == 0 {
		return unavailable("disk", "Disk", "No drives reported")
	}
	worst := disks[0]
	for _, d := range disks[1:] {
		if d.FreePercent < worst.FreePercent {
			worst = d
		}
	}
	return scored("disk", "Disk", diskFreeRule.score(worst.FreePercent),
		fmt.Sprintf("Lowest free space: %s at %.1f%% free (full score at ≥%.0f%%)", worst.Mountpoint, worst.FreePercent, diskFreeRule.good))
}

func gpuCategory(avg *float64, window int) CategoryScore {
	if avg == nil {
		return unavailable("gpu", "GPU", "GPU utilization is not available on this system")
	}
	return scored("gpu", "GPU", gpuRule.score(*avg),
		fmt.Sprintf("%d-minute average utilization %.0f%% (full score at ≤%.0f%%; heavy GPU use is normal while gaming)", window, *avg, gpuRule.good))
}

func temperatureCategory(cpu, gpu *float64) CategoryScore {
	if cpu == nil && gpu == nil {
		return unavailable("temperature", "Temperature", "No temperature sensors are readable")
	}
	hottest, label := 0.0, ""
	var reasons []string
	if cpu != nil {
		hottest, label = *cpu, "CPU"
		reasons = append(reasons, fmt.Sprintf("CPU %.0f°C", *cpu))
	}
	if gpu != nil {
		if *gpu > hottest || label == "" {
			hottest, label = *gpu, "GPU"
		}
		reasons = append(reasons, fmt.Sprintf("GPU %.0f°C", *gpu))
	}
	reasons = append(reasons, fmt.Sprintf("Scored on hottest reading (%s, full score at ≤%.0f°C)", label, temperatureRule.good))
	return scored("temperature", "Temperature", temperatureRule.score(hottest), reasons...)
}

func summarize(cats []CategoryScore, grade string) string {
	worst := -1
	for i, c := range cats {
		if c.Available && (worst < 0 || c.Score < cats[worst].Score) {
			worst = i
		}
	}
	if worst < 0 {
		return "Not enough data to score system health yet."
	}
	if grade == "good" && cats[worst].Score >= 85 {
		return "All monitored components are within normal ranges."
	}
	return fmt.Sprintf("%s is the main factor: %s.", cats[worst].Label, cats[worst].Reasons[0])
}
