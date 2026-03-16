# Design Context — blockhost-monitor

> Captured from design conversations. This is the starting point, not a spec.

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

## Provisioner interface (IMPLEMENTED)

Both provisioners (libvirt and Proxmox) now implement the full metrics and throttle contracts. The specs are in `facts/PROVISIONER_INTERFACE.md` sections 2.12 and 2.13.

### `blockhost-vm-metrics <vm-name>`
Returns standardized JSON with all fields defined in the contract:
- CPU: `cpu_percent`, `cpu_count`
- Memory: `memory_used_mb`, `memory_total_mb`
- Disk: `disk_used_mb`, `disk_total_mb`, `disk_read_iops`, `disk_write_iops`, `disk_read_bytes_sec`, `disk_write_bytes_sec`
- Network: `net_rx_bytes_sec`, `net_tx_bytes_sec`, `net_connections`
- Health: `guest_agent_responsive`, `uptime_seconds`, `state`

Rate-based fields (IOPS, bytes/sec) are derived from cumulative counters with delta calculation against a previous sample stored in `/var/lib/blockhost/metrics/<name>.json`.

Fields requiring the guest agent return -1 when the agent is unresponsive. The command never blocks waiting for the agent.

### `blockhost-vm-throttle <vm-name> [options]`
Options: `--cpu-shares`, `--cpu-quota`, `--bandwidth-in`, `--bandwidth-out`, `--iops-read`, `--iops-write`, `--reset`. Additive — only specified limits change.

## Engine/monitor responsibility split

The current architecture has engines doing double duty: watching chain events AND managing VM lifecycle (GC, suspension, destruction). The long-term plan:

- **Engine**: watches chain, answers questions. Pure chain reader.
  - "Did a new subscription appear?"
  - "Is subscription X still active?"
  - "What's the expiry?"
- **Monitor**: polls the engine's answers, decides what to do.
  - "This subscription expired → suspend VM"
  - "Grace period passed → destroy"
  - "New subscription → tell provisioner to create"

The GC logic is identical across engines — check expiry, suspend, wait grace period, destroy. Only the "check expiry" part is chain-specific. This migration happens after the monitor is proven stable with metrics collection. Don't absorb responsibilities before the foundation works.

### Subscription liveness predicate

The engine's `is` CLI needs a new predicate: `is active <subscription-id>` — checks whether the subscription backing a VM is still funded/valid. Engine-specific implementation (EVM checks contract storage, OPNet checks contract state, Cardano checks UTXO existence), chain-agnostic question. The monitor calls it without needing to understand chain internals.

This is not yet in `ENGINE_INTERFACE.md` — it will be added when the monitor is ready to consume it.

## Behavioral baselines (future)

Beyond static limit enforcement, the monitor should learn what "normal" looks like per VM:

- **Per-VM behavioral profiles**: learn baseline patterns over the first week. CPU at 80% is abuse for a web server, idle for a build machine.
- **Temporal correlation**: disk I/O spike + network spike + new outbound connections = probable exfiltration. CPU spike alone = probably a deployment.
- **Cross-VM correlation**: three VMs from different wallets hitting the same destination simultaneously = botnet. One VM doing it = maybe just a CDN.
- **Lifecycle awareness**: brand new VM immediately maxing CPU = cryptominer. VM running fine for months suddenly doing it = compromised.

The 15 years of sysadmin experience is really a lookup table of "pattern X in context Y usually means Z." That's buildable. Start with baselines, add correlation rules incrementally.

## Log anomaly detection (future, exploratory)

Train a small language model on what healthy log output looks like. Flag when the live stream diverges from the learned distribution.

- Character-level LSTM or small transformer on tokenized log lines
- Log vocabulary is tiny (~5,000 tokens) vs natural language
- A 50MB quantized model doing inference every few seconds is feasible on a host
- Not classification ("is this line bad?") — anomaly detection on sequences (high perplexity = unexpected)
- Challenges: host resource budget, baseline contamination, update sensitivity, explainability
- Must surface *what* about the sequence is anomalous, not just "model flagged this"

Detailed notes in `~/projects/IDEAS.md` under "Log Anomaly Detection."

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
- **State persistence**: Does the monitor need to remember historical metrics for trend analysis, or is it purely reactive? (Leaning yes — baselines need history)
- **Admin notification mechanism**: Log-only for v1? Webhook? Push to admin panel?
- **systemd integration**: Watchdog timer? Restart policies? Resource limits on the monitor itself (don't let the cop eat all the donuts)?

## Build order

1. ~~**Metrics interface**~~ — DONE. Contract defined, both provisioners implemented.
2. **Metrics polling loop** — the daemon. Poll all active VMs, store samples, detect basic threshold violations.
3. **Plan enforcement** — compare metrics against plan limits, trigger throttle on sustained violations.
4. **Health monitoring** — detect crashed VMs, unresponsive agents, disk full.
5. **Behavioral baselines** — learn per-VM normals, flag anomalies.
6. **GC absorption** — take over subscription expiry → suspend → destroy from engines.
7. **Log consolidation** — structured pipeline.
8. **Log anomaly model** — exploratory, after everything else is solid.
