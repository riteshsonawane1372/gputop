---
title: Architecture
---

# Architecture

gputop is organized as a one-way pipeline:

```text
Providers ──► Collectors ──► Publisher ──► Snapshot ──► TUI / JSON / API / Prometheus
                                 │
                                 ├─► derive (state, efficiency, imbalance, health)
                                 ├─► events + alerts
                                 └─► history store
```

Each stage has one owner. Collectors never render. The history store never
collects. The UI never calls a provider.

## Vendor-neutral core (`internal/gpu`)

`gpu.Provider` is the only interface a vendor backend implements:

```go
type Provider interface {
    Name() string
    Vendor() Vendor
    Open(ctx) (Diagnostics, error)
    Close() error
    System(ctx) (SystemInfo, error)
    Devices(ctx) ([]Device, error)
    Sample(ctx, id) (Sample, error)
    Processes(ctx, id) ([]Process, error)
    Health(ctx, id) (HealthCounters, error)
    Links(ctx, id) ([]Link, error)
    Partitions(ctx, id) ([]Partition, error)
}
```

Optional interfaces: `gpu.EventSource` (event-driven notifications such as
Xids) and `gpu.TopologyProvider` (PCIe relationships).

Design rules:

- **Stable identity.** Devices are keyed by vendor UUID (`gpu.ID`), never by
  enumeration index.
- **Optional values.** Every reading is a `metric.Opt[T]`. Unsupported,
  denied or failed readings are unavailable, not zero.
- **Neutral concepts.** NVIDIA clock event reasons map to `gpu.ThrottleReasons`,
  MIG instances to `gpu.Partition`, NVLink to `gpu.Link`. An AMD or Intel
  provider maps its own concepts onto the same types.
- **Classified errors.** Providers wrap `gpu.ErrNotSupported`,
  `ErrNoPermission`, `ErrDeviceLost`, `ErrNotFound` and `ErrUnavailable`, so the
  collector can tell "feature absent" from "something broke".
- **Diagnostics.** `Open` reports every check it performed; the UI shows them
  when no GPU is found.

Adding a vendor means creating `internal/gpu/<vendor>`, implementing `Provider`,
and registering it in `internal/app`. No other package changes.

## NVIDIA provider (`internal/gpu/nvidia`)

- `nvml/` is a minimal binding to `libnvidia-ml.so.1`. It uses
  [purego](https://github.com/ebitengine/purego) to `dlopen` the library at
  runtime, so gputop is built with `CGO_ENABLED=0` for every target and runs
  (with diagnostics) where no driver exists. Symbols missing from older drivers
  return `ERROR_FUNCTION_NOT_FOUND` instead of failing to load.
- Struct layouts and constants come from `nvml.h` (as vendored by NVIDIA's
  go-nvml) and are asserted by `layout_test.go`. `scripts/test-fake-nvml.sh`
  runs the Linux binary against a C implementation of the signatures on amd64
  and arm64.
- `nvml.API` is a Go interface; provider tests use in-memory fakes built on
  `nvml.Unsupported`.
- The provider remembers per-device capabilities. A call that returns
  `NOT_SUPPORTED` or `NO_PERMISSION` is not repeated on every refresh.
- Expensive calls are avoided on the fast path. PCIe throughput uses the
  cumulative `NVML_FI_DEV_PCIE_COUNT_{TX,RX}_BYTES` counters where available;
  the ~20 ms sampling `nvmlDeviceGetPcieThroughput` fallback runs at most every
  5 seconds.
- Xid, ECC and MIG configuration events are delivered via NVML event sets
  (`nvmlEventSetWait_v2`), not polling. Xid descriptions and severity come from
  NVIDIA's published Xid catalog (`xid_catalog.go`, generated).

## Apple silicon provider (`internal/gpu/apple`)

- Selected by `gpu.providers: [auto]` on macOS (NVIDIA elsewhere), or
  explicitly with `apple`.
- Bindings to CoreFoundation, IOKit and `libIOReport.dylib` are loaded with
  purego (`cf_darwin.go`), keeping the macOS build cgo-free. Nothing needs root.
- Sources: the `IOAccelerator` entry's `PerformanceStatistics` (device
  utilization, GPU memory in use and allocated); an IOReport subscription to
  the "Energy Model" and "GPU Stats / GPU Performance States" channels (GPU
  energy per sample interval → power; P-state residency weighted by the `pmgr`
  DVFS table → average active frequency); the SMC `Tg??` float keys averaged
  (GPU die temperature, with the HID "GPU MTR Temp Sensor" services as a
  fallback on older SoCs); and the `AGXDeviceUserClient` children's
  `AppUsage.accumulatedGPUTime` summed per PID (per-process GPU time share).
- Hardware access sits behind a small `backend` interface; provider tests use a
  fake, and `iokit_darwin_test.go` reads the real GPU when one is present.
- Energy is accumulated from provider start. ECC, PCIe, NVLink, MIG, power
  limits and throttle reasons are reported as unsupported; the TUI hides or
  replaces the views that depend on them.

## Collector engine (`internal/collector`)

| Tier | Default | Collectors |
|---|---|---|
| fast | 1s | `gpu` (samples), `host` (CPU, memory) |
| normal | 3s | `processes`, `health`, `links`, `network`, `disks` |
| slow | 30s | `partitions` (MIG), `filesystems`, `kubernetes` |
| inventory | 5m | `inventory` (discovery, static info, topology) |
| event | — | provider event sources |

Each collector:

- runs in its own goroutine loop with its own ticker;
- is bounded by `refresh.timeout`;
- has an in-flight flag, so a slow run is never started twice (reported as an
  overrun);
- records runs, errors, durations and health, shown in the Health tab and
  exported as Prometheus metrics;
- recovers from panics and reports them as errors.

Per-device work runs on a bounded worker pool (4 by default). In-flight state is
tracked per (collector, device): a hung call on one GPU does not delay the
others, and the pass returns at its deadline. A GPU with no fresh sample for
`max(3×interval, 2×timeout)` is marked unavailable ("not responding").

A GPU reported as lost (`ERROR_GPU_IS_LOST`) becomes unavailable, generates an
event, and scores 0 health. Other GPUs keep updating. A device that disappears
from enumeration triggers an inventory refresh.

## Publisher and snapshots

On every fast tick the publisher:

1. copies collector state into a new `model.Snapshot` (deep-copying slices);
2. runs `derive.Tracker.Apply`: state, VRAM headroom, idle tracking, efficiency,
   NVLink rates, health score and fleet summary;
3. runs `events.Detector.Diff` against the previous snapshot and adds provider
   events; computes active alerts;
4. feeds the history store;
5. atomically stores the snapshot and notifies subscribers.

Published snapshots are immutable. Subscribers receive the latest snapshot on a
buffered channel of size one; slow consumers skip intermediate snapshots but
never block the engine.

## History (`internal/history`)

- In memory: one timestamp ring plus one fixed-width `float32` column block per
  series (GPU UUID or `host`), sized `retention / resolution + 1`. A series is
  dropped once all of its slots have rolled off.
- Samples are averaged per resolution bucket; throttle masks are OR-ed.
- On disk: append-only segment files (`seg-<unix_ms>.gth`) of CRC32C-framed
  records (frame, series metadata, event). Segments rotate every
  `clamp(retention/10, 1m, 1h)` and are deleted by retention and disk cap.
  Frames are flushed on commit and `fsync`ed at most every 30 seconds.
- Replay stops at the first damaged record of a segment and keeps everything
  before it. A `LOCK` file (`flock`) prevents two writers; the second process
  runs in memory-only mode.
- `history.Reader` is implemented by the store and by the remote client, so the
  History tab works the same for remote agents.

## TUI (`internal/tui`)

- Bubble Tea model with a registry of tabs. Each tab defines `visible`, `view`,
  key handling and footer hints.
- Every view returns lines of an exact width (`widgets.Fit`, ANSI-aware
  truncation), so resizing never produces horizontal overflow. Render tests
  assert dimensions for every tab at 80×24 through 220×60.
- Tables drop low-priority columns and shrink flexible columns on narrow
  terminals.
- A small in-memory "live" buffer (600 samples) backs short-term charts, so
  charts work with history disabled and for remote sources.
- All keys go through `internal/keymap` actions; all colors through
  `internal/theme` roles.

## Service and remote

- `internal/server` serves JSON and Prometheus from the same snapshots. See
  [remote.md](remote.md) for the security model.
- `internal/remote.Client` polls an agent and implements the same `Source`
  interface as the local engine; the TUI cannot tell the difference.

## Concurrency and cancellation

- A single root context (SIGINT/SIGTERM) cancels collectors, event watchers,
  servers and remote polling.
- Shared collector state is protected by one mutex held only for copies; no
  provider calls are made under it.
- The whole test suite runs with `-race` in CI.
