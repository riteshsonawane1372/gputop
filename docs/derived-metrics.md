---
title: Derived metrics
---

# Derived metrics

Derived metrics are **computed by gputop** from vendor readings. They are
tagged with source `derived`, and each is documented with its inputs, formula,
assumptions and limitations. When an input is unavailable, the derived value is
unavailable; gputop never fills gaps with guesses.

All windows use the fast-tier samples kept by the derive tracker (up to 1024
per GPU).

## VRAM utilization and headroom

- **Inputs:** `memory_used_bytes`, `memory_total_bytes` (NVML).
- **Formula:** `vram_used_fraction = used / total`;
  `vram_headroom_bytes = total − used`.
- **Limitations:** NVML "used" includes driver and framework allocations
  (caching allocators such as PyTorch's reserve memory they are not actively
  using). High VRAM usage does not by itself mean memory pressure.

## Power fraction

- **Formula:** `power_w / power_limit_w` (enforced limit).

## State

- **Inputs:** `util_percent`, availability.
- **Rules:** `unavailable` when not readable; `unknown` when utilization is not
  supported; `busy` when utilization ≥ 60%; `active` when ≥
  `gpu.idle_threshold` (default 5%); otherwise `idle`.
- **Throttled:** any of `sw_power_cap`, `hw_slowdown`, `sw_thermal`,
  `hw_thermal`, `hw_power_brake`, `board_limit`, `reliability` or `sync_boost`
  is active.
- **Limitations:** NVML utilization is a time-slice measure. A GPU executing a
  tiny kernel continuously reports 100%.

## Allocation and idle-but-allocated GPUs

- **Inputs:** process count (NVML), utilization.
- **Allocated:** the GPU has at least one process with a context on it.
- **idle_for:** time since utilization was last at or above the idle threshold.
- **idle_allocated:** allocated, currently idle, and `idle_for ≥ gpu.idle_after`
  (default 5 minutes).
- **Limitations:** allocation is inferred from processes. A GPU assigned to a
  Kubernetes pod whose process has not created a CUDA context yet is not counted
  as allocated. The kubelet pod-resources API would close this gap and is
  planned.

## Unused allocated capacity (GPU-equivalents)

- **Inputs:** 5-minute average utilization of allocated, available GPUs.
- **Formula:** `Σ_allocated (1 − avg_util / 100)`.
- **Interpretation:** "2.4 GPU-eq unused" means allocated GPUs left the
  equivalent of 2.4 whole GPUs idle over the window.
- **Limitations:** inherits the time-slice nature of utilization. It is an
  upper bound on waste (a GPU at 100% time-slice utilization may still be
  compute-bound by memory bandwidth or small kernels).

## Utilization imbalance and stragglers

Designed for distributed workloads, where one slow GPU slows every rank.

- **Cohort:** the largest set of GPUs whose processes share a workload key:
  Kubernetes workload, else pod, else container, else process name + user. If
  no workload spans at least two GPUs, the cohort is all active GPUs (when there
  are at least three).
- **Inputs:** the 5-minute average utilization of cohort members.
- **Statistics:** median, min, max, spread (`max − min`), standard deviation,
  slowest and fastest GPU.
- **Low outlier:** `median − util ≥ max(gpu.outlier_min_delta, 3 × 1.4826 × MAD)`
  where MAD is the median absolute deviation. Only evaluated when the cohort
  median is at least 30% and the cohort has at least three members.
- **Why MAD:** it is robust; a single straggler does not inflate the threshold
  that detects it.
- **Limitations:** utilization is a proxy. A GPU can show equal utilization
  while being slower (for example throttled, or waiting in collectives that
  still count as kernel time). gputop does not have NCCL-level or step-time
  metrics; the imbalance view points at candidates. Check PCIe width, throttle
  reasons and NVLink errors (all shown for the GPU) to find the cause.

## Efficiency score

A **utilization-based efficiency indicator**, deliberately not "GPU utilization
renamed".

- **Computed when:** the GPU is available, allocated, supports utilization, and
  at least one minute of samples exists within the 5-minute window.
- **Inputs (window averages):**
  - `C` compute utilization (`util_percent`)
  - `B` memory bandwidth utilization (`memory_bandwidth_util_percent`)
  - `V` VRAM used percentage
  - `T` throttled fraction (share of samples with a performance throttle reason)
- **Formula:**

  ```text
  raw   = (0.60·C + 0.25·B + 0.15·V) × (1 − 0.5·T)
  score = round(raw / 5) × 5            # 5-point steps
  ```

  If memory bandwidth is unsupported, its weight moves to compute (60%) and VRAM
  (40%). If VRAM is unsupported, its weight moves to compute.
- **Grades:** ≥80 high, 50–79 moderate, 20–49 low, <20 very low.
- **Assumptions:** busy compute and memory paths indicate productive use;
  throttling wastes part of that activity.
- **Limitations:**
  - It does **not** measure throughput (samples/s, tokens/s). A GPU can score
    "high" while running inefficient code.
  - Weights are heuristic and workload-dependent (inference often has low
    bandwidth utilization by design).
  - VRAM usage includes framework caching.
  - Rounded to 5-point steps to avoid false precision.
  - When application throughput telemetry becomes available, it should replace
    the compute term; that is on the roadmap.

## NVLink rates

- **Inputs:** cumulative NVLink data counters (KiB).
- **Formula:** `Δbytes / Δt` between counter refreshes (normal tier), carried
  forward on fast ticks in between. Counter decreases (resets) produce
  unavailable rates, not negative values.

## Fleet summary

Counts (busy, active, idle, unavailable, allocated, idle-allocated, throttled),
sums (power, power limit, VRAM), averages (utilization, temperature, health) and
maxima (temperature) over available GPUs. The health average counts unavailable
GPUs as 0.
