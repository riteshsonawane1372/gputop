// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package nvidia

import (
	"context"
	"errors"
	"testing"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
)

// TestIntegrationRealNVML exercises the real libnvidia-ml. It skips when
// no NVIDIA driver is present. Run with: make test-nvidia
func TestIntegrationRealNVML(t *testing.T) {
	ctx := context.Background()
	p := New(Options{})
	diag, err := p.Open(ctx)
	if err != nil {
		t.Skipf("NVML unavailable (%v): %+v", err, diag.Checks)
	}
	defer func() { _ = p.Close() }()

	sys, err := p.System(ctx)
	if err != nil || sys.DriverVersion == "" {
		t.Fatalf("system info: %+v %v", sys, err)
	}
	devs, err := p.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) == 0 {
		t.Skip("driver loaded but no devices visible")
	}
	t.Logf("driver %s, CUDA %s, NVML %s", sys.DriverVersion, sys.RuntimeVersion, sys.LibraryVersion)
	for _, d := range devs {
		t.Logf("GPU %d %s %s arch=%s bus=%s links=%d mig=%+v", d.Index, d.ID, d.Name, d.Architecture, d.PCI.BusID, d.LinkCount, d.MIG)
		if d.ID == "" || d.Name == "" {
			t.Errorf("device %d lacks identity", d.Index)
		}
		s, err := p.Sample(ctx, d.ID)
		if err != nil {
			t.Fatalf("sample %s: %v", d.ID, err)
		}
		if !s.MemTotal.OK || s.MemTotal.V == 0 {
			t.Errorf("GPU %d memory total missing", d.Index)
		}
		if s.UtilPercent.OK && (s.UtilPercent.V < 0 || s.UtilPercent.V > 100) {
			t.Errorf("GPU %d utilization out of range: %v", d.Index, s.UtilPercent.V)
		}
		if s.TempC.OK && (s.TempC.V < 0 || s.TempC.V > 130) {
			t.Errorf("GPU %d temperature implausible: %v", d.Index, s.TempC.V)
		}
		if s.PowerW.OK && s.PowerLimitW.OK && s.PowerW.V > s.PowerLimitW.V*1.5 {
			t.Errorf("GPU %d power %v far above limit %v (units?)", d.Index, s.PowerW.V, s.PowerLimitW.V)
		}
		t.Logf("  util=%v mem=%v/%v temp=%v power=%v/%v clocks=%v/%v pcie=Gen%v x%v throttle=%v",
			s.UtilPercent, s.MemUsed, s.MemTotal, s.TempC, s.PowerW, s.PowerLimitW, s.ClockCoreMHz, s.ClockMemMHz, s.PCIeGen, s.PCIeWidth, s.Throttle)
		procs, err := p.Processes(ctx, d.ID)
		if err != nil && !errors.Is(err, gpu.ErrNoPermission) && !errors.Is(err, gpu.ErrNotSupported) {
			t.Errorf("processes: %v", err)
		}
		t.Logf("  %d processes", len(procs))
		if _, err := p.Health(ctx, d.ID); err != nil {
			t.Errorf("health: %v", err)
		}
		if _, err := p.Links(ctx, d.ID); err != nil && !errors.Is(err, gpu.ErrNotSupported) {
			t.Errorf("links: %v", err)
		}
		if _, err := p.Partitions(ctx, d.ID); err != nil && !errors.Is(err, gpu.ErrNoPermission) {
			t.Errorf("partitions: %v", err)
		}
	}
}
