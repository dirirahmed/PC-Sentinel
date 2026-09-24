package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAreValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || cfg != Default() {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadPartialFileMergesOntoDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"port": 9000, "thresholds": {"cpuUsage": {"warning": 80, "critical": 90, "sustainSeconds": 30}}}`), 0o644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9000 || cfg.Thresholds.CPUUsage.Warning != 80 {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.RetentionDays != Default().RetentionDays || cfg.Thresholds.MemoryUsage != Default().Thresholds.MemoryUsage {
		t.Error("unspecified fields should keep defaults")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"retentionDays": 0}`), 0o644)
	cfg, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "retentionDays") {
		t.Fatalf("expected retentionDays error, got %v", err)
	}
	if cfg != Default() {
		t.Error("invalid file should fall back to defaults")
	}
}

func TestValidateThresholdOrdering(t *testing.T) {
	c := Default()
	c.Thresholds.CPUUsage.Critical = c.Thresholds.CPUUsage.Warning - 1
	c.Thresholds.DiskFreePercent.Critical = c.Thresholds.DiskFreePercent.Warning + 1
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "cpuUsage.critical") || !strings.Contains(err.Error(), "diskFreePercent.critical") {
		t.Fatalf("expected both ordering errors, got %v", err)
	}
}

func TestStoreUpdatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	s := NewStore(path, Default())
	next := Default()
	next.SampleIntervalSeconds = 5
	if err := s.Update(next); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.SampleIntervalSeconds != 5 {
		t.Fatalf("persisted config = %+v, err %v", loaded, err)
	}
	bad := next
	bad.Port = 0
	if err := s.Update(bad); err == nil {
		t.Fatal("invalid update accepted")
	}
	if s.Get().Port != Default().Port {
		t.Error("rejected update must not change live config")
	}
}

// The documented example must stay loadable and in sync with the defaults.
func TestExampleConfigMatchesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Errorf("config.example.json differs from Default():\n%+v\n%+v", cfg, Default())
	}
}
