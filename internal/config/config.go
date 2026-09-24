// Package config loads, validates and persists PC Sentinel settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Threshold is a two-level alert threshold. For "above" metrics (usage,
// temperature) an alert fires when the value is >= the threshold; for disk
// free space it fires when free percent is <= the threshold.
type Threshold struct {
	Warning        float64 `json:"warning"`
	Critical       float64 `json:"critical"`
	SustainSeconds int     `json:"sustainSeconds"`
}

// SustainedLoad flags long-running high CPU that stays under the usage
// thresholds but is still unusual for an idle-ish desktop.
type SustainedLoad struct {
	AveragePercent float64 `json:"averagePercent"`
	WindowMinutes  int     `json:"windowMinutes"`
}

type Thresholds struct {
	CPUUsage        Threshold     `json:"cpuUsage"`
	MemoryUsage     Threshold     `json:"memoryUsage"`
	DiskFreePercent Threshold     `json:"diskFreePercent"`
	CPUTemperature  Threshold     `json:"cpuTemperature"`
	GPUTemperature  Threshold     `json:"gpuTemperature"`
	SustainedCPU    SustainedLoad `json:"sustainedCpu"`
}

type Config struct {
	ListenAddress          string     `json:"listenAddress"`
	Port                   int        `json:"port"`
	SampleIntervalSeconds  int        `json:"sampleIntervalSeconds"`
	HistoryIntervalSeconds int        `json:"historyIntervalSeconds"`
	RetentionDays          int        `json:"retentionDays"`
	AlertCooldownSeconds   int        `json:"alertCooldownSeconds"`
	Thresholds             Thresholds `json:"thresholds"`
}

func Default() Config {
	return Config{
		ListenAddress:          "127.0.0.1",
		Port:                   8787,
		SampleIntervalSeconds:  2,
		HistoryIntervalSeconds: 10,
		RetentionDays:          7,
		AlertCooldownSeconds:   300,
		Thresholds: Thresholds{
			CPUUsage:        Threshold{Warning: 90, Critical: 97, SustainSeconds: 60},
			MemoryUsage:     Threshold{Warning: 85, Critical: 95, SustainSeconds: 30},
			DiskFreePercent: Threshold{Warning: 10, Critical: 5},
			CPUTemperature:  Threshold{Warning: 85, Critical: 95, SustainSeconds: 30},
			GPUTemperature:  Threshold{Warning: 83, Critical: 90, SustainSeconds: 30},
			SustainedCPU:    SustainedLoad{AveragePercent: 75, WindowMinutes: 15},
		},
	}
}

func (c Config) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	check(c.Port >= 1 && c.Port <= 65535, "port must be between 1 and 65535")
	check(c.ListenAddress != "", "listenAddress must not be empty")
	check(c.SampleIntervalSeconds >= 1 && c.SampleIntervalSeconds <= 60, "sampleIntervalSeconds must be between 1 and 60")
	check(c.HistoryIntervalSeconds >= c.SampleIntervalSeconds && c.HistoryIntervalSeconds <= 600,
		"historyIntervalSeconds must be between sampleIntervalSeconds and 600")
	check(c.RetentionDays >= 1 && c.RetentionDays <= 90, "retentionDays must be between 1 and 90")
	check(c.AlertCooldownSeconds >= 0 && c.AlertCooldownSeconds <= 3600, "alertCooldownSeconds must be between 0 and 3600")

	t := c.Thresholds
	checkAbove := func(name string, th Threshold, max float64) {
		check(th.Warning > 0 && th.Warning <= max, "%s.warning must be in (0, %g]", name, max)
		check(th.Critical >= th.Warning && th.Critical <= max, "%s.critical must be >= warning and <= %g", name, max)
		check(th.SustainSeconds >= 0 && th.SustainSeconds <= 3600, "%s.sustainSeconds must be between 0 and 3600", name)
	}
	checkAbove("cpuUsage", t.CPUUsage, 100)
	checkAbove("memoryUsage", t.MemoryUsage, 100)
	checkAbove("cpuTemperature", t.CPUTemperature, 125)
	checkAbove("gpuTemperature", t.GPUTemperature, 125)
	check(t.DiskFreePercent.Warning > 0 && t.DiskFreePercent.Warning < 100, "diskFreePercent.warning must be in (0, 100)")
	check(t.DiskFreePercent.Critical > 0 && t.DiskFreePercent.Critical <= t.DiskFreePercent.Warning,
		"diskFreePercent.critical must be > 0 and <= warning")
	check(t.SustainedCPU.AveragePercent > 0 && t.SustainedCPU.AveragePercent <= 100, "sustainedCpu.averagePercent must be in (0, 100]")
	check(t.SustainedCPU.WindowMinutes >= 1 && t.SustainedCPU.WindowMinutes <= 25,
		"sustainedCpu.windowMinutes must be between 1 and 25")
	return errors.Join(errs...)
}

// Load reads a config file. A missing file yields defaults; fields absent
// from the file keep their default values.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Default(), fmt.Errorf("invalid config %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config atomically so a crash mid-write can't leave a
// truncated file behind.
func Save(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Store is the live, concurrency-safe configuration shared by the monitor and API.
type Store struct {
	mu   sync.RWMutex
	cfg  Config
	path string
}

func NewStore(path string, cfg Config) *Store {
	return &Store{cfg: cfg, path: path}
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update validates, persists and then applies cfg. Listen address and port
// changes are persisted but only take effect after a restart.
func (s *Store) Update(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path != "" {
		if err := Save(s.path, cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	s.cfg = cfg
	return nil
}
