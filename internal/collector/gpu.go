package collector

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// gpuSource is implemented per platform (gpu_windows.go, gpu_other.go).
type gpuSource interface {
	collect(ctx context.Context, now time.Time) models.GPUReport
	close()
}

// Windows "GPU Engine" counter instances look like
//
//	pid_1234_luid_0x00000000_0x0000D1B5_phys_0_eng_3_engtype_VideoDecode
//
// with one instance per process per engine.
var engineInstanceRe = regexp.MustCompile(`^pid_\d+_luid_(0x[0-9a-fA-F]+)_(0x[0-9a-fA-F]+)_phys_(\d+)_eng_(\d+)_engtype_(.*)$`)

// "GPU Adapter Memory" instances look like luid_0x00000000_0x0000D1B5_phys_0.
var adapterMemoryInstanceRe = regexp.MustCompile(`^luid_(0x[0-9a-fA-F]+)_(0x[0-9a-fA-F]+)_phys_\d+$`)

func luidKey(high, low string) string {
	return strings.ToLower(high + "_" + low)
}

func formatLUID(high int32, low uint32) string {
	return fmt.Sprintf("0x%08x_0x%08x", uint32(high), low)
}

type adapterUsage struct {
	usage   float64
	engines []models.GPUEngine
}

// aggregateEngines reproduces Task Manager's math: sum each physical engine
// across processes, report per-type utilization as the busiest engine of
// that type, and the adapter's overall utilization as its busiest engine.
func aggregateEngines(values map[string]float64) map[string]adapterUsage {
	type engineID struct {
		luid       string
		phys, eng  int
		engineType string
	}
	perEngine := map[engineID]float64{}
	for inst, v := range values {
		m := engineInstanceRe.FindStringSubmatch(inst)
		if m == nil {
			continue
		}
		phys, _ := strconv.Atoi(m[3])
		eng, _ := strconv.Atoi(m[4])
		perEngine[engineID{luidKey(m[1], m[2]), phys, eng, engineTypeLabel(m[5])}] += v
	}

	byType := map[string]map[string]float64{}
	for id, v := range perEngine {
		v = clampPercent(v)
		types := byType[id.luid]
		if types == nil {
			types = map[string]float64{}
			byType[id.luid] = types
		}
		if cur, ok := types[id.engineType]; !ok || v > cur {
			types[id.engineType] = v
		}
	}

	out := make(map[string]adapterUsage, len(byType))
	for luid, types := range byType {
		var au adapterUsage
		for name, v := range types {
			au.engines = append(au.engines, models.GPUEngine{Name: name, UsagePercent: v})
			au.usage = max(au.usage, v)
		}
		sort.Slice(au.engines, func(i, j int) bool {
			if au.engines[i].UsagePercent != au.engines[j].UsagePercent {
				return au.engines[i].UsagePercent > au.engines[j].UsagePercent
			}
			return au.engines[i].Name < au.engines[j].Name
		})
		out[luid] = au
	}
	return out
}

// aggregateAdapterMemory sums dedicated memory usage per adapter LUID.
func aggregateAdapterMemory(values map[string]float64) map[string]uint64 {
	out := map[string]uint64{}
	for inst, v := range values {
		m := adapterMemoryInstanceRe.FindStringSubmatch(inst)
		if m == nil || v < 0 {
			continue
		}
		out[luidKey(m[1], m[2])] += uint64(v)
	}
	return out
}

func engineTypeLabel(raw string) string {
	switch raw {
	case "":
		return "Other"
	case "VideoDecode":
		return "Video Decode"
	case "VideoEncode":
		return "Video Encode"
	case "VideoProcessing":
		return "Video Processing"
	case "LegacyOverlay":
		return "Legacy Overlay"
	}
	return strings.ReplaceAll(raw, "_", " ")
}

func vendorName(id uint32) string {
	switch id {
	case 0x10DE:
		return "NVIDIA"
	case 0x1002, 0x1022:
		return "AMD"
	case 0x8086:
		return "Intel"
	case 0x5143:
		return "Qualcomm"
	case 0x1414:
		return "Microsoft"
	}
	return "Unknown"
}

func unavailableGPU(reason string) models.GPUReport {
	return models.GPUReport{Available: false, Reason: reason, Adapters: []models.GPUStats{}}
}
