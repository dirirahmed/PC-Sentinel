package analyzer

import "github.com/dirirahmed/pc-sentinel/internal/models"

// Stat is the average and peak of one metric over a period. Available is
// false when no point in the period carried the metric.
type Stat struct {
	Available bool    `json:"available"`
	Avg       float64 `json:"avg"`
	Peak      float64 `json:"peak"`
	Current   float64 `json:"current"`
}

type SeriesSummary struct {
	CPU       Stat `json:"cpu"`
	Memory    Stat `json:"memory"`
	GPU       Stat `json:"gpu"`
	CPUTemp   Stat `json:"cpuTemp"`
	GPUTemp   Stat `json:"gpuTemp"`
	NetRx     Stat `json:"netRx"`
	NetTx     Stat `json:"netTx"`
	DiskRead  Stat `json:"diskRead"`
	DiskWrite Stat `json:"diskWrite"`
}

type accumulator struct {
	sum, peak, last float64
	n               int
}

func (a *accumulator) add(v float64, peak float64) {
	if a.n == 0 || peak > a.peak {
		a.peak = peak
	}
	a.sum += v
	a.last = v
	a.n++
}

func (a *accumulator) addPtr(v, peak *float64) {
	if v == nil {
		return
	}
	p := *v
	if peak != nil {
		p = *peak
	}
	a.add(*v, p)
}

func (a accumulator) stat() Stat {
	if a.n == 0 {
		return Stat{}
	}
	return Stat{Available: true, Avg: a.sum / float64(a.n), Peak: a.peak, Current: a.last}
}

// Summarize computes per-metric average and peak over a series. Peaks use the
// *Max columns so spikes inside a downsampled bucket aren't lost.
func Summarize(points []models.HistoryPoint) SeriesSummary {
	var cpu, memory, gpu, cpuT, gpuT, rx, tx, dr, dw accumulator
	for _, p := range points {
		cpu.add(p.CPU, max(p.CPUMax, p.CPU))
		memory.add(p.Memory, p.Memory)
		gpu.addPtr(p.GPU, p.GPUMax)
		cpuT.addPtr(p.CPUTemp, nil)
		gpuT.addPtr(p.GPUTemp, nil)
		rx.add(p.NetRx, p.NetRx)
		tx.add(p.NetTx, p.NetTx)
		dr.addPtr(p.DiskRead, nil)
		dw.addPtr(p.DiskWrite, nil)
	}
	return SeriesSummary{
		CPU: cpu.stat(), Memory: memory.stat(), GPU: gpu.stat(), CPUTemp: cpuT.stat(), GPUTemp: gpuT.stat(),
		NetRx: rx.stat(), NetTx: tx.stat(), DiskRead: dr.stat(), DiskWrite: dw.stat(),
	}
}
