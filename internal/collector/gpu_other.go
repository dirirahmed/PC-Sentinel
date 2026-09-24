//go:build !windows

package collector

import (
	"context"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// PC Sentinel targets Windows. On other platforms (used for development and
// CI) the only GPU source is nvidia-smi when it happens to be installed.
type otherGPU struct {
	smi *nvidiaSMI
}

func newGPUSource() gpuSource { return &otherGPU{smi: findNvidiaSMI()} }

func (g *otherGPU) collect(ctx context.Context, now time.Time) models.GPUReport {
	if g.smi == nil {
		return unavailableGPU("GPU metrics on this platform require nvidia-smi; vendor-neutral GPU counters are Windows-only")
	}
	readings := g.smi.read(ctx, now)
	if len(readings) == 0 {
		return unavailableGPU("nvidia-smi returned no GPUs")
	}
	report := models.GPUReport{Available: true, Source: "nvidia-smi"}
	const mib = 1024 * 1024
	for _, r := range readings {
		s := models.GPUStats{Name: r.name, Vendor: "NVIDIA", UsagePercent: r.utilization,
			TemperatureC: r.temperature, Engines: []models.GPUEngine{}}
		if r.temperature != nil {
			s.TemperatureSource = "nvidia-smi"
		}
		if r.memUsedMiB != nil {
			s.MemoryUsedBytes = ptr(uint64(*r.memUsedMiB * mib))
		}
		if r.memTotalMiB != nil {
			s.MemoryTotalBytes = ptr(uint64(*r.memTotalMiB * mib))
		}
		report.Adapters = append(report.Adapters, s)
	}
	return report
}

func (g *otherGPU) close() {}
