---
title: Metrics and provenance
---

# Metrics and provenance

Every value gputop shows has a source:

| Source | Meaning |
|---|---|
| `nvml` | Read from the NVIDIA Management Library |
| `host` | Operating system counters (via gopsutil or `/sys`) |
| `procfs` | Per-process data from `/proc` |
| `kubernetes` | Kubernetes API, pod log directories, cgroups |
| `application` | Reported by a workload, such as an inference server's Prometheus endpoint (see [inference.md](inference.md)) |
| `derived` | **Computed by gputop** from other values (documented in [derived-metrics.md](derived-metrics.md) and [health-score.md](health-score.md)) |
| `simulated` | `--demo` mode; never hardware |

A metric that cannot be read (not supported by the GPU or driver, permission
denied, error) is **unavailable**: shown as `N/A`, encoded as `null` in JSON,
and omitted from Prometheus output. It is never reported as `0`.

## GPU inventory (inventory tier)

| JSON field (`device.*`) | NVML source |
|---|---|
| `id` | `nvmlDeviceGetUUID` |
| `index` | `nvmlDeviceGetIndex` |
| `name`, `brand`, `architecture` | `nvmlDeviceGetName`, `nvmlDeviceGetBrand`, `nvmlDeviceGetArchitecture` |
| `compute_capability` | `nvmlDeviceGetCudaComputeCapability` |
| `serial`, `part_number`, `firmware_version` | `nvmlDeviceGetSerial`, `nvmlDeviceGetBoardPartNumber`, `nvmlDeviceGetVbiosVersion` |
| `pci.bus_id`, `pci.device_id` | `nvmlDeviceGetPciInfo_v3` (bus ID normalized to a 4-digit domain) |
| `numa_node` | `nvmlDeviceGetNumaNodeId` |
| `memory_total_bytes` | `nvmlDeviceGetMemoryInfo_v2` (fallback `nvmlDeviceGetMemoryInfo`) |
| `persistence_mode`, `compute_mode`, `ecc_enabled` | `nvmlDeviceGetPersistenceMode`, `nvmlDeviceGetComputeMode`, `nvmlDeviceGetEccMode` |
| `power_limit_default_w`, `power_limit_min_w`, `power_limit_max_w` | `nvmlDeviceGetPowerManagementDefaultLimit`, `nvmlDeviceGetPowerManagementLimitConstraints` |
| `temp_slowdown_c`, `temp_shutdown_c`, `temp_max_operating_c`, `memory_temp_max_c` | `nvmlDeviceGetTemperatureThreshold` (SLOWDOWN, SHUTDOWN, GPU_MAX, MEM_MAX) |
| `clock_core_max_mhz`, `clock_mem_max_mhz` | `nvmlDeviceGetMaxClockInfo` (GRAPHICS, MEM) |
| `pcie_max_gen`, `pcie_max_width`, `pcie_device_max_gen` | `nvmlDeviceGetMaxPcieLinkGeneration`, `nvmlDeviceGetMaxPcieLinkWidth`, `nvmlDeviceGetGpuMaxPcieLinkGeneration` |
| `mig.*` | `nvmlDeviceGetMigMode`, `nvmlDeviceGetMaxMigDeviceCount` |
| `link_count` | field `NVML_FI_DEV_NVLINK_LINK_COUNT` (fallback: probing `nvmlDeviceGetNvLinkState`) |
| `system.driver_version`, `library_version`, `runtime_version` | `nvmlSystemGetDriverVersion`, `nvmlSystemGetNVMLVersion`, `nvmlSystemGetCudaDriverVersion_v2` |
| topology `pcie` level | `nvmlDeviceGetTopologyCommonAncestor` |

## GPU samples (fast tier)

| JSON field (`sample.*`) | Unit | NVML source |
|---|---|---|
| `util_percent` | % | `nvmlDeviceGetUtilizationRates().gpu`: percent of time over the past sample period during which one or more kernels was executing on the GPU |
| `memory_bandwidth_util_percent` | % | `nvmlDeviceGetUtilizationRates().memory`: percent of time over the past sample period during which device memory was being read or written |
| `encoder_util_percent`, `decoder_util_percent` | % | `nvmlDeviceGetEncoderUtilization`, `nvmlDeviceGetDecoderUtilization` |
| `jpeg_util_percent`, `ofa_util_percent` | % | `nvmlDeviceGetJpgUtilization`, `nvmlDeviceGetOfaUtilization` |
| `memory_total/used/free/reserved_bytes` | bytes | `nvmlDeviceGetMemoryInfo_v2` (`reserved` unavailable with the v1 fallback) |
| `temp_c` | °C | `nvmlDeviceGetTemperatureV` (fallback `nvmlDeviceGetTemperature`, sensor GPU) |
| `memory_temp_c` | °C | field `NVML_FI_DEV_MEMORY_TEMP` |
| `fan_percent` | % | `nvmlDeviceGetFanSpeed` (typically unsupported on passively cooled data-center GPUs) |
| `power_w` | W | `nvmlDeviceGetPowerUsage` (milliwatts ÷ 1000) |
| `power_limit_w` | W | `nvmlDeviceGetEnforcedPowerLimit` |
| `energy_j` | J | `nvmlDeviceGetTotalEnergyConsumption` (millijoules since driver load) |
| `clock_core_mhz`, `clock_mem_mhz` | MHz | `nvmlDeviceGetClockInfo` (GRAPHICS, MEM) |
| `pstate` | — | `nvmlDeviceGetPerformanceState` |
| `throttle_reasons` | set | `nvmlDeviceGetCurrentClocksEventReasons` (fallback `…ClocksThrottleReasons`) mapped to `idle`, `app_clock_setting`, `sw_power_cap`, `hw_slowdown`, `sync_boost`, `sw_thermal`, `hw_thermal`, `hw_power_brake`, `display_clock_setting`, `board_limit`, `reliability` |
| `pcie_gen`, `pcie_width` | — | `nvmlDeviceGetCurrPcieLinkGeneration`, `nvmlDeviceGetCurrPcieLinkWidth` |
| `pcie_tx_bps`, `pcie_rx_bps` | bytes/s | rate of fields `NVML_FI_DEV_PCIE_COUNT_TX_BYTES` / `RX_BYTES`; fallback `nvmlDeviceGetPcieThroughput` (KB/s sampled over ~20 ms, refreshed every 5 s) |

**Not exposed:** NVML has no public GPU hotspot temperature. gputop shows
`N/A (not exposed by the driver API)` and never estimates it. Device-level
streaming-multiprocessor (SM) utilization is not a separate NVML device metric;
per-process SM utilization is shown in the process view.

## Health counters (normal tier)

| JSON field (`health_counters.*`) | NVML source |
|---|---|
| `ecc_{corrected,uncorrected}_{volatile,aggregate}` | `nvmlDeviceGetTotalEccErrors` |
| `remapped_rows_{correctable,uncorrectable}`, `remap_pending`, `remap_failure` | `nvmlDeviceGetRemappedRows` (Ampere and newer) |
| `retired_pages_{sbe,dbe}`, `retired_pages_pending` | `nvmlDeviceGetRetiredPages`, `nvmlDeviceGetRetiredPagesPendingStatus` (older architectures; not queried when row remapping is supported) |
| `pcie_replay_counter` | `nvmlDeviceGetPcieReplayCounter` |
| `pcie_{correctable,nonfatal,fatal}_errors` | fields `NVML_FI_DEV_PCIE_COUNT_CORRECTABLE_ERRORS`, `…NON_FATAL_ERROR`, `…FATAL_ERROR` |
| `recovery_action` | field `NVML_FI_DEV_GET_GPU_RECOVERY_ACTION` |
| `violation_power_ns`, `violation_thermal_ns` | `nvmlDeviceGetViolationStatus` (POWER, THERMAL) |

## NVLink (normal tier)

| JSON field (`links[].*`) | NVML source |
|---|---|
| `state` | `nvmlDeviceGetNvLinkState` |
| `version` | `nvmlDeviceGetNvLinkVersion` |
| `remote_pci_bus_id`, `remote_type` | `nvmlDeviceGetNvLinkRemotePciInfo_v2`, `nvmlDeviceGetNvLinkRemoteDeviceType` |
| `remote_id` | derived: remote bus ID matched to a local GPU |
| `tx_bytes`, `rx_bytes` | fields `NVML_FI_DEV_NVLINK_THROUGHPUT_DATA_TX` / `RX` (KiB, per-link scope) |
| `tx_bps`, `rx_bps` | derived: counter rates |
| `err_replay`, `err_recovery`, `err_crc_flit`, `err_crc_data` | `nvmlDeviceGetNvLinkErrorCounter` |

## MIG (slow tier)

| JSON field (`partitions[].*`) | NVML source |
|---|---|
| `id`, `name` | `nvmlDeviceGetMigDeviceHandleByIndex` + `nvmlDeviceGetUUID`, `nvmlDeviceGetName` |
| `profile` | parsed from the MIG device name (e.g. `3g.40gb`) |
| `gpu_instance_id`, `compute_instance_id` | `nvmlDeviceGetGpuInstanceId`, `nvmlDeviceGetComputeInstanceId` |
| `memory_total_bytes`, `memory_used_bytes` | memory info of the MIG device handle |

Per-MIG-instance compute utilization is not available through NVML; it requires
DCGM (planned).

## Processes (normal tier)

| JSON field (`processes[].*`) | Source |
|---|---|
| `pid`, `memory_used_bytes`, `partition_id` | NVML: `nvmlDeviceGetComputeRunningProcesses_v3` (fallback `_v2`), `nvmlDeviceGetGraphicsRunningProcesses_v3`; on MIG GPUs also each MIG device handle. `NVML_VALUE_NOT_AVAILABLE` memory is `null`. |
| `sm_util_percent`, `mem_util_percent`, `enc_util_percent`, `dec_util_percent` | NVML: `nvmlDeviceGetProcessUtilization` (latest sample since the previous query) |
| `name`, `user`, `command`, `start_time` | procfs: `/proc/<pid>/{comm,cmdline,status,stat}` |
| `kubernetes.container_id`, `pod_uid`, `qos`, `runtime` | cgroups: `/proc/<pid>/cgroup` |
| `kubernetes.pod`, `namespace`, `container`, `workload_*` | kubernetes: pod log directories and the in-cluster API |

NVML reports host PIDs. When gputop runs in a nested PID namespace (a container
without `hostPID`), it does not resolve those PIDs and says so, instead of
mislabelling unrelated processes.

## Events

| Kind | Source |
|---|---|
| `xid` | NVML event set (`nvmlEventTypeXidCriticalError`); description and severity from NVIDIA's Xid catalog |
| `ecc_single_bit`, `ecc_double_bit`, `partition_config_change`, `gpu_unavailable`, `recovery_action` | NVML event set |
| `gpu_discovered`, `gpu_disappeared`, `gpu_unavailable`, `gpu_recovered`, `gpu_reset` (inferred from an energy counter reset), `ecc_*`, `row_remap*`, `pages_retired`, `pcie_*`, `nvlink_*`, `mig_*`, `throttle_start/end`, `health_degraded`, `process_start/stop`, `collector_failed/recovered` | derived from consecutive snapshots |

## Host

| JSON field (`host.*`) | Source |
|---|---|
| `cpu.util_percent`, `iowait_percent`, `steal_percent`, `per_core_percent` | CPU time deltas (gopsutil) |
| `cpu.load1/5/15` | load averages |
| `cpu.freq_mhz` | Linux `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq` (average) |
| `memory.*` | virtual memory and swap statistics |
| `filesystems[]` | mounted filesystems (pseudo-filesystems excluded), 1 s timeout per mount |
| `block_devices[]` | disk I/O counter rates; `busy_percent` from I/O time (Linux) |
| `network[]` | per-interface counter rates; Linux interface speed from `/sys/class/net/<if>/speed` |
| InfiniBand ports | `/sys/class/infiniband/<dev>/ports/<n>/counters/port_{rcv,xmit}_{data,packets}` (data counters are octets ÷ 4 per the kernel ABI) |
