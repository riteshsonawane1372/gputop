# Health score

The health score is a **gputop-derived** 0–100 summary of reliability signals.
It is a transparent heuristic, **not an NVIDIA metric** and not a validated
failure predictor. Its purpose is triage: quickly finding the GPU that deserves
attention, and showing exactly why.

Every deduction is reported as a reason (in the GPU detail view, the Health tab,
JSON `health.reasons`, and as alerts).

## Bands

| Score | Band |
|---|---|
| 90–100 | healthy |
| 75–89 | good |
| 50–74 | degraded |
| 25–49 | unhealthy |
| 0–24 | critical |

## Rules

The score starts at 100. Deductions are summed and the result is clamped to 0.
Any **critical** reason caps the score at 49, so a critical condition can never
look "good". An unavailable GPU scores 0.

| Condition | Deduction | Severity | Inputs |
|---|---|---|---|
| GPU unavailable (lost, not responding) | score = 0 | critical | provider errors |
| Row remapping failure | 60 | critical | `remap_failure` |
| Uncorrectable ECC errors since driver load | 40 | critical | `ecc_uncorrected_volatile > 0` |
| Driver recommends a recovery action | 40 | critical | `recovery_action` ≠ `none` |
| Critical Xid in the last 10 minutes | 35 | critical | Xid events (severity below) |
| Critical Xid 10–60 minutes ago | 17 | warning | Xid events |
| Warning Xid (each, capped at 20) | 10 (5 if older than 10 min) | warning | Xid events |
| Informational Xid (capped at 4) | 2 | info | Xid events |
| Memory remap or page retirement pending | 25 | warning | `remap_pending`, `retired_pages_pending` |
| Pages retired after double-bit ECC errors | 10 | warning | `retired_pages_dbe > 0` |
| ≥100 corrected ECC errors within the window | 5 | warning | `ecc_corrected_volatile` growth |
| Hardware or hardware-thermal slowdown active | 20 | warning | throttle `hw_slowdown`, `hw_thermal` |
| Software thermal slowdown active | 10 | warning | throttle `sw_thermal` |
| Temperature within 3 °C of slowdown (when not already throttling) | 10 | warning | `temp_c`, `temp_slowdown_c` |
| Memory temperature within 3 °C of its limit | 10 | warning | `memory_temp_c`, `memory_temp_max_c` |
| External power brake asserted | 15 | warning | throttle `hw_power_brake` |
| PCIe link width below maximum | 15 | warning | `pcie_width`, `pcie_max_width` |
| PCIe generation below maximum **while utilization ≥ 50%** | 5 | warning | `pcie_gen`, `pcie_max_gen`, `util_percent` |
| Fatal PCIe errors | 20 | critical | `pcie_fatal_errors > 0` |
| ≥10 PCIe replays within the window | 5 | warning | `pcie_replay_counter` growth |
| NVLink links inactive while others are active | 10 per link, max 30 | warning | link states |
| NVLink error counters increasing | 5 per link, max 15 | warning | link error growth |

### Xid severity

Xid severity comes from the *Resolution Bucket (Immediate Action)* column of
[NVIDIA's Xid catalog](https://docs.nvidia.com/deploy/xid-errors/), bundled in
`internal/gpu/nvidia/xid_catalog.go`:

- **critical:** `RESET_GPU`, `RESTART_BM`, `RESTART_VM`, `CONTACT_SUPPORT`,
  `CHECK_MECHANICALS`, `WORKFLOW_*`;
- **warning:** `RESTART_APP`, `UPDATE_SWFW`, `CHECK_UVM`, `XID_154`;
- **info:** everything else (e.g. `IGNORE`).

Xid codes not present in the bundled catalog are treated as warnings.

### Windows and baselines

Counter-growth rules compare current counters with a baseline captured about
10 minutes earlier (checkpoints are kept every minute, up to 16). Until that
much history exists, the oldest available checkpoint is used. Xid events count
for one hour. Absolute lifetime counters (for example aggregate corrected ECC)
are displayed but do not reduce the score, because they say little about the
current state.

## Principles and limitations

- **Missing data never lowers the score.** A GPU that does not support ECC or
  row remapping is not penalized for it. As a consequence, a GPU with few
  supported counters can score high simply because less is observable.
- **Weights are judgment calls.** They order conditions by operational urgency
  but have not been calibrated against fleet failure data. Treat the score as a
  pointer to reasons, not as a probability.
- **Transient conditions.** Thermal and power-brake deductions reflect the
  current sample and disappear when the condition clears; the History and
  Events tabs show whether it keeps happening.
- **PCIe generation.** GPUs lower their link generation when idle to save
  power, which is normal. gputop only flags a lower generation under load.
- The score does not measure performance; see
  [derived-metrics.md](derived-metrics.md) for utilization-based indicators.

Improvements to these rules are welcome. Please include evidence (for example
field incident data) in the proposal.
