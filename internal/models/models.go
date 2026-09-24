// Package models holds the telemetry types shared by the collector, analyzer,
// storage and API layers. Pointer fields are nil when a metric is unavailable
// on the current machine; they serialize as JSON null.
package models

import "time"

// Snapshot is one complete collection pass.
type Snapshot struct {
	Timestamp     time.Time         `json:"timestamp"`
	UptimeSeconds uint64            `json:"uptimeSeconds"`
	CPU           CPUStats          `json:"cpu"`
	Memory        MemoryStats       `json:"memory"`
	GPU           GPUReport         `json:"gpu"`
	Disks         []DiskStats       `json:"disks"`
	DiskIO        DiskIO            `json:"diskIO"`
	Network       NetworkStats      `json:"network"`
	Collectors    []CollectorStatus `json:"collectors"`
	// CollectionMillis is how long this pass took, used to monitor our own overhead.
	CollectionMillis float64 `json:"collectionMillis"`
}

type CPUStats struct {
	Model               string    `json:"model"`
	PhysicalCores       int       `json:"physicalCores"`
	LogicalCores        int       `json:"logicalCores"`
	BaseFrequencyMHz    *float64  `json:"baseFrequencyMHz"`
	CurrentFrequencyMHz *float64  `json:"currentFrequencyMHz"`
	UsagePercent        float64   `json:"usagePercent"`
	PerCorePercent      []float64 `json:"perCorePercent"`
	TemperatureC        *float64  `json:"temperatureC"`
	TemperatureSource   string    `json:"temperatureSource,omitempty"`
}

type MemoryStats struct {
	TotalBytes     uint64  `json:"totalBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedPercent    float64 `json:"usedPercent"`
}

// GPUReport wraps per-adapter stats. Available is false when no source could
// produce any GPU data; Reason then explains why.
type GPUReport struct {
	Available bool       `json:"available"`
	Reason    string     `json:"reason,omitempty"`
	Source    string     `json:"source,omitempty"`
	Adapters  []GPUStats `json:"adapters"`
}

type GPUStats struct {
	Name              string      `json:"name"`
	Vendor            string      `json:"vendor"`
	UsagePercent      *float64    `json:"usagePercent"`
	MemoryUsedBytes   *uint64     `json:"memoryUsedBytes"`
	MemoryTotalBytes  *uint64     `json:"memoryTotalBytes"`
	TemperatureC      *float64    `json:"temperatureC"`
	TemperatureSource string      `json:"temperatureSource,omitempty"`
	Engines           []GPUEngine `json:"engines"`
}

type GPUEngine struct {
	Name         string  `json:"name"`
	UsagePercent float64 `json:"usagePercent"`
}

// Disk space states reported per drive.
const (
	SpaceOK       = "ok"
	SpaceWarning  = "warning"
	SpaceCritical = "critical"
)

type DiskStats struct {
	Mountpoint       string   `json:"mountpoint"`
	Device           string   `json:"device"`
	FSType           string   `json:"fsType"`
	TotalBytes       uint64   `json:"totalBytes"`
	UsedBytes        uint64   `json:"usedBytes"`
	FreeBytes        uint64   `json:"freeBytes"`
	UsedPercent      float64  `json:"usedPercent"`
	FreePercent      float64  `json:"freePercent"`
	ReadBytesPerSec  *float64 `json:"readBytesPerSec"`
	WriteBytesPerSec *float64 `json:"writeBytesPerSec"`
	SpaceStatus      string   `json:"spaceStatus"`
}

// DiskIO is the aggregate across all physical disks.
type DiskIO struct {
	ReadBytesPerSec  *float64 `json:"readBytesPerSec"`
	WriteBytesPerSec *float64 `json:"writeBytesPerSec"`
}

type NetworkStats struct {
	RxBytesPerSec float64 `json:"rxBytesPerSec"`
	TxBytesPerSec float64 `json:"txBytesPerSec"`
	// Totals are the OS interface counters (bytes since the adapters came up),
	// excluding loopback interfaces.
	TotalRxBytes uint64 `json:"totalRxBytes"`
	TotalTxBytes uint64 `json:"totalTxBytes"`
	Interfaces   int    `json:"interfaces"`
}

type CollectorStatus struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type Process struct {
	PID         int32    `json:"pid"`
	Name        string   `json:"name"`
	CPUPercent  *float64 `json:"cpuPercent"`
	MemoryBytes uint64   `json:"memoryBytes"`
	MemoryPct   float64  `json:"memoryPercent"`
	Path        string   `json:"path"`
	Status      string   `json:"status"`
}

type HostInfo struct {
	Hostname        string    `json:"hostname"`
	OS              string    `json:"os"`
	Platform        string    `json:"platform"`
	PlatformVersion string    `json:"platformVersion"`
	KernelArch      string    `json:"kernelArch"`
	BootTime        time.Time `json:"bootTime"`
}

// AgentStats describes PC Sentinel's own resource usage.
type AgentStats struct {
	PID                  int     `json:"pid"`
	CPUPercent           float64 `json:"cpuPercent"`
	AvgCPUPercent        float64 `json:"avgCpuPercent"`
	MemoryRSSBytes       uint64  `json:"memoryRssBytes"`
	GoHeapBytes          uint64  `json:"goHeapBytes"`
	Goroutines           int     `json:"goroutines"`
	UptimeSeconds        float64 `json:"uptimeSeconds"`
	AvgCollectionMillis  float64 `json:"avgCollectionMillis"`
	LastCollectionMillis float64 `json:"lastCollectionMillis"`
	StorageAvailable     bool    `json:"storageAvailable"`
	StorageError         string  `json:"storageError,omitempty"`
}

// HistoryPoint is one row of time-series history. Live points come straight
// from snapshots; stored points are averages over a bucket, with the *Max
// fields preserving short spikes.
type HistoryPoint struct {
	Timestamp  time.Time `json:"t"`
	CPU        float64   `json:"cpu"`
	CPUMax     float64   `json:"cpuMax"`
	Memory     float64   `json:"memory"`
	MemoryUsed uint64    `json:"memoryUsed"`
	GPU        *float64  `json:"gpu"`
	GPUMax     *float64  `json:"gpuMax"`
	CPUTemp    *float64  `json:"cpuTemp"`
	GPUTemp    *float64  `json:"gpuTemp"`
	NetRx      float64   `json:"netRx"`
	NetTx      float64   `json:"netTx"`
	DiskRead   *float64  `json:"diskRead"`
	DiskWrite  *float64  `json:"diskWrite"`
}

// DiskUsagePoint records drive fill level over time.
type DiskUsagePoint struct {
	Timestamp   time.Time `json:"t"`
	Mountpoint  string    `json:"mountpoint"`
	UsedPercent float64   `json:"usedPercent"`
	FreeBytes   uint64    `json:"freeBytes"`
}
