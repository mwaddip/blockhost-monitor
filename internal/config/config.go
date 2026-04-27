package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type MonitorConfig struct {
	Polling PollingConfig `yaml:"polling"`
	Paths   PathsConfig   `yaml:"paths"`
	Log     LogConfig     `yaml:"log"`
}

type PollingConfig struct {
	BudgetMs      int `yaml:"budget_ms"`
	MinIntervalMs int `yaml:"min_interval_ms"`
}

type PathsConfig struct {
	ProvisionerManifest string `yaml:"provisioner_manifest"`
	DbConfig            string `yaml:"db_config"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func LoadMonitorConfig(path string) (*MonitorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read monitor config: %w", err)
	}
	var cfg MonitorConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse monitor config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("monitor config: %w", err)
	}
	return &cfg, nil
}

func (c *MonitorConfig) Validate() error {
	if c.Polling.BudgetMs <= 0 {
		return fmt.Errorf("polling.budget_ms must be > 0, got %d", c.Polling.BudgetMs)
	}
	if c.Polling.MinIntervalMs <= 0 {
		return fmt.Errorf("polling.min_interval_ms must be > 0, got %d", c.Polling.MinIntervalMs)
	}
	if c.Paths.ProvisionerManifest == "" {
		return fmt.Errorf("paths.provisioner_manifest must be set")
	}
	if c.Paths.DbConfig == "" {
		return fmt.Errorf("paths.db_config must be set")
	}
	return nil
}

// DbConfig captures the subset of db.yaml the monitor needs.
// The full schema (ip_pool, grace_days, etc.) is in COMMON_INTERFACE.md section 7.
type DbConfig struct {
	DbFile string `yaml:"db_file"`
}

func LoadDbConfig(path string) (*DbConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read db config: %w", err)
	}
	var cfg DbConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	if cfg.DbFile == "" {
		return nil, fmt.Errorf("db config: db_file must be set")
	}
	return &cfg, nil
}
