# Metrics Polling Daemon Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the metrics polling daemon — a Go binary that continuously polls all active VMs for resource usage within a configurable time budget, storing samples for future enforcement and health monitoring layers.

**Architecture:** Budget-based polling loop. The daemon reads config (monitor settings, plan profiles, provisioner manifest, VM database), then enters a loop: discover active VMs, collect metrics from each via the provisioner's `metrics` CLI command, store samples, sleep. The polling budget is advisory in v1 — the daemon logs a warning when a cycle exceeds budget but does not truncate the cycle. This gives us real data to tune the budget before adding hard enforcement. All VM interaction goes through the provisioner CLI (never hypervisor APIs directly).

**Tech Stack:** Go 1.23, `gopkg.in/yaml.v3` (YAML config), `log/slog` (structured logging), stdlib for everything else (JSON, subprocess, signals, sync).

---

## File Structure

```
blockhost-monitor/
├── main.go                          # entry point: flags, wiring, signal handling
├── internal/
│   ├── config/
│   │   ├── config.go                # MonitorConfig, PlansConfig, DbConfig types + loaders
│   │   └── config_test.go
│   ├── prov/
│   │   ├── prov.go                  # provisioner manifest reader + command resolution
│   │   └── prov_test.go
│   ├── vmdb/
│   │   ├── vmdb.go                  # vms.json reader, active VM discovery
│   │   └── vmdb_test.go
│   ├── collector/
│   │   ├── collector.go             # single-VM metrics collection via CLI
│   │   └── collector_test.go
│   └── poller/
│       ├── poller.go                # budget-based polling loop
│       ├── store.go                 # thread-safe in-memory sample storage
│       ├── poller_test.go
│       └── store_test.go
├── go.mod
├── go.sum
├── testdata/
│   ├── monitor.yaml                 # example monitor config
│   ├── plans.yaml                   # example plan profiles
│   ├── db.yaml                      # example db config (just db_file path)
│   ├── provisioner.json             # example provisioner manifest
│   ├── vms.json                     # example VM database
│   └── metrics_output.json          # example metrics CLI output
└── systemd/
    └── blockhost-monitor.service    # systemd unit file
```

**Package responsibilities:**

| Package | Concern | Key types |
|---------|---------|-----------|
| `config` | Read all YAML/JSON config, expose typed structs | `MonitorConfig`, `PlansConfig`, `DbConfig` |
| `prov` | Read provisioner manifest, resolve command names | `Manifest` |
| `vmdb` | Read vms.json, filter active VMs | `VM` |
| `collector` | Execute metrics CLI for one VM, parse JSON result | `Metrics`, `Collector` |
| `poller` | Budget scheduling, orchestrate collectors, store samples | `Poller`, `Store`, `Sample` |

**Dependency flow:** `main` → `poller` → `collector` → (injected `RunFunc`). `main` → `config`, `prov`, `vmdb` (wiring only). Packages don't import each other except `poller` importing `collector` for the `Metrics` type.

---

## Chunk 1: Project Setup + Config

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `testdata/monitor.yaml`
- Create: `testdata/plans.yaml`
- Create: `testdata/db.yaml`
- Create: `testdata/provisioner.json`
- Create: `testdata/vms.json`
- Create: `testdata/metrics_output.json`
- Modify: `.gitignore`

- [ ] **Step 1: Initialize Go module**

```bash
cd /home/mwaddip/projects/blockhost-monitor
go mod init github.com/mwaddip/blockhost-monitor
```

- [ ] **Step 2: Update .gitignore for Go**

Replace the Python-centric patterns with Go patterns:

```gitignore
# Binary
blockhost-monitor

# Build
build/
dist/

# Go
*.test
*.out

# IDE
.idea/
.vscode/
*.swp

# Environment
.env
```

- [ ] **Step 3: Create testdata fixtures**

`testdata/monitor.yaml` — monitor daemon config (relative paths for development; production uses `/etc/blockhost/monitor.yaml` with absolute paths):
```yaml
polling:
  budget_ms: 500
  min_interval_ms: 2000
  max_interval_ms: 30000

paths:
  provisioner_manifest: testdata/provisioner.json
  db_config: testdata/db.yaml
  plans_config: testdata/plans.yaml

log:
  level: info
  format: text
```

`testdata/plans.yaml` — plan resource profiles (from DESIGN_CONTEXT.md):
```yaml
plans:
  1:
    name: "Basic VM"
    cpu_cores: 2
    memory_mb: 4096
    disk_mb: 51200
    bandwidth_mbps: 100
    iops_limit: 1000
    burst:
      cpu_cores: 4
      duration_sec: 300
  2:
    name: "Pro VM"
    cpu_cores: 4
    memory_mb: 8192
    disk_mb: 102400
    bandwidth_mbps: 500
    iops_limit: 5000
    burst:
      cpu_cores: 8
      duration_sec: 600
```

`testdata/db.yaml` — DB config (subset: the monitor only needs `db_file` to find vms.json):
```yaml
db_file: testdata/vms.json
```

`testdata/provisioner.json` — provisioner manifest (the monitor only reads `name` and `commands`, but the fixture includes all required fields for schema fidelity):
```json
{
  "name": "libvirt",
  "version": "0.1.0",
  "display_name": "libvirt (KVM)",
  "commands": {
    "create": "blockhost-vm-create",
    "destroy": "blockhost-vm-destroy",
    "start": "blockhost-vm-start",
    "stop": "blockhost-vm-stop",
    "kill": "blockhost-vm-kill",
    "status": "blockhost-vm-status",
    "list": "blockhost-vm-list",
    "metrics": "blockhost-vm-metrics",
    "throttle": "blockhost-vm-throttle",
    "build-template": "blockhost-build-template",
    "gc": "blockhost-vm-gc",
    "resume": "blockhost-vm-resume",
    "update-gecos": "blockhost-vm-update-gecos"
  },
  "setup": {
    "first_boot_hook": "/usr/share/blockhost/provisioner-hooks/first-boot.sh",
    "detect": "blockhost-provisioner-detect",
    "wizard_module": "blockhost.provisioner_libvirt.wizard",
    "finalization_steps": ["libvirt"]
  },
  "root_agent_actions": "/usr/share/blockhost/root-agent-actions/libvirt.py",
  "config_keys": {
    "session_key": "provisioner_libvirt",
    "provisioner_config": ["storage_pool"]
  }
}
```

`testdata/vms.json` — VM database (matches COMMON_INTERFACE.md VM record schema):
```json
{
  "vms": {
    "web1": {
      "vm_name": "web1",
      "vmid": "web1",
      "ip_address": "192.168.122.200",
      "ipv6_address": null,
      "status": "active",
      "owner": "alice",
      "wallet_address": "0x1234567890abcdef1234567890abcdef12345678",
      "purpose": "",
      "created_at": "2026-03-01T00:00:00Z",
      "expires_at": "2026-03-31T00:00:00Z"
    },
    "db1": {
      "vm_name": "db1",
      "vmid": "db1",
      "ip_address": "192.168.122.201",
      "ipv6_address": null,
      "status": "active",
      "owner": "bob",
      "wallet_address": "0xabcdef1234567890abcdef1234567890abcdef12",
      "purpose": "",
      "created_at": "2026-03-05T00:00:00Z",
      "expires_at": "2026-04-04T00:00:00Z"
    },
    "old1": {
      "vm_name": "old1",
      "vmid": "old1",
      "ip_address": "192.168.122.202",
      "ipv6_address": null,
      "status": "suspended",
      "owner": "charlie",
      "wallet_address": null,
      "purpose": "",
      "created_at": "2026-01-01T00:00:00Z",
      "expires_at": "2026-02-01T00:00:00Z",
      "suspended_at": "2026-02-02T00:00:00Z"
    }
  },
  "reserved_nft_tokens": {}
}
```

`testdata/metrics_output.json` — provisioner metrics command output (matches PROVISIONER_INTERFACE.md section 2.12):
```json
{
  "cpu_percent": 45.2,
  "cpu_count": 2,
  "memory_used_mb": 1824,
  "memory_total_mb": 4096,
  "disk_used_mb": 12400,
  "disk_total_mb": 51200,
  "disk_read_iops": 150,
  "disk_write_iops": 42,
  "disk_read_bytes_sec": 6291456,
  "disk_write_bytes_sec": 1048576,
  "net_rx_bytes_sec": 1048576,
  "net_tx_bytes_sec": 524288,
  "net_connections": 847,
  "guest_agent_responsive": true,
  "uptime_seconds": 86400,
  "state": "running"
}
```

- [ ] **Step 4: Create minimal main.go**

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "blockhost-monitor: not yet implemented")
	os.Exit(1)
}
```

- [ ] **Step 5: Verify build**

```bash
go build -o blockhost-monitor .
```

Expected: binary compiles, exits 1 with "not yet implemented".

- [ ] **Step 6: Commit**

```bash
git add go.mod main.go .gitignore testdata/
git commit -m "scaffold: Go module, testdata fixtures, minimal main"
```

---

### Task 2: Config Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Add yaml dependency**

```bash
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Write failing tests for MonitorConfig**

`internal/config/config_test.go`:
```go
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
```

- [ ] **Step 3: Run tests, verify they fail**

```bash
go test ./internal/config/ -v
```

Expected: compilation error — `LoadMonitorConfig` not defined.

- [ ] **Step 4: Implement config types and LoadMonitorConfig**

`internal/config/config.go`:
```go
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
```

- [ ] **Step 5: Run tests, verify they pass**

```bash
go test ./internal/config/ -v
```

Expected: PASS.

- [ ] **Step 6: Write failing tests for PlansConfig**

Add to `internal/config/config_test.go`:
```go
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
```

- [ ] **Step 7: Run tests, verify fail**

```bash
go test ./internal/config/ -run TestLoadPlansConfig -v
```

Expected: compilation error — `LoadPlansConfig` not defined.

- [ ] **Step 8: Implement PlansConfig types and loader**

Add to `internal/config/config.go`:
```go
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
```

- [ ] **Step 9: Run tests, verify pass**

```bash
go test ./internal/config/ -v
```

Expected: PASS (all config tests).

- [ ] **Step 10: Write failing test for DbConfig**

Add to `internal/config/config_test.go`:
```go
func TestLoadDbConfig(t *testing.T) {
	cfg, err := LoadDbConfig("../../testdata/db.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DbFile != "testdata/vms.json" {
		t.Errorf("db_file = %q, want %q", cfg.DbFile, "testdata/vms.json")
	}
}
```

- [ ] **Step 11: Run test, verify fail**

```bash
go test ./internal/config/ -run TestLoadDbConfig -v
```

Expected: compilation error — `LoadDbConfig` not defined.

- [ ] **Step 12: Implement DbConfig and loader**

Add to `internal/config/config.go`:
```go
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
```

- [ ] **Step 13: Run all config tests**

```bash
go test ./internal/config/ -v
```

Expected: PASS (all 4 tests).

- [ ] **Step 14: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat(config): monitor, plans, and db config loading"
```

---

### Task 3: Provisioner Manifest

**Files:**
- Create: `internal/prov/prov.go`
- Create: `internal/prov/prov_test.go`

- [ ] **Step 1: Write failing tests**

`internal/prov/prov_test.go`:
```go
package prov

import (
	"testing"
)

func TestLoadManifest(t *testing.T) {
	m, err := LoadManifest("../../testdata/provisioner.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "libvirt" {
		t.Errorf("name = %q, want %q", m.Name, "libvirt")
	}
}

func TestGetCommand(t *testing.T) {
	m, err := LoadManifest("../../testdata/provisioner.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	cmd, err := m.GetCommand("metrics")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "blockhost-vm-metrics" {
		t.Errorf("metrics command = %q, want %q", cmd, "blockhost-vm-metrics")
	}
}

func TestGetCommand_Unknown(t *testing.T) {
	m, err := LoadManifest("../../testdata/provisioner.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	_, err = m.GetCommand("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown verb")
	}
}

func TestLoadManifest_FileNotFound(t *testing.T) {
	_, err := LoadManifest("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/prov/ -v
```

Expected: compilation error.

- [ ] **Step 3: Implement Manifest**

`internal/prov/prov.go`:
```go
package prov

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest holds the subset of the provisioner manifest the monitor needs.
// The full schema (setup, root_agent_actions, config_keys) is in
// PROVISIONER_INTERFACE.md section 1.
type Manifest struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Version     string            `json:"version"`
	Commands    map[string]string `json:"commands"`
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read provisioner manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse provisioner manifest: %w", err)
	}
	return &m, nil
}

func (m *Manifest) GetCommand(verb string) (string, error) {
	cmd, ok := m.Commands[verb]
	if !ok {
		return "", fmt.Errorf("unknown provisioner verb: %q", verb)
	}
	return cmd, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/prov/ -v
```

Expected: PASS (all 4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/prov/
git commit -m "feat(prov): provisioner manifest loading and command resolution"
```

---

## Chunk 2: VM Discovery + Metrics Collection

### Task 4: VM Database Reader

**Files:**
- Create: `internal/vmdb/vmdb.go`
- Create: `internal/vmdb/vmdb_test.go`

**Note on file safety:** The Python VM database uses atomic write (temp file + rename). `rename()` is atomic on Linux for same-filesystem operations, so the Go daemon reading vms.json without locks is safe — it always sees either the complete old version or the complete new version. The poller re-reads the file each cycle (no file watcher), so new/removed VMs are picked up naturally.

- [ ] **Step 1: Write failing tests**

`internal/vmdb/vmdb_test.go`:
```go
package vmdb

import (
	"os"
	"testing"
)

func TestLoadActiveVMs(t *testing.T) {
	vms, err := LoadActiveVMs("../../testdata/vms.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// vms.json has 2 active (web1, db1) and 1 suspended (old1)
	if len(vms) != 2 {
		t.Fatalf("got %d active VMs, want 2", len(vms))
	}

	// Map iteration order is non-deterministic — check by name
	names := map[string]bool{}
	for _, vm := range vms {
		names[vm.Name] = true
	}
	if !names["web1"] {
		t.Error("missing active VM: web1")
	}
	if !names["db1"] {
		t.Error("missing active VM: db1")
	}
	if names["old1"] {
		t.Error("suspended VM old1 should not be in active list")
	}
}

func TestLoadActiveVMs_FileNotFound(t *testing.T) {
	_, err := LoadActiveVMs("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadActiveVMs_EmptyDB(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.json"
	if err := os.WriteFile(path, []byte(`{"vms":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	vms, err := LoadActiveVMs(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vms) != 0 {
		t.Errorf("got %d VMs, want 0", len(vms))
	}
}
```

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/vmdb/ -v
```

Expected: compilation error.

- [ ] **Step 3: Implement vmdb**

`internal/vmdb/vmdb.go`:
```go
package vmdb

import (
	"encoding/json"
	"fmt"
	"os"
)

type VM struct {
	Name          string  `json:"vm_name"`
	// VMID is int for Proxmox, string (domain name) for libvirt.
	VMID          any     `json:"vmid"`
	IPAddress     string  `json:"ip_address"`
	Status        string  `json:"status"`
	Owner         string  `json:"owner"`
	WalletAddress *string `json:"wallet_address"`
	CreatedAt     string  `json:"created_at"`
	ExpiresAt     string  `json:"expires_at"`
}

type database struct {
	VMs map[string]VM `json:"vms"`
}

// LoadActiveVMs reads vms.json and returns only VMs with status "active".
// The returned slice has no guaranteed order.
func LoadActiveVMs(path string) ([]VM, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read vm database: %w", err)
	}
	var db database
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("parse vm database: %w", err)
	}

	var active []VM
	for _, vm := range db.VMs {
		if vm.Status == "active" {
			active = append(active, vm)
		}
	}
	return active, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/vmdb/ -v
```

Expected: PASS (all 3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/vmdb/
git commit -m "feat(vmdb): read vms.json and filter active VMs"
```

---

### Task 5: Metrics Types and Parsing

**Files:**
- Create: `internal/collector/collector.go`
- Create: `internal/collector/collector_test.go`

- [ ] **Step 1: Write failing test for metrics parsing**

`internal/collector/collector_test.go`:
```go
package collector

import (
	"os"
	"testing"
)

func TestParseMetrics(t *testing.T) {
	data, err := os.ReadFile("../../testdata/metrics_output.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	m, err := ParseMetrics(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.CPUPercent != 45.2 {
		t.Errorf("cpu_percent = %f, want 45.2", m.CPUPercent)
	}
	if m.CPUCount != 2 {
		t.Errorf("cpu_count = %d, want 2", m.CPUCount)
	}
	if m.MemoryUsedMB != 1824 {
		t.Errorf("memory_used_mb = %d, want 1824", m.MemoryUsedMB)
	}
	if m.NetConnections != 847 {
		t.Errorf("net_connections = %d, want 847", m.NetConnections)
	}
	if !m.GuestAgentResponsive {
		t.Error("guest_agent_responsive should be true")
	}
	if m.State != "running" {
		t.Errorf("state = %q, want %q", m.State, "running")
	}
}

func TestParseMetrics_GuestAgentDown(t *testing.T) {
	data := []byte(`{
		"cpu_percent": 10.0, "cpu_count": 1,
		"memory_used_mb": 512, "memory_total_mb": 1024,
		"disk_used_mb": -1, "disk_total_mb": 20480,
		"disk_read_iops": 0, "disk_write_iops": 0,
		"disk_read_bytes_sec": 0, "disk_write_bytes_sec": 0,
		"net_rx_bytes_sec": 0, "net_tx_bytes_sec": 0,
		"net_connections": -1,
		"guest_agent_responsive": false,
		"uptime_seconds": 3600,
		"state": "running"
	}`)

	m, err := ParseMetrics(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.NetConnections != -1 {
		t.Errorf("net_connections = %d, want -1", m.NetConnections)
	}
	if m.DiskUsedMB != -1 {
		t.Errorf("disk_used_mb = %d, want -1", m.DiskUsedMB)
	}
	if m.GuestAgentResponsive {
		t.Error("guest_agent_responsive should be false")
	}
}

func TestParseMetrics_InvalidJSON(t *testing.T) {
	_, err := ParseMetrics([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
```

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/collector/ -v
```

Expected: compilation error.

- [ ] **Step 3: Implement Metrics type and ParseMetrics**

`internal/collector/collector.go`:
```go
package collector

import (
	"encoding/json"
	"fmt"
)

// Metrics matches the JSON output of the provisioner metrics command.
// See PROVISIONER_INTERFACE.md section 2.12.
type Metrics struct {
	CPUPercent           float64 `json:"cpu_percent"`
	CPUCount             int     `json:"cpu_count"`
	MemoryUsedMB         int     `json:"memory_used_mb"`
	MemoryTotalMB        int     `json:"memory_total_mb"`
	DiskUsedMB           int     `json:"disk_used_mb"`
	DiskTotalMB          int     `json:"disk_total_mb"`
	DiskReadIOPS         int     `json:"disk_read_iops"`
	DiskWriteIOPS        int     `json:"disk_write_iops"`
	DiskReadBytesSec     int     `json:"disk_read_bytes_sec"`
	DiskWriteBytesSec    int     `json:"disk_write_bytes_sec"`
	NetRxBytesSec        int     `json:"net_rx_bytes_sec"`
	NetTxBytesSec        int     `json:"net_tx_bytes_sec"`
	NetConnections       int     `json:"net_connections"`
	GuestAgentResponsive bool    `json:"guest_agent_responsive"`
	UptimeSeconds        int     `json:"uptime_seconds"`
	State                string  `json:"state"`
}

func ParseMetrics(data []byte) (*Metrics, error) {
	var m Metrics
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse metrics: %w", err)
	}
	return &m, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/collector/ -v
```

Expected: PASS (all 3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/collector/
git commit -m "feat(collector): metrics types and JSON parsing"
```

---

### Task 6: Metrics Collection via CLI

**Files:**
- Modify: `internal/collector/collector.go` (replace entire file)
- Modify: `internal/collector/collector_test.go` (replace entire file)

- [ ] **Step 1: Write failing tests for Collector.Collect**

Replace `internal/collector/collector_test.go` with the complete file including both parsing and collection tests:
```go
package collector

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestParseMetrics(t *testing.T) {
	data, err := os.ReadFile("../../testdata/metrics_output.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	m, err := ParseMetrics(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.CPUPercent != 45.2 {
		t.Errorf("cpu_percent = %f, want 45.2", m.CPUPercent)
	}
	if m.CPUCount != 2 {
		t.Errorf("cpu_count = %d, want 2", m.CPUCount)
	}
	if m.MemoryUsedMB != 1824 {
		t.Errorf("memory_used_mb = %d, want 1824", m.MemoryUsedMB)
	}
	if m.NetConnections != 847 {
		t.Errorf("net_connections = %d, want 847", m.NetConnections)
	}
	if !m.GuestAgentResponsive {
		t.Error("guest_agent_responsive should be true")
	}
	if m.State != "running" {
		t.Errorf("state = %q, want %q", m.State, "running")
	}
}

func TestParseMetrics_GuestAgentDown(t *testing.T) {
	data := []byte(`{
		"cpu_percent": 10.0, "cpu_count": 1,
		"memory_used_mb": 512, "memory_total_mb": 1024,
		"disk_used_mb": -1, "disk_total_mb": 20480,
		"disk_read_iops": 0, "disk_write_iops": 0,
		"disk_read_bytes_sec": 0, "disk_write_bytes_sec": 0,
		"net_rx_bytes_sec": 0, "net_tx_bytes_sec": 0,
		"net_connections": -1,
		"guest_agent_responsive": false,
		"uptime_seconds": 3600,
		"state": "running"
	}`)

	m, err := ParseMetrics(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.NetConnections != -1 {
		t.Errorf("net_connections = %d, want -1", m.NetConnections)
	}
	if m.DiskUsedMB != -1 {
		t.Errorf("disk_used_mb = %d, want -1", m.DiskUsedMB)
	}
	if m.GuestAgentResponsive {
		t.Error("guest_agent_responsive should be false")
	}
}

func TestParseMetrics_InvalidJSON(t *testing.T) {
	_, err := ParseMetrics([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestCollect_Success(t *testing.T) {
	fixture, err := os.ReadFile("../../testdata/metrics_output.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	mock := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "blockhost-vm-metrics" {
			t.Errorf("command = %q, want %q", name, "blockhost-vm-metrics")
		}
		if len(args) != 1 || args[0] != "web1" {
			t.Errorf("args = %v, want [web1]", args)
		}
		return fixture, nil
	}

	c := New("blockhost-vm-metrics", mock)
	m, dur, err := c.Collect(context.Background(), "web1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.CPUPercent != 45.2 {
		t.Errorf("cpu_percent = %f, want 45.2", m.CPUPercent)
	}
	if dur < 0 {
		t.Errorf("duration should be non-negative, got %v", dur)
	}
}

func TestCollect_CommandError(t *testing.T) {
	mock := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("command failed")
	}

	c := New("blockhost-vm-metrics", mock)
	_, _, err := c.Collect(context.Background(), "web1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCollect_BadJSON(t *testing.T) {
	mock := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return []byte(`not json`), nil
	}

	c := New("blockhost-vm-metrics", mock)
	_, _, err := c.Collect(context.Background(), "web1")
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/collector/ -v
```

Expected: compilation error — `New`, `RunFunc` not defined. The 3 parsing tests from Task 5 would pass, but `TestCollect_*` fail.

- [ ] **Step 3: Implement Collector**

Replace `internal/collector/collector.go` with the complete merged file:
```go
package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Metrics matches the JSON output of the provisioner metrics command.
// See PROVISIONER_INTERFACE.md section 2.12.
type Metrics struct {
	CPUPercent           float64 `json:"cpu_percent"`
	CPUCount             int     `json:"cpu_count"`
	MemoryUsedMB         int     `json:"memory_used_mb"`
	MemoryTotalMB        int     `json:"memory_total_mb"`
	DiskUsedMB           int     `json:"disk_used_mb"`
	DiskTotalMB          int     `json:"disk_total_mb"`
	DiskReadIOPS         int     `json:"disk_read_iops"`
	DiskWriteIOPS        int     `json:"disk_write_iops"`
	DiskReadBytesSec     int     `json:"disk_read_bytes_sec"`
	DiskWriteBytesSec    int     `json:"disk_write_bytes_sec"`
	NetRxBytesSec        int     `json:"net_rx_bytes_sec"`
	NetTxBytesSec        int     `json:"net_tx_bytes_sec"`
	NetConnections       int     `json:"net_connections"`
	GuestAgentResponsive bool    `json:"guest_agent_responsive"`
	UptimeSeconds        int     `json:"uptime_seconds"`
	State                string  `json:"state"`
}

func ParseMetrics(data []byte) (*Metrics, error) {
	var m Metrics
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse metrics: %w", err)
	}
	return &m, nil
}

// RunFunc executes an external command and returns its stdout.
type RunFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// Collector collects metrics for a single VM by executing the provisioner
// metrics command and parsing its JSON output.
type Collector struct {
	command string
	run     RunFunc
}

func New(command string, run RunFunc) *Collector {
	return &Collector{command: command, run: run}
}

// Collect runs the metrics command for vmName and returns parsed metrics,
// the wall-clock duration of the collection, and any error.
func (c *Collector) Collect(ctx context.Context, vmName string) (*Metrics, time.Duration, error) {
	start := time.Now()
	out, err := c.run(ctx, c.command, vmName)
	dur := time.Since(start)
	if err != nil {
		return nil, dur, fmt.Errorf("metrics %s: %w", vmName, err)
	}
	m, err := ParseMetrics(out)
	if err != nil {
		return nil, dur, fmt.Errorf("metrics %s: %w", vmName, err)
	}
	return m, dur, nil
}
```

- [ ] **Step 4: Run tests, verify pass**

```bash
go test ./internal/collector/ -v
```

Expected: PASS (all 6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/collector/
git commit -m "feat(collector): metrics collection via injected command runner"
```

---

## Chunk 3: Budget Poller + Main Loop

### Task 7: Sample Store

**Files:**
- Create: `internal/poller/store.go`
- Create: `internal/poller/store_test.go`

- [ ] **Step 1: Write failing tests**

`internal/poller/store_test.go`:
```go
package poller

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
)

func TestStore_RecordAndLatest(t *testing.T) {
	s := NewStore()

	s.Record(&Sample{
		VMName:    "web1",
		Metrics:   &collector.Metrics{CPUPercent: 45.2},
		Timestamp: time.Now(),
		Duration:  50 * time.Millisecond,
	})

	got := s.Latest("web1")
	if got == nil {
		t.Fatal("expected sample for web1")
	}
	if got.Metrics.CPUPercent != 45.2 {
		t.Errorf("cpu = %f, want 45.2", got.Metrics.CPUPercent)
	}
}

func TestStore_LatestMissing(t *testing.T) {
	s := NewStore()
	if s.Latest("nonexistent") != nil {
		t.Error("expected nil for missing VM")
	}
}

func TestStore_LatestAll(t *testing.T) {
	s := NewStore()
	now := time.Now()

	s.Record(&Sample{VMName: "web1", Metrics: &collector.Metrics{CPUPercent: 10}, Timestamp: now, Duration: time.Millisecond})
	s.Record(&Sample{VMName: "db1", Metrics: &collector.Metrics{CPUPercent: 20}, Timestamp: now, Duration: time.Millisecond})

	all := s.LatestAll()
	if len(all) != 2 {
		t.Fatalf("got %d entries, want 2", len(all))
	}
}

func TestStore_AvgPollDuration(t *testing.T) {
	s := NewStore()
	now := time.Now()

	s.Record(&Sample{VMName: "web1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: 40 * time.Millisecond})
	s.Record(&Sample{VMName: "db1", Metrics: &collector.Metrics{}, Timestamp: now, Duration: 60 * time.Millisecond})

	avg := s.AvgPollDuration()
	if avg != 50*time.Millisecond {
		t.Errorf("avg = %v, want 50ms", avg)
	}
}

func TestStore_AvgPollDuration_Empty(t *testing.T) {
	s := NewStore()
	if s.AvgPollDuration() != 0 {
		t.Error("expected 0 for empty store")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup

	// Concurrent writers with distinct keys to stress map growth
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Record(&Sample{
				VMName:    fmt.Sprintf("vm-%d", n),
				Metrics:   &collector.Metrics{CPUPercent: float64(n)},
				Timestamp: time.Now(),
				Duration:  time.Millisecond,
			})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Latest("vm-0")
			s.LatestAll()
			s.AvgPollDuration()
		}()
	}

	wg.Wait()
}
```

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/poller/ -v
```

Expected: compilation error.

- [ ] **Step 3: Implement Store**

`internal/poller/store.go`:
```go
package poller

import (
	"sync"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
)

type Sample struct {
	VMName    string
	Metrics   *collector.Metrics
	Timestamp time.Time
	Duration  time.Duration
}

type Store struct {
	mu     sync.RWMutex
	latest map[string]*Sample
}

func NewStore() *Store {
	return &Store{
		latest: make(map[string]*Sample),
	}
}

func (s *Store) Record(sample *Sample) {
	s.mu.Lock()
	s.latest[sample.VMName] = sample
	s.mu.Unlock()
}

func (s *Store) Latest(vmName string) *Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest[vmName]
}

func (s *Store) LatestAll() map[string]*Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]*Sample, len(s.latest))
	for k, v := range s.latest {
		cp[k] = v
	}
	return cp
}

// AvgPollDuration returns the average collection duration across all VMs'
// latest samples. Intended for future adaptive scheduling — not yet wired
// into the poller. Stale entries for removed VMs will skew the average;
// eviction will be added when this is consumed.
func (s *Store) AvgPollDuration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.latest) == 0 {
		return 0
	}
	var total time.Duration
	for _, sample := range s.latest {
		total += sample.Duration
	}
	return total / time.Duration(len(s.latest))
}
```

- [ ] **Step 4: Run tests with race detector**

```bash
go test ./internal/poller/ -v -race
```

Expected: PASS (all 6 tests, no race conditions).

- [ ] **Step 5: Commit**

```bash
git add internal/poller/
git commit -m "feat(poller): thread-safe in-memory sample store"
```

---

### Task 8: Budget Poller

**Files:**
- Create: `internal/poller/poller.go`
- Create: `internal/poller/poller_test.go`

- [ ] **Step 1: Write failing tests**

`internal/poller/poller_test.go`:
```go
package poller

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

func mockDiscoverer(vms []vmdb.VM) func() ([]vmdb.VM, error) {
	return func() ([]vmdb.VM, error) {
		return vms, nil
	}
}

func mockCollector(delay time.Duration) *collector.Collector {
	return collector.New("mock-metrics", func(ctx context.Context, name string, args ...string) ([]byte, error) {
		time.Sleep(delay)
		return []byte(`{"cpu_percent":10,"cpu_count":1,"memory_used_mb":512,"memory_total_mb":1024,"disk_used_mb":5000,"disk_total_mb":20480,"disk_read_iops":0,"disk_write_iops":0,"disk_read_bytes_sec":0,"disk_write_bytes_sec":0,"net_rx_bytes_sec":0,"net_tx_bytes_sec":0,"net_connections":10,"guest_agent_responsive":true,"uptime_seconds":3600,"state":"running"}`), nil
	})
}

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestRunOnce_CollectsAllVMs(t *testing.T) {
	vms := []vmdb.VM{
		{Name: "web1", Status: "active"},
		{Name: "db1", Status: "active"},
	}

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	all := store.LatestAll()
	if len(all) != 2 {
		t.Fatalf("got %d samples, want 2", len(all))
	}
	if all["web1"] == nil {
		t.Error("missing sample for web1")
	}
	if all["db1"] == nil {
		t.Error("missing sample for db1")
	}
}

func TestRunOnce_EmptyVMList(t *testing.T) {
	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   mockCollector(0),
		Store:       store,
		DiscoverVMs: mockDiscoverer(nil),
		Budget:      500 * time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if len(store.LatestAll()) != 0 {
		t.Error("expected no samples for empty VM list")
	}
}

func TestRunOnce_BudgetExceededWarning(t *testing.T) {
	vms := []vmdb.VM{{Name: "slow1", Status: "active"}}

	store := NewStore()
	var logBuf bytes.Buffer
	// Budget of 1ms but collector takes 50ms
	p := New(Options{
		Collector:   mockCollector(50 * time.Millisecond),
		Store:       store,
		DiscoverVMs: mockDiscoverer(vms),
		Budget:      time.Millisecond,
		MinInterval: time.Second,
		Log:         testLogger(&logBuf),
	})

	p.RunOnce(context.Background())

	if !bytes.Contains(logBuf.Bytes(), []byte("poll cycle exceeded budget")) {
		t.Error("expected budget exceeded warning in logs")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	// Use an atomic counter to confirm at least one collection happened
	var collected atomic.Int32
	coll := collector.New("mock-metrics", func(ctx context.Context, name string, args ...string) ([]byte, error) {
		collected.Add(1)
		return []byte(`{"cpu_percent":10,"cpu_count":1,"memory_used_mb":512,"memory_total_mb":1024,"disk_used_mb":5000,"disk_total_mb":20480,"disk_read_iops":0,"disk_write_iops":0,"disk_read_bytes_sec":0,"disk_write_bytes_sec":0,"net_rx_bytes_sec":0,"net_tx_bytes_sec":0,"net_connections":10,"guest_agent_responsive":true,"uptime_seconds":3600,"state":"running"}`), nil
	})

	store := NewStore()
	var logBuf bytes.Buffer
	p := New(Options{
		Collector:   coll,
		Store:       store,
		DiscoverVMs: mockDiscoverer([]vmdb.VM{{Name: "vm1", Status: "active"}}),
		Budget:      time.Second,
		MinInterval: 50 * time.Millisecond,
		Log:         testLogger(&logBuf),
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Run in a goroutine, cancel after first collection
	done := make(chan error, 1)
	go func() {
		done <- p.Run(ctx)
	}()

	// Wait for at least one collection, then cancel
	deadline := time.After(2 * time.Second)
	for collected.Load() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for first collection")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	err := <-done

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if store.Latest("vm1") == nil {
		t.Error("expected at least one sample")
	}
}
```

- [ ] **Step 2: Run tests, verify fail**

```bash
go test ./internal/poller/ -run "TestRunOnce|TestRun_Stops" -v
```

Expected: compilation error.

- [ ] **Step 3: Implement Poller**

`internal/poller/poller.go`:
```go
package poller

import (
	"context"
	"log/slog"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

type Options struct {
	Collector   *collector.Collector
	Store       *Store
	DiscoverVMs func() ([]vmdb.VM, error)
	Budget      time.Duration
	MinInterval time.Duration
	Log         *slog.Logger
}

type Poller struct {
	collector   *collector.Collector
	store       *Store
	discoverVMs func() ([]vmdb.VM, error)
	budget      time.Duration
	minInterval time.Duration
	log         *slog.Logger
}

func New(opts Options) *Poller {
	return &Poller{
		collector:   opts.Collector,
		store:       opts.Store,
		discoverVMs: opts.DiscoverVMs,
		budget:      opts.Budget,
		minInterval: opts.MinInterval,
		log:         opts.Log,
	}
}

// RunOnce executes a single poll cycle: discover VMs, collect metrics, store.
// Logs a warning if the cycle exceeds the configured budget.
func (p *Poller) RunOnce(ctx context.Context) {
	cycleStart := time.Now()

	vms, err := p.discoverVMs()
	if err != nil {
		p.log.Error("vm discovery failed", "error", err)
		return
	}
	if len(vms) == 0 {
		return
	}

	for _, vm := range vms {
		if ctx.Err() != nil {
			return
		}
		m, dur, err := p.collector.Collect(ctx, vm.Name)
		if err != nil {
			p.log.Error("metrics collection failed", "vm", vm.Name, "error", err)
			continue
		}
		p.store.Record(&Sample{
			VMName:    vm.Name,
			Metrics:   m,
			Timestamp: time.Now(),
			Duration:  dur,
		})
	}

	elapsed := time.Since(cycleStart)
	if elapsed > p.budget {
		p.log.Warn("poll cycle exceeded budget",
			"elapsed_ms", elapsed.Milliseconds(),
			"budget_ms", p.budget.Milliseconds(),
			"vm_count", len(vms),
		)
	}
}

// Run enters the main poll loop. Calls RunOnce each cycle, then sleeps
// until minInterval has elapsed since the cycle started. Returns when
// the context is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	for {
		cycleStart := time.Now()
		p.RunOnce(ctx)

		elapsed := time.Since(cycleStart)
		sleep := p.minInterval - elapsed
		if sleep <= 0 {
			p.log.Warn("poll cycle took longer than min interval",
				"elapsed_ms", elapsed.Milliseconds(),
				"min_interval_ms", p.minInterval.Milliseconds(),
			)
			sleep = 0
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}
```

- [ ] **Step 4: Run all poller tests with race detector**

```bash
go test ./internal/poller/ -v -race
```

Expected: PASS (all poller + store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/poller/
git commit -m "feat(poller): budget-based polling loop with VM discovery"
```

---

### Task 9: Main Entry Point + Systemd

**Files:**
- Modify: `main.go`
- Create: `systemd/blockhost-monitor.service`

- [ ] **Step 1: Write test for setupLogger**

Create `main_test.go`:
```go
package main

import (
	"testing"

	"github.com/mwaddip/blockhost-monitor/internal/config"
)

func TestSetupLogger_JSON(t *testing.T) {
	log := setupLogger(config.LogConfig{Level: "debug", Format: "json"})
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
	if !log.Enabled(nil, -4) { // slog.LevelDebug = -4
		t.Error("debug level should be enabled")
	}
}

func TestSetupLogger_Text(t *testing.T) {
	log := setupLogger(config.LogConfig{Level: "warn", Format: "text"})
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
	if log.Enabled(nil, 0) { // slog.LevelInfo = 0
		t.Error("info level should not be enabled at warn level")
	}
}
```

- [ ] **Step 2: Run test, verify fail**

```bash
go test -run TestSetupLogger -v
```

Expected: compilation error — `setupLogger` not defined.

- [ ] **Step 3: Implement main.go**

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/mwaddip/blockhost-monitor/internal/collector"
	"github.com/mwaddip/blockhost-monitor/internal/config"
	"github.com/mwaddip/blockhost-monitor/internal/poller"
	"github.com/mwaddip/blockhost-monitor/internal/prov"
	"github.com/mwaddip/blockhost-monitor/internal/vmdb"
)

func main() {
	configPath := flag.String("config", "/etc/blockhost/monitor.yaml", "path to monitor config")
	flag.Parse()

	cfg, err := config.LoadMonitorConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log := setupLogger(cfg.Log)

	manifest, err := prov.LoadManifest(cfg.Paths.ProvisionerManifest)
	if err != nil {
		log.Error("provisioner manifest", "error", err)
		os.Exit(1)
	}

	metricsCmd, err := manifest.GetCommand("metrics")
	if err != nil {
		log.Error("provisioner missing metrics command", "error", err)
		os.Exit(1)
	}

	dbCfg, err := config.LoadDbConfig(cfg.Paths.DbConfig)
	if err != nil {
		log.Error("db config", "error", err)
		os.Exit(1)
	}

	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		out, err := cmd.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
				return nil, fmt.Errorf("%w: %s", err, exitErr.Stderr)
			}
			return nil, err
		}
		return out, nil
	}

	store := poller.NewStore()
	coll := collector.New(metricsCmd, run)
	discover := func() ([]vmdb.VM, error) {
		return vmdb.LoadActiveVMs(dbCfg.DbFile)
	}

	p := poller.New(poller.Options{
		Collector:   coll,
		Store:       store,
		DiscoverVMs: discover,
		Budget:      time.Duration(cfg.Polling.BudgetMs) * time.Millisecond,
		MinInterval: time.Duration(cfg.Polling.MinIntervalMs) * time.Millisecond,
		Log:         log,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("starting blockhost-monitor",
		"provisioner", manifest.Name,
		"budget_ms", cfg.Polling.BudgetMs,
		"min_interval_ms", cfg.Polling.MinIntervalMs,
	)

	if err := p.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("poller exited", "error", err)
		os.Exit(1)
	}

	log.Info("blockhost-monitor stopped")
}

func setupLogger(cfg config.LogConfig) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{}

	switch cfg.Level {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}

	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
```

- [ ] **Step 4: Run tests, verify pass**

```bash
go test -run TestSetupLogger -v
```

Expected: PASS.

- [ ] **Step 5: Verify build**

```bash
go build -o blockhost-monitor .
```

Expected: compiles cleanly.

- [ ] **Step 6: Smoke test with testdata config**

```bash
./blockhost-monitor --config testdata/monitor.yaml &
PID=$!
sleep 2
kill $PID
wait $PID 2>/dev/null
```

Expected: starts, logs provisioner name, fails on metrics command (not installed), logs errors for each poll attempt, shuts down cleanly on SIGTERM. The daemon keeps running even when metrics collection fails — this is expected resilience behavior.

- [ ] **Step 7: Create systemd service file**

`systemd/blockhost-monitor.service`:
```ini
[Unit]
Description=BlockHost VM Monitor
Documentation=https://github.com/mwaddip/blockhost-monitor
After=network.target
Wants=network.target

[Service]
Type=simple
User=blockhost
Group=blockhost
ExecStart=/usr/bin/blockhost-monitor --config /etc/blockhost/monitor.yaml
Restart=always
RestartSec=10

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=blockhost-monitor

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadOnlyPaths=/etc/blockhost
ReadOnlyPaths=/usr/share/blockhost
# The monitor spawns provisioner commands (blockhost-vm-metrics) as child
# processes that inherit this namespace. The provisioner writes delta samples
# to /var/lib/blockhost/metrics/, so this path must be read-write.
ReadWritePaths=/var/lib/blockhost

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 8: Run all tests**

```bash
go test ./... -v -race
```

Expected: all tests pass, no race conditions.

- [ ] **Step 9: Commit**

```bash
git add main.go main_test.go systemd/
git commit -m "feat: main entry point with signal handling and systemd service"
```

---

## Verification

After completing all tasks, the daemon should:

1. **Compile:** `go build -o blockhost-monitor .` succeeds
2. **Test:** `go test ./... -race` passes all tests
3. **Start:** `./blockhost-monitor --config testdata/monitor.yaml` starts and enters poll loop
4. **Shutdown:** responds to SIGINT/SIGTERM with graceful shutdown via `signal.NotifyContext`
5. **Budget:** logs a warning when poll cycle exceeds the configured budget (advisory in v1)
6. **Discovery:** re-reads vms.json each cycle (picks up new/removed VMs)
7. **Resilience:** keeps running when individual VM metrics collection fails
8. **Stderr:** includes provisioner stderr output in error messages for debugging

## What this does NOT include (deferred to future plans)

- **Plan enforcement** — comparing metrics against resource envelopes (requires plan-to-VM mapping, which is not yet in the VM record schema; see DESIGN_CONTEXT.md "Open questions")
- Throttle/suspend actions (graduated response model)
- Abuse detection (behavioral patterns)
- Health monitoring (crashed VMs, guest agent, disk full, OOM)
- Behavioral baselines (per-VM normals)
- GC absorption from engines
- Log consolidation
- Config hot-reload (SIGHUP)
- Concurrent VM polling within a cycle (parallel budget spending)
- Priority-based poll scheduling (hot VMs polled more often)
- Adaptive interval from measured poll cost (AvgPollDuration is available but not yet consumed)
- Hard budget enforcement (skip remaining VMs when budget exhausted)
