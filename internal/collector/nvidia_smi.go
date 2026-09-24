package collector

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// nvidiaSMI is an optional enhancement: Windows has no vendor-neutral GPU
// temperature API, so when NVIDIA's own CLI is installed we use it for
// temperature. It is never required; every other vendor simply reports
// temperature as unavailable.
type nvidiaSMI struct {
	path      string
	lastPoll  time.Time
	nextRetry time.Time
	cache     []nvidiaGPU
}

type nvidiaGPU struct {
	name        string
	temperature *float64
	utilization *float64
	memUsedMiB  *float64
	memTotalMiB *float64
}

func findNvidiaSMI() *nvidiaSMI {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	return &nvidiaSMI{path: path}
}

// read polls at most every tempPollInterval; spawning a process every
// sample would noticeably raise our own CPU usage.
func (n *nvidiaSMI) read(ctx context.Context, now time.Time) []nvidiaGPU {
	if n == nil {
		return nil
	}
	if now.Before(n.nextRetry) || now.Sub(n.lastPoll) < tempPollInterval {
		return n.cache
	}
	n.lastPoll = now
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, n.path,
		"--query-gpu=name,temperature.gpu,utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		n.cache = nil
		n.nextRetry = now.Add(tempRetryBackoff)
		return nil
	}
	n.cache = parseNvidiaSMI(string(out))
	return n.cache
}

func parseNvidiaSMI(out string) []nvidiaGPU {
	var gpus []nvidiaGPU
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 5 {
			continue
		}
		num := func(s string) *float64 {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil // "[N/A]", "[Not Supported]"
			}
			return &v
		}
		gpus = append(gpus, nvidiaGPU{
			name:        strings.TrimSpace(fields[0]),
			temperature: num(fields[1]),
			utilization: num(fields[2]),
			memUsedMiB:  num(fields[3]),
			memTotalMiB: num(fields[4]),
		})
	}
	return gpus
}

// matchNvidia finds the nvidia-smi entry for an adapter by name, falling back
// to position when both sides list exactly one NVIDIA GPU.
func matchNvidia(name string, nvidiaCount int, readings []nvidiaGPU) *nvidiaGPU {
	for i := range readings {
		if strings.EqualFold(readings[i].name, name) {
			return &readings[i]
		}
	}
	if nvidiaCount == 1 && len(readings) == 1 {
		return &readings[0]
	}
	return nil
}
