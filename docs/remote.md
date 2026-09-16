---
title: Service mode and remote monitoring
---

# Service mode and remote monitoring

> 🧪 Experimental: the API is versioned (`/api/v1`) but may change before 1.0.

```text
GPU node                                   workstation
┌────────────────────────────┐    HTTPS    ┌─────────────────────────────┐
│ gputop --service           │ ◄───────────│ gputop --remote gpu-node-01 │
│  collectors + history      │  bearer     │  (same TUI, same tabs)      │
│  /api/v1/*   /metrics      │  token/mTLS └─────────────────────────────┘
└────────────────────────────┘
```

gputop never runs commands on remote machines. A remote node runs its own agent,
and the client only reads that agent's read-only API.

## Quick start with an SSH tunnel (simplest and safe)

On the GPU node, keep the default loopback address:

```bash
gputop --service              # listens on 127.0.0.1:9469, no auth needed on loopback
```

On your workstation:

```bash
ssh -N -L 9469:127.0.0.1:9469 gpu-node-01 &
gputop --remote http://127.0.0.1:9469
```

## Exposing the agent on the network

gputop refuses to listen on a non-loopback address unless **both** TLS and
token authentication are configured.

1. Create a token and keep only its digest on the agent:

   ```bash
   gputop --gen-token
   # token:        6f1c…
   # token_sha256: 9a0e…
   ```

2. Agent configuration (`/etc/gputop/config.yaml`):

   ```yaml
   server:
     listen: 0.0.0.0:9469
     tls_cert_file: /etc/gputop/tls.crt
     tls_key_file: /etc/gputop/tls.key
     token_sha256: 9a0e…            # digest only
     # client_ca_file: /etc/gputop/clients-ca.crt   # optional mutual TLS
     # expose_processes: false                     # hide process names/users
   ```

   Run it with `gputop --service --config /etc/gputop/config.yaml`, or use the
   systemd unit in `deploy/systemd/`.

3. Client configuration (`~/.config/gputop/config.yaml`):

   ```yaml
   remote:
     nodes:
       - name: gpu-node-01
         address: gpu-node-01.example.com
         token_file: ~/.config/gputop/tokens/gpu-node-01   # chmod 600
         ca_file: ~/.config/gputop/internal-ca.pem
   ```

   ```bash
   gputop --remote gpu-node-01
   gputop --remote gpu-node-01 --once --json
   ```

All configured nodes are polled every 10 seconds for summaries shown in the
**Nodes** tab of the local TUI.

## Security properties

| Property | Implementation |
|---|---|
| Safe default | Listens on `127.0.0.1` |
| No accidental exposure | Non-loopback listen requires TLS and a token |
| Encryption | TLS 1.2+ |
| Authentication | Bearer token compared in constant time against SHA-256; optional mTLS |
| No plaintext secrets in config | Server stores a digest; clients read tokens from files or environment variables |
| Server verification | System roots or `ca_file`; no option to skip verification |
| Plaintext only for tunnels | Clients accept `http://` only for loopback hosts |
| Least privilege | Read-only GET/HEAD API |
| Data minimization | Command lines are never served; names/users optional |
| Robustness | Header/read/write/idle timeouts, header size limits |

## API

All endpoints except `/healthz` require `Authorization: Bearer <token>` when a
token is configured.

| Endpoint | Description |
|---|---|
| `GET /healthz` | `ok` (no authentication, no data) |
| `GET /api/v1/snapshot` | Full snapshot ([json-schema.md](json-schema.md)), processes redacted as configured |
| `GET /api/v1/summary` | Node, providers, fleet summary, alerts and history status |
| `GET /api/v1/history` | History query. Parameters: `since` (duration such as `15m`, or RFC 3339), `keys` (comma-separated GPU UUIDs or `host`), `metrics` (comma-separated metric keys, e.g. `util,power,temp`), `max_points` (≤ 10000) |
| `GET /api/v1/events` | Events since `since` (default `1h`) |
| `GET /metrics` | Prometheus text format |

History metric keys: `util`, `mem_bandwidth`, `vram_pct`, `vram_gib`, `power`,
`temp`, `mem_temp`, `clock_core`, `clock_mem`, `pcie_tx`, `pcie_rx`,
`nvlink_tx`, `nvlink_rx`, `encoder`, `decoder`, `fan`, `proc_vram`,
`proc_count`, `health`, `throttle`, and for `host`: `host_cpu`, `host_mem`,
`host_net_rx`, `host_net_tx`, `host_disk_read`, `host_disk_write`, `host_load1`.

## Prometheus

Example scrape configuration:

```yaml
scrape_configs:
  - job_name: gputop
    scheme: https
    authorization:
      credentials_file: /etc/prometheus/gputop-token
    tls_config:
      ca_file: /etc/prometheus/internal-ca.pem
    static_configs:
      - targets: ["gpu-node-01.example.com:9469"]
```

Metric families use the `gputop_` prefix and only the `gpu` (index) and `uuid`
labels, plus small fixed label sets such as `reason`, `type` and `collector`.
There are no PID, container or pod labels. Values that are unavailable are
omitted rather than exported as 0. gputop-derived values (`gputop_gpu_health_score`,
`gputop_gpu_efficiency_score`, `gputop_gpu_idle_allocated`,
`gputop_gpu_utilization_outlier`, `gputop_fleet_unused_allocated_gpu_equivalents`)
say so in their help text. The agent also exports its own collector durations,
error and overrun counts, history write latency, CPU and memory.

If you already run NVIDIA DCGM Exporter, gputop's metrics complement it; they
are not a replacement for DCGM profiling fields.
