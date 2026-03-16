# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## SETTINGS.md (HIGHEST PRIORITY)

**Read and internalize `SETTINGS.md` at the start of every session.** It defines persona, preferences, and behavioral overrides. It takes precedence over all other instructions in this file.

## Plan Mode (PERSISTENT RULE)

**Every plan must begin by reading `SETTINGS.md`.** When entering plan mode, the first action before any exploration or planning is to read and internalize `SETTINGS.md`. Context clears between plan mode and implementation — the persona and preferences do not survive unless explicitly reloaded.

## Interface Contracts (REFERENCE)

**Contract specs live in the `facts/` submodule (blockhost-facts repo).** Read and internalize the relevant contract before modifying any code that touches a boundary.

| Contract | Covers | Read when touching... |
|----------|--------|----------------------|
| `facts/PROVISIONER_INTERFACE.md` | CLI commands, manifest schema, metrics, throttle | Anything that calls provisioner commands |
| `facts/COMMON_INTERFACE.md` | Config API, VM database, root agent protocol | Any import from `blockhost.*`, config files |
| `facts/ENGINE_INTERFACE.md` | Engine CLIs, monitor, fund manager | Anything that reads engine config or chain state |

## Interface Integrity (PERSISTENT RULE)

**When interfaces don't match, fix the interface — never wrap the mismatch.** If the provisioner's metrics output doesn't match what you expect, the contract needs updating — don't add parsing hacks.

## Project Overview

blockhost-monitor is the host's resource enforcement and health monitoring daemon. It runs continuously on the BlockHost host, watching all VMs and enforcing plan-defined resource limits.

### Responsibilities

1. **Metrics collection** — Poll VM resource usage (CPU, memory, disk, network, IOPS) via provisioner CLI
2. **Limit enforcement** — Compare metrics against plan-defined resource envelopes, throttle or suspend VMs that exceed limits
3. **Abuse detection** — Identify behavioral patterns indicating abuse (cryptomining, port scanning, DDoS, ransomware I/O patterns) without inspecting packet contents or violating VM privacy
4. **Health monitoring** — Detect crashed VMs, unresponsive guest agents, disk full, OOM conditions
5. **Noisy neighbor detection** — Identify VMs degrading host performance for others, even within their own limits
6. **Logging consolidation** — Structured, queryable log pipeline for all host events

### Architecture Principles

- **Provisioner-agnostic**: Uses the provisioner CLI interface (metrics, throttle commands), never calls hypervisor APIs directly
- **Cheap polling**: Every cycle multiplies by VM count. Performance is non-negotiable.
- **Graduated response**: log → warn → throttle → suspend → destroy. Never jump to the nuclear option.
- **Behavioral detection, not content inspection**: Watch patterns (connection counts, CPU profiles, I/O shapes), never packet contents. Privacy is a hard line.
- **Plan-driven limits**: Resource envelopes are defined per plan, managed through the admin panel. The monitor reads config, it doesn't define policy.

### Key Interfaces

- **Provisioner metrics**: `blockhost-vm-metrics <vm-name>` → standardized JSON (CPU %, memory %, disk, network, connections)
- **Provisioner throttle**: `blockhost-vm-throttle <vm-name> --cpu <shares> --bandwidth <kbps>`
- **VM database**: reads `vms.json` for active VMs and their plan assignments
- **Plan config**: reads plan resource profiles (local config mapping plan IDs to resource envelopes)
- **Root agent**: for privileged operations (iptables, cgroup adjustments)

## Rules

- **Documentation sync**: After completing any code change, check whether `README.md` or `CLAUDE.md` need updating to reflect the change.
