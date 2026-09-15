# Contributing to gputop

Thanks for helping build gputop. This guide covers how to get a working
development environment, the project's conventions, and what reviewers look for.

## Getting started

Requirements: Go 1.25+, `make`, and optionally
[golangci-lint](https://golangci-lint.run) v2 and Docker.

```bash
git clone https://github.com/gputop/gputop
cd gputop
make build
./bin/gputop --demo       # simulated GPUs, works on any machine
make test race lint
```

No NVIDIA hardware is needed for day-to-day development. The `--demo` provider
simulates a realistic 8-GPU node, and most packages are tested with fakes.

## Project layout

| Path | Responsibility |
|---|---|
| `cmd/gputop` | `main` only |
| `internal/cli` | flags, modes (TUI, once, JSON, service, remote) |
| `internal/app` | wiring configuration into a running engine |
| `internal/gpu` | **vendor-neutral** device model and `Provider` interface |
| `internal/gpu/nvidia` | NVML provider (the only place with NVIDIA-specific logic) |
| `internal/gpu/nvidia/nvml` | minimal dlopen binding to libnvidia-ml |
| `internal/gpu/sim` | simulated provider for `--demo`, tests and benchmarks |
| `internal/collector` | tiered scheduling, per-collector health, snapshot publishing |
| `internal/model` | the immutable snapshot and its JSON schema |
| `internal/derive`, `internal/health`, `internal/events` | derived metrics, health score, events and alerts |
| `internal/history` | time-series ring and on-disk segments |
| `internal/host`, `internal/procinfo`, `internal/kube` | host metrics, process metadata, Kubernetes correlation |
| `internal/server`, `internal/remote` | service-mode API, Prometheus, remote client |
| `internal/tui`, `internal/tui/widgets` | Bubble Tea UI and rendering primitives |
| `internal/theme`, `internal/keymap`, `internal/config` | themes, key bindings, configuration |

Read [docs/architecture.md](docs/architecture.md) before larger changes.

## Ground rules

- **No vendor logic outside providers.** UI, collector, history and server code
  must only use `internal/gpu` types. If you need a new concept, add it to the
  neutral model.
- **Unavailable is not zero.** Use `metric.Opt[T]`; render `N/A`; encode `null`.
- **Never invent metric names or semantics.** Every NVML function, field ID,
  constant and struct layout must match `nvml.h`. Cite the source in comments,
  and verify layouts with `internal/gpu/nvidia/nvml/layout_test.go` and
  `scripts/test-fake-nvml.sh`.
- **Label derived values.** Anything computed by gputop has source `derived` and
  must be documented in [docs/derived-metrics.md](docs/derived-metrics.md) or
  [docs/health-score.md](docs/health-score.md), including inputs, formula,
  assumptions and limitations.
- **Graceful degradation.** Missing drivers, unsupported features, lost GPUs,
  denied permissions and corrupted history files must never panic or stop other
  collectors.
- **Keep the collector cheap.** No subprocesses on refresh paths; avoid
  per-sample allocations that grow with history size; run `make bench` for
  changes to hot paths.
- **No hard-coded keys or colors in the UI.** Use `keymap` actions and `theme`
  roles.
- **Security first for remote features.** Safe defaults, no plaintext secrets in
  config, no unauthenticated non-loopback listeners.

## Code style

- `gofmt`/`goimports`, `go vet` (also with `GOOS=linux` and `GOOS=darwin`), and
  `golangci-lint run` must pass.
- Every Go file starts with the SPDX header:

  ```go
  // Copyright 2026 The gputop Authors
  // SPDX-License-Identifier: Apache-2.0
  ```

- Exported identifiers have doc comments. Comments explain *why*.
- Prefer small, focused packages over new abstraction layers.

## Tests

- `make test` must pass without GPUs. Use `internal/gpu/sim` or
  `nvml.Unsupported`-based fakes.
- UI changes: `internal/tui/render_test.go` renders every tab at several sizes
  and asserts exact dimensions. To look at frames:

  ```bash
  GPUTOP_DUMP_DIR=/tmp/frames go test ./internal/tui -run TestDumpFrames -count=1
  ```

- NVML binding changes: extend `testdata/fakenvml.c` and the checks in
  `scripts/test-fake-nvml.sh`.
- If you have NVIDIA hardware, please run `make test-nvidia` and include the
  output (GPU model, driver version) in your pull request.

## Pull requests

1. Open an issue first for large features or design changes.
2. Keep pull requests focused; include tests and documentation updates.
3. Update `CHANGELOG.md` under *Unreleased*.
4. Describe what you validated (unit tests, fake NVML, real hardware).

By contributing you agree that your contributions are licensed under the
Apache License 2.0.

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).
