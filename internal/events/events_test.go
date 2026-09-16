// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/health"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

var t0 = time.Unix(1_800_000_000, 0)

func snap(at time.Time, gpus ...model.GPU) *model.Snapshot {
	return &model.Snapshot{Ready: true, Time: at, GPUs: gpus}
}

func mkGPU(id string, idx int) model.GPU {
	return model.GPU{
		Device:    gpu.Device{ID: gpu.ID(id), Index: idx, Name: "Test GPU", Vendor: gpu.VendorNVIDIA, PCIeMaxWidth: metric.Some(16)},
		Available: true,
		Sample:    gpu.Sample{Throttle: metric.Some(gpu.ThrottleReasons(0)), PCIeWidth: metric.Some(16)},
		Health:    health.Result{Score: 100, Band: health.Healthy},
	}
}

func kinds(evs []model.Event) string {
	var k []string
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return strings.Join(k, ",")
}

func TestDiscoveryAndDisappearance(t *testing.T) {
	d := NewDetector()
	a, b := mkGPU("A", 0), mkGPU("B", 1)
	first := d.Diff(nil, snap(t0, a, b))
	if kinds(first) != "gpu_discovered" || !strings.Contains(first[0].Message, "2× Test GPU") {
		t.Fatalf("first: %+v", first)
	}
	lost := b
	lost.Available, lost.Error = false, "device lost"
	evs := d.Diff(snap(t0, a, b), snap(t0.Add(time.Second), a, lost))
	if kinds(evs) != "gpu_unavailable" || evs[0].Severity != model.SevCritical || evs[0].DeviceIndex != 1 {
		t.Fatalf("lost: %+v", evs)
	}
	evs = d.Diff(snap(t0, a, lost), snap(t0.Add(2*time.Second), a))
	if kinds(evs) != "gpu_disappeared" {
		t.Fatalf("disappeared: %+v", evs)
	}
}

func TestThrottleDebounce(t *testing.T) {
	d := NewDetector()
	g := mkGPU("A", 0)
	prev := snap(t0, g)
	d.Diff(nil, prev)
	step := func(sec int, reasons gpu.ThrottleReasons) []model.Event {
		c := g
		c.Sample.Throttle = metric.Some(reasons)
		c.Sample.TempC = metric.Some(88.0)
		cur := snap(t0.Add(time.Duration(sec)*time.Second), c)
		out := d.Diff(prev, cur)
		prev = cur
		return out
	}
	if e := step(1, gpu.ThrottleHWThermal); len(e) != 0 {
		t.Fatalf("must debounce start: %+v", e)
	}
	if e := step(2, 0); len(e) != 0 {
		t.Fatalf("blip must not emit: %+v", e)
	}
	step(3, gpu.ThrottleHWThermal)
	e := step(5, gpu.ThrottleHWThermal)
	if kinds(e) != "throttle_start" || !strings.Contains(e[0].Message, "88°C") {
		t.Fatalf("start: %+v", e)
	}
	if e := step(6, 0); len(e) != 0 {
		t.Fatalf("end must debounce: %+v", e)
	}
	if e := step(12, 0); kinds(e) != "throttle_end" {
		t.Fatalf("end: %+v", e)
	}
}

func TestCountersLinksPartitionsProcesses(t *testing.T) {
	d := NewDetector()
	p := mkGPU("A", 0)
	p.Counters.ECCUncorrectedVolatile = metric.Some(uint64(0))
	p.Links = []gpu.Link{{Index: 0, State: gpu.LinkActive}, {Index: 1, State: gpu.LinkActive}}
	p.Partitions = []gpu.Partition{{ID: "MIG-1", Profile: "1g.10gb"}}
	prev := snap(t0, p)
	prev.Processes = []model.Process{{Process: gpu.Process{PID: 1, DeviceID: "A"}, Name: "old"}}
	d.Diff(nil, prev)

	c := p
	c.Counters.ECCUncorrectedVolatile = metric.Some(uint64(2))
	c.Links = []gpu.Link{{Index: 0, State: gpu.LinkActive, ErrCRCData: metric.Some(uint64(4))}, {Index: 1, State: gpu.LinkInactive}}
	c.Partitions = []gpu.Partition{{ID: "MIG-2", Profile: "2g.20gb"}}
	c.Sample.PCIeWidth = metric.Some(8)
	c.Health = health.Result{Score: 40, Band: health.Unhealthy}
	cur := snap(t0.Add(time.Second), c)
	cur.Processes = []model.Process{{Process: gpu.Process{PID: 2, DeviceID: "A"}, Name: "train"}}
	got := kinds(d.Diff(prev, cur))
	for _, want := range []string{"ecc_uncorrectable", "nvlink_down", "nvlink_errors", "mig_created", "mig_destroyed", "pcie_degraded", "health_degraded", "process_start", "process_stop"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestAlertsSinceIsStable(t *testing.T) {
	d := NewDetector()
	g := mkGPU("A", 0)
	g.Health.Reasons = []health.Reason{{Penalty: 15, Severity: "warning", Code: "pcie_width_degraded", Text: "PCIe x8"}, {Penalty: 2, Severity: "info", Code: "xid_info"}}
	a1 := d.Alerts(snap(t0, g))
	a2 := d.Alerts(snap(t0.Add(time.Minute), g))
	if len(a1) != 1 || len(a2) != 1 || !a2[0].Since.Equal(t0) {
		t.Fatalf("alerts: %+v %+v", a1, a2)
	}
	g.Health.Reasons = nil
	if a := d.Alerts(snap(t0.Add(2*time.Minute), g)); len(a) != 0 {
		t.Fatal("resolved alert must clear")
	}
}

func TestLogRing(t *testing.T) {
	l := NewLog(3)
	for i := 0; i < 5; i++ {
		l.Append(model.Event{Time: t0.Add(time.Duration(i) * time.Second), Message: string(rune('a' + i))})
	}
	r := l.Recent(0)
	if len(r) != 3 || r[0].Message != "c" || r[2].Message != "e" || l.Len() != 3 {
		t.Fatalf("recent: %+v", r)
	}
	if s := l.Since(t0.Add(3 * time.Second)); len(s) != 2 || s[0].Message != "d" {
		t.Fatalf("since: %+v", s)
	}
	if r := l.Recent(1); len(r) != 1 || r[0].Message != "e" {
		t.Fatalf("recent(1): %+v", r)
	}
}
