package collector

import (
	"context"
	"fmt"

	"github.com/dirirahmed/pc-sentinel/internal/models"
	"github.com/shirou/gopsutil/v4/mem"
)

func collectMemory(ctx context.Context) (models.MemoryStats, error) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return models.MemoryStats{}, fmt.Errorf("virtual memory: %w", err)
	}
	return memoryFromVirtual(vm), nil
}

// memoryFromVirtual derives "used" as total minus available. That matches
// Task Manager's "In use" figure more closely than gopsutil's Used field,
// whose meaning varies by platform.
func memoryFromVirtual(vm *mem.VirtualMemoryStat) models.MemoryStats {
	s := models.MemoryStats{TotalBytes: vm.Total, AvailableBytes: vm.Available}
	if vm.Available <= vm.Total {
		s.UsedBytes = vm.Total - vm.Available
	}
	if vm.Total > 0 {
		s.UsedPercent = float64(s.UsedBytes) / float64(vm.Total) * 100
	}
	return s
}
