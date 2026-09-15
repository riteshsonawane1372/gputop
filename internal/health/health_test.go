// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package health

import (
	"testing"
	"time"

	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/metric"
)

var now = time.Unix(1_800_000_000, 0)

func baseInput() Input {
	return Input{
		Now: now, Available: true,
		Device: gpu.Device{TempSlowdownC: metric.Some(90.0), MemTempMaxC: metric.Some(95.0), PCIeMaxWidth: metric.Some(16), PCIeMaxGen: metric.Some(5)},
		Sample: gpu.Sample{TempC: metric.Some(60.0), PCIeWidth: metric.Some(16), PCIeGen: metric.Some(5), UtilPercent: metric.Some(90.0), Throttle: metric.Some(gpu.ThrottleReasons(0))},
	}
}

func TestHealthyGPU(t *testing.T) {
	r := Score(baseInput())
	if r.Score != 100 || r.Band != Healthy || len(r.Reasons) != 0 || r.Source != "derived" {
		t.Fatalf("got %+v", r)
	}
}

func TestUnavailable(t *testing.T) {
	in := baseInput()
	in.Available, in.Error = false, "device lost"
	r := Score(in)
	if r.Score != 0 || r.Band != Critical || r.Reasons[0].Code != "unavailable" {
		t.Fatalf("got %+v", r)
	}
}

func TestMissingDataIsNotPenalized(t *testing.T) {
	in := Input{Now: now, Available: true}
	if r := Score(in); r.Score != 100 {
		t.Fatalf("unsupported counters must not reduce the score: %+v", r)
	}
}

func TestCriticalCapsScore(t *testing.T) {
	in := baseInput()
	in.Counters.ECCUncorrectedVolatile = metric.Some(uint64(1))
	r := Score(in)
	if r.Score > 49 || r.Band != Unhealthy {
		t.Fatalf("critical condition must cap score at 49: %+v", r)
	}
}

func TestPenaltiesAccumulate(t *testing.T) {
	in := baseInput()
	in.Sample.PCIeWidth = metric.Some(8)
	in.Sample.Throttle = metric.Some(gpu.ThrottleHWThermal)
	in.Links = []gpu.Link{{Index: 0, State: gpu.LinkActive}, {Index: 1, State: gpu.LinkInactive}}
	r := Score(in)
	if r.Score != 100-15-20-10 {
		t.Fatalf("score = %d reasons=%+v", r.Score, r.Reasons)
	}
	if r.Reasons[0].Penalty < r.Reasons[len(r.Reasons)-1].Penalty {
		t.Fatal("reasons must be sorted by penalty")
	}
}

func TestPCIeGenOnlyUnderLoad(t *testing.T) {
	in := baseInput()
	in.Sample.PCIeGen = metric.Some(1)
	in.Sample.UtilPercent = metric.Some(0.0)
	if r := Score(in); r.Score != 100 {
		t.Fatalf("idle link downtraining is normal: %+v", r)
	}
	in.Sample.UtilPercent = metric.Some(80.0)
	if r := Score(in); r.Score != 95 {
		t.Fatalf("under load: %+v", r)
	}
}

func TestXIDWeighting(t *testing.T) {
	in := baseInput()
	in.XIDs = []XID{
		{Code: 79, Severity: "critical", Time: now.Add(-2 * time.Minute)},
		{Code: 79, Severity: "critical", Time: now.Add(-3 * time.Minute)},
		{Code: 13, Severity: "warning", Time: now.Add(-30 * time.Minute)},
		{Code: 13, Severity: "warning", Time: now.Add(-2 * time.Hour)},
	}
	r := Score(in)
	// 35 (one critical, recent) + 5 (warning, half weight); old event ignored; capped at 49.
	if r.Score != 49 {
		t.Fatalf("score = %d reasons = %+v", r.Score, r.Reasons)
	}
	in.XIDs = []XID{{Code: 13, Severity: "warning", Time: now.Add(-time.Minute)}}
	if r := Score(in); r.Score != 90 || r.Band != Healthy {
		t.Fatalf("single warning xid: %+v", r)
	}
}

func TestCounterGrowthUsesBaseline(t *testing.T) {
	in := baseInput()
	in.Counters.PCIeReplays = metric.Some(uint64(120))
	if r := Score(in); r.Score != 100 {
		t.Fatal("absolute replay count without baseline must not penalize")
	}
	in.Baseline = &gpu.HealthCounters{PCIeReplays: metric.Some(uint64(100))}
	if r := Score(in); r.Score != 95 {
		t.Fatalf("replay growth: %+v", r)
	}
	in.Links = []gpu.Link{{Index: 0, State: gpu.LinkActive, ErrCRCData: metric.Some(uint64(7))}}
	in.LinkErrBaseline = map[int]uint64{0: 5}
	if r := Score(in); r.Score != 90 {
		t.Fatalf("link error growth: %+v", r)
	}
}

func TestBands(t *testing.T) {
	for score, want := range map[int]Band{100: Healthy, 90: Healthy, 89: Good, 75: Good, 74: Degraded, 50: Degraded, 49: Unhealthy, 25: Unhealthy, 24: Critical, 0: Critical} {
		if got := BandFor(score); got != want {
			t.Errorf("BandFor(%d) = %s want %s", score, got, want)
		}
	}
}
