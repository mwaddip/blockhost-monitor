package config

import (
	"os"
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

func TestLoadDbConfig(t *testing.T) {
	cfg, err := LoadDbConfig("../../testdata/db.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DbFile != "testdata/vms.json" {
		t.Errorf("db_file = %q, want %q", cfg.DbFile, "testdata/vms.json")
	}
}

func TestMonitorConfig_Validate(t *testing.T) {
	good := func() MonitorConfig {
		return MonitorConfig{
			Polling: PollingConfig{BudgetMs: 500, MinIntervalMs: 2000},
			Paths:   PathsConfig{ProvisionerManifest: "p", DbConfig: "d"},
		}
	}

	cases := []struct {
		name    string
		mutate  func(*MonitorConfig)
		wantErr bool
	}{
		{"valid", func(c *MonitorConfig) {}, false},
		{"zero budget", func(c *MonitorConfig) { c.Polling.BudgetMs = 0 }, true},
		{"negative budget", func(c *MonitorConfig) { c.Polling.BudgetMs = -1 }, true},
		{"zero min interval", func(c *MonitorConfig) { c.Polling.MinIntervalMs = 0 }, true},
		{"negative min interval", func(c *MonitorConfig) { c.Polling.MinIntervalMs = -1 }, true},
		{"empty manifest path", func(c *MonitorConfig) { c.Paths.ProvisionerManifest = "" }, true},
		{"empty db config path", func(c *MonitorConfig) { c.Paths.DbConfig = "" }, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := good()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadDbConfig_EmptyDbFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.yaml"
	if err := os.WriteFile(path, []byte("db_file: \"\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDbConfig(path); err == nil {
		t.Fatal("expected error for empty db_file")
	}
}
