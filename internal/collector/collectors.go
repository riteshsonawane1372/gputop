// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/inference"
	"github.com/riteshsonawane1372/gputop/internal/kube"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

// collector is one independently scheduled unit of collection.
type collector struct {
	name     string
	tier     string
	interval time.Duration
	fn       func(ctx context.Context) error

	inflight atomic.Bool
	done     chan struct{} // closed when the in-flight run finishes

	mu           sync.Mutex
	runs, errs   uint64
	overruns     uint64
	lastRun      time.Time
	lastDuration time.Duration
	avgDuration  time.Duration
	lastErr      string
	healthy      bool
}

var closedCh = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

// start launches a run unless one is in flight. The returned channel is
// closed when the run (or the already in-flight run) completes.
func (e *Engine) start(ctx context.Context, c *collector) <-chan struct{} {
	if c == nil {
		return closedCh
	}
	if !c.inflight.CompareAndSwap(false, true) {
		c.mu.Lock()
		c.overruns++
		done := c.done
		c.mu.Unlock()
		if done == nil {
			return closedCh
		}
		return done
	}
	done := make(chan struct{})
	c.mu.Lock()
	c.done = done
	c.mu.Unlock()
	go func() {
		defer close(done)
		defer c.inflight.Store(false)
		cctx, cancel := context.WithTimeout(ctx, e.o.Intervals.Timeout)
		defer cancel()
		begin := time.Now()
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic: %v", r)
					e.log.Error("collector panic", "collector", c.name, "panic", r)
				}
			}()
			return c.fn(cctx)
		}()
		d := time.Since(begin)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.runs++
		c.lastRun, c.lastDuration = time.Now(), d
		if c.avgDuration == 0 {
			c.avgDuration = d
		} else {
			c.avgDuration = (c.avgDuration*7 + d) / 8
		}
		if err != nil && ctx.Err() == nil {
			c.errs++
			c.lastErr = err.Error()
			c.healthy = false
			e.log.Debug("collector error", "collector", c.name, "err", err)
		} else if err == nil {
			c.lastErr = ""
			c.healthy = true
		}
	}()
	return done
}

func (e *Engine) collectorStatusLocked() []model.CollectorStatus {
	names := make([]string, 0, len(e.collectors))
	for n := range e.collectors {
		names = append(names, n)
	}
	order := map[string]int{"fast": 0, "normal": 1, "slow": 2, "inventory": 3, "event": 4}
	sort.Slice(names, func(i, j int) bool {
		a, b := e.collectors[names[i]], e.collectors[names[j]]
		if order[a.tier] != order[b.tier] {
			return order[a.tier] < order[b.tier]
		}
		return a.name < b.name
	})
	out := make([]model.CollectorStatus, 0, len(names))
	for _, n := range names {
		c := e.collectors[n]
		c.mu.Lock()
		out = append(out, model.CollectorStatus{
			Name: c.name, Tier: c.tier, Interval: c.interval, Runs: c.runs, Errors: c.errs, Overruns: c.overruns,
			LastRun: c.lastRun, LastDuration: c.lastDuration, AvgDuration: c.avgDuration, LastError: c.lastErr,
			Healthy: c.healthy || c.runs == 0,
		})
		c.mu.Unlock()
	}
	return out
}

func (e *Engine) addCollectors() {
	iv := e.o.Intervals
	add := func(name, tier string, interval time.Duration, fn func(context.Context) error) {
		e.collectors[name] = &collector{name: name, tier: tier, interval: interval, fn: fn}
	}
	add("inventory", "inventory", iv.Inventory, e.collectInventory)
	add("gpu", "fast", iv.Fast, e.collectSamples)
	add("health", "normal", iv.Normal, e.collectHealth)
	add("links", "normal", iv.Normal, e.collectLinks)
	add("processes", "normal", iv.Normal, e.collectProcesses)
	add("partitions", "slow", iv.Slow, e.collectPartitions)
	if e.o.Host != nil {
		add("host", "fast", iv.Fast, e.collectHostFast)
		add("network", "normal", iv.Normal, e.collectNetwork)
		add("disks", "normal", iv.Normal, e.collectDisks)
		add("filesystems", "slow", iv.Slow, e.collectFilesystems)
	}
	if e.o.Inference != nil {
		add("inference", "normal", iv.Normal, e.collectInference)
	}
	if e.o.Kube != nil {
		add("kubernetes", "slow", iv.Slow, func(ctx context.Context) error { return e.o.Kube.Refresh(ctx) })
	}
}

func (e *Engine) collectInventory(ctx context.Context) error {
	var errs []error
	type found struct {
		ps   *provState
		devs []gpu.Device
	}
	var results []found
	for _, ps := range e.provs {
		e.mu.Lock()
		open := ps.open
		e.mu.Unlock()
		if !open {
			diag, err := ps.p.Open(ctx)
			e.mu.Lock()
			ps.diag = diag
			if err != nil {
				ps.err = err.Error()
			} else {
				ps.open, ps.err = true, ""
			}
			e.mu.Unlock()
			if err != nil {
				// A missing provider is a normal state, not a collector failure.
				continue
			}
		}
		sys, err := ps.p.System(ctx)
		if err == nil {
			e.mu.Lock()
			ps.system = sys
			e.mu.Unlock()
		}
		devs, err := ps.p.Devices(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ps.p.Name(), err))
			if len(devs) == 0 {
				// Keep previously known devices; mark them via sampling.
				continue
			}
		}
		results = append(results, found{ps, devs})
	}

	e.mu.Lock()
	seen := map[gpu.ID]bool{}
	var order []gpu.ID
	for _, r := range results {
		for _, d := range r.devs {
			seen[d.ID] = true
			entry := e.devices[d.ID]
			if entry == nil {
				entry = &devEntry{prov: r.ps, available: true}
				e.devices[d.ID] = entry
			}
			entry.device = d
			order = append(order, d.ID)
		}
	}
	// Devices that vanished from a provider that answered are removed;
	// devices of a provider that failed to answer are kept (unavailable).
	answered := map[*provState]bool{}
	for _, r := range results {
		answered[r.ps] = true
	}
	for id, d := range e.devices {
		if seen[id] {
			continue
		}
		if answered[d.prov] {
			delete(e.devices, id)
		} else {
			d.available, d.err = false, "provider not responding"
			order = append(order, id)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := e.devices[order[i]].device, e.devices[order[j]].device
		if a.Vendor != b.Vendor {
			return a.Vendor < b.Vendor
		}
		return a.Index < b.Index
	})
	e.order = order
	e.ready = true
	e.mu.Unlock()

	e.collectTopology(ctx)
	return errors.Join(errs...)
}

func (e *Engine) collectTopology(ctx context.Context) {
	e.mu.Lock()
	type pair struct {
		tp   gpu.TopologyProvider
		a, b gpu.ID
	}
	var pairs []pair
	if len(e.order) <= 32 {
		for i := 0; i < len(e.order); i++ {
			for j := i + 1; j < len(e.order); j++ {
				da, db := e.devices[e.order[i]], e.devices[e.order[j]]
				if da.prov != db.prov {
					continue
				}
				if tp, ok := da.prov.p.(gpu.TopologyProvider); ok {
					pairs = append(pairs, pair{tp, e.order[i], e.order[j]})
				}
			}
		}
	}
	e.mu.Unlock()
	edges := make([]model.TopologyEdge, 0, len(pairs))
	for _, p := range pairs {
		if ctx.Err() != nil {
			return
		}
		lvl, err := p.tp.Topology(ctx, p.a, p.b)
		if err != nil {
			lvl = gpu.TopoUnknown
		}
		edges = append(edges, model.TopologyEdge{A: p.a, B: p.b, Level: lvl})
	}
	e.mu.Lock()
	e.topology = edges
	e.mu.Unlock()
}

func (e *Engine) collectSamples(ctx context.Context) error {
	needInventory := false
	var mu sync.Mutex
	err := e.forEachDevice(ctx, "gpu", func(ctx context.Context, id gpu.ID, d *devEntry, v devView) error {
		if !v.open {
			return nil
		}
		s, err := d.prov.p.Sample(ctx, id)
		if caps, ok := d.prov.p.(interface{ Capabilities(gpu.ID) gpu.Capabilities }); ok {
			if c := caps.Capabilities(id); c != nil {
				e.mu.Lock()
				d.device.Capabilities = c
				e.mu.Unlock()
			}
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		switch {
		case err == nil:
			d.sample, d.available, d.err = s, true, ""
			return nil
		case errors.Is(err, gpu.ErrNotFound):
			mu.Lock()
			needInventory = true
			mu.Unlock()
			d.available, d.err = false, "device not found"
		default:
			d.available, d.err = false, err.Error()
		}
		if errors.Is(err, gpu.ErrDeviceLost) {
			return nil // reported as device state and event, not collector failure
		}
		return err
	})
	if needInventory {
		go func() {
			ictx, cancel := context.WithTimeout(context.Background(), e.o.Intervals.Timeout)
			defer cancel()
			<-e.start(ictx, e.collectors["inventory"])
		}()
	}
	return err
}

func (e *Engine) collectHealth(ctx context.Context) error {
	return e.forEachDevice(ctx, "health", func(ctx context.Context, id gpu.ID, d *devEntry, v devView) error {
		if !v.open || !v.available {
			return nil
		}
		hc, err := d.prov.p.Health(ctx, id)
		if !ignorable(err) {
			if errors.Is(err, gpu.ErrDeviceLost) {
				return nil
			}
			return err
		}
		e.mu.Lock()
		d.counters = hc
		e.mu.Unlock()
		return nil
	})
}

func (e *Engine) collectLinks(ctx context.Context) error {
	return e.forEachDevice(ctx, "links", func(ctx context.Context, id gpu.ID, d *devEntry, v devView) error {
		if !v.open || !v.available || v.device.LinkCount == 0 {
			return nil
		}
		links, err := d.prov.p.Links(ctx, id)
		if !ignorable(err) {
			if errors.Is(err, gpu.ErrDeviceLost) {
				return nil
			}
			return err
		}
		e.mu.Lock()
		d.links = links
		e.mu.Unlock()
		return nil
	})
}

func (e *Engine) collectPartitions(ctx context.Context) error {
	return e.forEachDevice(ctx, "partitions", func(ctx context.Context, id gpu.ID, d *devEntry, v devView) error {
		if !v.open || !v.available || !v.device.MIG.Supported {
			return nil
		}
		parts, err := d.prov.p.Partitions(ctx, id)
		if !ignorable(err) {
			if errors.Is(err, gpu.ErrDeviceLost) {
				return nil
			}
			return err
		}
		e.mu.Lock()
		d.parts = parts
		e.mu.Unlock()
		return nil
	})
}

func (e *Engine) collectProcesses(ctx context.Context) error {
	err := e.forEachDevice(ctx, "processes", func(ctx context.Context, id gpu.ID, d *devEntry, v devView) error {
		if !v.open || !v.available {
			return nil
		}
		procs, err := d.prov.p.Processes(ctx, id)
		if !ignorable(err) && !errors.Is(err, gpu.ErrNoPermission) {
			if errors.Is(err, gpu.ErrDeviceLost) {
				return nil
			}
			return err
		}
		enriched := make([]model.Process, 0, len(procs))
		for _, p := range procs {
			enriched = append(enriched, e.enrich(p, v.device))
		}
		e.mu.Lock()
		d.procs = enriched
		e.mu.Unlock()
		return nil
	})
	if e.o.Procs != nil {
		e.o.Procs.Prune(10 * time.Minute)
	}
	return err
}

func (e *Engine) enrich(p gpu.Process, dev gpu.Device) model.Process {
	mp := model.Process{Process: p, DeviceIndex: dev.Index, PartitionIndex: -1}
	if m := p.Meta; m != nil {
		mp.Name, mp.User, mp.Command, mp.StartTime, mp.Visible = m.Name, m.User, m.Command, m.StartTime, true
		mp.Kube = kube.Attribution{
			ContainerRef: kube.ContainerRef{ContainerID: m.ContainerID, PodUID: m.PodUID},
			PodName:      m.PodName, Namespace: m.Namespace, WorkloadKind: m.WorkloadKind, WorkloadName: m.WorkloadName,
		}
		return mp
	}
	if e.o.Procs == nil {
		return mp
	}
	info := e.o.Procs.Lookup(p.PID)
	mp.Name, mp.User, mp.Command, mp.StartTime, mp.Visible, mp.Reason = info.Name, info.User, info.Command, info.StartTime, info.Visible, info.Reason
	if info.Cgroup != "" {
		ref := kube.ParseCgroup(info.Cgroup)
		if e.o.Kube != nil {
			mp.Kube = e.o.Kube.Resolve(ref)
		} else {
			mp.Kube = kube.Attribution{ContainerRef: ref}
		}
	}
	return mp
}

func (e *Engine) collectHostFast(ctx context.Context) error {
	h := e.o.Host
	cpu := h.CPU(ctx)
	mem := h.Memory(ctx)
	info := h.Info(ctx)
	e.mu.Lock()
	e.hostSnap.Time = e.now()
	e.hostSnap.CPU, e.hostSnap.Memory, e.hostSnap.Info = cpu, mem, info
	e.mu.Unlock()
	return nil
}

func (e *Engine) collectNetwork(ctx context.Context) error {
	n := e.o.Host.Network(ctx)
	e.mu.Lock()
	e.hostSnap.Net = n
	e.mu.Unlock()
	return nil
}

func (e *Engine) collectDisks(ctx context.Context) error {
	d := e.o.Host.Disks(ctx)
	e.mu.Lock()
	e.hostSnap.IO = d
	e.mu.Unlock()
	return nil
}

func (e *Engine) collectFilesystems(ctx context.Context) error {
	fs := e.o.Host.Filesystems(ctx)
	e.mu.Lock()
	e.hostSnap.Disks = fs
	e.mu.Unlock()
	return nil
}

// collectInference scrapes configured and discovered inference servers.
func (e *Engine) collectInference(ctx context.Context) error {
	targets := append([]inference.Target(nil), e.o.Endpoints...)
	if e.o.Discover {
		targets = append(targets, inference.Discover(e.inferenceProcs())...)
	}
	return e.o.Inference.Scrape(ctx, targets)
}

// inferenceProcs lists GPU processes with the pod IPs discovery needs to
// reach containerized servers.
func (e *Engine) inferenceProcs() []inference.Proc {
	podIPs := map[string]string{}
	var pods []kube.PodInfo
	if sim := e.kubeSimulator(); sim != nil {
		pods = sim.SimulatedPods()
	} else if e.o.Kube != nil {
		pods = e.o.Kube.Pods(nil)
	}
	for _, p := range pods {
		podIPs[p.UID] = p.PodIP
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []inference.Proc
	for _, id := range e.order {
		for _, p := range e.devices[id].procs {
			if p.Command == "" {
				continue
			}
			out = append(out, inference.Proc{
				PID: p.PID, Command: p.Command, GPU: p.DeviceIndex,
				Containerized: p.Kube.ContainerID != "" || p.Kube.PodUID != "",
				PodIP:         podIPs[p.Kube.PodUID], Pod: p.Kube.PodName,
			})
		}
	}
	return out
}
