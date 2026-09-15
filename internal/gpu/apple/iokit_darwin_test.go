// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package apple

import (
	"context"
	"testing"
	"time"
)

// TestIOKitHardware reads the real GPU; it skips on Macs without an Apple
// silicon GPU. Virtual machines may expose a paravirtual GPU with fewer
// statistics, so only structure and plausibility are asserted.
func TestIOKitHardware(t *testing.T) {
	p := New(Options{})
	ctx := context.Background()
	if diag, err := p.Open(ctx); err != nil {
		t.Skipf("no Apple silicon GPU: %v (%+v)", err, diag.Checks)
	}
	defer p.Close()
	devs, err := p.Devices(ctx)
	if err != nil || len(devs) != 1 || devs[0].Name == "" || !devs[0].Memory.OK {
		t.Fatalf("devices = %+v, %v", devs, err)
	}
	s, err := p.Sample(ctx, devs[0].ID)
	if err != nil {
		t.Fatalf("sample = %+v, %v", s, err)
	}
	time.Sleep(200 * time.Millisecond)
	s, _ = p.Sample(ctx, devs[0].ID)
	t.Logf("%s: util %v%% power %v W freq %v MHz temp %v °C", devs[0].Name, s.UtilPercent.V, s.PowerW.V, s.ClockCoreMHz.V, s.TempC.V)
	if s.PowerW.OK && (s.PowerW.V < 0 || s.PowerW.V > 500) {
		t.Fatalf("implausible GPU power %v W", s.PowerW.V)
	}
	if _, err := p.Processes(ctx, devs[0].ID); err != nil {
		t.Fatal(err)
	}
}
