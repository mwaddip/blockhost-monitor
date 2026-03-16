package config

import (
	"testing"
)

func TestLoadMonitorConfig(t *testing.T) {
	cfg, err := LoadMonitorConfig("../../testdata/monitor.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Polling.BudgetMs != 500 {
		t.Errorf("budget_ms = %d, want 500", cfg.Polling.BudgetMs)
	}
	if cfg.Polling.MinIntervalMs != 2000 {
		t.Errorf("min_interval_ms = %d, want 2000", cfg.Polling.MinIntervalMs)
	}
	if cfg.Polling.MaxIntervalMs != 30000 {
		t.Errorf("max_interval_ms = %d, want 30000", cfg.Polling.MaxIntervalMs)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.Format != "text" {
		t.Errorf("log format = %q, want %q", cfg.Log.Format, "text")
	}
}

func TestLoadMonitorConfig_FileNotFound(t *testing.T) {
	_, err := LoadMonitorConfig("nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadPlansConfig(t *testing.T) {
	cfg, err := LoadPlansConfig("../../testdata/plans.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Plans) != 2 {
		t.Fatalf("got %d plans, want 2", len(cfg.Plans))
	}

	basic := cfg.Plans[1]
	if basic.Name != "Basic VM" {
		t.Errorf("plan 1 name = %q, want %q", basic.Name, "Basic VM")
	}
	if basic.CPUCores != 2 {
		t.Errorf("plan 1 cpu_cores = %d, want 2", basic.CPUCores)
	}
	if basic.Burst == nil {
		t.Fatal("plan 1 burst is nil")
	}
	if basic.Burst.CPUCores != 4 {
		t.Errorf("plan 1 burst cpu_cores = %d, want 4", basic.Burst.CPUCores)
	}
}

func TestLoadDbConfig(t *testing.T) {
	cfg, err := LoadDbConfig("../../testdata/db.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DbFile != "testdata/vms.json" {
		t.Errorf("db_file = %q, want %q", cfg.DbFile, "testdata/vms.json")
	}
}
