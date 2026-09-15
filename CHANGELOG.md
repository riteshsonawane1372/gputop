# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-09-15

### Added

- k9s-style Kubernetes tab: GPU pods list with READY, STATUS, RESTARTS, GPUs,
  GPU requests, SM%, VRAM, workload and age; a describe view (containers,
  requests/limits, conditions, labels, events, live GPU usage, processes) and a
  container logs view (node CRI log files or the API; autoscroll, wrap,
  container switching, search). Clickable breadcrumbs. The snapshot JSON gains
  `kubernetes.pods`. RBAC for logs (`pods/log`) and events is optional.
- Command bar (`:`) with k9s-style aliases, `:ns NAME`, pod lookup and tab
  completion.
- Dashboard tab modelled on the NVIDIA DCGM Grafana dashboard: stat tiles,
  multi-GPU time-series panels in Grafana's palette with DCGM field names,
  a reliability table, time ranges from the history store, panel zoom and
  GPU isolation (`[`/`]` or click the legend). `D` opens it.
- `--demo` simulates GPU pods, logs and events.

### Changed

- `l` opens pod logs; the unused `right` action has no default key.

## [0.1.0] - 2026-09-15

### Added

- Apple silicon provider (macOS, M1 and later), selected automatically by
  `gpu.providers: [auto]`: device utilization and GPU memory in use from IOKit,
  GPU power, energy and average active frequency from IOReport, GPU die
  temperature from the SMC, and per-process GPU time from Metal user clients.
  No root and no cgo. The UI adapts to Apple GPUs: unified-memory labels,
  Apple-specific Memory, Power and Thermals tables, and no PCIe/NVLink/MIG tabs.
- Process metadata (name, user, command line, start time) on macOS.
- Mouse support: click tabs, table rows, column headers and footer hints;
  double-click rows for details; wheel scrolling (`ui.mouse`, on by default).
- The GPU detail pane in the GPUs tab scrolls (`PgUp`/`PgDn`, mouse wheel) and
  shows a scrollbar.
- Semantic prereleases (`vX.Y.Z-main.N`) with binaries for every platform on
  each push to `main`; stable releases from tags or a manual workflow run.
  Releases attach ready-to-run executables (`gputop-<os>-<arch>`) instead of
  archives.

### Changed

- `←` / `→` switch to the previous / next tab. GPU selection keeps `[` / `]`
  (and `↑` / `↓`); process sorting keeps `s` / `S`.
- `gpu.providers: [auto]` selects the Apple provider on macOS instead of NVIDIA.
- The rolling `main-build` prerelease is replaced by semantic prereleases.

## [0.0.1] - 2026-09-15

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

[Unreleased]: https://github.com/riteshsonawane1372/gputop/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/riteshsonawane1372/gputop/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/riteshsonawane1372/gputop/compare/v0.0.1...v0.1.0
[0.0.1]: https://github.com/riteshsonawane1372/gputop/releases/tag/v0.0.1
