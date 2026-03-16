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
