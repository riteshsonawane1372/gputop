# gputop

**A fast, keyboard-first terminal monitor for GPU infrastructure.**

gputop is to GPUs what btop is to hosts: a dense, low-overhead TUI that answers
*"what is happening with my GPUs right now?"* and, with its built-in time
machine, *"what happened 15 minutes ago?"*

It currently supports **NVIDIA GPUs** through NVML, on a vendor-neutral core
designed for AMD, Intel and other accelerators.

```text
 ◆ gputop ▸ gpu-node-01             DEMO · SIMULATED          driver simulated · CUDA 12.6 · 8× H100 80GB HBM        ⟳ 1s  11:42:26
 1 Overview  2 GPUs  3 Processes  4 Memory  5 Power  6 Thermals  7 NVLink  8 PCIe  9 MIG  Nodes  Kubernetes  Workloads  Network  ›
╭─ Fleet ─────────────────────────────────────────────────── simulated ─╮╭─ Live ──────────────────────────────────────────── 10m ─╮
│GPUs       8   ● 5 busy  ◐ 2 active  ○ 1 idle                          ││avg utilization 71%                                      │
│Allocated  8/8                                                         ││⣀⢀⢀⣀ ⣀⣀⢀⡀⡀⣀⣀⣀⡀ ⣀   ⢀⡀   ⢀⡀   ⣀⣀⣀⢀   ⣀ ⣀ ⣀⣀⢀⢀⣀⡀⣀⡀⢀⣀⣀⢀   ⣀⣀│
│Health     96 avg · min 75 (GPU 3)  ▰▰▰▰▰▰▰▰▰▰                         ││⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣾⣿⣿⣷⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡄ ⢠⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣶⣿⣿│
│Power      ███████████████▍░░░░░░ 3926 W / 5600 W 70%                  ││⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣇⣀⣸⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿│
│VRAM       ███████████████▍░░░░░░ 447 GiB / 640 GiB 70%                ││total power 3926 W                                       │
│Temp       61°C avg · max 72°C                                         ││⣶⣴⣼⣿⣤⣿⣿⣼⣧⣧⣿⣿⣷⣧⣤⣶⣶⣰⣶⣶⣆⣶⣶⣆⣰⣶⣶⣶⣶⣶⣶⣶⣴   ⣿⣤⣿⣤⣿⣿⣶⣼⣿⣧⣿⣦⣴⣶⣶⣴⣤⣰⣀⣶⣶│
│Balance    ⚠ outlier GPU 3 -39pp · spread 40pp across 6 GPUs           ││⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡆ ⢸⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿│
│Capacity   2.5 GPU-eq unused of allocated (5m)                         ││⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡇ ⢸⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿│
│Host       CPU N/A · RAM 79% · net ↓11.8 KiB/s ↑73.8 KiB/s             ││⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿│
╰───────────────────────────────────────────────────────────────────────╯╰─────────────────────────────────────────────────────────╯
╭─ GPUs ────────────────────────────────────────────────────────────────────────────────────────────────────────────────── 8 GPUs ─╮
│ # NAME                            UTIL               VRAM                    TEMP      POWER HEALTH STATE    UTIL 60s            │
│ 0 H100 80GB HBM                   ████████████▋  97% ██████████▊░░ 66.2/80G  72°C   641/700W    100 ● BUSY   ███▅███▅████████████│
│ 1 H100 80GB HBM                   ████████████▊  98% ██████████▊░░ 66.0/80G  70°C   653/700W    100 ● BUSY   ███▅███▅████████████│
│ 2 H100 80GB HBM                   ████████████▌  96% ██████████▊░░ 66.0/80G  71°C   638/700W     90 ● BUSY   ███▅███▅████████████│
│ 3 ⚠ H100 80GB HBM                 ███████▏░░░░░  55% ██████████▊░░ 66.4/80G  54°C   396/700W     75 ◐ ACTIVE ▅▅▅▅▅▅▅▄▅▅▅▅▅▅▅▅▅▅▅▅│
│ 4 H100 80GB HBM                   ████████████▋  97% ██████████▉░░ 66.6/80G  72°C   637/700W    100 ● BUSY   ███▅███▅████████████│
│ 5 H100 80GB HBM                   ████████████▋  97% ██████████▉░░ 66.6/80G  71°C   648/700W    100 ● BUSY   ███▅███▅████████████│
│ 6 H100 80GB HBM                   ███▌░░░░░░░░░  27% █████▍░░░░░░░ 33.1/80G  45°C   233/700W    100 ◐ ACTIVE ▃▄▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃│
│ 7 H100 80GB HBM                   ▏░░░░░░░░░░░░   1% ██▋░░░░░░░░░░ 16.4/80G  33°C    80/700W    100 ○ IDLE   ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁│
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ Top processes ────────────────────────────────────── by VRAM ─╮╭─ Alerts & events ─────────────────────────────────── 4 active ─╮
│    PID PROCESS        GPU     VRAM▼  SM% POD                   ││▲ GPU 2 Xid 13 in the last 10 minutes · 5s                      │
│ 210400 python           0  65.6 GiB   98 llama-70b-pretrain-wo…││▲ GPU 3 PCIe link width x8 below maximum x16 · 7s               │
│ 210401 python           1  65.6 GiB   97 llama-70b-pretrain-wo…││▲ GPU 3 23 PCIe replays in the recent window · 4s               │
│ 210402 python           2  65.6 GiB   98 llama-70b-pretrain-wo…││▲ GPU 3 Error counters increasing on 1 link(s) · 7s             │
│ 210403 python           3  65.6 GiB   53 llama-70b-pretrain-wo…││── recent events ───────────────────────────────────────────────│
│ 210404 python           4  65.6 GiB   98 llama-70b-pretrain-wo…││11:42:22 • GPU 1  1 new corrected ECC error(s)                  │
│ 210405 python           5  65.6 GiB   96 llama-70b-pretrain-wo…││11:42:20 ▲ GPU 2  Xid 13: Graphics Engine Exception (simulated) │
│ 188100 vllm           6:0  24.2 GiB   19 chat-api-7d9f8b6c5-x2…││11:42:19 ▲ GPU 3  Link 7 error counters increased by 1          │
│  99321 python           7  16.0 GiB    0 notebook-alice-0      ││11:42:18 ▲ GPU 3  PCIe link at x8 (max x16)                     │
│ 188240 tritonserver   6:1  11.2 GiB   13 embed-svc-6c7d9f5b8-p…││11:42:18 •       Discovered 8× NVIDIA H100 80GB HBM (simulated) │
│                                                                ││                                                                │
╰────────────────────────────────────────────────────────────────╯╰────────────────────────────────────────────────────────────────╯
 ↑ select  ⏎ GPU detail  h history  p pause  ? help  q quit                                                           ▲ 4  hist 30m
```

<sub>Rendered from `gputop --demo` (simulated GPUs) at 132×38 with colors removed. GPU 3 is the
demo's straggler: its PCIe link trained at x8.</sub>

---

## Contents

- [Why gputop](#why-gputop)
- [Status](#status)
- [Installation](#installation)
- [Requirements](#requirements)
- [Usage](#usage)
- [Keyboard](#keyboard)
- [Tabs](#tabs)
- [Configuration](#configuration)
- [Themes](#themes)
- [History (time machine)](#history-time-machine)
- [Health, events and derived metrics](#health-events-and-derived-metrics)
- [Service mode, Prometheus and remote monitoring](#service-mode-prometheus-and-remote-monitoring)
- [Kubernetes](#kubernetes)
- [Machine-readable output](#machine-readable-output)
- [Architecture](#architecture)
- [Performance](#performance)
- [Development](#development)
- [Roadmap](#roadmap)
- [License](#license)

## Why gputop

| Question | Where gputop answers it |
|---|---|
| Which GPU is busy? | **Overview**: per-GPU utilization, VRAM, power, temperature, health, state |
| Which process is consuming VRAM? | **Processes**: sortable, searchable, with container, pod and workload |
| Why is GPU 4 slow? | **GPUs** detail + **History** + **Health** (throttling, PCIe width, Xids, outliers) |
| Is this GPU throttling? | **Power** / **Thermals**: live clock event reasons and thresholds |
| Is NVLink healthy? | **NVLink**: per-link state, throughput, error counters, topology |
| What happened 15 minutes ago? | **History**: scrub any metric back in time, with events on the timeline |
| Which workload owns this GPU? | GPU → process → container → pod → workload correlation |
| Are we wasting GPUs? | **Workloads**: idle-but-allocated GPUs, stragglers, efficiency |

Design principles: no metric without a reason; unsupported values are shown as
`N/A`, never as `0`; official vendor metrics are always distinguishable from
gputop-derived ones; collection never slows down the UI.

## Status

gputop is pre-1.0. Features are marked as:

- ✅ **Implemented**: built, tested, documented.
- 🧪 **Experimental**: implemented; interfaces or scoring may change.
- 🗺 **Planned**: designed for, not built yet.

| Area | Status |
|---|---|
| NVIDIA via NVML: utilization, memory, power, energy, temperatures, clocks, throttle reasons, PCIe, NVLink, MIG, ECC, row remapping, retired pages, Xid events, processes | ✅ |
| Interactive TUI: 16 tabs, sorting, search, filters, detail views, resizing (80×24 and up) | ✅ |
| Custom themes and key bindings | ✅ |
| History: 30-minute default, configurable retention, on-disk persistence, charts, time scrubbing | ✅ |
| Host CPU, RAM, disks, network and InfiniBand counters | ✅ |
| Events timeline and active alerts | ✅ |
| Health score (0–100) with itemized reasons | 🧪 |
| Derived metrics: idle-allocated GPUs, unused capacity, straggler detection, efficiency score | 🧪 |
| Process → container → pod → workload correlation (cgroups, pod logs, in-cluster API) | 🧪 |
| `--once` and `--json` output with a versioned schema | ✅ |
| Service mode: HTTP API, Prometheus `/metrics`, TLS, token auth, mTLS | 🧪 |
| Remote TUI (`--remote`) and remote node summaries | 🧪 |
| `--demo` simulated GPUs | ✅ |
| NVIDIA DCGM (per-MIG utilization, profiling metrics) | 🗺 |
| AMD, Intel and Apple Silicon providers | 🗺 |
| Kubelet pod-resources API (allocation without running processes) | 🗺 |
| deb/rpm/Homebrew packages, container image | 🗺 |

> **Hardware validation.** The NVML binding is verified by unit tests against
> NVIDIA's cgo-generated struct layouts, fixture-based provider tests, and an
> end-to-end ABI test that runs the Linux binary (amd64 and arm64) against a C
> library implementing the NVML function signatures. It has not yet been
> validated across real GPU generations; please share results from
> `make test-nvidia`.

## Installation

Pre-built binaries for Linux and macOS (amd64, arm64) are attached to each
[GitHub release](https://github.com/gputop/gputop/releases). Download the
archive for your platform plus `checksums.txt`, then:

```bash
sha256sum --ignore-missing -c checksums.txt
tar xzf gputop_<version>_linux_amd64.tar.gz
sudo install gputop /usr/local/bin/
```

With Go 1.25 or newer:

```bash
go install github.com/gputop/gputop/cmd/gputop@latest
```

From source:

```bash
git clone https://github.com/gputop/gputop
cd gputop
make build
./bin/gputop --demo
```

## Requirements

- **Linux (amd64/arm64)** for NVIDIA monitoring, with the NVIDIA driver
  installed (it provides `libnvidia-ml.so.1`). No CUDA toolkit, DCGM or root
  access is required. Some counters (for example per-process data on MIG
  devices) need elevated privileges; gputop shows `N/A` and continues when they
  are denied.
- **Containers:** run with the NVIDIA Container Toolkit (`--gpus all`,
  `NVIDIA_DRIVER_CAPABILITIES=utility`) and with `--pid=host` / `hostPID: true`
  so that host PIDs reported by NVML can be resolved.
- **macOS:** builds and runs (host metrics, `--demo`, `--remote`). NVIDIA no
  longer ships macOS drivers, so there is no local NVIDIA monitoring.
- The Linux binary is built without cgo and loads NVML with `dlopen`. It needs a
  glibc-compatible dynamic loader (on Alpine, install `gcompat`).
- Terminal: 256-color or truecolor recommended; minimum 60×15, full layout from
  80×24.

## Usage

```bash
gputop                        # interactive TUI
gputop --demo                 # simulated GPUs: explore without hardware
gputop --once                 # one-shot text summary (exit code 3 when no GPU is found)
gputop --once --json          # one JSON snapshot
gputop --json                 # stream JSON snapshots, one per line
gputop --service              # headless agent: collectors + history + HTTP API
gputop --remote gpu-node-01   # TUI for a remote agent
gputop --print-config         # effective configuration (defaults + file + flags)
gputop --version
```

Other flags: `--config PATH`, `--theme NAME`, `--list-themes`, `--interval 2s`,
`--retention 2h`, `--no-history`, `--listen ADDR`, `--demo-gpus N`, `--debug`,
`--log-file PATH`, `--gen-token`. Run `gputop --help` for details.

If no supported GPU is found, gputop does not crash. It shows each check it
performed (kernel driver, device nodes, NVML library, `nvmlInit`, enumeration)
with troubleshooting hints, and host monitoring keeps working.

## Keyboard

All bindings are configurable (see [Configuration](#configuration)).

| Key | Action | Key | Action |
|---|---|---|---|
| `q` / `ctrl+c` | quit | `?` | help |
| `Tab` / `Shift+Tab` | next / previous tab | `1`…`9` | jump to tab |
| `↑` `↓` / `j` `k` | move | `←` `→` | move / change sort |
| `Enter` | open detail | `Esc` | back / clear filters |
| `/` | search | `f` | filter, e.g. `gpu:0 user:alice ns:ml sev:critical kind:all` |
| `s` / `S` | next sort column / reverse | `PgUp` `PgDn` `g` `G` | page / first / last |
| `h` | history (time machine) | `r` | refresh now |
| `p` / `space` | pause display (collection continues) | `[` `]` | previous / next GPU |
| `+` / `-` | zoom history window | `m` / `M` | next / previous metric |
| `,` / `.` | scrub back / forward in time | `n` | jump to now |

## Tabs

Tabs appear only when they have something meaningful to show: **NVLink** only
with NVLink hardware, **MIG** only on MIG-capable GPUs, **Kubernetes** only when
a Kubernetes context or pod processes are detected, GPU tabs only when GPUs are
present.

| Tab | Shows |
|---|---|
| **Overview** | fleet summary (busy/active/idle, allocation, health, power, VRAM, temperatures, imbalance, unused capacity, host), live charts, GPU table with trends, top processes, alerts and recent events |
| **GPUs** | GPU list with a detail pane; `Enter` for full detail: identity, compute, memory, thermals, power, PCIe, NVLink, health reasons, processes, live trends, capability matrix |
| **Processes** | PID, process, user, GPU, MIG instance, VRAM, SM%, memory%, runtime, container, pod, namespace, workload; sort, search, filter, detail with command line |
| **Memory** | VRAM usage, free and reserved memory, memory bandwidth utilization, top consumer, ECC and row remapping |
| **Power** | draw vs. enforced limit, default/min/max limits, energy since driver load, P-state, clocks, active throttle reasons |
| **Thermals** | GPU and memory temperature against slowdown/shutdown thresholds, headroom, fan, thermal state |
| **NVLink** | per-GPU link summary; per-link state, version, peer, throughput and errors; GPU topology matrix |
| **PCIe** | bus ID, current vs. maximum generation and width (degradation flagged), throughput, replays, AER counters, NUMA node |
| **MIG** | MIG mode, instances (profile, GPU/compute instance IDs, memory) and attached processes |
| **Nodes** | host identity, CPU (per core, load, frequency), memory, filesystems, disk I/O, remote node summaries |
| **Kubernetes** | detected mode, API status, GPU → pod → workload table |
| **Workloads** | processes grouped by workload: GPUs, VRAM, utilization, efficiency, idle/straggler/throttling signals |
| **Network** | Ethernet and InfiniBand interfaces with RX/TX rates, packets, errors and drops |
| **History** | time machine: any metric for any GPU or the host, GPU comparison, value inspector and events at the cursor |
| **Events** | timeline of discoveries, Xids, ECC errors, throttling, PCIe/NVLink changes, MIG changes, process start/stop |
| **Health** | health scores with reasons, active alerts, collector status and gputop's own resource usage |

## Configuration

gputop reads `~/.config/gputop/config.yaml` (honouring `XDG_CONFIG_HOME`).
Every key is optional and unknown keys are rejected with a line number. Start
from [`examples/config.yaml`](examples/config.yaml) or `gputop --print-config`.

```yaml
refresh:
  interval: 1s        # fast tier: utilization, memory, power, temperature
history:
  enabled: true
  retention: 30m      # 2h, 24h, 7d ...
  resolution: 5s
theme:
  name: green
keys:
  quit: q
  history: h
kubernetes:
  enabled: auto
```

See [docs/configuration.md](docs/configuration.md) for every option and all
bindable actions.

## Themes

Built-in themes: `green` (default, black and green), `amber`, `ice`, `mono`,
`dusk`. Custom themes live in `~/.config/gputop/themes/<name>.yaml`, may
`extends` another theme, and can override any of 19 semantic color roles:

```yaml
name: corp
extends: ice
colors:
  primary: "#ff00ff"
  warning: "214"       # ANSI 256-color numbers work too
```

Override individual roles without a theme file via `theme.colors`, or keep your
terminal's background with `theme.transparent: true`. See
[docs/themes.md](docs/themes.md) and
[`examples/themes/solarized-dark.yaml`](examples/themes/solarized-dark.yaml).

## History (time machine)

gputop keeps the last **30 minutes** of telemetry by default:

- Samples are averaged into 5-second buckets. Throttle reasons are OR-ed, so a
  short throttle burst is never averaged away.
- Buckets live in a bounded in-memory ring and are appended to CRC-protected
  segment files under `~/.local/state/gputop/history`, so history survives
  restarts.
- Old segments are removed by retention and by a disk cap
  (`history.max_disk_mb`, default 256 MB).
- A torn write after a crash loses only the damaged tail of a segment.
- Two gputop processes never write to the same directory; the second keeps its
  history in memory and says so.

Retention is configurable (`2h`, `24h`, `7d`); memory is bounded by requiring
`retention / resolution ≤ 20000` points. In the **History** tab pick a GPU or
the host (`[` `]`), a metric (`m`) and a window (`+` `-`), then scrub (`,` `.`)
to inspect every metric and the events around that moment.

## Health, events and derived metrics

- **Metrics** are sampled values. **Events** are transitions worth remembering:
  GPU lost or recovered, Xid, ECC, throttling start/end (debounced), PCIe width
  changes, NVLink up/down/errors, MIG changes, process start/stop. **Alerts** are
  conditions that are active now.
- **Health score** (0–100, gputop-derived). Deductions for uncorrectable ECC
  errors, row remap failures and pending remaps, driver-recommended recovery
  actions, Xids (severity taken from NVIDIA's Xid catalog), hardware and thermal
  slowdown, power brake, PCIe width degradation and errors, NVLink links down or
  accumulating errors. Missing data never lowers the score, and every deducted
  point is explained. Bands: 90+ healthy, 75+ good, 50+ degraded, 25+ unhealthy,
  below 25 critical. See [docs/health-score.md](docs/health-score.md).
- **Derived metrics**: VRAM utilization and headroom; busy/active/idle state;
  idle-but-allocated GPUs; unused allocated capacity (GPU-equivalents);
  utilization imbalance and low-outlier (straggler) detection within a workload
  cohort; a transparent efficiency score. Inputs, formulas and limitations are
  in [docs/derived-metrics.md](docs/derived-metrics.md).

Every metric has a recorded provenance (`nvml`, `host`, `procfs`, `kubernetes`,
`derived`, `simulated`); see [docs/metrics.md](docs/metrics.md).

## Service mode, Prometheus and remote monitoring

```bash
gputop --gen-token      # prints a random token and its SHA-256 digest
gputop --service        # 127.0.0.1:9469 — /api/v1/{snapshot,summary,history,events}, /metrics, /healthz
```

Security defaults:

- The agent listens on loopback. Binding to any other address **requires TLS
  and token authentication**; otherwise gputop refuses to start.
- Tokens are checked in constant time against a SHA-256 digest, so the agent
  never needs the plaintext token. Mutual TLS is optional.
- The API is read-only and never serves process command lines.
- Prometheus metrics use bounded labels only (`gpu`, `uuid`). PIDs, containers
  and pods are never labels.

On your workstation:

```yaml
remote:
  nodes:
    - name: gpu-node-01
      address: gpu-node-01.example.com     # https, port 9469 by default
      token_file: ~/.config/gputop/tokens/gpu-node-01
      ca_file: ~/.config/gputop/ca.pem
```

```bash
gputop --remote gpu-node-01

# Or keep the agent on loopback and use an SSH tunnel:
ssh -N -L 9469:127.0.0.1:9469 gpu-node-01 &
gputop --remote http://127.0.0.1:9469
```

Configured nodes also appear with live summaries in the **Nodes** tab. See
[docs/remote.md](docs/remote.md); a hardened systemd unit is in
[`deploy/systemd/`](deploy/systemd/gputop.service).

## Kubernetes

Kubernetes is optional. gputop detects whether it runs inside a pod, on a node,
or with a kubeconfig, and correlates GPU processes using:

1. **cgroups** (`/proc/<pid>/cgroup`): container ID, pod UID and QoS class
   (cgroup v1/v2, cgroupfs/systemd drivers; containerd, CRI-O, Docker, Podman);
2. **pod log directories** on the node: pod name and namespace;
3. **the in-cluster API** (read-only service account): container names and the
   owning Deployment, StatefulSet, DaemonSet, Job or CronJob.

When only naming patterns are available a Deployment may be inferred; such
results are marked as inferred. A DaemonSet manifest with minimal RBAC is in
[`deploy/kubernetes/`](deploy/kubernetes/daemonset.yaml). See
[docs/kubernetes.md](docs/kubernetes.md).

## Machine-readable output

`gputop --once --json` prints one snapshot; `gputop --json` streams one snapshot
per line. The schema is versioned (`"schema": "gputop.snapshot/v1"`),
unavailable values are `null` (never `0`), and field names are stable within a
schema version. See [docs/json-schema.md](docs/json-schema.md).

```bash
gputop --once --json | jq '.gpus[] | {gpu: .device.index, util: .sample.util_percent, health: .health.score}'
```

## Architecture

```text
 providers: gpu/nvidia (NVML), gpu/sim          host · procfs · kubernetes
                 │                                       │
                 ▼                                       ▼
   collectors: fast 1s · normal 3s · slow 30s · inventory 5m · event-driven
   (independent timeouts, per-device isolation, health and self-metrics)
                 │
                 ▼
   publisher ─► derive (state, efficiency, imbalance, health score)
             ─► events + alerts ─► history store (ring + segments)
                 │
                 ▼  immutable snapshot
      ┌──────────┼──────────────┬──────────────┐
     TUI    --json / --once   HTTP API       /metrics
      ▲
      └── remote client (same Source interface) ◄── gputop --service on a GPU node
```

- The core model (`internal/gpu`) is vendor-neutral; NVIDIA-specific code lives
  only in `internal/gpu/nvidia`.
- NVML is loaded at runtime with `dlopen` (via
  [purego](https://github.com/ebitengine/purego)), so every binary is built with
  `CGO_ENABLED=0` and runs on machines without drivers.
- A hung NVML call on one GPU cannot stall the other GPUs or the UI.

More in [docs/architecture.md](docs/architecture.md).

## Performance

Measured on an Apple M4 with the simulated provider (real NVML call latency is
excluded; method and full results in [docs/performance.md](docs/performance.md)):

| | 1 GPU | 8 GPUs | 16 GPUs |
|---|---|---|---|
| Fast collection pass (gputop overhead) | 5.6 µs | 23 µs | 32 µs |
| Snapshot publish (derive, health, events, history) | 20 µs | 77 µs | 174 µs |
| History commit with disk append | 18 µs | 59 µs | 73 µs |
| Overview render at 200×60 | 1.2 ms | 2.0 ms | 2.8 ms |
| TUI process CPU over 20 s (1 s refresh) | 0.12 s | 0.15 s | 0.19 s |
| TUI process RSS | 20 MB | 22 MB | 23 MB |

## Development

```bash
make build                 # bin/gputop
make test                  # unit tests (no GPU required)
make race                  # with the race detector
make lint                  # golangci-lint
make cross                 # linux/darwin × amd64/arm64
make bench                 # benchmarks
make test-nvidia           # integration test on a real NVIDIA host (skips otherwise)
scripts/test-fake-nvml.sh  # NVML ABI test against a fake libnvidia-ml (Linux or Docker)
./bin/gputop --demo        # develop the UI without hardware
```

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Roadmap

- DCGM provider (per-MIG-instance utilization, profiling fields)
- AMD (ROCm SMI), Intel (Level Zero / xpu-smi) and Apple Silicon providers
- Kubelet pod-resources API for GPU allocation without running processes
- Kubernetes API access through kubeconfig
- Application metrics (training throughput, NCCL) when exposed by workloads
- Switching between remote nodes inside the TUI; multi-node fleet views
- Packages (deb, rpm, Homebrew) and a container image

## License

Apache License 2.0; see [LICENSE](LICENSE). NVIDIA, NVML, CUDA and NVLink are
trademarks of NVIDIA Corporation. gputop is not affiliated with NVIDIA.
