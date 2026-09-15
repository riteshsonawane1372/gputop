# Configuration

gputop reads `$XDG_CONFIG_HOME/gputop/config.yaml` (default
`~/.config/gputop/config.yaml`), or the file given with `--config`. All keys are
optional. Unknown keys and invalid values are rejected with an explanation; all
problems are reported at once. Print the effective configuration with
`gputop --print-config`; its output is itself a valid config file.

Command-line flags override the file: `--interval`, `--retention`,
`--no-history`, `--theme`, `--listen`, `--debug`, `--log-file`.

Durations accept Go syntax (`500ms`, `5s`, `30m`, `24h`), days (`7d`) and plain
numbers of seconds.

## `refresh`

| Key | Default | Description |
|---|---|---|
| `interval` | `1s` | Fast tier: GPU samples, host CPU/memory. 100ms–1m. |
| `normal` | `3s` | Processes, health counters, NVLink, network, disk I/O. ≥ `interval`. |
| `slow` | `30s` | MIG instances, filesystems, Kubernetes metadata. ≥ `normal`. |
| `inventory` | `5m` | Device discovery, static inventory, topology. ≥ `slow`. |
| `timeout` | `5s` | Deadline for one collection pass. |

## `history`

| Key | Default | Description |
|---|---|---|
| `enabled` | `true` | Keep history. |
| `retention` | `30m` | How far back history goes. At least `1m`. |
| `resolution` | `5s` | Bucket size. At least `1s`; `retention/resolution` must be ≤ 20000. |
| `persist` | `true` | Also write history to disk. |
| `path` | `$XDG_STATE_HOME/gputop/history` | History directory (`history-demo` in `--demo` mode). |
| `max_disk_mb` | `256` | Disk cap; oldest segments are removed first. |

## `theme`

| Key | Default | Description |
|---|---|---|
| `name` | `green` | Built-in (`green`, `amber`, `ice`, `mono`, `dusk`) or a file in `~/.config/gputop/themes/`. |
| `colors` | — | Map of role → color overriding the selected theme. See [themes.md](themes.md). |
| `transparent` | `false` | Use the terminal's background instead of painting the theme background. |

## `keys`

Map of action → key or list of keys. An entry replaces all default keys for
that action. Binding one key to two actions is an error. Key names follow the
terminal library: letters and symbols as typed (`G` differs from `g`), `enter`,
`esc`, `tab`, `shift+tab`, `up`, `down`, `left`, `right`, `pgup`, `pgdown`,
`home`, `end`, `space`, `ctrl+<letter>`.

| Action | Default keys |
|---|---|
| `quit` | `q`, `ctrl+c` |
| `help` | `?` |
| `next_tab` / `prev_tab` | `tab` / `shift+tab` |
| `tab_1` … `tab_9` | `1` … `9` |
| `history` | `h` |
| `refresh` | `r` |
| `filter` / `search` | `f` / `/` |
| `select` / `back` | `enter` / `esc` |
| `up` / `down` | `up`, `k` / `down`, `j` |
| `left` / `right` | `left` / `right`, `l` |
| `page_up` / `page_down` | `pgup`, `ctrl+u` / `pgdown`, `ctrl+d` |
| `home` / `end` | `home`, `g` / `end`, `G` |
| `zoom_in` / `zoom_out` | `+`, `=` / `-`, `_` |
| `sort_next` / `sort_reverse` | `s` / `S` |
| `pause` | `p`, `space` |
| `next_metric` / `prev_metric` | `m` / `M` |
| `next_gpu` / `prev_gpu` | `]` / `[` |
| `scrub_back` / `scrub_forward` / `scrub_now` | `,` / `.` / `n` |

Example:

```yaml
keys:
  quit: [q, x]
  history: H
  next_gpu: n        # must also rebind scrub_now, which uses n by default
  scrub_now: N
```

Invalid bindings do not prevent startup: gputop falls back to the defaults and
shows the problem in the footer.

## `gpu`

| Key | Default | Description |
|---|---|---|
| `providers` | `[auto]` | `auto` or `nvidia`. |
| `idle_threshold` | `5` | Utilization (%) below which a GPU is idle. |
| `idle_after` | `5m` | Idle duration before an allocated GPU is flagged. |
| `outlier_min_delta` | `20` | Minimum gap (percentage points) below the cohort median for a straggler. |
| `nvidia.library_paths` | `[]` | Extra `libnvidia-ml.so.1` locations to try. The defaults cover the dynamic linker path, common distribution paths, the Kubernetes device plugin mount, the GPU Operator driver container and WSL2. |

## `kubernetes`

| Key | Default | Description |
|---|---|---|
| `enabled` | `auto` | `auto` (detect), `true`, `false`. |
| `node_name` | `$NODE_NAME` or hostname | Node used to filter pods from the API. |
| `pod_logs_dir` | `/var/log/pods` | Scanned to map pod UIDs to names on a node. |
| `api` | `auto` | Query the API server with the in-cluster service account (`auto` = when running in a pod). |

## `remote`

`remote.nodes` is a list of agents:

| Key | Description |
|---|---|
| `name` | Name used with `--remote` and in the Nodes tab. |
| `address` | Host, `host:port`, or URL. The scheme defaults to `https`; plain `http` is accepted only for loopback addresses. |
| `port` | Port when `address` has none (default `9469`). |
| `token_file` / `token_env` | Where to read the bearer token. If neither is set, `$GPUTOP_TOKEN` is used when present. |
| `ca_file` | CA certificate(s) to verify the agent (system roots otherwise). |
| `server_name` | TLS server name override. |
| `cert_file` / `key_file` | Client certificate for mutual TLS. |

## `server` (service mode)

| Key | Default | Description |
|---|---|---|
| `listen` | `127.0.0.1:9469` | Listen address. Non-loopback addresses require TLS and a token. |
| `tls_cert_file` / `tls_key_file` | — | Serving certificate and key. |
| `client_ca_file` | — | Require and verify client certificates (mTLS). |
| `token_file` | — | File containing the bearer token. |
| `token_sha256` | — | Hex SHA-256 of the token (preferred: no plaintext on the agent). |
| `expose_processes` | `true` | Serve process names and users. Command lines are never served. |

## `prometheus`

| Key | Default | Description |
|---|---|---|
| `enabled` | `true` | Serve metrics in service mode. |
| `path` | `/metrics` | Metrics path. |

## `logging`

| Key | Default | Description |
|---|---|---|
| `level` | `warn` | `debug`, `info`, `warn`, `error`. |
| `file` | — | Log file. In TUI mode logs are never written to the terminal; with `--debug` they go to `$XDG_STATE_HOME/gputop/gputop.log`. Service and one-shot modes log to stderr; service mode uses JSON when stderr is not a terminal. |

## `ui`

| Key | Default | Description |
|---|---|---|
| `default_tab` | `overview` | Tab shown at start (`overview`, `gpus`, `processes`, `history`, …). |
| `temperature_unit` | `c` | `c` or `f`. |
| `show_command_lines` | `false` | Add a COMMAND column to the process table. |
