# Performance

A monitoring tool must not become the problem. gputop's performance targets:
low idle CPU, a small bounded memory footprint, no subprocesses on refresh
paths, collection that never blocks the UI, and bounded history.

## Environment

- Apple M4 (10 cores), macOS 26, Go 1.25.3.
- `--demo` simulated provider. Real NVML calls add latency on top of these
  figures (typically sub-millisecond to a few milliseconds per call, depending
  on the driver and whether persistence mode is enabled). The collector runs
  device calls in parallel (4 workers) and isolates slow devices.
- Benchmarks: `make bench` (`go test -bench`).

## Microbenchmarks

| Benchmark | 1 GPU | 4 GPUs | 8 GPUs | 16 GPUs |
|---|---|---|---|---|
| `BenchmarkCollectSamples`: one fast-tier device pass | 5.6 µs, 15 allocs | 17 µs | 23 µs, 93 allocs | 32 µs, 241 allocs |
| `BenchmarkPublish`: snapshot, derive, health, events, alerts, history ingest (5 s resolution) | 20 µs, 23 KB | 41 µs, 92 KB | 77 µs, 183 KB | 174 µs, 376 KB |
| `BenchmarkObserveCommitPersist`: history bucket commit + disk append (worst case) | 18 µs | — | 59 µs | 73 µs |
| `BenchmarkRenderOverview`: full frame at 200×60 | 1.2 ms | 1.6 ms | 2.0 ms | 2.8 ms |
| `BenchmarkQuery30m`: history query, 8 GPUs, all metrics, 320 points | — | — | 131 µs | — |

## Process measurements

Each run lasted 20 seconds with a 1-second refresh; CPU time and RSS were read
with `ps`. The TUI ran in a pseudo-terminal.

| Mode | GPUs | CPU time over 20 s | Average CPU | RSS |
|---|---|---|---|---|
| TUI | 1 | 0.12 s | 0.6% of one core | 19.8 MB |
| TUI | 8 | 0.15 s | 0.75% | 21.9 MB |
| TUI | 16 | 0.19 s | 0.95% | 22.9 MB |
| `--json` stream | 8 | 0.16 s | 0.8% | 21.4 MB |

Startup: `gputop --version` completes in a few milliseconds. `gputop --once`
takes about one second, because it waits 500 ms to compute rate-based metrics
(CPU, network, PCIe and NVLink throughput).

## Memory bounds

| Component | Bound |
|---|---|
| History ring | `(retention / resolution + 1) × 20 metrics × 4 bytes` per GPU (30m/5s ≈ 29 KB per GPU; 24h/5s ≈ 1.4 MB per GPU); limited to 20000 points per series |
| History disk | `history.max_disk_mb` (default 256 MB), about 120 bytes per GPU per bucket (≈ 90 KB per GPU for 30 minutes at 5 s) |
| Derive tracker | 1024 samples per GPU |
| Live chart buffer (TUI) | 600 samples per series |
| Event log | 2000 events |

## Design choices that keep overhead low

- NVML is called directly (no `nvidia-smi` subprocesses).
- Static inventory is collected every 5 minutes, not every second.
- Unsupported NVML functions are remembered per device and not retried.
- PCIe throughput uses cumulative counters instead of the ~20 ms sampling call.
- Events such as Xids use NVML event sets instead of polling.
- Snapshots are built once per refresh and shared read-only by the UI, JSON
  output, API and history store.
- Rendering happens only when a snapshot arrives or a key is pressed.

## Validating on real hardware

Please share results for your GPUs:

```bash
make bench
make test-nvidia
gputop --json > /dev/null &  sleep 60; ps -o time=,rss= -p $!; kill $!
```

Open an issue with the GPU model, GPU count, driver version and output.
