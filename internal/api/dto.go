package api

import (
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/analyzer"
	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/models"
)

// Response shapes. Telemetry structs from models are embedded as-is; these
// types only add the context each endpoint needs (ranges, summaries, status).

type SystemResponse struct {
	Host         models.HostInfo          `json:"host"`
	Timestamp    time.Time                `json:"timestamp"`
	Uptime       uint64                   `json:"uptimeSeconds"`
	Health       analyzer.HealthReport    `json:"health"`
	CPU          models.CPUStats          `json:"cpu"`
	Memory       models.MemoryStats       `json:"memory"`
	GPU          models.GPUReport         `json:"gpu"`
	Disks        []models.DiskStats       `json:"disks"`
	DiskIO       models.DiskIO            `json:"diskIO"`
	Network      models.NetworkStats      `json:"network"`
	ActiveAlerts []models.Alert           `json:"activeAlerts"`
	Collectors   []models.CollectorStatus `json:"collectors"`
	Agent        models.AgentStats        `json:"agent"`
}

type ComponentResponse[T any] struct {
	Timestamp time.Time      `json:"timestamp"`
	Current   T              `json:"current"`
	Range     string         `json:"range,omitempty"`
	Summary   *analyzer.Stat `json:"summary,omitempty"`
}

type NetworkResponse struct {
	Timestamp time.Time           `json:"timestamp"`
	Current   models.NetworkStats `json:"current"`
	Range     string              `json:"range"`
	Download  analyzer.Stat       `json:"download"`
	Upload    analyzer.Stat       `json:"upload"`
}

type DisksResponse struct {
	Timestamp time.Time          `json:"timestamp"`
	Disks     []models.DiskStats `json:"disks"`
	IO        models.DiskIO      `json:"io"`
}

type MetricsResponse struct {
	Range         string                  `json:"range"`
	BucketSeconds float64                 `json:"bucketSeconds"`
	Points        []models.HistoryPoint   `json:"points"`
	Summary       analyzer.SeriesSummary  `json:"summary"`
	Disks         []models.DiskUsagePoint `json:"disks,omitempty"`
	DisksError    string                  `json:"disksError,omitempty"`
}

type ProcessesResponse struct {
	Timestamp time.Time        `json:"timestamp"`
	Count     int              `json:"count"`
	Processes []models.Process `json:"processes"`
	Note      string           `json:"note"`
}

type AlertsResponse struct {
	Active  []models.Alert `json:"active"`
	History []models.Alert `json:"history"`
	// HistoryError is set when stored history couldn't be read; active
	// alerts are still returned from memory.
	HistoryError string `json:"historyError,omitempty"`
}

type SettingsResponse struct {
	Config          config.Config `json:"config"`
	RestartRequired []string      `json:"restartRequired,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}
