# Design Context — blockhost-monitor

> Captured from the initial design conversation. This is the starting point, not a spec.

## What this component is

The host's immune system. A daemon that runs continuously on every BlockHost host, watching all VMs for resource abuse, health issues, and policy violations. It enforces plan-defined resource limits and detects anomalous behavior without invading VM privacy.

## What it is NOT

- Not the engine's blockchain monitor (that watches on-chain events)
- Not the provisioner (that creates/destroys VMs)
- Not a general-purpose monitoring stack (no Prometheus/Grafana — keep it self-contained)

## Core responsibilities

### 1. Metrics collection
- Poll VM resource usage via provisioner CLI (`blockhost-vm-metrics`)
- CPU, memory, disk usage, disk I/O, network bandwidth, connection counts
- Must be as cheap as possible per poll — every wasted cycle multiplies by VM count × frequency
- Polling interval should be determined empirically: measure the cost of a poll first, then derive a meaningful interval

### 2. Resource limit enforcement
- Plans define resource envelopes (CPU cores, RAM, disk, bandwidth, IOPS)
- Plans are an off-chain ID — the on-chain plan only has an ID and price
- The host maps plan IDs to local resource profiles via config
- The admin panel will manage plan resource profiles (future: plan manager in admin panel)
- Baseline vs burst: sustained access to X, allowed to burst briefly, throttle when burst becomes the norm
- Soft vs hard limits: CPU/bandwidth degrade gracefully, disk is a hard wall

### 3. Abuse detection (behavioral, not content-based)
- **Privacy is a hard line**: watch patterns, never packet contents
- Detectable from hypervisor level without inspecting VM internals:
  - Network flow patterns: connection counts, bandwidth per destination, port scanning signatures
  - CPU profiles: cryptomining has a distinctive flat-line 100% pattern
  - Disk I/O patterns: ransomware has a recognizable encrypt-everything write pattern
  - Process count spikes via guest agent (count, not names)
- Possible future: known malware signature detection without privacy invasion (needs research)

### 4. Noisy neighbor detection
- A VM within its own limits can still degrade the host (random 4K IOPS at max, heavy network)
- Host-level metrics (overall CPU steal, I/O wait, network saturation) correlated with per-VM metrics
- In scope from day one

### 5. Health monitoring
- VM crashed / unresponsive
- Guest agent down
- Disk full
- OOM-killed processes
- The monitor must be the last thing standing — if the host is degraded, this daemon keeps running

### 6. Logging pipeline
- Currently scattered: systemd journal, /var/log/blockhost-*.log, Flask output, engine stdout
- Needs consolidation into structured, queryable format
- Not a full ELK stack — something lightweight and self-contained
- Admin reads these logs — clarity matters

## Graduated response model

Never jump to the nuclear option. Escalation path:

1. **Log** — record the anomaly, no action
2. **Warn** — notify admin (mechanism TBD: log, webhook, admin panel alert)
3. **Throttle** — reduce VM resources via provisioner (`blockhost-vm-throttle`)
4. **Suspend** — stop the VM, preserve data
5. **Destroy** — last resort, only for clear abuse + admin confirmation

Thresholds and timing for each escalation step are policy-defined, configurable per plan.

## Provisioner interface requirements

Two new commands needed in the provisioner contract:

### `blockhost-vm-metrics <vm-name>`
Returns standardized JSON:
```json
{
  "cpu_percent": 45.2,
  "memory_used_mb": 1824,
  "memory_total_mb": 4096,
  "disk_used_mb": 12400,
  "disk_total_mb": 51200,
  "disk_read_iops": 150,
  "disk_write_iops": 42,
  "net_rx_bytes_sec": 1048576,
  "net_tx_bytes_sec": 524288,
  "net_connections": 847,
  "guest_agent_responsive": true
}
```

Implementation is provisioner-specific:
- libvirt: `virsh domstats`, `virsh domifstat`, guest agent queries
- Proxmox: `/api2/json/nodes/{node}/qemu/{vmid}/status/current`, PVE firewall stats

### `blockhost-vm-throttle <vm-name> --cpu <shares> --bandwidth <kbps>`
Already listed in provisioner manifests but not implemented. Needs:
- CPU: cgroup quota adjustment (libvirt), Proxmox API resource limits
- Bandwidth: tc/nftables shaping (libvirt), PVE firewall bandwidth limits

## Plan resource profiles

Config file (YAML) mapping plan IDs to resource envelopes:

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
      cpu_cores: 4        # allowed burst
      duration_sec: 300   # max burst duration before throttle
  2:
    name: "Pro VM"
    cpu_cores: 4
    memory_mb: 8192
    # ...
```

## Wizard integration

CPU and bandwidth limits should be configurable in the installer wizard with sensible defaults based on system state (detected CPU count, available RAM, network interface speed). The wizard step would set global defaults; per-plan customization happens in the admin panel.

## Open questions

- **Language choice**: Python (consistent with installer/common)? Rust (performance for tight polling loops)? Go?
- **State persistence**: Does the monitor need to remember historical metrics for trend analysis, or is it purely reactive?
- **Admin notification mechanism**: Log-only for v1? Webhook? Push to admin panel?
- **Relationship with engine monitor**: The engine's blockchain monitor and this host monitor both run as daemons. Do they share a process? Communicate? Or completely independent?
- **systemd integration**: Watchdog timer? Restart policies? Resource limits on the monitor itself (don't let the cop eat all the donuts)?

## First step

**Metrics interface.** Can't enforce what you can't measure. Define the `blockhost-vm-metrics` contract in facts/, implement in both provisioners, measure polling cost. Everything else builds on that data.
