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
