// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package derive

import (
	"fmt"
	"testing"
	"time"

	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
)

var t0 = time.Unix(1_800_000_000, 0)

func mk(i int, util float64, procs int) model.GPU {
	return model.GPU{
		Device:    gpu.Device{ID: gpu.ID(fmt.Sprintf("GPU-%d", i)), Index: i},
		Available: true, Processes: procs,
		Sample: gpu.Sample{
			UtilPercent: metric.Some(util), MemBandwidthPercent: metric.Some(util / 2),
			MemTotal: metric.Some(uint64(80 << 30)), MemUsed: metric.Some(uint64(40 << 30)),
			PowerW: metric.Some(300.0), PowerLimitW: metric.Some(600.0), TempC: metric.Some(60.0),
			Throttle: metric.Some(gpu.ThrottleReasons(0)),
		},
	}
}

func proc(gpuIdx int, workload string) model.Process {
	p := model.Process{Process: gpu.Process{PID: 100 + gpuIdx, DeviceID: gpu.ID(fmt.Sprintf("GPU-%d", gpuIdx))}}
	p.Kube.Namespace, p.Kube.WorkloadKind, p.Kube.WorkloadName = "ml", "Job", workload
	return p
}

func run(tr *Tracker, seconds int, build func() *model.Snapshot) *model.Snapshot {
	var s *model.Snapshot
	for i := 0; i <= seconds; i++ {
		s = build()
		s.Time = t0.Add(time.Duration(i) * time.Second)
		tr.Apply(s, nil)
	}
	return s
}

func TestStatesFleetAndIdleAllocated(t *testing.T) {
	o := DefaultOptions()
	o.IdleAfter = 30 * time.Second
	tr := NewTracker(o)
	s := run(tr, 90, func() *model.Snapshot {
		lost := mk(3, 0, 0)
		lost.Available = false
		lost.Sample = gpu.Sample{}
		return &model.Snapshot{GPUs: []model.GPU{mk(0, 95, 1), mk(1, 30, 1), mk(2, 1, 1), lost}}
	})
	g := s.GPUs
	if g[0].Derived.State != model.StateBusy || g[1].Derived.State != model.StateActive || g[2].Derived.State != model.StateIdle || g[3].Derived.State != model.StateUnavailable {
		t.Fatalf("states: %s %s %s %s", g[0].Derived.State, g[1].Derived.State, g[2].Derived.State, g[3].Derived.State)
	}
	if !g[2].Derived.IdleAllocated || g[1].Derived.IdleAllocated {
		t.Fatalf("idle allocated: %+v", g[2].Derived)
	}
	f := s.Fleet
	if f.GPUs != 4 || f.Unavailable != 1 || f.Busy != 1 || f.Active != 2 || f.Idle != 1 || f.IdleAllocated != 1 || f.Allocated != 3 {
		t.Fatalf("fleet: %+v", f)
	}
	if f.PowerW.V != 900 || f.VRAMFraction.V != 0.5 || f.HealthMin.V != 0 {
		t.Fatalf("fleet aggregates: %+v", f)
	}
	// Unused allocated capacity: (1-.95)+(1-.30)+(1-.01) = 1.74
	if !f.UnusedAllocated.OK || f.UnusedAllocated.V < 1.73 || f.UnusedAllocated.V > 1.75 {
		t.Fatalf("unused: %+v", f.UnusedAllocated)
	}
}

func TestEfficiencyRequiresCoverageAndAllocation(t *testing.T) {
	tr := NewTracker(DefaultOptions())
	s := run(tr, 30, func() *model.Snapshot { return &model.Snapshot{GPUs: []model.GPU{mk(0, 90, 1), mk(1, 90, 0)}} })
	if s.GPUs[0].Derived.Efficiency.Score.OK {
		t.Fatal("efficiency needs a minute of samples")
	}
	s = run(tr, 90, func() *model.Snapshot { return &model.Snapshot{GPUs: []model.GPU{mk(0, 90, 1), mk(1, 90, 0)}} })
	e := s.GPUs[0].Derived.Efficiency
	// 0.6*90 + 0.25*45 + 0.15*50 = 72.75 -> 75 (5-point steps)
	if !e.Score.OK || e.Score.V != 75 || e.Grade != "moderate" {
		t.Fatalf("efficiency: %+v", e)
	}
	if s.GPUs[1].Derived.Efficiency.Score.OK {
		t.Fatal("unallocated GPU has no efficiency score")
	}
}

func TestStragglerOutlier(t *testing.T) {
	tr := NewTracker(DefaultOptions())
	s := run(tr, 10, func() *model.Snapshot {
		utils := []float64{97, 96, 98, 51, 95, 97}
		snap := &model.Snapshot{}
		for i, u := range utils {
			snap.GPUs = append(snap.GPUs, mk(i, u, 1))
			snap.Processes = append(snap.Processes, proc(i, "llama"))
		}
		// An unrelated GPU must not join the cohort.
		snap.GPUs = append(snap.GPUs, mk(6, 20, 1))
		snap.Processes = append(snap.Processes, proc(6, "notebook"))
		return snap
	})
	im := s.Fleet.Imbalance
	if !im.Valid || im.Members != 6 || im.Cohort != "ml/llama" || len(im.Outliers) != 1 || im.Outliers[0] != "GPU-3" {
		t.Fatalf("imbalance: %+v", im)
	}
	if !s.GPUs[3].Derived.Outlier || s.GPUs[0].Derived.Outlier || s.GPUs[6].Derived.Outlier {
		t.Fatal("outlier flags")
	}
	if im.Slowest != "GPU-3" || im.Spread != 47 {
		t.Fatalf("spread/slowest: %+v", im)
	}
}

func TestNoFalseOutliersWhenBalancedOrLowLoad(t *testing.T) {
	tr := NewTracker(DefaultOptions())
	s := run(tr, 5, func() *model.Snapshot {
		snap := &model.Snapshot{}
		for i, u := range []float64{20, 12, 25, 8} { // low load: median < 30
			snap.GPUs = append(snap.GPUs, mk(i, u, 1))
			snap.Processes = append(snap.Processes, proc(i, "x"))
		}
		return snap
	})
	if len(s.Fleet.Imbalance.Outliers) != 0 {
		t.Fatalf("low-load cohorts must not produce outliers: %+v", s.Fleet.Imbalance)
	}
}

func TestLinkRatesAndHealthApplied(t *testing.T) {
	tr := NewTracker(DefaultOptions())
	var tx uint64
	s := run(tr, 3, func() *model.Snapshot {
		tx += 1000
		g := mk(0, 50, 1)
		g.Links = []gpu.Link{{Index: 0, State: gpu.LinkActive, TxBytes: metric.Some(tx), RxBytes: metric.Some(tx * 2)}}
		return &model.Snapshot{GPUs: []model.GPU{g}}
	})
	g := s.GPUs[0]
	if !g.Links[0].TxBps.OK || g.Links[0].TxBps.V != 1000 || g.Derived.NVLinkRxBps.V != 2000 || g.Derived.LinksActive != 1 {
		t.Fatalf("link rates: %+v derived=%+v", g.Links[0], g.Derived)
	}
	if g.Health.Score != 100 || g.Health.Source != "derived" {
		t.Fatalf("health: %+v", g.Health)
	}
}

func TestLinkRatesCarriedBetweenCounterRefreshes(t *testing.T) {
	tr := NewTracker(DefaultOptions())
	counter := uint64(0)
	var s *model.Snapshot
	for i := 0; i <= 9; i++ {
		if i%3 == 0 { // counters refresh every 3s
			counter += 3000
		}
		g := mk(0, 50, 1)
		g.Links = []gpu.Link{{Index: 0, State: gpu.LinkActive, TxBytes: metric.Some(counter), RxBytes: metric.Some(counter)}}
		s = &model.Snapshot{GPUs: []model.GPU{g}, Time: t0.Add(time.Duration(i) * time.Second)}
		tr.Apply(s, nil)
	}
	if l := s.GPUs[0].Links[0]; !l.TxBps.OK || l.TxBps.V != 1000 {
		t.Fatalf("carried rate: %+v", l)
	}
}
