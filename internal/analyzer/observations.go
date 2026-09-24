package analyzer

import (
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// BuildObservations maps a snapshot onto the configured alert rules.
// sustainedCPUAvg is the CPU average over the sustained-load window, or nil
// until that much history exists.
func BuildObservations(s models.Snapshot, t config.Thresholds, sustainedCPUAvg *float64) []Observation {
	sec := func(n int) time.Duration { return time.Duration(n) * time.Second }
	levels := func(th config.Threshold) []Level {
		return []Level{
			{Severity: models.SeverityWarning, Threshold: th.Warning},
			{Severity: models.SeverityCritical, Threshold: th.Critical},
		}
	}

	obs := []Observation{
		{
			Key: "cpu_usage", Rule: "cpu_usage", Component: "cpu", Title: "CPU usage", Unit: "%",
			Value: s.CPU.UsagePercent, Levels: levels(t.CPUUsage), Sustain: sec(t.CPUUsage.SustainSeconds), Hysteresis: 10,
		},
		{
			Key: "memory_usage", Rule: "memory_usage", Component: "memory", Title: "Memory usage", Unit: "%",
			Value: s.Memory.UsedPercent, Levels: levels(t.MemoryUsage), Sustain: sec(t.MemoryUsage.SustainSeconds), Hysteresis: 5,
		},
	}
	if sustainedCPUAvg != nil {
		obs = append(obs, Observation{
			Key: "sustained_cpu", Rule: "sustained_cpu", Component: "cpu", Title: "Sustained CPU load", Unit: "%",
			Value:      *sustainedCPUAvg,
			Levels:     []Level{{Severity: models.SeverityInfo, Threshold: t.SustainedCPU.AveragePercent}},
			Hysteresis: 10,
		})
	}
	for _, d := range s.Disks {
		obs = append(obs, Observation{
			Key: "disk_free:" + d.Mountpoint, Rule: "disk_free", Component: "disk", Target: d.Mountpoint,
			Title: "Free space on " + d.Mountpoint, Unit: "%", Value: d.FreePercent, Below: true,
			Levels: []Level{
				{Severity: models.SeverityWarning, Threshold: t.DiskFreePercent.Warning},
				{Severity: models.SeverityCritical, Threshold: t.DiskFreePercent.Critical},
			},
			Hysteresis: 1,
		})
	}
	if s.CPU.TemperatureC != nil {
		obs = append(obs, Observation{
			Key: "cpu_temperature", Rule: "cpu_temperature", Component: "temperature", Target: "CPU",
			Title: "CPU temperature", Unit: "°C", Value: *s.CPU.TemperatureC,
			Levels: levels(t.CPUTemperature), Sustain: sec(t.CPUTemperature.SustainSeconds), Hysteresis: 5,
		})
	}
	for _, g := range s.GPU.Adapters {
		if g.TemperatureC == nil {
			continue
		}
		obs = append(obs, Observation{
			Key: "gpu_temperature:" + g.Name, Rule: "gpu_temperature", Component: "temperature", Target: g.Name,
			Title: g.Name + " temperature", Unit: "°C", Value: *g.TemperatureC,
			Levels: levels(t.GPUTemperature), Sustain: sec(t.GPUTemperature.SustainSeconds), Hysteresis: 5,
		})
	}
	return obs
}

// DiskSpaceStatus classifies a drive against the configured free-space thresholds.
func DiskSpaceStatus(freePercent float64, t config.Threshold) string {
	switch {
	case freePercent <= t.Critical:
		return models.SpaceCritical
	case freePercent <= t.Warning:
		return models.SpaceWarning
	}
	return models.SpaceOK
}
