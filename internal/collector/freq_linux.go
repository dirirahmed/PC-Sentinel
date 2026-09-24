package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type sysfsFrequencyReader struct{}

func newFrequencyReader() frequencyReader { return sysfsFrequencyReader{} }

// currentMHz averages scaling_cur_freq across cores (values are in kHz).
func (sysfsFrequencyReader) currentMHz(_ *float64) *float64 {
	paths, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	var sum float64
	var n int
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		khz, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil || khz <= 0 {
			continue
		}
		sum += khz / 1000
		n++
	}
	if n == 0 {
		return nil
	}
	return ptr(sum / float64(n))
}
