// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/derive"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/gpu/sim"
	"github.com/riteshsonawane1372/gputop/internal/history"
	"github.com/riteshsonawane1372/gputop/internal/host"
	"github.com/riteshsonawane1372/gputop/internal/kube"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

func fastIntervals() Intervals {
	return Intervals{Fast: 40 * time.Millisecond, Normal: 80 * time.Millisecond, Slow: 200 * time.Millisecond,
		Inventory: time.Second, Timeout: 500 * time.Millisecond}
}

func runFor(t *testing.T, e *Engine, d time.Duration) *model.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = e.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(d + 5*time.Second):
		t.Fatal("engine did not stop after context cancellation")
	}
	return e.Latest()
}

func TestEngineWithSimulatedGPUs(t *testing.T) {
	store, _, err := history.Open(history.Options{Retention: time.Minute, Resolution: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	e := New(Options{
		Providers: []gpu.Provider{sim.New(sim.Options{GPUs: 8})},
		Intervals: fastIntervals(), Derive: derive.DefaultOptions(),
		Host: host.NewCollector(), History: store, Demo: true,
	})
	sub, cancelSub := e.Subscribe()
	defer cancelSub()

	s := runFor(t, e, 1200*time.Millisecond)
	if !s.Ready || len(s.GPUs) != 8 || s.Schema != model.SchemaVersion || !s.Node.Demo {
		t.Fatalf("snapshot: ready=%v gpus=%d", s.Ready, len(s.GPUs))
	}
	select {
	case got := <-sub:
		if got == nil {
			t.Fatal("nil snapshot from subscription")
		}
	default:
		t.Fatal("subscriber received nothing")
	}
	if len(s.Processes) < 8 {
		t.Fatalf("processes: %d", len(s.Processes))
	}
	for _, p := range s.Processes {
		if p.Name == "" || p.Kube.PodName == "" || p.Meta != nil {
			t.Fatalf("process not enriched: %+v", p)
		}
	}
	if s.Fleet.GPUs != 8 || !s.Fleet.PowerW.OK || s.Fleet.Allocated != 8 {
		t.Fatalf("fleet: %+v", s.Fleet)
	}
	if s.Host == nil || s.Host.Info.OS == "" {
		t.Fatal("host info missing")
	}
	mig := s.GPUs[6]
	if len(mig.Partitions) != 3 {
		t.Fatalf("partitions on MIG GPU: %d", len(mig.Partitions))
	}
	var migProc bool
	for _, p := range s.Processes {
		if p.PartitionID != "" && p.PartitionIndex >= 0 {
			migProc = true
		}
	}
	if !migProc {
		t.Fatal("expected a process attributed to a MIG instance")
	}
	if len(s.GPUs[0].Links) != 18 || len(s.Topology) != 28 {
		t.Fatalf("links=%d topology=%d", len(s.GPUs[0].Links), len(s.Topology))
	}
	for _, c := range s.Collectors {
		if !c.Healthy {
			t.Errorf("collector %s unhealthy: %s", c.Name, c.LastError)
		}
	}
	if s.History.Points == 0 {
		t.Fatal("history received no points")
	}
	if !strings.Contains(s.Events[0].Message, "Discovered") {
		t.Fatalf("first event: %+v", s.Events[0])
	}
	// The straggler GPU in the simulation must show a degraded PCIe width in its health.
	var pcie bool
	for _, r := range s.GPUs[3].Health.Reasons {
		if r.Code == "pcie_width_degraded" {
			pcie = true
		}
	}
	if !pcie {
		t.Fatalf("GPU 3 health reasons: %+v", s.GPUs[3].Health.Reasons)
	}
}

type failingProvider struct{ *sim.Provider }

func (*failingProvider) Name() string { return "broken" }
func (*failingProvider) Open(context.Context) (gpu.Diagnostics, error) {
	return gpu.Diagnostics{Provider: "broken", Checks: []gpu.Check{{Name: "library", OK: false, Detail: "not found"}}},
		gpu.ErrUnavailable
}

func TestEngineWithoutGPUs(t *testing.T) {
	e := New(Options{Providers: []gpu.Provider{&failingProvider{sim.New(sim.Options{})}}, Intervals: fastIntervals()})
	s := runFor(t, e, 300*time.Millisecond)
	if !s.Ready || len(s.GPUs) != 0 {
		t.Fatalf("ready=%v gpus=%d", s.Ready, len(s.GPUs))
	}
	if len(s.Providers) != 1 || s.Providers[0].Available || len(s.Providers[0].Diagnostics.Checks) != 1 {
		t.Fatalf("providers: %+v", s.Providers)
	}
	for _, c := range s.Collectors {
		if !c.Healthy {
			t.Errorf("missing hardware must not mark collector %s unhealthy: %s", c.Name, c.LastError)
		}
	}
}

// flakyProvider loses GPU 1 after lostAfter samples and hangs GPU 2.
type flakyProvider struct {
	*sim.Provider
	samples atomic.Int64
	hang    bool
}

func (f *flakyProvider) Sample(ctx context.Context, id gpu.ID) (gpu.Sample, error) {
	if id == sim.ID(1) && f.samples.Add(1) > 3 {
		return gpu.Sample{}, errors.Join(gpu.ErrDeviceLost, errors.New("fell off the bus"))
	}
	if f.hang && id == sim.ID(2) {
		time.Sleep(700 * time.Millisecond)
	}
	return f.Provider.Sample(ctx, id)
}

func TestEngineDeviceLostAndHangingCalls(t *testing.T) {
	p := &flakyProvider{Provider: sim.New(sim.Options{GPUs: 3}), hang: true}
	e := New(Options{Providers: []gpu.Provider{p}, Intervals: fastIntervals()})
	var published atomic.Int64
	sub, cancel := e.Subscribe()
	defer cancel()
	go func() {
		for range sub {
			published.Add(1)
		}
	}()
	s := runFor(t, e, 1500*time.Millisecond)

	if s.GPUs[1].Available || s.GPUs[1].Health.Score != 0 || s.GPUs[1].Derived.State != model.StateUnavailable {
		t.Fatalf("lost GPU: available=%v score=%d", s.GPUs[1].Available, s.GPUs[1].Health.Score)
	}
	if !s.GPUs[0].Available {
		t.Fatal("healthy GPU must stay available")
	}
	var lostEvent bool
	for _, ev := range s.Events {
		if ev.Kind == "gpu_unavailable" && ev.DeviceIndex == 1 {
			lostEvent = true
		}
	}
	if !lostEvent {
		t.Fatalf("no gpu_unavailable event: %+v", s.Events)
	}
	// Hanging GPU 2 (700ms per call) must not stall publishing: the fast
	// tick waits at most 90% of the interval.
	if n := published.Load(); n < 10 {
		t.Fatalf("only %d snapshots published in 1.5s", n)
	}
	var gpuCol model.CollectorStatus
	for _, c := range s.Collectors {
		if c.Name == "gpu" {
			gpuCol = c
		}
	}
	if gpuCol.Overruns == 0 {
		t.Fatalf("expected overruns for the hanging collector: %+v", gpuCol)
	}
}

func TestOnce(t *testing.T) {
	e := New(Options{Providers: []gpu.Provider{sim.New(sim.Options{GPUs: 2})}, Intervals: fastIntervals(), Host: host.NewCollector()})
	s := e.Once(context.Background(), 50*time.Millisecond)
	if !s.Ready || len(s.GPUs) != 2 || !s.GPUs[0].Sample.UtilPercent.OK || s.Host == nil {
		t.Fatalf("once: %+v", s)
	}
}

func TestEngineDemoKubernetes(t *testing.T) {
	e := New(Options{Providers: []gpu.Provider{sim.New(sim.Options{GPUs: 8})}, Intervals: fastIntervals(), Demo: true})
	s := e.Once(context.Background(), 500*time.Millisecond)
	k := s.Kubernetes
	if !k.Inspect || len(k.Pods) < 8 {
		t.Fatalf("demo pods: inspect=%v pods=%d", k.Inspect, len(k.Pods))
	}
	var pending, worker kube.PodInfo
	for _, p := range k.Pods {
		switch p.Name {
		case "llama-70b-eval-0":
			pending = p
		case "llama-70b-pretrain-worker-3":
			worker = p
		}
	}
	if pending.Status() != "Pending" || pending.GPURequests != 4 || worker.Restarts() == 0 {
		t.Fatalf("pending=%+v worker=%+v", pending, worker)
	}
	ref := kube.PodRef{UID: worker.UID, Name: worker.Name, Namespace: worker.Namespace}
	lines, source, err := e.PodLogs(context.Background(), ref, "trainer", 20)
	if err != nil || source != "simulated" || len(lines) != 20 {
		t.Fatalf("logs: %d %s %v", len(lines), source, err)
	}
	evs, err := e.PodEvents(context.Background(), ref)
	if err != nil || len(evs) == 0 {
		t.Fatalf("events: %v %v", evs, err)
	}

	real := New(Options{Providers: []gpu.Provider{sim.New(sim.Options{GPUs: 1})}, Intervals: fastIntervals()})
	if _, _, err := real.PodLogs(context.Background(), ref, "trainer", 1); !errors.Is(err, kube.ErrNoInspect) {
		t.Fatalf("non-demo without kubernetes: %v", err)
	}
}
