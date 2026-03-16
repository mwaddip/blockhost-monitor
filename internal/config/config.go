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
	MaxIntervalMs int `yaml:"max_interval_ms"`
}

type PathsConfig struct {
	ProvisionerManifest string `yaml:"provisioner_manifest"`
	DbConfig            string `yaml:"db_config"`
	PlansConfig         string `yaml:"plans_config"`
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
	return &cfg, nil
}

type PlansConfig struct {
	Plans map[int]PlanProfile `yaml:"plans"`
}

type PlanProfile struct {
	Name          string       `yaml:"name"`
	CPUCores      int          `yaml:"cpu_cores"`
	MemoryMB      int          `yaml:"memory_mb"`
	DiskMB        int          `yaml:"disk_mb"`
	BandwidthMbps int          `yaml:"bandwidth_mbps"`
	IOPSLimit     int          `yaml:"iops_limit"`
	Burst         *BurstConfig `yaml:"burst"`
}

type BurstConfig struct {
	CPUCores    int `yaml:"cpu_cores"`
	DurationSec int `yaml:"duration_sec"`
}

func LoadPlansConfig(path string) (*PlansConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plans config: %w", err)
	}
	var cfg PlansConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse plans config: %w", err)
	}
	return &cfg, nil
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
	return &cfg, nil
}
