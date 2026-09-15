// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"testing"
	"time"

	"github.com/gputop/gputop/internal/metric"
)

func TestCollectorSmoke(t *testing.T) {
	ctx := context.Background()
	c := NewCollector()
	if info := c.Info(ctx); info.OS == "" || info.Arch == "" {
		t.Fatalf("info: %+v", info)
	}
	first := c.CPU(ctx)
	if first.UtilPercent.OK {
		t.Fatal("first CPU sample must be unavailable (no delta yet)")
	}
	time.Sleep(50 * time.Millisecond)
	second := c.CPU(ctx)
	if second.Threads <= 0 {
		t.Fatalf("threads = %d", second.Threads)
	}
	if m := c.Memory(ctx); !m.Total.OK || m.Total.V == 0 {
		t.Fatalf("memory: %+v", m)
	}
	_ = c.Network(ctx)
	_ = c.Disks(ctx)
	_ = c.Filesystems(ctx)
}

func TestRatesAndWrap(t *testing.T) {
	t0 := time.Unix(100, 0)
	a := counterSample{at: t0, rx: 1000, tx: 500}
	b := counterSample{at: t0.Add(2 * time.Second), rx: 3000, tx: 400}
	rx, tx, _, _ := rates(a, b)
	if !rx.OK || rx.V != 1000 {
		t.Fatalf("rx = %+v", rx)
	}
	if tx.OK {
		t.Fatal("counter reset must yield unavailable, not a negative rate")
	}
}

func TestClassification(t *testing.T) {
	cases := map[string]string{"lo": "loopback", "eth0": "ethernet", "ens5f0np0": "ethernet", "veth12ab": "virtual", "ib0": "infiniband", "cali1234": "virtual"}
	for name, want := range cases {
		if got := classifyInterface(name, nil); got != want {
			t.Errorf("%s: %s want %s", name, got, want)
		}
	}
	for name, want := range map[string]bool{"nvme0n1": true, "nvme0n1p2": false, "sda": true, "sda1": false, "loop3": false, "dm-0": false} {
		if physicalDisk(name) != want {
			t.Errorf("physicalDisk(%s) != %v", name, want)
		}
	}
	m := Memory{Total: metric.Some(uint64(100)), Available: metric.Some(uint64(25))}
	if f := m.UsedFraction(); f.V != 0.75 {
		t.Fatalf("used fraction %v", f)
	}
}
