# JSON output

```bash
gputop --once --json     # one indented snapshot
gputop --json            # one compact snapshot per line (NDJSON), every refresh
gputop --remote node --once --json
```

The same document is served by `GET /api/v1/snapshot` in service mode (with
process command lines removed).

## Stability

- `schema` identifies the format: currently `gputop.snapshot/v1`.
- Within a schema version, fields are only added, never renamed or removed, and
  their meaning does not change. Breaking changes bump the version.
- **Unavailable values are `null`.** A reading that is not supported, not
  permitted or failed is never `0`.
- Units are in field names: `_bytes`, `_bps` (bytes/s), `_w`, `_j`, `_c`,
  `_mhz`, `_percent` (0–100), `_fraction` (0–1), `_ns` (durations in
  nanoseconds). Timestamps are RFC 3339.
- Values from gputop's own calculations live under `derived`, `health` and
  `fleet`, and are documented in [derived-metrics.md](derived-metrics.md) and
  [health-score.md](health-score.md).

## Top level

| Field | Type | Description |
|---|---|---|
| `schema` | string | `gputop.snapshot/v1` |
| `seq` | int | Snapshot sequence number (per process) |
| `time` | time | Snapshot time |
| `ready` | bool | `false` until the first device inventory completed |
| `node` | object | `hostname`, `source` (`local`/`remote`), `remote`, `demo`, `gputop_version` |
| `providers` | array | `name`, `vendor`, `available`, `error`, `system` (driver/library/runtime versions), `diagnostics` (checks and hints) |
| `gpus` | array | Per-GPU objects (below) |
| `processes` | array | GPU processes (below) |
| `topology` | array | Pairs `a`, `b` (GPU UUIDs), `pcie` level (`internal`, `pix`, `pxb`, `phb`, `node`, `sys`, `unknown`), `nvlinks` count |
| `host` | object | `info`, `cpu`, `memory`, `filesystems`, `block_devices`, `network` |
| `kubernetes` | object | Detected `environment`, API status, pods known, and `pods`: GPU pods with `uid`, `name`, `namespace`, `node`, `pod_ip`, `phase`, `qos`, `created`, `labels`, `workload_kind`/`workload_name`, `gpu_requests`, `containers` (image, state, ready, restarts, last state, requests, limits), `conditions` and `source` (`api` or `node`) |
| `fleet` | object | Fleet summary and `imbalance` |
| `events` | array | Up to 200 most recent events, oldest first |
| `alerts` | array | Active alerts |
| `collectors` | array | Collector self-observability |
| `history` | object | History store status |
| `self` | object | gputop's CPU, memory, goroutines, last collection time |

## `gpus[]`

| Field | Description |
|---|---|
| `device` | Static inventory: `id` (UUID, the stable key), `vendor`, `index` (display only), `name`, `architecture`, `compute_capability`, `serial`, `firmware_version`, `pci`, `numa_node`, limits and thresholds, `mig`, `link_count`, `capabilities` (map of capability → `supported`/`unsupported`/`no_permission`/`unknown`) |
| `provider` | Provider name (`nvml`, `simulated`) |
| `available` / `error` | Whether the GPU could be read and why not |
| `sample` | Fast-tier readings (see [metrics.md](metrics.md)); `throttle_reasons` is a list of names |
| `health_counters` | ECC, row remapping, retired pages, PCIe errors, violation times, recovery action |
| `links` | NVLink links with state, peer, byte counters and rates, error counters |
| `partitions` | MIG instances |
| `health` | `score` (0–100), `band`, `reasons` (`penalty`, `severity`, `code`, `text`), `source: "derived"` |
| `derived` | `state`, `throttled`, `vram_used_fraction`, `vram_headroom_bytes`, `power_fraction_of_limit`, `allocated`, `idle_for_ns`, `idle_allocated`, `util_avg_window`, `outlier`, `outlier_delta_pp`, `efficiency`, `links_active`, `nvlink_tx_bps`, `nvlink_rx_bps` |
| `process_count` | Number of processes on the GPU |

## `processes[]`

| Field | Description |
|---|---|
| `pid` | Host PID |
| `device_id`, `device_index` | GPU |
| `partition_id`, `partition_index` | MIG instance (`partition_index` is `-1` without MIG) |
| `type` | `compute` or `graphics` |
| `memory_used_bytes`, `sm_util_percent`, `mem_util_percent`, `enc_util_percent`, `dec_util_percent` | NVML per-process values |
| `name`, `user`, `command`, `start_time` | OS metadata (`command` omitted by the HTTP API) |
| `visible`, `reason` | Whether OS metadata could be resolved, and why not |
| `kubernetes` | `container_id`, `runtime`, `pod_uid`, `qos`, `pod`, `namespace`, `container`, `workload_kind`, `workload_name`, `workload_inferred` |

## `fleet`

`gpus`, `available`, `unavailable`, `allocated`, `active`, `busy`, `idle`,
`idle_allocated`, `throttled`, `processes`, `util_avg_percent`, `power_w`,
`power_limit_w`, `temp_avg_c`, `temp_max_c`, `vram_used_bytes`,
`vram_total_bytes`, `vram_used_fraction`, `health_avg`, `health_min`,
`unused_allocated_gpu_equivalents`, and `imbalance` (`valid`, `cohort`,
`members`, `median_pct`, `min_pct`, `max_pct`, `spread_pp`, `stddev_pp`,
`slowest`, `fastest`, `outliers`).

## `events[]` and `alerts[]`

Events: `time`, `kind`, `severity` (`info`/`warning`/`critical`), `device_id`,
`device_index` (`-1` when not tied to a GPU), `message`, `source`, `attrs`.

Alerts: `key` (stable identity), `severity`, `device_id`, `device_index`,
`title`, `detail`, `since`.

## Examples

```bash
# GPUs that are allocated but idle
gputop --once --json | jq -r '.gpus[] | select(.derived.idle_allocated) | .device.id'

# Processes using more than 10 GiB of VRAM, with their pods
gputop --once --json | jq '.processes[] | select(.memory_used_bytes > 10737418240) | {pid, name, pod: .kubernetes.pod}'

# Stream the average fleet utilization
gputop --json | jq --unbuffered '.fleet.util_avg_percent'
```
