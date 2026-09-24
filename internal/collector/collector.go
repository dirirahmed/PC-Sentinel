// Package collector gathers local hardware telemetry. Every sub-collector is
// isolated: an error or even a panic in one produces an "unavailable"
// status for that component while the rest of the snapshot is still filled.
package collector

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/shirou/gopsutil/v4/host"
)

// Collector is not safe for concurrent use; the monitor calls Collect from a
// single goroutine because rate calculations depend on the previous call.
type Collector struct {
	cpu  *cpuCollector
	temp *cpuTempReader
	disk diskCollector
	net  networkCollector
	gpu  gpuSource
	log  *slog.Logger
}

func New(ctx context.Context, log *slog.Logger) *Collector {
	c := &Collector{
		cpu:  newCPUCollector(ctx),
		temp: newCPUTempReader(),
		log:  log,
	}
	guard(log, "gpu init", func() error {
		c.gpu = newGPUSource()
		return nil
	})
	return c
}

func (c *Collector) Close() {
	if c.gpu != nil {
		c.gpu.close()
	}
}

func (c *Collector) Collect(ctx context.Context) models.Snapshot {
	start := time.Now()
	now := start
	snap := models.Snapshot{Timestamp: now, Disks: []models.DiskStats{}}
	status := func(name string, err error) {
		st := models.CollectorStatus{Name: name, OK: err == nil}
		if err != nil {
			st.Error = err.Error()
		}
		snap.Collectors = append(snap.Collectors, st)
	}

	status("cpu", guard(c.log, "cpu", func() error {
		var err error
		snap.CPU, err = c.cpu.collect(ctx)
		snap.CPU.TemperatureC, snap.CPU.TemperatureSource = c.temp.reading(ctx, now)
		return err
	}))
	status("memory", guard(c.log, "memory", func() error {
		var err error
		snap.Memory, err = collectMemory(ctx)
		return err
	}))
	status("disk", guard(c.log, "disk", func() error {
		var err error
		snap.Disks, snap.DiskIO, err = c.disk.collect(ctx, now)
		if snap.Disks == nil {
			snap.Disks = []models.DiskStats{}
		}
		return err
	}))
	status("network", guard(c.log, "network", func() error {
		var err error
		snap.Network, err = c.net.collect(ctx, now)
		return err
	}))
	status("gpu", guard(c.log, "gpu", func() error {
		if c.gpu == nil {
			snap.GPU = unavailableGPU("GPU source failed to initialise")
			return nil
		}
		snap.GPU = c.gpu.collect(ctx, now)
		return nil
	}))
	status("uptime", guard(c.log, "uptime", func() error {
		var err error
		snap.UptimeSeconds, err = host.UptimeWithContext(ctx)
		return err
	}))

	snap.CollectionMillis = float64(time.Since(start).Microseconds()) / 1000
	return snap
}

// guard runs fn and converts a panic into an error so a misbehaving driver or
// OS API can't take the whole agent down.
func guard(log *slog.Logger, name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s collector panicked: %v", name, r)
			if log != nil {
				log.Error("collector panic", "collector", name, "panic", r)
			}
		}
	}()
	return fn()
}

// HostInfo is read once at startup.
func HostInfo(ctx context.Context) models.HostInfo {
	info, err := host.InfoWithContext(ctx)
	if err != nil || info == nil {
		return models.HostInfo{OS: "unknown"}
	}
	return models.HostInfo{
		Hostname:        info.Hostname,
		OS:              info.OS,
		Platform:        info.Platform,
		PlatformVersion: info.PlatformVersion,
		KernelArch:      info.KernelArch,
		BootTime:        time.Unix(int64(info.BootTime), 0).UTC(),
	}
}
