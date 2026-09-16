// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package nvidia

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/gpu/nvidia/nvml"
)

// fakeNVML simulates two GPUs:
//
//	handle 1: "GPU-aaaa" A100 with MIG enabled (one instance), NVLink x2
//	handle 2: "GPU-bbbb" consumer card with most features unsupported
//
// MIG device handle 101 belongs to handle 1.
type fakeNVML struct {
	nvml.Unsupported
	lost        bool
	calls       map[string]int
	procUtilRet nvml.Return
}

func newFake() *fakeNVML { return &fakeNVML{calls: map[string]int{}, procUtilRet: nvml.SUCCESS} }

func (f *fakeNVML) Init() Return                              { return nvml.SUCCESS }
func (f *fakeNVML) Shutdown() Return                          { return nvml.SUCCESS }
func (f *fakeNVML) LibraryPath() string                       { return "/fake/libnvidia-ml.so.1" }
func (f *fakeNVML) DeviceGetCount() (int, Return)             { return 2, nvml.SUCCESS }
func (f *fakeNVML) SystemGetDriverVersion() (string, Return)  { return "560.35.03", nvml.SUCCESS }
func (f *fakeNVML) SystemGetCudaDriverVersion() (int, Return) { return 12060, nvml.SUCCESS }

type Return = nvml.Return

func (f *fakeNVML) DeviceGetHandleByIndex(i int) (nvml.Device, Return) {
	return nvml.Device(i + 1), nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetUUID(d nvml.Device) (string, Return) {
	switch d {
	case 1:
		return "GPU-aaaa", nvml.SUCCESS
	case 2:
		return "GPU-bbbb", nvml.SUCCESS
	case 101:
		return "MIG-1111", nvml.SUCCESS
	}
	return "", nvml.ERROR_INVALID_ARGUMENT
}

func (f *fakeNVML) DeviceGetName(d nvml.Device) (string, Return) {
	switch d {
	case 1:
		return "NVIDIA A100-SXM4-40GB", nvml.SUCCESS
	case 101:
		return "NVIDIA A100-SXM4-40GB MIG 1g.5gb", nvml.SUCCESS
	}
	return "NVIDIA GeForce RTX 4090", nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetArchitecture(d nvml.Device) (uint32, Return) { return 7, nvml.SUCCESS }

func (f *fakeNVML) DeviceGetPciInfo(d nvml.Device) (nvml.PciInfo, Return) {
	var p nvml.PciInfo
	copy(p.BusId[:], []byte("00000000:0"+string(rune('0'+d))+":00.0\x00"))
	return p, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetMemoryInfoV2(d nvml.Device) (nvml.Memory_v2, Return) {
	if d == 2 {
		return nvml.Memory_v2{}, nvml.ERROR_FUNCTION_NOT_FOUND
	}
	if d == 101 {
		return nvml.Memory_v2{Version: 1, Total: 5 << 30, Used: 1 << 30}, nvml.SUCCESS
	}
	return nvml.Memory_v2{Version: nvml.Memory_v2Version, Total: 40 << 30, Used: 10 << 30, Free: 30 << 30, Reserved: 512 << 20}, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetMemoryInfo(d nvml.Device) (nvml.Memory, Return) {
	return nvml.Memory{Total: 24 << 30, Used: 2 << 30, Free: 22 << 30}, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetUtilizationRates(d nvml.Device) (nvml.Utilization, Return) {
	f.calls["util"]++
	if f.lost && d == 1 {
		return nvml.Utilization{}, nvml.ERROR_GPU_IS_LOST
	}
	return nvml.Utilization{Gpu: 87, Memory: 41}, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetTemperature(d nvml.Device) (int, Return)   { return 66, nvml.SUCCESS }
func (f *fakeNVML) DeviceGetPowerUsage(d nvml.Device) (uint32, Return) { return 250500, nvml.SUCCESS }
func (f *fakeNVML) DeviceGetEnforcedPowerLimit(d nvml.Device) (uint32, Return) {
	return 400000, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetEncoderUtilization(d nvml.Device) (uint32, uint32, Return) {
	f.calls["enc"]++
	return 0, 0, nvml.ERROR_NOT_SUPPORTED
}

func (f *fakeNVML) DeviceGetClockInfo(d nvml.Device, c int) (uint32, Return) {
	if c == nvml.CLOCK_MEM {
		return 1250, nvml.SUCCESS
	}
	return 2400, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetCurrentClocksEventReasons(d nvml.Device) (uint64, Return) {
	return nvml.ClocksEventReasonSwPowerCap | nvml.ClocksEventReasonHwThermalSlowdown, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetFieldValues(d nvml.Device, values []nvml.FieldValue) Return {
	for i := range values {
		v := &values[i]
		v.NvmlReturn = uint32(nvml.ERROR_NOT_SUPPORTED)
		if d != 1 {
			continue
		}
		switch v.FieldId {
		case nvml.FI_DEV_MEMORY_TEMP:
			v.ValueType = nvml.VALUE_TYPE_UNSIGNED_INT
			binary.LittleEndian.PutUint32(v.Value[:], 71)
			v.NvmlReturn = 0
		case nvml.FI_DEV_NVLINK_LINK_COUNT:
			v.ValueType = nvml.VALUE_TYPE_UNSIGNED_INT
			binary.LittleEndian.PutUint32(v.Value[:], 2)
			v.NvmlReturn = 0
		case nvml.FI_DEV_NVLINK_THROUGHPUT_DATA_TX:
			v.ValueType = nvml.VALUE_TYPE_UNSIGNED_LONG_LONG
			binary.LittleEndian.PutUint64(v.Value[:], 100)
			v.NvmlReturn = 0
		case nvml.FI_DEV_GET_GPU_RECOVERY_ACTION:
			v.ValueType = nvml.VALUE_TYPE_UNSIGNED_INT
			binary.LittleEndian.PutUint32(v.Value[:], 1)
			v.NvmlReturn = 0
		}
	}
	return nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetMigMode(d nvml.Device) (uint32, uint32, Return) {
	if d == 1 {
		return 1, 1, nvml.SUCCESS
	}
	return 0, 0, nvml.ERROR_NOT_SUPPORTED
}

func (f *fakeNVML) DeviceGetMaxMigDeviceCount(d nvml.Device) (int, Return) { return 7, nvml.SUCCESS }

func (f *fakeNVML) DeviceGetMigDeviceHandleByIndex(d nvml.Device, i int) (nvml.Device, Return) {
	if d == 1 && i == 0 {
		return 101, nvml.SUCCESS
	}
	return 0, nvml.ERROR_NOT_FOUND
}

func (f *fakeNVML) DeviceGetGpuInstanceId(d nvml.Device) (int, Return)     { return 5, nvml.SUCCESS }
func (f *fakeNVML) DeviceGetComputeInstanceId(d nvml.Device) (int, Return) { return 0, nvml.SUCCESS }

func (f *fakeNVML) DeviceGetComputeRunningProcesses(d nvml.Device) ([]nvml.ProcessInfo, Return) {
	switch d {
	case 1:
		return nil, nvml.ERROR_NO_PERMISSION // aggregate MIG query needs privileges
	case 101:
		return []nvml.ProcessInfo{{Pid: 4242, UsedGpuMemory: 900 << 20, GpuInstanceId: 5}}, nvml.SUCCESS
	case 2:
		return []nvml.ProcessInfo{
			{Pid: 10, UsedGpuMemory: 1 << 30},
			{Pid: 11, UsedGpuMemory: nvml.ValueNotAvailable},
		}, nvml.SUCCESS
	}
	return nil, nvml.ERROR_NOT_SUPPORTED
}

func (f *fakeNVML) DeviceGetProcessUtilization(d nvml.Device, last uint64) ([]nvml.ProcessUtilizationSample, Return) {
	if f.procUtilRet != nvml.SUCCESS {
		return nil, f.procUtilRet
	}
	return []nvml.ProcessUtilizationSample{{Pid: 10, TimeStamp: last + 10, SmUtil: 55}}, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetNvLinkState(d nvml.Device, l int) (uint32, Return) {
	if d != 1 || l >= 2 {
		return 0, nvml.ERROR_NOT_SUPPORTED
	}
	return uint32(1 - l), nvml.SUCCESS // link0 active, link1 inactive
}

func (f *fakeNVML) DeviceGetNvLinkRemotePciInfo(d nvml.Device, l int) (nvml.PciInfo, Return) {
	return f.DeviceGetPciInfo(2)
}

func (f *fakeNVML) DeviceGetNvLinkRemoteDeviceType(d nvml.Device, l int) (uint32, Return) {
	return nvml.NVLINK_DEVICE_TYPE_GPU, nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetNvLinkErrorCounter(d nvml.Device, l, c int) (uint64, Return) {
	return uint64(c), nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetTotalEccErrors(d nvml.Device, et, ct int) (uint64, Return) {
	if d == 2 {
		return 0, nvml.ERROR_NOT_SUPPORTED
	}
	return uint64(et*10 + ct), nvml.SUCCESS
}

func (f *fakeNVML) DeviceGetRemappedRows(d nvml.Device) (uint32, uint32, bool, bool, Return) {
	if d == 2 {
		return 0, 0, false, false, nvml.ERROR_NOT_SUPPORTED
	}
	return 1, 0, true, false, nvml.SUCCESS
}

func newTestProvider(t *testing.T, f *fakeNVML) *Provider {
	t.Helper()
	p := New(Options{Loader: func([]string) (nvml.API, error) { return f, nil }})
	// Open performs Linux-only filesystem checks; bypass it on other OSes.
	p.api = f
	return p
}

func TestProviderInventoryAndSample(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	p := newTestProvider(t, f)

	devs, err := p.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) != 2 {
		t.Fatalf("got %d devices", len(devs))
	}
	a := devs[0]
	if a.ID != "GPU-aaaa" || a.Architecture != "Ampere" || a.PCI.BusID != "0000:01:00.0" {
		t.Fatalf("unexpected identity: %+v", a)
	}
	if !a.MIG.Enabled || a.MIG.MaxInstances != 7 || a.LinkCount != 2 {
		t.Fatalf("mig/links: %+v links=%d", a.MIG, a.LinkCount)
	}
	if devs[1].Capabilities.State(gpu.CapMIG) != gpu.CapUnsupported {
		t.Fatalf("device b MIG should be unsupported")
	}

	s, err := p.Sample(ctx, "GPU-aaaa")
	if err != nil {
		t.Fatal(err)
	}
	if s.UtilPercent.V != 87 || s.MemBandwidthPercent.V != 41 || s.PowerW.V != 250.5 || s.PowerLimitW.V != 400 {
		t.Fatalf("sample values: %+v", s)
	}
	if s.MemReserved.V != 512<<20 || !s.MemTempC.OK || s.MemTempC.V != 71 {
		t.Fatalf("memory: %+v", s)
	}
	if s.ClockMemMHz.V != 1250 || s.ClockCoreMHz.V != 2400 {
		t.Fatalf("clocks: %+v %+v", s.ClockCoreMHz, s.ClockMemMHz)
	}
	want := gpu.ThrottleSWPowerCap | gpu.ThrottleHWThermal
	if !s.Throttle.OK || s.Throttle.V != want {
		t.Fatalf("throttle = %v", s.Throttle)
	}
	if s.EncoderPercent.OK {
		t.Fatal("unsupported encoder must be unavailable, not zero")
	}
	// Unsupported calls are remembered and not retried every tick.
	_, _ = p.Sample(ctx, "GPU-aaaa")
	if f.calls["enc"] != 1 {
		t.Fatalf("encoder called %d times, want 1", f.calls["enc"])
	}

	// Device b: v2 memory missing -> falls back to v1.
	sb, err := p.Sample(ctx, "GPU-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if sb.MemTotal.V != 24<<30 || sb.MemReserved.OK || sb.MemTempC.OK {
		t.Fatalf("fallback memory: %+v", sb)
	}
}

func TestProviderLostDevice(t *testing.T) {
	f := newFake()
	p := newTestProvider(t, f)
	if _, err := p.Devices(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.lost = true
	_, err := p.Sample(context.Background(), "GPU-aaaa")
	if !errors.Is(err, gpu.ErrDeviceLost) {
		t.Fatalf("err = %v, want ErrDeviceLost", err)
	}
	if _, err := p.Sample(context.Background(), "GPU-zzzz"); !errors.Is(err, gpu.ErrNotFound) {
		t.Fatalf("unknown id err = %v", err)
	}
}

func TestProviderProcessesMIGAndUnavailableMemory(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	p := newTestProvider(t, f)
	if _, err := p.Devices(ctx); err != nil {
		t.Fatal(err)
	}
	pa, err := p.Processes(ctx, "GPU-aaaa")
	if err != nil {
		t.Fatal(err)
	}
	if len(pa) != 1 || pa[0].PID != 4242 || pa[0].PartitionID != "MIG-1111" || pa[0].MemUsed.V != 900<<20 {
		t.Fatalf("mig processes: %+v", pa)
	}
	pb, err := p.Processes(ctx, "GPU-bbbb")
	if err != nil {
		t.Fatal(err)
	}
	if len(pb) != 2 {
		t.Fatalf("processes b: %+v", pb)
	}
	if pb[1].MemUsed.OK {
		t.Fatal("NVML_VALUE_NOT_AVAILABLE must map to unavailable")
	}
	if !pb[0].SMUtil.OK || pb[0].SMUtil.V != 55 || pb[1].SMUtil.OK {
		t.Fatalf("sm util: %+v / %+v", pb[0].SMUtil, pb[1].SMUtil)
	}

	parts, err := p.Partitions(ctx, "GPU-aaaa")
	if err != nil || len(parts) != 1 || parts[0].Profile != "1g.5gb" || parts[0].InstanceID.V != 5 {
		t.Fatalf("partitions: %+v %v", parts, err)
	}
	if parts, err := p.Partitions(ctx, "GPU-bbbb"); err != nil || parts != nil {
		t.Fatalf("partitions b: %+v %v", parts, err)
	}
}

func TestProviderHealthAndLinks(t *testing.T) {
	ctx := context.Background()
	f := newFake()
	p := newTestProvider(t, f)
	if _, err := p.Devices(ctx); err != nil {
		t.Fatal(err)
	}
	h, err := p.Health(ctx, "GPU-aaaa")
	if err != nil {
		t.Fatal(err)
	}
	if h.ECCUncorrectedAggregate.V != 11 || h.ECCCorrectedVolatile.V != 0 || !h.ECCCorrectedVolatile.OK {
		t.Fatalf("ecc: %+v", h)
	}
	if !h.RemapPending.V || h.RemappedCorrectable.V != 1 || h.RetiredPagesSBE.OK {
		t.Fatalf("remap: %+v", h)
	}
	if h.RecoveryAction.V != "gpu_reset" {
		t.Fatalf("recovery action = %+v", h.RecoveryAction)
	}
	hb, _ := p.Health(ctx, "GPU-bbbb")
	if hb.ECCCorrectedVolatile.OK {
		t.Fatal("unsupported ECC must be unavailable")
	}

	links, err := p.Links(ctx, "GPU-aaaa")
	if err != nil || len(links) != 2 {
		t.Fatalf("links: %+v %v", links, err)
	}
	if links[0].State != gpu.LinkActive || links[0].RemoteType != gpu.EndpointGPU || links[0].RemoteBusID != "0000:02:00.0" {
		t.Fatalf("link0: %+v", links[0])
	}
	if links[0].TxBytes.V != 100*1024 || links[0].ErrCRCData.V != 3 {
		t.Fatalf("link0 counters: %+v", links[0])
	}
	if links[1].State != gpu.LinkInactive || links[1].RemoteBusID != "" {
		t.Fatalf("link1: %+v", links[1])
	}
	if l, err := p.Links(ctx, "GPU-bbbb"); err != nil || l != nil {
		t.Fatalf("links b: %+v %v", l, err)
	}
}

func TestHelpers(t *testing.T) {
	if FormatCUDAVersion(12040) != "12.4" || FormatCUDAVersion(11080) != "11.8" {
		t.Fatal("cuda version format")
	}
	if MIGProfileFromName("NVIDIA H100 80GB MIG 3g.40gb") != "3g.40gb" || MIGProfileFromName("x") != "" {
		t.Fatal("mig profile")
	}
	if XIDDescription(79) != "GPU has fallen off the bus" || XIDSeverity(79) != "critical" {
		t.Fatalf("xid 79: %q %q", XIDDescription(79), XIDSeverity(79))
	}
	if XIDSeverity(13) != "warning" || XIDSeverity(92) != "info" || XIDSeverity(99999) != "warning" {
		t.Fatal("xid severity buckets")
	}
	if normalizeBusID("00000000:3B:00.0") != "0000:3b:00.0" {
		t.Fatal("bus id normalization")
	}
	_ = time.Second
}

func (f *fakeNVML) DeviceGetEccMode(d nvml.Device) (uint32, uint32, Return) {
	if d == 1 {
		return 1, 1, nvml.SUCCESS
	}
	return 0, 0, nvml.ERROR_NOT_SUPPORTED
}
