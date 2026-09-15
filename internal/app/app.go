// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package app wires configuration into a running collection engine. It is
// shared by the TUI, service and one-shot modes.
package app

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gputop/gputop/internal/collector"
	"github.com/gputop/gputop/internal/config"
	"github.com/gputop/gputop/internal/derive"
	"github.com/gputop/gputop/internal/events"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/gpu/apple"
	"github.com/gputop/gputop/internal/gpu/nvidia"
	"github.com/gputop/gputop/internal/gpu/sim"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/host"
	"github.com/gputop/gputop/internal/kube"
	"github.com/gputop/gputop/internal/paths"
	"github.com/gputop/gputop/internal/procinfo"
)

// Options select runtime behaviour beyond the config file.
type Options struct {
	Config   config.Config
	Demo     bool
	DemoGPUs int
	Log      *slog.Logger
	// NoPersist keeps history in memory (used by --once).
	NoPersist bool
}

// Runtime is a wired engine with its resources.
type Runtime struct {
	Engine  *collector.Engine
	History *history.Store
	Close   func()
}

// Providers builds the configured accelerator providers. "auto" detects the
// platform's vendor: Apple silicon on macOS, NVIDIA everywhere else.
func Providers(cfg config.Config, demo bool, demoGPUs int) []gpu.Provider {
	if demo {
		return []gpu.Provider{sim.New(sim.Options{GPUs: demoGPUs})}
	}
	var out []gpu.Provider
	added := map[string]bool{}
	for _, name := range cfg.GPU.Providers {
		if name == "auto" {
			name = autoProvider(runtime.GOOS)
		}
		if added[name] {
			continue
		}
		added[name] = true
		switch name {
		case "nvidia":
			out = append(out, nvidia.New(nvidia.Options{LibraryPaths: cfg.GPU.NVIDIA.LibraryPaths}))
		case "apple":
			out = append(out, apple.New(apple.Options{}))
		}
	}
	return out
}

// autoProvider is the provider "auto" selects on goos.
func autoProvider(goos string) string {
	if goos == "darwin" {
		return "apple"
	}
	return "nvidia"
}

// Build creates the engine and its dependencies.
func Build(o Options) (*Runtime, error) {
	cfg := o.Config
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	evlog := events.NewLog(2000)
	rt := &Runtime{Close: func() {}}

	if cfg.History.Enabled {
		dir := paths.Expand(cfg.History.Path)
		if dir == "" {
			dir = paths.HistoryDir()
			if o.Demo {
				// Never mix simulated history with real hardware history.
				dir = filepath.Join(paths.StateDir(), "history-demo")
			}
		}
		store, recovered, err := history.Open(history.Options{
			Retention: cfg.History.Retention.D(), Resolution: cfg.History.Resolution.D(),
			Dir: dir, Persist: cfg.History.Persist && !o.NoPersist,
			MaxDiskBytes: int64(cfg.History.MaxDiskMB) << 20, Log: log,
		})
		if err != nil {
			return nil, fmt.Errorf("history: %w", err)
		}
		evlog.Append(recovered...)
		rt.History = store
	}

	env := kube.Environment{Mode: kube.ModeNone}
	var corr *kube.Correlator
	if cfg.Kubernetes.Enabled != config.False && !o.Demo {
		env = kube.Detect(kube.DetectOptions{NodeName: cfg.Kubernetes.NodeName, PodLogsDir: cfg.Kubernetes.PodLogsDir})
		if env.Mode == kube.ModeInCluster || env.Mode == kube.ModeNode || cfg.Kubernetes.Enabled == config.True {
			var api *kube.APIClient
			useAPI := cfg.Kubernetes.API == config.True || (cfg.Kubernetes.API == config.Auto && env.Mode == kube.ModeInCluster)
			if useAPI {
				c, err := kube.NewInClusterClient("")
				if err != nil {
					log.Info("kubernetes API unavailable", "err", err)
				} else {
					api = c
				}
			}
			logsDir := cfg.Kubernetes.PodLogsDir
			if env.Mode == kube.ModeKubeconfig {
				logsDir = ""
			}
			corr = kube.NewCorrelator(env, logsDir, api, log)
		}
	}

	provs := Providers(cfg, o.Demo, o.DemoGPUs)
	eng := collector.New(collector.Options{
		Providers: provs,
		Intervals: collector.Intervals{
			Fast: cfg.Refresh.Interval.D(), Normal: cfg.Refresh.Normal.D(), Slow: cfg.Refresh.Slow.D(),
			Inventory: cfg.Refresh.Inventory.D(), Timeout: cfg.Refresh.Timeout.D(),
		},
		Derive: derive.Options{
			IdleThreshold: cfg.GPU.IdleThreshold, IdleAfter: cfg.GPU.IdleAfter.D(),
			OutlierMinDelta: cfg.GPU.OutlierMinDelta, Window: 5 * time.Minute,
		},
		Host: host.NewCollector(), Procs: procinfo.NewResolver(), Kube: corr, KubeEnv: env,
		History: rt.History, Events: evlog, Demo: o.Demo, Log: log,
	})
	rt.Engine = eng
	rt.Close = func() {
		for _, p := range provs {
			if err := p.Close(); err != nil {
				log.Warn("closing provider", "provider", p.Name(), "err", err)
			}
		}
		if rt.History != nil {
			if err := rt.History.Close(); err != nil {
				log.Warn("closing history", "err", err)
			}
		}
	}
	return rt, nil
}
