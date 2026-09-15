// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package apple

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/metric"
)

type fakeBackend struct {
	openErr error
	info    staticInfo
	read    reading
	clients []client
	closed  bool
}

func (f *fakeBackend) Open() (staticInfo, []gpu.Check, error) {
	return f.info, []gpu.Check{{Name: "GPU", OK: f.openErr == nil}}, f.openErr
}
func (f *fakeBackend) Read() reading     { return f.read }
func (f *fakeBackend) Clients() []client { return f.clients }
func (f *fakeBackend) Close()            { f.closed = true }

func newFake(t *testing.T, fb *fakeBackend, now *time.Time) *Provider {
	t.Helper()
	p := New(Options{Backend: func() backend { return fb }, Now: func() time.Time { return *now }})
	if _, err := p.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProviderInventoryAndSample(t *testing.T) {
	now := time.Unix(1000, 0)
	fb := &fakeBackend{
		info: staticInfo{Model: "Apple M4", Cores: 10, RegistryID: 0xabc, Architecture: "AGX G16G", Driver: "340.26.3", OSVersion: "26.0", MemTotal: 16 << 30, MaxFreqMHz: 1578},
		read: reading{
			UtilPercent: metric.Some(42.0), MemInUse: metric.Some(uint64(2 << 30)), MemAlloc: metric.Some(uint64(3 << 30)),
			EnergyJ: metric.Some(5.0), Interval: 500 * time.Millisecond, FreqMHz: metric.Some(900.0), TempC: metric.Some(51.5),
		},
	}
	p := newFake(t, fb, &now)
	ctx := context.Background()

	sys, err := p.System(ctx)
	if err != nil || sys.RuntimeName != "macOS" || sys.DriverVersion != "340.26.3" {
		t.Fatalf("system = %+v, %v", sys, err)
	}
	devs, err := p.Devices(ctx)
	if err != nil || len(devs) != 1 {
		t.Fatalf("devices = %v, %v", devs, err)
	}
	d := devs[0]
	if d.Vendor != gpu.VendorApple || d.Name != "Apple M4 10-core GPU" || d.ID != "apple-gpu-abc" || d.Memory.V != 16<<30 || d.ClockCoreMaxMHz.V != 1578 {
		t.Fatalf("device = %+v", d)
	}
	if d.PCI.BusID != "" || d.LinkCount != 0 || d.MIG.Supported {
		t.Fatalf("integrated GPU must not report PCIe/NVLink/MIG: %+v", d)
	}

	s, err := p.Sample(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.UtilPercent.V != 42 || s.MemUsed.V != 2<<30 || s.MemTotal.V != 16<<30 || s.TempC.V != 51.5 || s.ClockCoreMHz.V != 900 {
		t.Fatalf("sample = %+v", s)
	}
	if math.Abs(s.PowerW.V-10) > 1e-9 || s.EnergyJ.V != 5 {
		t.Fatalf("power %v energy %v, want 10 W and 5 J", s.PowerW, s.EnergyJ)
	}
	if s, _ = p.Sample(ctx, d.ID); s.EnergyJ.V != 10 {
		t.Fatalf("energy must accumulate, got %v", s.EnergyJ)
	}
	if s.PowerLimitW.OK || s.Throttle.OK || s.PCIeGen.OK {
		t.Fatalf("unsupported metrics must be N/A: %+v", s)
	}

	if _, err := p.Sample(ctx, "GPU-other"); !errors.Is(err, gpu.ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := p.Health(ctx, d.ID); !errors.Is(err, gpu.ErrNotSupported) {
		t.Fatalf("health: %v", err)
	}
	if err := p.Close(); err != nil || !fb.closed {
		t.Fatal("close did not release the backend")
	}
	if _, err := p.Devices(ctx); !errors.Is(err, gpu.ErrUnavailable) {
		t.Fatalf("after close: %v", err)
	}
}

func TestProviderProcessUtilization(t *testing.T) {
	now := time.Unix(1000, 0)
	fb := &fakeBackend{info: staticInfo{Model: "Apple M1", RegistryID: 1}}
	p := newFake(t, fb, &now)
	ctx := context.Background()
	id := deviceID(fb.info)

	fb.clients = []client{{PID: 10, GPUTime: time.Second}, {PID: 11, GPUTime: 0}}
	procs, err := p.Processes(ctx, id)
	if err != nil || len(procs) != 1 || procs[0].PID != 10 || procs[0].SMUtil.OK {
		t.Fatalf("first call: %+v, %v (idle clients hidden, no rate yet)", procs, err)
	}

	now = now.Add(2 * time.Second)
	fb.clients = []client{{PID: 10, GPUTime: 1500 * time.Millisecond}, {PID: 11, GPUTime: 4 * time.Second}}
	procs, _ = p.Processes(ctx, id)
	util := map[int]metric.Opt[float64]{}
	for _, pr := range procs {
		util[pr.PID] = pr.SMUtil
	}
	if u := util[10]; !u.OK || math.Abs(u.V-25) > 1e-9 {
		t.Fatalf("pid 10 util = %v, want 25%%", u)
	}
	if u := util[11]; !u.OK || u.V != 100 {
		t.Fatalf("pid 11 util = %v, want clamped 100%% (0 -> 4s in 2s)", u)
	}
}

func TestProviderOpenFailure(t *testing.T) {
	fb := &fakeBackend{openErr: errors.New("no Apple silicon GPU found")}
	p := New(Options{Backend: func() backend { return fb }})
	diag, err := p.Open(context.Background())
	if !errors.Is(err, gpu.ErrUnavailable) || len(diag.Hints) == 0 || !fb.closed {
		t.Fatalf("open failure: diag=%+v err=%v closed=%v", diag, err, fb.closed)
	}
}

func TestParseCreator(t *testing.T) {
	for in, want := range map[string]int{"pid 407, WindowServer": 407, "pid 1, launchd": 1, "WindowServer": 0, "pid x, y": 0, "pid 0, kernel": 0} {
		pid, ok := parseCreator(in)
		if pid != want || ok != (want > 0) {
			t.Errorf("parseCreator(%q) = %d, %v", in, pid, ok)
		}
	}
}

func TestResidency(t *testing.T) {
	freqs := []float64{400, 800, 1200}
	states := []stateResidency{{"OFF", 50}, {"P1", 25}, {"P2", 0}, {"P3", 25}}
	active, freq := residency(states, freqs)
	if active.V != 50 || freq.V != 800 {
		t.Fatalf("active %v freq %v, want 50%% and 800 MHz", active, freq)
	}
	if active, freq = residency([]stateResidency{{"OFF", 10}, {"P1", 0}}, freqs); active.V != 0 || !freq.OK || freq.V != 0 {
		t.Fatalf("idle GPU: active %v freq %v", active, freq)
	}
	if active, _ = residency(nil, freqs); active.OK {
		t.Fatal("no residency data must be N/A")
	}
}

func TestUnitConversions(t *testing.T) {
	for _, c := range []struct {
		v    int64
		unit string
		want float64
	}{{1500, "mJ", 1.5}, {2e6, "uJ", 2}, {3e9, "nJ", 3}} {
		if got, ok := energyJoules(c.v, c.unit); !ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("energyJoules(%d, %s) = %v", c.v, c.unit, got)
		}
	}
	if _, ok := energyJoules(1, "W"); ok {
		t.Error("unknown unit accepted")
	}
	if got := scaleFrequencies([]uint32{338000000, 1578000000}); got[0] != 338 || got[1] != 1578 {
		t.Errorf("Hz table scaled to %v", got)
	}
	if got := scaleFrequencies([]uint32{338000, 1578000}); got[0] != 338 || got[1] != 1578 {
		t.Errorf("kHz table scaled to %v", got)
	}
	if got := averageTemp([]float64{40, 50, 0, 400}); got.V != 45 {
		t.Errorf("averageTemp ignores implausible sensors: %v", got)
	}
	if got := averageTemp(nil); got.OK {
		t.Error("no sensors must be N/A")
	}
}
