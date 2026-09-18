---
title: Inference serving metrics
---

# Inference serving metrics

GPU counters tell you a GPU is busy. They don't tell you whether users are
waiting. For that, gputop scrapes the Prometheus endpoints of LLM inference
servers and shows serving metrics next to the GPUs that run them:

| Metric | Meaning |
|---|---|
| **TTFT** | Time to first token: how long a request waits before output starts (queueing + prefill) |
| **ITL** | Inter-token latency (time per output token): decode speed as the user sees it |
| **E2E** | End-to-end request latency |
| **Queue** | Time a request waited before it was scheduled |
| **Running / waiting** | Requests in the batch / queued for admission |
| **KV cache** | Fraction of KV-cache blocks in use. At 100%, new requests queue and running ones may be preempted |
| **Prefix hit** | Fraction of prompt tokens served from the prefix cache |
| **Throughput** | Completed requests/s, prompt tokens/s and generated tokens/s |
| **Preemptions** | Requests evicted from the batch per second (vLLM) |

These values have the `application` source: the server reports them and
gputop only aggregates them.

## Supported servers

| Server | Recognized command | Metrics port | TTFT | ITL | E2E | Queue | KV cache |
|---|---|---|---|---|---|---|---|
| vLLM | `vllm serve`, `vllm.entrypoints.*` | `--port` (8000) | ✅ | ✅ | ✅ | ✅ | ✅ |
| SGLang | `sglang.launch_server` (needs `--enable-metrics`) | `--port` (30000) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Text Generation Inference | `text-generation-launcher` / `-router` | `--prometheus-port` (9000) | — | ✅ | ✅ | ✅ | — |
| llama.cpp | `llama-server` (needs `--metrics`) | `--port` (8080) | — | — | — | — | ✅ |

The engine is detected from the metric names, so a configured endpoint doesn't
need to say what it is. Values a server doesn't export are shown as `N/A`.
Metric names that changed between vLLM and SGLang versions (for example
`time_per_output_token_seconds` → `inter_token_latency_seconds`) are both
recognized.

## Finding servers

**Discovery** (on by default). gputop recognizes the commands above among the
processes using a GPU and derives the metrics URL from `--port` and `--host`.
Processes started by the same server (tensor-parallel workers) are merged into
one entry that lists all of their GPUs. Containerized servers are reached at
their pod IP, which requires Kubernetes pod metadata (see
[kubernetes.md](kubernetes.md)). A container without a known pod IP is skipped.

**Configured endpoints**, for servers on other hosts, behind a service, or on
ports discovery can't see:

```yaml
inference:
  endpoints:
    - name: chat-api
      url: http://10.0.0.5:8000/metrics
```

A configured endpoint that can't be scraped makes the `inference` collector
unhealthy, so it shows up as an alert. A discovered server that can't be
scraped is only shown as down in the Inference tab, because it may simply not
export metrics.

## How values are computed

Servers export cumulative counters and histograms. gputop scrapes each server
every `refresh.normal` interval (3s by default) and keeps the scrapes of the last
`inference.window` (1 minute by default). Rates and latencies describe that
window: they are the difference between the newest scrape and the oldest one
in the window, the way PromQL's `rate()` and `histogram_quantile()` work.

- Percentiles are estimated by linear interpolation inside histogram buckets,
  so their precision depends on the server's bucket boundaries. A p99 in the
  last finite bucket reports that bucket's upper bound.
- Rates and percentiles are unavailable until two scrapes exist, and latencies
  are unavailable while no request completed in the window.
- When a counter goes backwards (the server restarted), the window starts over
  instead of reporting negative rates.

## Where it shows up

- **Inference tab**: every server with state, queue, KV cache, TTFT p50/p99,
  ITL, E2E, request and token rates. The selected server has a latency table
  (mean, p50, p90, p99, count), load details and live charts of TTFT, requests,
  generation throughput and KV cache.
- **Overview**: a *Serving* line with the worst TTFT p99 and ITL p50, total
  generation throughput and queued requests.
- **Alerts**: *KV cache full* when a server's KV cache is at least 95% used
  while requests are waiting.
- **JSON**: `inference[]` in every snapshot (see [json-schema.md](json-schema.md)).
- **Prometheus**: `gputop_inference_*` gauges labeled by `server` and `engine`;
  latencies carry a `quantile` label (`0.5`, `0.9`, `0.99`).

Serving metrics are not written to the history store; the live charts cover
the last 10 minutes.
