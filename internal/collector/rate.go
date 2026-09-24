package collector

// counterRate converts two readings of a monotonically increasing OS counter
// into a per-second rate. Counters reset when an adapter reconnects or a
// drive is re-attached; a decrease is treated as a reset and reported as 0
// rather than a huge bogus number.
func counterRate(prev, cur uint64, seconds float64) float64 {
	if seconds <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / seconds
}

func ptr[T any](v T) *T { return &v }

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	}
	return v
}
