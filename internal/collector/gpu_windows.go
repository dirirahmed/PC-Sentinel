package collector

import (
	"context"
	"errors"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

const (
	gpuEngineCounter = `\GPU Engine(*)\Utilization Percentage`
	gpuMemoryCounter = `\GPU Adapter Memory(*)\Dedicated Usage`
	adapterRefresh   = 10 * time.Minute
)

type windowsGPU struct {
	adapters   []dxgiAdapter
	adapterErr error
	adaptersAt time.Time
	query      *pdhQuery
	queryErr   error
	smi        *nvidiaSMI
}

func newGPUSource() gpuSource {
	g := &windowsGPU{smi: findNvidiaSMI()}
	g.adapters, g.adapterErr = enumerateDXGIAdapters()
	g.adaptersAt = time.Now()
	g.query, g.queryErr = openPDHQuery(gpuEngineCounter, gpuMemoryCounter)
	return g
}

func (g *windowsGPU) collect(ctx context.Context, now time.Time) models.GPUReport {
	// Adapters rarely change, but eGPUs and driver resets do happen.
	if now.Sub(g.adaptersAt) > adapterRefresh {
		if a, err := enumerateDXGIAdapters(); err == nil {
			g.adapters, g.adapterErr = a, nil
		}
		g.adaptersAt = now
	}

	var engines map[string]adapterUsage
	var memory map[string]uint64
	if g.query != nil && g.query.collect() == nil {
		if g.query.has(gpuEngineCounter) {
			if v, err := g.query.values(gpuEngineCounter); err == nil {
				engines = aggregateEngines(v)
			}
		}
		if g.query.has(gpuMemoryCounter) {
			if v, err := g.query.values(gpuMemoryCounter); err == nil {
				memory = aggregateAdapterMemory(v)
			}
		}
	}

	adapters := g.adapters
	if len(adapters) == 0 {
		// DXGI failed; still report whatever adapters the counters know about.
		for luid := range engines {
			adapters = append(adapters, dxgiAdapter{name: "GPU " + luid, luid: luid})
		}
	}
	if len(adapters) == 0 {
		return unavailableGPU(errors.Join(g.adapterErr, g.queryErr, errors.New("no GPU adapters detected")).Error())
	}

	nvidiaCount := 0
	for _, a := range adapters {
		if a.vendorID == 0x10DE {
			nvidiaCount++
		}
	}
	var smiReadings []nvidiaGPU
	if nvidiaCount > 0 {
		smiReadings = g.smi.read(ctx, now)
	}

	report := models.GPUReport{Available: true, Source: "Windows performance counters + DXGI"}
	for _, a := range adapters {
		s := models.GPUStats{Name: a.name, Vendor: vendorName(a.vendorID), Engines: []models.GPUEngine{}}
		if a.dedicatedVRAM > 0 {
			s.MemoryTotalBytes = ptr(a.dedicatedVRAM)
		}
		if engines != nil {
			// No engine instances for an adapter means no process is using it.
			u := engines[a.luid]
			s.UsagePercent = ptr(u.usage)
			if u.engines != nil {
				s.Engines = u.engines
			}
		}
		if used, ok := memory[a.luid]; ok {
			s.MemoryUsedBytes = ptr(used)
		}
		if a.vendorID == 0x10DE {
			if r := matchNvidia(a.name, nvidiaCount, smiReadings); r != nil && r.temperature != nil {
				s.TemperatureC = r.temperature
				s.TemperatureSource = "nvidia-smi"
			}
		}
		report.Adapters = append(report.Adapters, s)
	}
	if engines == nil {
		report.Reason = "GPU performance counters unavailable (requires Windows 10 1709+ and a WDDM 2.x driver)"
	}
	return report
}

func (g *windowsGPU) close() { g.query.close() }
