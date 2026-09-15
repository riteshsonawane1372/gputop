// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package collector schedules telemetry collection and publishes
// immutable snapshots.
//
//	providers/host/kubernetes ──► collectors (tiered, independent)
//	                                   │ shared state (mutex)
//	                                   ▼
//	                      publisher (fast tick) ──► derive ──► events/alerts
//	                                   │                         │
//	                                   ├──► history store ◄──────┘
//	                                   └──► subscribers (TUI, API, JSON)
//
// Every collector has its own interval, timeout and health status. A slow
// or failing collector never blocks the others or the UI: calls that exceed
// their timeout are reported as overruns and are not started again until
// they return.
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gputop/gputop/internal/buildinfo"
	"github.com/gputop/gputop/internal/derive"
	"github.com/gputop/gputop/internal/events"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/health"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/host"
	"github.com/gputop/gputop/internal/kube"
	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/procinfo"
)

// Intervals are the collection tiers.
type Intervals struct {
	Fast      time.Duration
	Normal    time.Duration
	Slow      time.Duration
	Inventory time.Duration
	Timeout   time.Duration
}

// Options configure an Engine.
type Options struct {
	Providers []gpu.Provider
	Intervals Intervals
	Derive    derive.Options

	Host    *host.Collector    // nil disables host metrics
	Procs   *procinfo.Resolver // nil disables process metadata
	Kube    *kube.Correlator   // nil disables pod resolution
	KubeEnv kube.Environment
	History *history.Store // nil disables history
	Events  *events.Log
	Demo    bool
	Log     *slog.Logger
	Now     func() time.Time
	// Workers bounds concurrent per-device provider calls (default 4).
	Workers int
}

// Engine runs collectors and publishes snapshots. Create with New.
type Engine struct {
	o   Options
	log *slog.Logger
	now func() time.Time

	mu         sync.Mutex
	provs      []*provState
	devices    map[gpu.ID]*devEntry
	order      []gpu.ID
	hostSnap   host.Snapshot
	topology   []model.TopologyEdge
	xids       map[gpu.ID][]health.XID
	pending    []model.Event
	ready      bool
	collectors map[string]*collector
	devBusy    sync.Map // collector/device -> *atomic.Bool

	latest  atomic.Pointer[model.Snapshot]
	seq     uint64
	tracker *derive.Tracker
	detect  *events.Detector

	subMu sync.Mutex
	subs  map[chan *model.Snapshot]struct{}

	refresh     chan struct{}
	started     time.Time
	cpuPrev     cpuSample
	lastCollect time.Duration
}

type provState struct {
	p      gpu.Provider
	open   bool
	diag   gpu.Diagnostics
	err    string
	system gpu.SystemInfo
}

type devEntry struct {
	prov      *provState
	device    gpu.Device
	sample    gpu.Sample
	available bool
	err       string
	counters  gpu.HealthCounters
	links     []gpu.Link
	parts     []gpu.Partition
	procs     []model.Process
}

// New creates an engine.
func New(o Options) *Engine {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Events == nil {
		o.Events = events.NewLog(2000)
	}
	if o.Workers <= 0 {
		o.Workers = 4
	}
	iv := &o.Intervals
	if iv.Fast <= 0 {
		iv.Fast = time.Second
	}
	if iv.Normal < iv.Fast {
		iv.Normal = 3 * iv.Fast
	}
	if iv.Slow < iv.Normal {
		iv.Slow = 10 * iv.Normal
	}
	if iv.Inventory < iv.Slow {
		iv.Inventory = 10 * iv.Slow
	}
	if iv.Timeout <= 0 {
		iv.Timeout = 5 * time.Second
	}
	e := &Engine{
		o: o, log: o.Log, now: o.Now,
		devices: map[gpu.ID]*devEntry{}, xids: map[gpu.ID][]health.XID{},
		collectors: map[string]*collector{}, subs: map[chan *model.Snapshot]struct{}{},
		tracker: derive.NewTracker(o.Derive), detect: events.NewDetector(),
		refresh: make(chan struct{}, 1), started: o.Now(),
	}
	for _, p := range o.Providers {
		e.provs = append(e.provs, &provState{p: p})
	}
	e.addCollectors()
	return e
}

// Latest returns the most recent snapshot (never nil).
func (e *Engine) Latest() *model.Snapshot {
	if s := e.latest.Load(); s != nil {
		return s
	}
	return &model.Snapshot{Schema: model.SchemaVersion, Time: e.now(), Node: e.nodeInfo()}
}

// Subscribe returns a channel receiving each new snapshot. Slow receivers
// only ever miss intermediate snapshots, never the latest one.
func (e *Engine) Subscribe() (<-chan *model.Snapshot, func()) {
	ch := make(chan *model.Snapshot, 1)
	e.subMu.Lock()
	e.subs[ch] = struct{}{}
	e.subMu.Unlock()
	return ch, func() {
		e.subMu.Lock()
		delete(e.subs, ch)
		e.subMu.Unlock()
	}
}

// RefreshNow requests an immediate collection pass.
func (e *Engine) RefreshNow() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

// History returns the history reader or nil.
func (e *Engine) History() history.Reader {
	if e.o.History == nil {
		return nil
	}
	return e.o.History
}

func (e *Engine) nodeInfo() model.Node {
	v, _, _ := buildinfo.Info()
	e.mu.Lock()
	hn := e.hostSnap.Info.Hostname
	e.mu.Unlock()
	return model.Node{Hostname: hn, Source: "local", Demo: e.o.Demo, Version: v}
}

// Run collects until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	iv := e.o.Intervals

	// Inventory first so the other tiers have devices to work with.
	<-e.start(ctx, e.collectors["inventory"])
	e.runNow(ctx, "health", "links", "processes", "partitions", "network", "disks", "filesystems", "kubernetes")

	var wg sync.WaitGroup
	spawn := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	spawn(func() { e.tierLoop(ctx, iv.Inventory, "inventory") })
	spawn(func() { e.tierLoop(ctx, iv.Normal, "health", "links", "processes", "network", "disks") })
	spawn(func() { e.tierLoop(ctx, iv.Slow, "partitions", "filesystems", "kubernetes") })
	spawn(func() { e.watchEvents(ctx) })

	fast := time.NewTicker(iv.Fast)
	defer fast.Stop()
	e.fastPass(ctx)
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-fast.C:
			e.fastPass(ctx)
		case <-e.refresh:
			e.runNow(ctx, "health", "links", "processes", "network", "disks")
			e.fastPass(ctx)
			fast.Reset(iv.Fast)
		}
	}
}

// Once performs a complete collection, waits settle for rate-based metrics
// and returns a single snapshot. Used by --once.
func (e *Engine) Once(ctx context.Context, settle time.Duration) *model.Snapshot {
	<-e.start(ctx, e.collectors["inventory"])
	e.runNow(ctx, "health", "links", "processes", "partitions", "network", "disks", "filesystems", "kubernetes")
	e.fastPass(ctx)
	if settle > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(settle):
		}
		e.runNow(ctx, "links", "network", "disks")
		e.fastPass(ctx)
	}
	return e.Latest()
}

func (e *Engine) tierLoop(ctx context.Context, every time.Duration, names ...string) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, n := range names {
				if c := e.collectors[n]; c != nil {
					e.start(ctx, c)
				}
			}
		}
	}
}

// runNow starts collectors and waits for them (bounded by the timeout).
func (e *Engine) runNow(ctx context.Context, names ...string) {
	var chans []<-chan struct{}
	for _, n := range names {
		if c := e.collectors[n]; c != nil {
			chans = append(chans, e.start(ctx, c))
		}
	}
	waitAll(ctx, chans, e.o.Intervals.Timeout)
}

func waitAll(ctx context.Context, chans []<-chan struct{}, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for _, ch := range chans {
		select {
		case <-ch:
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) fastPass(ctx context.Context) {
	start := time.Now()
	// Wait at most most of an interval so the UI keeps its cadence even if
	// a device call hangs.
	wait := min(e.o.Intervals.Timeout, e.o.Intervals.Fast*9/10)
	waitAll(ctx, []<-chan struct{}{e.start(ctx, e.collectors["gpu"]), e.start(ctx, e.collectors["host"])}, wait)
	e.lastCollect = time.Since(start)
	e.publish()
}

// devView is a consistent copy of the fields collectors decide on.
type devView struct {
	open      bool
	available bool
	device    gpu.Device
}

// forEachDevice runs f for every device with bounded concurrency. The view
// is captured under the lock; f must lock e.mu to write into d.
//
// Calls are tracked per (collector, device): a device whose previous call
// is still running is skipped, and forEachDevice returns when ctx expires
// even if some calls are stuck, so one hung GPU cannot stall the others.
func (e *Engine) forEachDevice(ctx context.Context, name string, f func(ctx context.Context, id gpu.ID, d *devEntry, v devView) error) error {
	e.mu.Lock()
	ids := append([]gpu.ID(nil), e.order...)
	entries := make([]*devEntry, len(ids))
	views := make([]devView, len(ids))
	for i, id := range ids {
		d := e.devices[id]
		entries[i] = d
		views[i] = devView{open: d.prov.open, available: d.available, device: d.device}
	}
	e.mu.Unlock()

	sem := make(chan struct{}, e.o.Workers)
	var (
		errMu   sync.Mutex
		errs    []error
		pending = make(chan struct{}, len(ids))
		started int
		stuck   []string
	)
	for i := range ids {
		key := name + "/" + string(ids[i])
		busy, _ := e.devBusy.LoadOrStore(key, new(atomic.Bool))
		flag := busy.(*atomic.Bool)
		if !flag.CompareAndSwap(false, true) {
			stuck = append(stuck, string(ids[i]))
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			flag.Store(false)
			return ctx.Err()
		}
		started++
		go func(id gpu.ID, d *devEntry, v devView) {
			defer func() { flag.Store(false); <-sem; pending <- struct{}{} }()
			if err := f(ctx, id, d, v); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", id, err))
				errMu.Unlock()
			}
		}(ids[i], entries[i], views[i])
	}
	for done := 0; done < started; {
		select {
		case <-pending:
			done++
		case <-ctx.Done():
			return fmt.Errorf("%d of %d device call(s) exceeded the collection timeout", started-done, len(ids))
		}
	}
	errMu.Lock()
	defer errMu.Unlock()
	if len(stuck) > 0 {
		errs = append(errs, fmt.Errorf("previous call still running for %d device(s)", len(stuck)))
	}
	return errors.Join(errs...)
}

// ignorable reports errors that mean "feature absent", not failure.
func ignorable(err error) bool {
	return err == nil || errors.Is(err, gpu.ErrNotSupported)
}

func (e *Engine) publish() {
	now := e.now()
	e.mu.Lock()
	e.seq++
	s := &model.Snapshot{
		Schema: model.SchemaVersion, Seq: e.seq, Time: now, Ready: e.ready,
		Topology: append([]model.TopologyEdge(nil), e.topology...),
	}
	for _, ps := range e.provs {
		s.Providers = append(s.Providers, model.ProviderStatus{
			Name: ps.p.Name(), Vendor: ps.p.Vendor(), Available: ps.open, Error: ps.err,
			System: ps.system, Diagnostics: ps.diag,
		})
	}
	partIndex := map[gpu.ID]int{}
	for _, id := range e.order {
		d := e.devices[id]
		available, errText := d.available, d.err
		if stale := max(3*e.o.Intervals.Fast, 2*e.o.Intervals.Timeout); available && !d.sample.Time.IsZero() && now.Sub(d.sample.Time) > stale {
			available, errText = false, fmt.Sprintf("not responding (no sample for %s)", now.Sub(d.sample.Time).Round(time.Second))
		}
		g := model.GPU{
			Device: d.device, Provider: d.prov.p.Name(), Available: available, Error: errText,
			Sample: d.sample, Counters: d.counters,
			Links:      append([]gpu.Link(nil), d.links...),
			Partitions: append([]gpu.Partition(nil), d.parts...),
			Processes:  len(d.procs),
		}
		g.Device.Capabilities = d.device.Capabilities.Clone()
		for _, pt := range d.parts {
			partIndex[pt.ID] = pt.Index
		}
		s.GPUs = append(s.GPUs, g)
		s.Processes = append(s.Processes, d.procs...)
	}
	if e.o.Host != nil {
		h := e.hostSnap
		h.Disks = append([]host.Filesystem(nil), h.Disks...)
		h.IO = append([]host.BlockDevice(nil), h.IO...)
		h.Net = append([]host.NetIf(nil), h.Net...)
		h.CPU.PerCore = append([]float64(nil), h.CPU.PerCore...)
		s.Host = &h
	}
	xids := make(map[gpu.ID][]health.XID, len(e.xids))
	cutoff := now.Add(-time.Hour)
	for id, list := range e.xids {
		kept := list[:0]
		for _, x := range list {
			if x.Time.After(cutoff) {
				kept = append(kept, x)
			}
		}
		e.xids[id] = kept
		xids[id] = append([]health.XID(nil), kept...)
	}
	pending := e.pending
	e.pending = nil
	s.Collectors = e.collectorStatusLocked()
	e.mu.Unlock()

	for i := range s.Processes {
		p := &s.Processes[i]
		p.Meta = nil
		if p.PartitionID != "" {
			p.PartitionIndex = partIndex[p.PartitionID]
		}
	}
	sort.SliceStable(s.Processes, func(i, j int) bool {
		if s.Processes[i].DeviceIndex != s.Processes[j].DeviceIndex {
			return s.Processes[i].DeviceIndex < s.Processes[j].DeviceIndex
		}
		return s.Processes[i].PID < s.Processes[j].PID
	})
	e.updateNVLinkTopology(s)

	switch sim := e.kubeSimulator(); {
	case e.o.Kube != nil:
		s.Kubernetes = e.o.Kube.Status()
		using := map[string]bool{}
		for _, p := range s.Processes {
			if p.Kube.PodUID != "" {
				using[p.Kube.PodUID] = true
			}
		}
		s.Kubernetes.Pods = e.o.Kube.Pods(using)
	case sim != nil:
		s.Kubernetes = kube.Status{Environment: kube.Environment{Mode: kube.ModeInCluster, NodeName: simNode}, APIEnabled: true, Inspect: true}
		s.Kubernetes.Pods = sim.SimulatedPods()
		s.Kubernetes.PodsKnown = len(s.Kubernetes.Pods)
		s.Kubernetes.LastRefresh = now
	default:
		s.Kubernetes = kube.Status{Environment: e.o.KubeEnv}
	}

	e.tracker.Apply(s, xids)

	prev := e.latest.Load()
	evs := append(pending, e.detect.Diff(prev, s)...)
	if len(evs) > 0 {
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].Time.Before(evs[j].Time) })
		e.o.Events.Append(evs...)
	}
	s.Alerts = e.detect.Alerts(s)
	s.Events = e.o.Events.Recent(200)

	if e.o.History != nil {
		e.o.History.Observe(s)
		e.o.History.AppendEvents(evs)
		s.History = e.o.History.Status()
	}
	s.Node = e.nodeInfo()
	s.Self = e.selfStats()

	e.latest.Store(s)
	e.subMu.Lock()
	for ch := range e.subs {
		select {
		case ch <- s:
		default:
			select { // drop the stale snapshot, deliver the latest
			case <-ch:
			default:
			}
			select {
			case ch <- s:
			default:
			}
		}
	}
	e.subMu.Unlock()
}

// updateNVLinkTopology counts links between local devices.
func (e *Engine) updateNVLinkTopology(s *model.Snapshot) {
	byBus := map[string]gpu.ID{}
	for _, g := range s.GPUs {
		if g.Device.PCI.BusID != "" {
			byBus[g.Device.PCI.BusID] = g.Device.ID
		}
	}
	counts := map[[2]gpu.ID]int{}
	for gi := range s.GPUs {
		g := &s.GPUs[gi]
		for li := range g.Links {
			l := &g.Links[li]
			if id, ok := byBus[l.RemoteBusID]; ok {
				l.RemoteID = id
				if l.State == gpu.LinkActive {
					a, b := g.Device.ID, id
					if b < a {
						a, b = b, a
					}
					counts[[2]gpu.ID{a, b}]++
				}
			}
		}
	}
	for i := range s.Topology {
		t := &s.Topology[i]
		a, b := t.A, t.B
		if b < a {
			a, b = b, a
		}
		// Each link is seen from both ends.
		t.NVLinks = (counts[[2]gpu.ID{a, b}] + 1) / 2
	}
}

func (e *Engine) selfStats() model.SelfStats {
	st := model.SelfStats{StartTime: e.started, Goroutines: runtime.NumGoroutine(), CollectTime: e.lastCollect}
	st.HeapBytes, st.SysBytes = memStats()
	if cur, ok := readCPU(); ok {
		if !e.cpuPrev.at.IsZero() {
			wall := cur.at.Sub(e.cpuPrev.at)
			if wall > 0 {
				st.CPUPercent = metric.Some(100 * float64(cur.cpu-e.cpuPrev.cpu) / float64(wall))
			}
		}
		e.cpuPrev = cur
	}
	return st
}

// handleDeviceEvent converts provider events to timeline events.
func (e *Engine) handleDeviceEvent(ps *provState, de gpu.DeviceEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	idx := -1
	name := ""
	if d, ok := e.devices[de.DeviceID]; ok {
		idx, name = d.device.Index, d.device.Name
	}
	sev := model.Severity(de.Severity)
	if sev == "" {
		sev = model.SevWarning
	}
	src := metric.SourceNVML
	if ps.p.Vendor() != gpu.VendorNVIDIA {
		src = metric.Source(ps.p.Name())
	}
	ev := model.Event{Time: de.Time, Kind: de.Kind, Severity: sev, DeviceID: de.DeviceID, DeviceIndex: idx, Source: src,
		Attrs: map[string]string{}}
	switch de.Kind {
	case "xid":
		ev.Message = fmt.Sprintf("Xid %d: %s", de.Code, de.Detail)
		ev.Attrs["xid"] = fmt.Sprint(de.Code)
		e.xids[de.DeviceID] = append(e.xids[de.DeviceID], health.XID{Code: de.Code, Severity: string(sev), Time: de.Time})
	case "partition_config_change":
		ev.Message = "MIG configuration changed"
		e.RefreshPartitionsLater()
	default:
		ev.Message = de.Kind
		if de.Detail != "" {
			ev.Message += ": " + de.Detail
		}
	}
	if name != "" {
		ev.Attrs["gpu_name"] = name
	}
	e.pending = append(e.pending, ev)
}

// RefreshPartitionsLater schedules a partition refresh (called with e.mu held).
func (e *Engine) RefreshPartitionsLater() {
	if c := e.collectors["partitions"]; c != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), e.o.Intervals.Timeout)
			defer cancel()
			<-e.start(ctx, c)
		}()
	}
}

func (e *Engine) watchEvents(ctx context.Context) {
	var wg sync.WaitGroup
	for _, ps := range e.provs {
		src, ok := ps.p.(gpu.EventSource)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(ps *provState, src gpu.EventSource) {
			defer wg.Done()
			backoff := time.Second
			for ctx.Err() == nil {
				e.mu.Lock()
				open := ps.open && len(e.order) > 0
				e.mu.Unlock()
				if open {
					err := src.WatchEvents(ctx, func(de gpu.DeviceEvent) { e.handleDeviceEvent(ps, de) })
					if ctx.Err() != nil {
						return
					}
					if errors.Is(err, gpu.ErrNotSupported) {
						e.log.Info("event source not supported", "provider", ps.p.Name(), "err", err)
						return
					}
					if err != nil {
						e.log.Warn("event source failed", "provider", ps.p.Name(), "err", err)
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, time.Minute)
			}
		}(ps, src)
	}
	wg.Wait()
}

// simNode is the node name reported for simulated pods.
const simNode = "gpu-node-01"

// kubeSimulator returns the demo provider's pod simulator, if any.
func (e *Engine) kubeSimulator() kube.Simulator {
	if !e.o.Demo {
		return nil
	}
	for _, ps := range e.provs {
		if sim, ok := ps.p.(kube.Simulator); ok {
			return sim
		}
	}
	return nil
}

// PodLogs returns the tail of a pod container's log and where it came from.
func (e *Engine) PodLogs(ctx context.Context, pod kube.PodRef, container string, tail int) ([]string, string, error) {
	if sim := e.kubeSimulator(); sim != nil {
		return sim.SimulatedLogs(pod, container, tail), "simulated", nil
	}
	if e.o.Kube == nil {
		return nil, "", kube.ErrNoInspect
	}
	return e.o.Kube.PodLogs(ctx, pod, container, tail)
}

// PodEvents lists a pod's Kubernetes events.
func (e *Engine) PodEvents(ctx context.Context, pod kube.PodRef) ([]kube.PodEvent, error) {
	if sim := e.kubeSimulator(); sim != nil {
		return sim.SimulatedEvents(pod), nil
	}
	if e.o.Kube == nil {
		return nil, kube.ErrNoInspect
	}
	return e.o.Kube.PodEvents(ctx, pod)
}
