package collector

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/shirou/gopsutil/v4/disk"
)

type diskCollector struct {
	prevIO   map[string]disk.IOCountersStat
	prevTime time.Time
}

func (c *diskCollector) collect(ctx context.Context, now time.Time) ([]models.DiskStats, models.DiskIO, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil && len(parts) == 0 {
		return nil, models.DiskIO{}, fmt.Errorf("partitions: %w", err)
	}
	parts = filterPartitions(parts)
	if len(parts) == 0 && runtime.GOOS != "windows" {
		// Containers and some VMs expose only overlay/virtual filesystems.
		parts = []disk.PartitionStat{{Mountpoint: "/", Device: "rootfs"}}
	}

	disks := make([]models.DiskStats, 0, len(parts))
	for _, p := range parts {
		// A drive can disappear (USB unplugged) between listing and querying.
		u, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		d := models.DiskStats{
			Mountpoint:  p.Mountpoint,
			Device:      p.Device,
			FSType:      p.Fstype,
			TotalBytes:  u.Total,
			UsedBytes:   u.Used,
			FreeBytes:   u.Free,
			UsedPercent: u.UsedPercent,
		}
		// UsedPercent is used/(used+free), the df/Explorer convention, so
		// filesystem-reserved blocks don't make the two figures disagree.
		d.FreePercent = 100 - u.UsedPercent
		disks = append(disks, d)
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Mountpoint < disks[j].Mountpoint })

	io := c.applyIO(ctx, disks, now)
	return disks, io, nil
}

// applyIO fills per-drive read/write rates. I/O counters are best effort:
// they need elevated rights on some systems and are absent in containers.
func (c *diskCollector) applyIO(ctx context.Context, disks []models.DiskStats, now time.Time) models.DiskIO {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil || len(counters) == 0 {
		c.prevIO = nil
		return models.DiskIO{}
	}
	var agg models.DiskIO
	elapsed := now.Sub(c.prevTime).Seconds()
	if c.prevIO != nil && elapsed > 0 {
		var totalR, totalW float64
		var matched bool
		for i := range disks {
			key := ioKey(disks[i])
			cur, ok1 := counters[key]
			prev, ok2 := c.prevIO[key]
			if !ok1 || !ok2 {
				continue
			}
			r := counterRate(prev.ReadBytes, cur.ReadBytes, elapsed)
			w := counterRate(prev.WriteBytes, cur.WriteBytes, elapsed)
			disks[i].ReadBytesPerSec, disks[i].WriteBytesPerSec = ptr(r), ptr(w)
			totalR += r
			totalW += w
			matched = true
		}
		if matched {
			agg = models.DiskIO{ReadBytesPerSec: ptr(totalR), WriteBytesPerSec: ptr(totalW)}
		}
	}
	c.prevIO, c.prevTime = counters, now
	return agg
}

// ioKey maps a partition to its IOCounters key: "C:" on Windows, the device
// basename (e.g. "nvme0n1p2") elsewhere.
func ioKey(d models.DiskStats) string {
	if runtime.GOOS == "windows" {
		return strings.TrimSuffix(d.Mountpoint, `\`)
	}
	return filepath.Base(d.Device)
}

// filterPartitions drops entries that aren't useful local drives and
// collapses bind mounts of the same device.
func filterPartitions(parts []disk.PartitionStat) []disk.PartitionStat {
	seen := map[string]bool{}
	out := parts[:0:0]
	for _, p := range parts {
		if p.Mountpoint == "" || !isLocalDrive(p.Mountpoint) || isReadOnly(p) {
			continue
		}
		if strings.HasPrefix(p.Mountpoint, "/snap/") || strings.HasPrefix(p.Mountpoint, "/boot/efi") {
			continue
		}
		if seen[p.Device] {
			continue
		}
		seen[p.Device] = true
		out = append(out, p)
	}
	return out
}

// Read-only volumes (squashfs images, mounted ISOs) are full by design and
// would otherwise trigger permanent low-space alerts.
func isReadOnly(p disk.PartitionStat) bool {
	for _, o := range p.Opts {
		if o == "ro" {
			return true
		}
	}
	return p.Fstype == "squashfs" || p.Fstype == "iso9660" || p.Fstype == "CDFS"
}
