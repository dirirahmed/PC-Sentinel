package monitor

import (
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// liveWindow bounds the in-memory full-resolution history. It must cover the
// longest rolling window used for scoring or alerts (sustained CPU <= 25 min).
const liveWindow = 30 * time.Minute

// ring keeps recent full-resolution points, trimmed by age.
type ring struct {
	points []models.HistoryPoint
}

func (r *ring) add(p models.HistoryPoint) {
	r.points = append(r.points, p)
	cutoff := p.Timestamp.Add(-liveWindow)
	drop := 0
	for drop < len(r.points) && r.points[drop].Timestamp.Before(cutoff) {
		drop++
	}
	if drop > 0 {
		// Copy down instead of reslicing so the backing array doesn't grow forever.
		r.points = append(r.points[:0], r.points[drop:]...)
	}
}

func (r *ring) since(t time.Time) []models.HistoryPoint {
	out := []models.HistoryPoint{}
	for _, p := range r.points {
		if !p.Timestamp.Before(t) {
			out = append(out, p)
		}
	}
	return out
}

// cpuAverage returns the mean CPU over the trailing window, requiring the
// buffer to span at least minCoverage of it so a freshly started agent doesn't
// judge "sustained" load from a few seconds of data.
func (r *ring) cpuAverage(now time.Time, window time.Duration, minCoverage float64) (float64, bool) {
	start := now.Add(-window)
	var sum float64
	var n int
	var first time.Time
	for _, p := range r.points {
		if p.Timestamp.Before(start) {
			continue
		}
		if n == 0 {
			first = p.Timestamp
		}
		sum += p.CPU
		n++
	}
	if n == 0 || now.Sub(first) < time.Duration(float64(window)*minCoverage) {
		return 0, false
	}
	return sum / float64(n), true
}

func (r *ring) gpuAverage(now time.Time, window time.Duration) *float64 {
	start := now.Add(-window)
	var sum float64
	var n int
	for _, p := range r.points {
		if p.Timestamp.Before(start) || p.GPU == nil {
			continue
		}
		sum += *p.GPU
		n++
	}
	if n == 0 {
		return nil
	}
	avg := sum / float64(n)
	return &avg
}

// pointFromSnapshot flattens a snapshot into one history row.
func pointFromSnapshot(s models.Snapshot) models.HistoryPoint {
	p := models.HistoryPoint{
		Timestamp:  s.Timestamp,
		CPU:        s.CPU.UsagePercent,
		CPUMax:     s.CPU.UsagePercent,
		Memory:     s.Memory.UsedPercent,
		MemoryUsed: s.Memory.UsedBytes,
		CPUTemp:    s.CPU.TemperatureC,
		NetRx:      s.Network.RxBytesPerSec,
		NetTx:      s.Network.TxBytesPerSec,
		DiskRead:   s.DiskIO.ReadBytesPerSec,
		DiskWrite:  s.DiskIO.WriteBytesPerSec,
	}
	// Multi-GPU systems record the busiest and hottest adapter.
	for _, g := range s.GPU.Adapters {
		if g.UsagePercent != nil && (p.GPU == nil || *g.UsagePercent > *p.GPU) {
			v := *g.UsagePercent
			p.GPU, p.GPUMax = &v, &v
		}
		if g.TemperatureC != nil && (p.GPUTemp == nil || *g.TemperatureC > *p.GPUTemp) {
			v := *g.TemperatureC
			p.GPUTemp = &v
		}
	}
	return p
}

// bucket averages live points before they are written to SQLite, which keeps
// the database about 5x smaller than storing every sample.
type bucket struct {
	start  time.Time
	points []models.HistoryPoint
}

func (b *bucket) add(p models.HistoryPoint) {
	if len(b.points) == 0 {
		b.start = p.Timestamp
	}
	b.points = append(b.points, p)
}

func (b *bucket) due(now time.Time, interval time.Duration) bool {
	return len(b.points) > 0 && now.Sub(b.start) >= interval
}

func (b *bucket) flush() (models.HistoryPoint, bool) {
	if len(b.points) == 0 {
		return models.HistoryPoint{}, false
	}
	out := averagePoints(b.points)
	b.points = b.points[:0]
	return out, true
}

func averagePoints(pts []models.HistoryPoint) models.HistoryPoint {
	n := float64(len(pts))
	out := models.HistoryPoint{Timestamp: pts[len(pts)-1].Timestamp}
	var memUsed float64
	var gpu, cpuT, gpuT, dr, dw optAvg
	var gpuMax *float64
	for _, p := range pts {
		out.CPU += p.CPU / n
		out.CPUMax = max(out.CPUMax, p.CPUMax)
		out.Memory += p.Memory / n
		memUsed += float64(p.MemoryUsed) / n
		out.NetRx += p.NetRx / n
		out.NetTx += p.NetTx / n
		gpu.add(p.GPU)
		cpuT.add(p.CPUTemp)
		gpuT.add(p.GPUTemp)
		dr.add(p.DiskRead)
		dw.add(p.DiskWrite)
		if p.GPUMax != nil && (gpuMax == nil || *p.GPUMax > *gpuMax) {
			v := *p.GPUMax
			gpuMax = &v
		}
	}
	out.MemoryUsed = uint64(memUsed)
	out.GPU, out.GPUMax, out.CPUTemp, out.GPUTemp = gpu.value(), gpuMax, cpuT.value(), gpuT.value()
	out.DiskRead, out.DiskWrite = dr.value(), dw.value()
	return out
}

type optAvg struct {
	sum float64
	n   int
}

func (o *optAvg) add(v *float64) {
	if v != nil {
		o.sum += *v
		o.n++
	}
}

func (o optAvg) value() *float64 {
	if o.n == 0 {
		return nil
	}
	v := o.sum / float64(o.n)
	return &v
}
