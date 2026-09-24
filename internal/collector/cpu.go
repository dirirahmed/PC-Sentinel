package collector

import (
	"context"
	"fmt"
	"strings"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/shirou/gopsutil/v4/cpu"
)

// cpuCollector computes utilization from raw CPU time counters rather than
// gopsutil's Percent(0), which keeps its baseline in package-level state.
type cpuCollector struct {
	info      cpuStaticInfo
	prevTotal *cpu.TimesStat
	prevCores []cpu.TimesStat
	freq      frequencyReader
}

type cpuStaticInfo struct {
	model    string
	physical int
	logical  int
	baseMHz  *float64
}

func newCPUCollector(ctx context.Context) *cpuCollector {
	c := &cpuCollector{freq: newFrequencyReader()}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		c.info.model = strings.TrimSpace(infos[0].ModelName)
		if infos[0].Mhz > 0 {
			c.info.baseMHz = ptr(infos[0].Mhz)
		}
	}
	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		c.info.physical = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		c.info.logical = n
	}
	return c
}

func (c *cpuCollector) collect(ctx context.Context) (models.CPUStats, error) {
	stats := models.CPUStats{
		Model:            c.info.model,
		PhysicalCores:    c.info.physical,
		LogicalCores:     c.info.logical,
		BaseFrequencyMHz: c.info.baseMHz,
	}
	if stats.Model == "" {
		stats.Model = "Unknown CPU"
	}
	if c.freq != nil {
		stats.CurrentFrequencyMHz = c.freq.currentMHz(c.info.baseMHz)
	}

	total, err := cpu.TimesWithContext(ctx, false)
	if err != nil || len(total) == 0 {
		return stats, fmt.Errorf("cpu times: %w", orNoData(err))
	}
	if c.prevTotal != nil {
		stats.UsagePercent = busyPercent(*c.prevTotal, total[0])
	}
	c.prevTotal = &total[0]

	// Per-core data is a nice-to-have; failure here doesn't fail the collector.
	if cores, err := cpu.TimesWithContext(ctx, true); err == nil {
		if len(cores) == len(c.prevCores) {
			stats.PerCorePercent = make([]float64, len(cores))
			for i := range cores {
				stats.PerCorePercent[i] = busyPercent(c.prevCores[i], cores[i])
			}
		}
		c.prevCores = cores
	}
	return stats, nil
}

// busyPercent returns the share of non-idle time between two samples.
func busyPercent(prev, cur cpu.TimesStat) float64 {
	idle := func(t cpu.TimesStat) float64 { return t.Idle + t.Iowait }
	total := func(t cpu.TimesStat) float64 {
		// Guest time is already counted in User on Linux; exclude it to avoid double counting.
		return t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
	}
	dTotal := total(cur) - total(prev)
	dIdle := idle(cur) - idle(prev)
	if dTotal <= 0 {
		return 0
	}
	return clampPercent((dTotal - dIdle) / dTotal * 100)
}

// frequencyReader reports the current effective clock where the platform exposes it.
type frequencyReader interface {
	currentMHz(base *float64) *float64
}

func orNoData(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("no data returned")
}
