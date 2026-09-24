package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/shirou/gopsutil/v4/net"
)

// networkCollector reads interface byte counters only. It never opens
// sockets or inspects packets.
type networkCollector struct {
	prev     map[string]net.IOCountersStat
	prevTime time.Time
}

func (c *networkCollector) collect(ctx context.Context, now time.Time) (models.NetworkStats, error) {
	counters, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return models.NetworkStats{}, fmt.Errorf("network counters: %w", err)
	}
	cur := make(map[string]net.IOCountersStat, len(counters))
	for _, ic := range counters {
		if isLoopback(ic.Name) {
			continue
		}
		cur[ic.Name] = ic
	}
	stats := aggregateNetwork(c.prev, cur, now.Sub(c.prevTime).Seconds())
	c.prev, c.prevTime = cur, now
	return stats, nil
}

// aggregateNetwork sums rates per interface so an adapter that disappears or
// resets only affects its own contribution.
func aggregateNetwork(prev, cur map[string]net.IOCountersStat, seconds float64) models.NetworkStats {
	var s models.NetworkStats
	for name, ic := range cur {
		s.TotalRxBytes += ic.BytesRecv
		s.TotalTxBytes += ic.BytesSent
		s.Interfaces++
		if p, ok := prev[name]; ok {
			s.RxBytesPerSec += counterRate(p.BytesRecv, ic.BytesRecv, seconds)
			s.TxBytesPerSec += counterRate(p.BytesSent, ic.BytesSent, seconds)
		}
	}
	return s
}

func isLoopback(name string) bool {
	n := strings.ToLower(name)
	return n == "lo" || strings.HasPrefix(n, "loopback")
}
