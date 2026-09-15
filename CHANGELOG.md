# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Interactive TUI with 16 tabs (Overview, GPUs, Processes, Memory, Power,
  Thermals, NVLink, PCIe, MIG, Nodes, Kubernetes, Workloads, Network, History,
  Events, Health) that hide automatically when irrelevant; sorting, search,
  `key:value` filters, detail views, pause, help and adaptive layouts from 80×24.
- NVIDIA provider using NVML through a pure-Go dlopen binding (no cgo):
  identity, utilization, memory bandwidth, encoder/decoder/JPEG/OFA, memory
  (v2 with reserved memory), GPU and memory temperatures and thresholds, fan,
  power, enforced/default limits, energy, clocks, P-state, clock event (throttle)
  reasons, PCIe link and throughput, PCIe replay and AER counters, NVLink state,
  peers, throughput and errors, MIG instances, ECC counters, row remapping,
  retired pages, recovery action, violation times, topology, processes with
  per-process utilization, and event-driven Xid/ECC/MIG events.
- Capability detection per device; unsupported calls are not retried on every
  refresh.
- Simulated provider (`--demo`) for exploring, testing and benchmarking.
- Tiered collector engine (fast/normal/slow/inventory/event) with per-collector
  timeouts, overrun protection, per-device isolation and self-metrics.
- History store: 30-minute default retention, configurable resolution and
  retention, CRC-protected on-disk segments, disk cap, corruption tolerance and
  directory locking; History tab with scrubbing, comparison and event markers.
- Events timeline and active alerts; debounced throttling events.
- gputop-derived health score with itemized reasons; derived metrics for
  VRAM headroom, idle-but-allocated GPUs, unused capacity, utilization imbalance
  and stragglers, and an efficiency indicator.
- Host metrics: CPU (per core, load, frequency), memory, filesystems, disk I/O,
  network and InfiniBand counters.
- Kubernetes awareness: environment detection, cgroup parsing, pod log
  resolution, in-cluster API workload resolution.
- `--once`, `--json` and a versioned JSON snapshot schema (`gputop.snapshot/v1`).
- Service mode with HTTP API (`/api/v1/snapshot`, `summary`, `history`,
  `events`), Prometheus metrics, TLS, hashed bearer tokens and mTLS; remote TUI
  (`--remote`) and remote node summaries.
- Configuration file with validation, custom themes (five built-in), and
  configurable key bindings.
- CI (lint, race tests on Linux amd64/arm64 and macOS, cross-compilation, NVML
  ABI test, smoke tests, GoReleaser check) and a GoReleaser release workflow.
