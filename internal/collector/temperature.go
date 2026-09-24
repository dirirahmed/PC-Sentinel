package collector

import (
	"context"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/sensors"
)

const (
	tempPollInterval = 10 * time.Second
	tempRetryBackoff = 5 * time.Minute
)

// cpuTempReader caches readings because sensor queries are comparatively
// expensive (a WMI round-trip on Windows) and temperatures change slowly.
// After a failure it backs off instead of hammering an unavailable source.
type cpuTempReader struct {
	read      func(context.Context) ([]sensors.TemperatureStat, error)
	lastPoll  time.Time
	nextRetry time.Time
	value     *float64
	source    string
}

func newCPUTempReader() *cpuTempReader {
	return &cpuTempReader{read: sensors.TemperaturesWithContext}
}

func (r *cpuTempReader) reading(ctx context.Context, now time.Time) (*float64, string) {
	if now.Before(r.nextRetry) || now.Sub(r.lastPoll) < tempPollInterval {
		return r.value, r.source
	}
	r.lastPoll = now
	temps, err := r.read(ctx)
	// gopsutil returns partial results alongside warnings; only give up when
	// nothing usable came back.
	v, src, ok := pickCPUTemperature(temps)
	if !ok {
		r.value, r.source = nil, ""
		if err != nil || len(temps) == 0 {
			r.nextRetry = now.Add(tempRetryBackoff)
		}
		return nil, ""
	}
	r.value, r.source = &v, src
	return r.value, r.source
}

// Sensor keys in rough order of how directly they measure the CPU die.
var cpuSensorPriority = []struct{ match, source string }{
	{"coretemp_package", "coretemp package"},
	{"k10temp_tctl", "k10temp Tctl"},
	{"k10temp_tdie", "k10temp Tdie"},
	{"zenpower", "zenpower"},
	{"x86_pkg_temp", "x86_pkg_temp"},
	{"coretemp", "coretemp"},
	{"k10temp", "k10temp"},
	{"cpu_thermal", "cpu_thermal"},
	{"cpu-thermal", "cpu_thermal"},
	{"cpu", "sensor"},
}

// pickCPUTemperature chooses the most CPU-specific plausible reading. On
// Windows the only built-in source is the ACPI thermal zone, which is often a
// motherboard sensor rather than the die, so it is labelled as such.
func pickCPUTemperature(temps []sensors.TemperatureStat) (float64, string, bool) {
	plausible := func(t float64) bool { return t > 5 && t < 125 }
	for _, pr := range cpuSensorPriority {
		for _, t := range temps {
			if strings.Contains(strings.ToLower(t.SensorKey), pr.match) && plausible(t.Temperature) {
				return t.Temperature, pr.source, true
			}
		}
	}
	for _, t := range temps {
		key := strings.ToLower(t.SensorKey)
		if (strings.Contains(key, "thermalzone") || strings.Contains(key, "_tz")) && plausible(t.Temperature) {
			return t.Temperature, "ACPI thermal zone (may not be the CPU die)", true
		}
	}
	return 0, "", false
}
