// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package nvidia implements gpu.Provider on top of NVML.
//
// All NVIDIA-specific knowledge (return codes, clock event reason bits, field
// IDs, MIG handles) is confined to this package; the rest of gputop sees only
// the vendor-neutral model in package gpu.
package nvidia

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/gpu/nvidia/nvml"
	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// Loader loads the NVML API. Replaced in tests.
type Loader func(paths []string) (nvml.API, error)

// Options configure the provider.
type Options struct {
	// LibraryPaths overrides the libnvidia-ml search path.
	LibraryPaths []string
	// Loader overrides how NVML is loaded (tests).
	Loader Loader
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Provider is the NVML-backed gpu.Provider.
type Provider struct {
	opts Options
	now  func() time.Time

	mu      sync.RWMutex
	api     nvml.API
	devices map[gpu.ID]*devState
	byHand  map[nvml.Device]gpu.ID
	order   []gpu.ID
}

// devState holds per-device handles, capability memory and counter state.
type devState struct {
	mu     sync.Mutex
	handle nvml.Device
	busID  string
	caps   gpu.Capabilities

	linkCount int // -1 unknown

	// Process utilization cursor (microseconds, NVML CPU timestamp).
	lastProcSample uint64

	// PCIe fallback throttling (nvmlDeviceGetPcieThroughput samples ~20ms
	// per call, so it is not called on every fast tick).
	pcieUseCounters    bool
	pcieCounterOK      bool
	lastPCIeTx         uint64
	lastPCIeRx         uint64
	lastPCIeAt         time.Time
	lastPCIeFallback   time.Time
	cachedTx, cachedRx metric.Opt[float64]
}

var (
	_ gpu.Provider         = (*Provider)(nil)
	_ gpu.EventSource      = (*Provider)(nil)
	_ gpu.TopologyProvider = (*Provider)(nil)
)

// New creates an NVIDIA provider. Call Open before use.
func New(opts Options) *Provider {
	if opts.Loader == nil {
		opts.Loader = nvml.Load
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Provider{opts: opts, now: now, devices: map[gpu.ID]*devState{}, byHand: map[nvml.Device]gpu.ID{}}
}

// Name implements gpu.Provider.
func (p *Provider) Name() string { return "nvml" }

// Vendor implements gpu.Provider.
func (p *Provider) Vendor() gpu.Vendor { return gpu.VendorNVIDIA }

// mapErr converts an NVML return into a classified Go error.
func mapErr(r nvml.Return) error {
	switch r {
	case nvml.SUCCESS:
		return nil
	case nvml.ERROR_NOT_SUPPORTED, nvml.ERROR_FUNCTION_NOT_FOUND:
		return fmt.Errorf("%w: %w", gpu.ErrNotSupported, r)
	case nvml.ERROR_NO_PERMISSION:
		return fmt.Errorf("%w: %w", gpu.ErrNoPermission, r)
	case nvml.ERROR_GPU_IS_LOST, nvml.ERROR_RESET_REQUIRED:
		return fmt.Errorf("%w: %w", gpu.ErrDeviceLost, r)
	case nvml.ERROR_UNINITIALIZED, nvml.ERROR_DRIVER_NOT_LOADED, nvml.ERROR_LIBRARY_NOT_FOUND:
		return fmt.Errorf("%w: %w", gpu.ErrUnavailable, r)
	}
	return r
}

// Open implements gpu.Provider. It records every check it performs so the
// UI can explain why no GPU is visible.
func (p *Provider) Open(ctx context.Context) (gpu.Diagnostics, error) {
	diag := gpu.Diagnostics{Provider: "NVIDIA (NVML)"}
	add := func(name string, ok bool, detail string) {
		diag.Checks = append(diag.Checks, gpu.Check{Name: name, OK: ok, Detail: detail})
	}

	if runtime.GOOS != "linux" {
		add("Platform", false, fmt.Sprintf("%s/%s — NVIDIA NVML is only available on Linux", runtime.GOOS, runtime.GOARCH))
		if runtime.GOOS == "darwin" {
			diag.Hints = append(diag.Hints, "On a Mac, gputop monitors the Apple silicon GPU: set gpu.providers to [auto] or [apple].")
		}
		diag.Hints = append(diag.Hints,
			"NVIDIA GPUs are monitored on Linux hosts. Run gputop on the GPU node, or connect to one with --remote.",
			"Try `gputop --demo` to explore the interface with simulated GPUs.")
		return diag, fmt.Errorf("%w: %w", gpu.ErrUnavailable, nvml.ErrPlatformUnsupported)
	}

	if b, err := os.ReadFile("/proc/driver/nvidia/version"); err == nil {
		first := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
		add("Kernel driver", true, first)
	} else {
		add("Kernel driver", false, "/proc/driver/nvidia/version not found (nvidia kernel module not loaded?)")
	}
	if _, err := os.Stat("/dev/nvidiactl"); err == nil {
		add("Device nodes", true, "/dev/nvidiactl present")
	} else {
		add("Device nodes", false, "/dev/nvidiactl not found (inside a container? check NVIDIA Container Toolkit)")
	}

	api, err := p.opts.Loader(p.opts.LibraryPaths)
	if err != nil {
		add("NVML library", false, err.Error())
		diag.Hints = append(diag.Hints,
			"Install the NVIDIA driver (it ships libnvidia-ml.so.1).",
			"In containers, run with the NVIDIA Container Toolkit (e.g. `--gpus all`) so the library is mounted.",
			"If the library lives in a custom location, set nvidia.library_paths in the config file.")
		return diag, fmt.Errorf("%w: %w", gpu.ErrUnavailable, err)
	}
	add("NVML library", true, api.LibraryPath())

	if r := api.Init(); r != nvml.SUCCESS {
		add("nvmlInit", false, r.Error())
		switch r {
		case nvml.ERROR_DRIVER_NOT_LOADED:
			diag.Hints = append(diag.Hints, "Load the NVIDIA kernel module (`sudo modprobe nvidia`) or reinstall the driver.")
		case nvml.ERROR_LIB_RM_VERSION_MISMATCH:
			diag.Hints = append(diag.Hints, "The user-space driver and kernel module versions differ. Reboot after a driver upgrade.")
		case nvml.ERROR_NO_PERMISSION:
			diag.Hints = append(diag.Hints, "Check permissions on /dev/nvidia* device nodes.")
		}
		return diag, fmt.Errorf("%w: %w", gpu.ErrUnavailable, r)
	}
	add("nvmlInit", true, "initialized")

	n, r := api.DeviceGetCount()
	if r != nvml.SUCCESS {
		add("Device enumeration", false, r.Error())
		_ = api.Shutdown()
		return diag, mapErr(r)
	}
	add("Device enumeration", n > 0, fmt.Sprintf("%d device(s)", n))
	if n == 0 {
		diag.Hints = append(diag.Hints,
			"NVML is working but reports no devices. In containers, check NVIDIA_VISIBLE_DEVICES.")
	}

	p.mu.Lock()
	p.api = api
	p.mu.Unlock()
	return diag, nil
}

// Close implements gpu.Provider.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.api == nil {
		return nil
	}
	r := p.api.Shutdown()
	p.api = nil
	if r != nvml.SUCCESS {
		return r
	}
	return nil
}

func (p *Provider) lib() (nvml.API, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.api == nil {
		return nil, gpu.ErrUnavailable
	}
	return p.api, nil
}

func (p *Provider) state(id gpu.ID) (nvml.API, *devState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.api == nil {
		return nil, nil, gpu.ErrUnavailable
	}
	st, ok := p.devices[id]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s", gpu.ErrNotFound, id)
	}
	return p.api, st, nil
}

// System implements gpu.Provider.
func (p *Provider) System(ctx context.Context) (gpu.SystemInfo, error) {
	api, err := p.lib()
	if err != nil {
		return gpu.SystemInfo{}, err
	}
	var info gpu.SystemInfo
	if v, r := api.SystemGetDriverVersion(); r == nvml.SUCCESS {
		info.DriverVersion = v
	}
	if v, r := api.SystemGetNVMLVersion(); r == nvml.SUCCESS {
		info.LibraryVersion = v
	}
	if v, r := api.SystemGetCudaDriverVersion(); r == nvml.SUCCESS && v > 0 {
		info.RuntimeName = "CUDA"
		info.RuntimeVersion = FormatCUDAVersion(v)
	}
	return info, nil
}

// FormatCUDAVersion converts NVML's integer CUDA version (e.g. 12040) to "12.4".
func FormatCUDAVersion(v int) string {
	return fmt.Sprintf("%d.%d", v/1000, (v%1000)/10)
}

// capRecord updates a capability state from an NVML return. Returns true
// when the call succeeded.
func capRecord(caps gpu.Capabilities, c gpu.Capability, r nvml.Return) bool {
	switch r {
	case nvml.SUCCESS:
		caps[c] = gpu.CapSupported
		return true
	case nvml.ERROR_NOT_SUPPORTED, nvml.ERROR_FUNCTION_NOT_FOUND, nvml.ERROR_INVALID_ARGUMENT:
		caps[c] = gpu.CapUnsupported
	case nvml.ERROR_NO_PERMISSION:
		caps[c] = gpu.CapNoPermission
	}
	return false
}

// skip reports whether a capability is known to be unavailable, so the
// fast path avoids repeated failing calls.
func skip(caps gpu.Capabilities, c gpu.Capability) bool {
	s := caps[c]
	return s == gpu.CapUnsupported || s == gpu.CapNoPermission
}

// Devices implements gpu.Provider.
func (p *Provider) Devices(ctx context.Context) ([]gpu.Device, error) {
	api, err := p.lib()
	if err != nil {
		return nil, err
	}
	n, r := api.DeviceGetCount()
	if r != nvml.SUCCESS {
		return nil, mapErr(r)
	}

	out := make([]gpu.Device, 0, n)
	seen := make(map[gpu.ID]bool, n)
	var firstErr error
	for i := 0; i < n; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		h, r := api.DeviceGetHandleByIndex(i)
		if r != nvml.SUCCESS {
			// A single broken device must not hide the others.
			if firstErr == nil {
				firstErr = fmt.Errorf("device %d: %w", i, mapErr(r))
			}
			continue
		}
		uuid, r := api.DeviceGetUUID(h)
		if r != nvml.SUCCESS || uuid == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("device %d uuid: %w", i, mapErr(r))
			}
			continue
		}
		id := gpu.ID(uuid)
		seen[id] = true

		p.mu.Lock()
		st := p.devices[id]
		if st == nil {
			st = &devState{caps: gpu.Capabilities{}, linkCount: -1, pcieUseCounters: true}
			p.devices[id] = st
		}
		if st.handle != h {
			delete(p.byHand, st.handle)
		}
		st.handle = h
		p.byHand[h] = id
		p.mu.Unlock()

		out = append(out, p.inventory(api, st, id, i))
	}

	// Forget devices that disappeared from enumeration.
	p.mu.Lock()
	for id, st := range p.devices {
		if !seen[id] {
			delete(p.byHand, st.handle)
			delete(p.devices, id)
		}
	}
	p.order = p.order[:0]
	for _, d := range out {
		p.order = append(p.order, d.ID)
	}
	p.mu.Unlock()

	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func (p *Provider) inventory(api nvml.API, st *devState, id gpu.ID, index int) gpu.Device {
	st.mu.Lock()
	defer st.mu.Unlock()
	h := st.handle
	caps := st.caps

	d := gpu.Device{ID: id, Vendor: gpu.VendorNVIDIA, Index: index}
	if v, r := api.DeviceGetIndex(h); r == nvml.SUCCESS {
		d.Index = v
	}
	if v, r := api.DeviceGetName(h); r == nvml.SUCCESS {
		d.Name = v
	}
	if v, r := api.DeviceGetBrand(h); r == nvml.SUCCESS {
		d.Brand = nvml.Brands[v]
	}
	if v, r := api.DeviceGetArchitecture(h); r == nvml.SUCCESS {
		d.Architecture = nvml.Architectures[v]
	}
	if maj, mnr, r := api.DeviceGetCudaComputeCapability(h); r == nvml.SUCCESS {
		d.ComputeCapability = fmt.Sprintf("%d.%d", maj, mnr)
	}
	if v, r := api.DeviceGetSerial(h); r == nvml.SUCCESS {
		d.Serial = v
	}
	if v, r := api.DeviceGetBoardPartNumber(h); r == nvml.SUCCESS {
		d.PartNumber = v
	}
	if v, r := api.DeviceGetVbiosVersion(h); r == nvml.SUCCESS {
		d.FirmwareVersion = v
	}
	if pci, r := api.DeviceGetPciInfo(h); r == nvml.SUCCESS {
		d.PCI = gpu.PCIAddress{
			BusID:       normalizeBusID(nvml.CString(pci.BusId[:])),
			DeviceID:    pci.PciDeviceId,
			SubsystemID: pci.PciSubSystemId,
		}
		st.busID = d.PCI.BusID
	}
	if v, r := api.DeviceGetNumaNodeId(h); r == nvml.SUCCESS {
		d.NUMANode = metric.Some(v)
	}
	if m, r := memoryInfo(api, h); r == nvml.SUCCESS {
		d.Memory = metric.Some(m.Total)
	}
	if v, r := api.DeviceGetPersistenceMode(h); r == nvml.SUCCESS {
		d.PersistenceMode = metric.Some(v == nvml.FEATURE_ENABLED)
	}
	if v, r := api.DeviceGetComputeMode(h); r == nvml.SUCCESS {
		d.ComputeMode = computeModeName(v)
	}
	if cur, _, r := api.DeviceGetEccMode(h); capRecord(caps, gpu.CapECC, r) {
		d.ECCEnabled = metric.Some(cur == nvml.FEATURE_ENABLED)
	}

	if v, r := api.DeviceGetPowerManagementDefaultLimit(h); capRecord(caps, gpu.CapPowerLimit, r) {
		d.PowerLimitDefaultW = metric.Some(float64(v) / 1000)
	}
	if lo, hi, r := api.DeviceGetPowerManagementLimitConstraints(h); r == nvml.SUCCESS {
		d.PowerLimitMinW = metric.Some(float64(lo) / 1000)
		d.PowerLimitMaxW = metric.Some(float64(hi) / 1000)
	}
	if v, r := api.DeviceGetTemperatureThreshold(h, nvml.TEMPERATURE_THRESHOLD_SLOWDOWN); r == nvml.SUCCESS && v > 0 {
		d.TempSlowdownC = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetTemperatureThreshold(h, nvml.TEMPERATURE_THRESHOLD_SHUTDOWN); r == nvml.SUCCESS && v > 0 {
		d.TempShutdownC = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetTemperatureThreshold(h, nvml.TEMPERATURE_THRESHOLD_GPU_MAX); r == nvml.SUCCESS && v > 0 {
		d.TempMaxOpC = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetTemperatureThreshold(h, nvml.TEMPERATURE_THRESHOLD_MEM_MAX); r == nvml.SUCCESS && v > 0 {
		d.MemTempMaxC = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetMaxClockInfo(h, nvml.CLOCK_GRAPHICS); r == nvml.SUCCESS && v > 0 {
		d.ClockCoreMaxMHz = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetMaxClockInfo(h, nvml.CLOCK_MEM); r == nvml.SUCCESS && v > 0 {
		d.ClockMemMaxMHz = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetMaxPcieLinkGeneration(h); r == nvml.SUCCESS && v > 0 {
		d.PCIeMaxGen = metric.Some(v)
	}
	if v, r := api.DeviceGetGpuMaxPcieLinkGeneration(h); r == nvml.SUCCESS && v > 0 {
		d.PCIeDeviceMaxGen = metric.Some(v)
	}
	if v, r := api.DeviceGetMaxPcieLinkWidth(h); r == nvml.SUCCESS && v > 0 {
		d.PCIeMaxWidth = metric.Some(v)
	}

	cur, pending, r := api.DeviceGetMigMode(h)
	if capRecord(caps, gpu.CapMIG, r) {
		d.MIG.Supported = true
		d.MIG.Enabled = cur == nvml.DEVICE_MIG_ENABLE
		d.MIG.Pending = pending == nvml.DEVICE_MIG_ENABLE
		if n, r := api.DeviceGetMaxMigDeviceCount(h); r == nvml.SUCCESS {
			d.MIG.MaxInstances = n
		}
	}

	if st.linkCount < 0 {
		st.linkCount = probeLinkCount(api, h)
	}
	d.LinkCount = st.linkCount
	if st.linkCount > 0 {
		caps[gpu.CapNVLink] = gpu.CapSupported
	} else {
		caps[gpu.CapNVLink] = gpu.CapUnsupported
	}

	if t, r := api.DeviceGetSupportedEventTypes(h); r == nvml.SUCCESS && t&nvml.EventTypeXidCriticalError != 0 {
		caps[gpu.CapXIDEvents] = gpu.CapSupported
	} else if r != nvml.SUCCESS {
		capRecord(caps, gpu.CapXIDEvents, r)
	}
	// NVML does not expose a GPU hotspot temperature sensor.
	caps[gpu.CapHotspotTemp] = gpu.CapUnsupported

	d.Capabilities = caps.Clone()
	return d
}

func memoryInfo(api nvml.API, h nvml.Device) (nvml.Memory_v2, nvml.Return) {
	if m, r := api.DeviceGetMemoryInfoV2(h); r == nvml.SUCCESS {
		return m, nvml.SUCCESS
	}
	m1, r := api.DeviceGetMemoryInfo(h)
	if r != nvml.SUCCESS {
		return nvml.Memory_v2{}, r
	}
	return nvml.Memory_v2{Total: m1.Total, Free: m1.Free, Used: m1.Used}, r
}

// probeLinkCount determines how many NVLink links a device has.
func probeLinkCount(api nvml.API, h nvml.Device) int {
	fv := []nvml.FieldValue{{FieldId: nvml.FI_DEV_NVLINK_LINK_COUNT}}
	if api.DeviceGetFieldValues(h, fv) == nvml.SUCCESS && nvml.Return(fv[0].NvmlReturn) == nvml.SUCCESS {
		if v, ok := fv[0].Uint64(); ok {
			return int(min(v, uint64(nvml.NVLinkMaxLinks)))
		}
	}
	// Older drivers: probe link states. Links not present report
	// NOT_SUPPORTED / INVALID_ARGUMENT.
	count := 0
	for l := 0; l < 18; l++ {
		if _, r := api.DeviceGetNvLinkState(h, l); r == nvml.SUCCESS {
			count = l + 1
		}
	}
	return count
}

func computeModeName(v uint32) string {
	switch v {
	case nvml.COMPUTEMODE_DEFAULT:
		return "default"
	case nvml.COMPUTEMODE_EXCLUSIVE_THREAD:
		return "exclusive_thread"
	case nvml.COMPUTEMODE_PROHIBITED:
		return "prohibited"
	case nvml.COMPUTEMODE_EXCLUSIVE_PROCESS:
		return "exclusive_process"
	}
	return "unknown"
}

// normalizeBusID lowercases and trims NVML's 8-digit domain to the 4-digit
// form used by Linux sysfs, so bus IDs from different sources compare.
func normalizeBusID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == len("00000000:00:00.0") && strings.HasPrefix(s, "0000") {
		s = s[4:]
	}
	return s
}

// ThrottleFromNVML maps NVML clock event reason bits to vendor-neutral flags.
func ThrottleFromNVML(bits uint64) gpu.ThrottleReasons {
	var t gpu.ThrottleReasons
	m := []struct {
		n uint64
		g gpu.ThrottleReasons
	}{
		{nvml.ClocksEventReasonGpuIdle, gpu.ThrottleIdle},
		{nvml.ClocksEventReasonApplicationsClocksSetting, gpu.ThrottleAppClockSetting},
		{nvml.ClocksEventReasonSwPowerCap, gpu.ThrottleSWPowerCap},
		{nvml.ClocksEventReasonHwSlowdown, gpu.ThrottleHWSlowdown},
		{nvml.ClocksEventReasonSyncBoost, gpu.ThrottleSyncBoost},
		{nvml.ClocksEventReasonSwThermalSlowdown, gpu.ThrottleSWThermal},
		{nvml.ClocksEventReasonHwThermalSlowdown, gpu.ThrottleHWThermal},
		{nvml.ClocksEventReasonHwPowerBrakeSlowdown, gpu.ThrottleHWPowerBrake},
		{nvml.ClocksEventReasonDisplayClockSetting, gpu.ThrottleDisplayClockSetting},
		{nvml.ClocksEventReasonBoardLimit, gpu.ThrottleBoardLimit},
		{nvml.ClocksEventReasonReliability, gpu.ThrottleReliability},
	}
	for _, x := range m {
		if bits&x.n != 0 {
			t |= x.g
		}
	}
	return t
}

// Sample implements gpu.Provider.
func (p *Provider) Sample(ctx context.Context, id gpu.ID) (gpu.Sample, error) {
	api, st, err := p.state(id)
	if err != nil {
		return gpu.Sample{}, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	h := st.handle
	caps := st.caps
	now := p.now()
	s := gpu.Sample{Time: now}

	u, r := api.DeviceGetUtilizationRates(h)
	if r == nvml.ERROR_GPU_IS_LOST || r == nvml.ERROR_RESET_REQUIRED {
		return s, mapErr(r)
	}
	if r == nvml.SUCCESS {
		s.UtilPercent = metric.Some(float64(u.Gpu))
		s.MemBandwidthPercent = metric.Some(float64(u.Memory))
	}

	if m, r := memoryInfo(api, h); r == nvml.SUCCESS {
		s.MemTotal = metric.Some(m.Total)
		s.MemUsed = metric.Some(m.Used)
		s.MemFree = metric.Some(m.Free)
		if m.Version != 0 || m.Reserved != 0 {
			s.MemReserved = metric.Some(m.Reserved)
		}
	} else if r == nvml.ERROR_GPU_IS_LOST {
		return s, mapErr(r)
	}

	if !skip(caps, gpu.CapEncoder) {
		if v, _, r := api.DeviceGetEncoderUtilization(h); capRecord(caps, gpu.CapEncoder, r) {
			s.EncoderPercent = metric.Some(float64(v))
		}
	}
	if !skip(caps, gpu.CapDecoder) {
		if v, _, r := api.DeviceGetDecoderUtilization(h); capRecord(caps, gpu.CapDecoder, r) {
			s.DecoderPercent = metric.Some(float64(v))
		}
	}
	if !skip(caps, gpu.CapJPEG) {
		if v, _, r := api.DeviceGetJpgUtilization(h); capRecord(caps, gpu.CapJPEG, r) {
			s.JPEGPercent = metric.Some(float64(v))
		}
	}
	if !skip(caps, gpu.CapOFA) {
		if v, _, r := api.DeviceGetOfaUtilization(h); capRecord(caps, gpu.CapOFA, r) {
			s.OFAPercent = metric.Some(float64(v))
		}
	}

	if v, r := api.DeviceGetTemperature(h); r == nvml.SUCCESS {
		s.TempC = metric.Some(float64(v))
	}
	if !skip(caps, gpu.CapFan) {
		if v, r := api.DeviceGetFanSpeed(h); capRecord(caps, gpu.CapFan, r) {
			s.FanPct = metric.Some(float64(v))
		}
	}
	if v, r := api.DeviceGetPowerUsage(h); r == nvml.SUCCESS {
		s.PowerW = metric.Some(float64(v) / 1000)
	}
	if v, r := api.DeviceGetEnforcedPowerLimit(h); r == nvml.SUCCESS && v > 0 {
		s.PowerLimitW = metric.Some(float64(v) / 1000)
	}
	if !skip(caps, gpu.CapEnergy) {
		if v, r := api.DeviceGetTotalEnergyConsumption(h); capRecord(caps, gpu.CapEnergy, r) {
			s.EnergyJ = metric.Some(float64(v) / 1000)
		}
	}
	if v, r := api.DeviceGetClockInfo(h, nvml.CLOCK_GRAPHICS); r == nvml.SUCCESS {
		s.ClockCoreMHz = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetClockInfo(h, nvml.CLOCK_MEM); r == nvml.SUCCESS {
		s.ClockMemMHz = metric.Some(float64(v))
	}
	if v, r := api.DeviceGetPerformanceState(h); r == nvml.SUCCESS && v < 32 {
		s.PState = metric.Some(v)
	}
	if !skip(caps, gpu.CapClockReasons) {
		if v, r := api.DeviceGetCurrentClocksEventReasons(h); capRecord(caps, gpu.CapClockReasons, r) {
			s.Throttle = metric.Some(ThrottleFromNVML(v))
		}
	}
	if v, r := api.DeviceGetCurrPcieLinkGeneration(h); r == nvml.SUCCESS && v > 0 {
		s.PCIeGen = metric.Some(v)
	}
	if v, r := api.DeviceGetCurrPcieLinkWidth(h); r == nvml.SUCCESS && v > 0 {
		s.PCIeWidth = metric.Some(v)
	}

	// One batched field query for values without dedicated functions.
	fv := []nvml.FieldValue{
		{FieldId: nvml.FI_DEV_MEMORY_TEMP},
		{FieldId: nvml.FI_DEV_PCIE_COUNT_TX_BYTES},
		{FieldId: nvml.FI_DEV_PCIE_COUNT_RX_BYTES},
	}
	fvOK := !skip(caps, gpu.CapMemoryTemp) || st.pcieUseCounters
	if fvOK && api.DeviceGetFieldValues(h, fv) == nvml.SUCCESS {
		if nvml.Return(fv[0].NvmlReturn) == nvml.SUCCESS {
			if v, ok := fv[0].Float64(); ok && v > 0 {
				s.MemTempC = metric.Some(v)
				caps[gpu.CapMemoryTemp] = gpu.CapSupported
			}
		} else {
			capRecord(caps, gpu.CapMemoryTemp, nvml.Return(fv[0].NvmlReturn))
		}
		if st.pcieUseCounters {
			tx, okTx := fieldUint(fv[1])
			rx, okRx := fieldUint(fv[2])
			if okTx && okRx {
				caps[gpu.CapPCIeThroughput] = gpu.CapSupported
				if st.pcieCounterOK {
					dt := now.Sub(st.lastPCIeAt).Seconds()
					if dt > 0 && tx >= st.lastPCIeTx && rx >= st.lastPCIeRx {
						s.PCIeTxBps = metric.Some(float64(tx-st.lastPCIeTx) / dt)
						s.PCIeRxBps = metric.Some(float64(rx-st.lastPCIeRx) / dt)
					}
				}
				st.lastPCIeTx, st.lastPCIeRx, st.lastPCIeAt, st.pcieCounterOK = tx, rx, now, true
			} else {
				st.pcieUseCounters = false
			}
		}
	} else if fvOK {
		capRecord(caps, gpu.CapMemoryTemp, nvml.ERROR_NOT_SUPPORTED)
		st.pcieUseCounters = false
	}

	if !st.pcieUseCounters && !skip(caps, gpu.CapPCIeThroughput) {
		// nvmlDeviceGetPcieThroughput samples for ~20ms per call; refresh at
		// most every 5s and reuse the last reading in between.
		if now.Sub(st.lastPCIeFallback) >= 5*time.Second {
			st.lastPCIeFallback = now
			st.cachedTx, st.cachedRx = metric.None[float64](), metric.None[float64]()
			if v, r := api.DeviceGetPcieThroughput(h, nvml.PCIE_UTIL_TX_BYTES); capRecord(caps, gpu.CapPCIeThroughput, r) {
				st.cachedTx = metric.Some(float64(v) * 1024) // KB/s
			}
			if v, r := api.DeviceGetPcieThroughput(h, nvml.PCIE_UTIL_RX_BYTES); r == nvml.SUCCESS {
				st.cachedRx = metric.Some(float64(v) * 1024)
			}
		}
		s.PCIeTxBps, s.PCIeRxBps = st.cachedTx, st.cachedRx
	}
	return s, nil
}

func fieldUint(f nvml.FieldValue) (uint64, bool) {
	if nvml.Return(f.NvmlReturn) != nvml.SUCCESS {
		return 0, false
	}
	return f.Uint64()
}

// Capabilities returns the latest detected capabilities of a device.
func (p *Provider) Capabilities(id gpu.ID) gpu.Capabilities {
	_, st, err := p.state(id)
	if err != nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.caps.Clone()
}

// Processes implements gpu.Provider.
func (p *Provider) Processes(ctx context.Context, id gpu.ID) ([]gpu.Process, error) {
	api, st, err := p.state(id)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	h := st.handle

	type key struct {
		pid       uint32
		partition gpu.ID
	}
	procs := map[key]*gpu.Process{}
	var order []key
	add := func(infos []nvml.ProcessInfo, typ gpu.ProcessType, partition gpu.ID) {
		for _, in := range infos {
			k := key{in.Pid, partition}
			pr, ok := procs[k]
			if !ok {
				pr = &gpu.Process{PID: int(in.Pid), DeviceID: id, PartitionID: partition, Type: typ}
				procs[k] = pr
				order = append(order, k)
			}
			if in.UsedGpuMemory != nvml.ValueNotAvailable {
				pr.MemUsed = metric.Some(pr.MemUsed.Or(0) + in.UsedGpuMemory)
			}
		}
	}

	firstErr := nvml.SUCCESS
	compute, r := api.DeviceGetComputeRunningProcesses(h)
	if r == nvml.ERROR_GPU_IS_LOST {
		return nil, mapErr(r)
	}
	if r != nvml.SUCCESS {
		firstErr = r
	}
	graphics, rg := api.DeviceGetGraphicsRunningProcesses(h)

	// MIG: per-instance handles give per-instance attribution without
	// requiring privileges for the aggregate parent query.
	migByInstance := map[[2]uint32]gpu.ID{}
	if cur, _, r := api.DeviceGetMigMode(h); r == nvml.SUCCESS && cur == nvml.DEVICE_MIG_ENABLE {
		n, _ := api.DeviceGetMaxMigDeviceCount(h)
		for i := 0; i < n; i++ {
			mh, r := api.DeviceGetMigDeviceHandleByIndex(h, i)
			if r != nvml.SUCCESS {
				continue
			}
			muuid, r := api.DeviceGetUUID(mh)
			if r != nvml.SUCCESS {
				continue
			}
			gi, _ := api.DeviceGetGpuInstanceId(mh)
			ci, _ := api.DeviceGetComputeInstanceId(mh)
			migByInstance[[2]uint32{uint32(gi), uint32(ci)}] = gpu.ID(muuid)
			if infos, r := api.DeviceGetComputeRunningProcesses(mh); r == nvml.SUCCESS {
				add(infos, gpu.ProcessCompute, gpu.ID(muuid))
			}
		}
	}

	if len(migByInstance) > 0 {
		// Parent results carry GI/CI ids; attribute them and avoid double
		// counting processes already seen through the MIG handle.
		for _, in := range compute {
			part := migByInstance[[2]uint32{in.GpuInstanceId, in.ComputeInstanceId}]
			if _, dup := procs[key{in.Pid, part}]; dup {
				continue
			}
			add([]nvml.ProcessInfo{in}, gpu.ProcessCompute, part)
		}
	} else {
		add(compute, gpu.ProcessCompute, "")
	}
	if rg == nvml.SUCCESS {
		for _, in := range graphics {
			if _, dup := procs[key{in.Pid, ""}]; dup {
				continue
			}
			add([]nvml.ProcessInfo{in}, gpu.ProcessGraphics, "")
		}
	}

	if len(procs) > 0 && !skip(st.caps, gpu.CapProcessUtil) {
		// NVML timestamps are CPU time in microseconds. Ask for samples from
		// the previous query onwards; on first use, the last 10 seconds.
		last := st.lastProcSample
		if last == 0 {
			last = uint64(p.now().Add(-10 * time.Second).UnixMicro())
		}
		samples, r := api.DeviceGetProcessUtilization(h, last)
		switch r {
		case nvml.SUCCESS:
			st.caps[gpu.CapProcessUtil] = gpu.CapSupported
			latest := map[uint32]nvml.ProcessUtilizationSample{}
			for _, smp := range samples {
				if cur, ok := latest[smp.Pid]; !ok || smp.TimeStamp > cur.TimeStamp {
					latest[smp.Pid] = smp
				}
				if smp.TimeStamp > st.lastProcSample {
					st.lastProcSample = smp.TimeStamp
				}
			}
			for _, pr := range procs {
				if smp, ok := latest[uint32(pr.PID)]; ok {
					pr.SMUtil = metric.Some(float64(smp.SmUtil))
					pr.MemUtil = metric.Some(float64(smp.MemUtil))
					pr.EncUtil = metric.Some(float64(smp.EncUtil))
					pr.DecUtil = metric.Some(float64(smp.DecUtil))
				}
			}
		case nvml.ERROR_NOT_FOUND:
			// No samples since last query: processes were idle.
			for _, pr := range procs {
				pr.SMUtil = metric.Some(0.0)
			}
		default:
			capRecord(st.caps, gpu.CapProcessUtil, r)
		}
	}

	out := make([]gpu.Process, 0, len(order))
	for _, k := range order {
		out = append(out, *procs[k])
	}
	if len(out) == 0 && firstErr != nvml.SUCCESS && len(migByInstance) == 0 {
		return nil, mapErr(firstErr)
	}
	return out, nil
}

// Health implements gpu.Provider.
func (p *Provider) Health(ctx context.Context, id gpu.ID) (gpu.HealthCounters, error) {
	api, st, err := p.state(id)
	if err != nil {
		return gpu.HealthCounters{}, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	h := st.handle
	caps := st.caps
	hc := gpu.HealthCounters{Time: p.now()}

	if !skip(caps, gpu.CapECC) {
		get := func(errType, counter int) metric.Opt[uint64] {
			v, r := api.DeviceGetTotalEccErrors(h, errType, counter)
			if r == nvml.SUCCESS {
				return metric.Some(v)
			}
			if r == nvml.ERROR_NOT_SUPPORTED {
				caps[gpu.CapECC] = gpu.CapUnsupported
			}
			return metric.None[uint64]()
		}
		hc.ECCCorrectedVolatile = get(nvml.MEMORY_ERROR_TYPE_CORRECTED, nvml.VOLATILE_ECC)
		hc.ECCUncorrectedVolatile = get(nvml.MEMORY_ERROR_TYPE_UNCORRECTED, nvml.VOLATILE_ECC)
		hc.ECCCorrectedAggregate = get(nvml.MEMORY_ERROR_TYPE_CORRECTED, nvml.AGGREGATE_ECC)
		hc.ECCUncorrectedAggregate = get(nvml.MEMORY_ERROR_TYPE_UNCORRECTED, nvml.AGGREGATE_ECC)
	}

	if !skip(caps, gpu.CapRemappedRows) {
		corr, unc, pending, failure, r := api.DeviceGetRemappedRows(h)
		if capRecord(caps, gpu.CapRemappedRows, r) {
			hc.RemappedCorrectable = metric.Some(uint64(corr))
			hc.RemappedUncorrectable = metric.Some(uint64(unc))
			hc.RemapPending = metric.Some(pending)
			hc.RemapFailure = metric.Some(failure)
		}
	}
	// Page retirement is the pre-Ampere mechanism; row remapping replaces it.
	if !caps.Has(gpu.CapRemappedRows) && !skip(caps, gpu.CapRetiredPages) {
		sbe, r := api.DeviceGetRetiredPagesCount(h, nvml.PAGE_RETIREMENT_CAUSE_MULTIPLE_SINGLE_BIT_ECC_ERRORS)
		if capRecord(caps, gpu.CapRetiredPages, r) {
			hc.RetiredPagesSBE = metric.Some(uint64(sbe))
			if dbe, r := api.DeviceGetRetiredPagesCount(h, nvml.PAGE_RETIREMENT_CAUSE_DOUBLE_BIT_ECC_ERROR); r == nvml.SUCCESS {
				hc.RetiredPagesDBE = metric.Some(uint64(dbe))
			}
			if v, r := api.DeviceGetRetiredPagesPendingStatus(h); r == nvml.SUCCESS {
				hc.RetiredPending = metric.Some(v == nvml.FEATURE_ENABLED)
			}
		}
	}

	if v, r := api.DeviceGetPcieReplayCounter(h); r == nvml.SUCCESS {
		hc.PCIeReplays = metric.Some(uint64(v))
	}

	fv := []nvml.FieldValue{
		{FieldId: nvml.FI_DEV_PCIE_COUNT_CORRECTABLE_ERRORS},
		{FieldId: nvml.FI_DEV_PCIE_COUNT_NON_FATAL_ERROR},
		{FieldId: nvml.FI_DEV_PCIE_COUNT_FATAL_ERROR},
		{FieldId: nvml.FI_DEV_GET_GPU_RECOVERY_ACTION},
	}
	if api.DeviceGetFieldValues(h, fv) == nvml.SUCCESS {
		if v, ok := fieldUint(fv[0]); ok {
			hc.PCIeCorrectableErrors = metric.Some(v)
		}
		if v, ok := fieldUint(fv[1]); ok {
			hc.PCIeNonFatalErrors = metric.Some(v)
		}
		if v, ok := fieldUint(fv[2]); ok {
			hc.PCIeFatalErrors = metric.Some(v)
		}
		if v, ok := fieldUint(fv[3]); ok {
			if name, known := nvml.RecoveryActions[v]; known {
				hc.RecoveryAction = metric.Some(name)
			} else {
				hc.RecoveryAction = metric.Some(fmt.Sprintf("action_%d", v))
			}
		}
	}

	if !skip(caps, gpu.CapViolationCounter) {
		if v, r := api.DeviceGetViolationStatus(h, nvml.PERF_POLICY_POWER); capRecord(caps, gpu.CapViolationCounter, r) {
			hc.ViolationPower = metric.Some(time.Duration(v.ViolationTime))
		}
		if v, r := api.DeviceGetViolationStatus(h, nvml.PERF_POLICY_THERMAL); r == nvml.SUCCESS {
			hc.ViolationThermal = metric.Some(time.Duration(v.ViolationTime))
		}
	}
	return hc, nil
}

// Links implements gpu.Provider.
func (p *Provider) Links(ctx context.Context, id gpu.ID) ([]gpu.Link, error) {
	api, st, err := p.state(id)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.linkCount <= 0 {
		return nil, nil
	}
	h := st.handle
	links := make([]gpu.Link, 0, st.linkCount)
	for l := 0; l < st.linkCount; l++ {
		state, r := api.DeviceGetNvLinkState(h, l)
		if r == nvml.ERROR_GPU_IS_LOST {
			return nil, mapErr(r)
		}
		if r != nvml.SUCCESS {
			continue
		}
		lk := gpu.Link{Index: l, Kind: "nvlink", State: linkState(state), RemoteType: gpu.EndpointUnknown}
		if v, r := api.DeviceGetNvLinkVersion(h, l); r == nvml.SUCCESS {
			lk.Version = metric.Some(int(v))
		}
		if lk.State == gpu.LinkActive || lk.State == gpu.LinkDisabled {
			if pci, r := api.DeviceGetNvLinkRemotePciInfo(h, l); r == nvml.SUCCESS {
				lk.RemoteBusID = normalizeBusID(nvml.CString(pci.BusId[:]))
			}
			if t, r := api.DeviceGetNvLinkRemoteDeviceType(h, l); r == nvml.SUCCESS {
				switch t {
				case nvml.NVLINK_DEVICE_TYPE_GPU:
					lk.RemoteType = gpu.EndpointGPU
				case nvml.NVLINK_DEVICE_TYPE_SWITCH:
					lk.RemoteType = gpu.EndpointSwitch
				case nvml.NVLINK_DEVICE_TYPE_IBMNPU:
					lk.RemoteType = gpu.EndpointCPU
				}
			}
		}
		counter := func(c int) metric.Opt[uint64] {
			if v, r := api.DeviceGetNvLinkErrorCounter(h, l, c); r == nvml.SUCCESS {
				return metric.Some(v)
			}
			return metric.None[uint64]()
		}
		lk.ErrReplay = counter(nvml.NVLINK_ERROR_DL_REPLAY)
		lk.ErrRecovery = counter(nvml.NVLINK_ERROR_DL_RECOVERY)
		lk.ErrCRCFlit = counter(nvml.NVLINK_ERROR_DL_CRC_FLIT)
		lk.ErrCRCData = counter(nvml.NVLINK_ERROR_DL_CRC_DATA)

		fv := []nvml.FieldValue{
			{FieldId: nvml.FI_DEV_NVLINK_THROUGHPUT_DATA_TX, ScopeId: uint32(l)},
			{FieldId: nvml.FI_DEV_NVLINK_THROUGHPUT_DATA_RX, ScopeId: uint32(l)},
		}
		if api.DeviceGetFieldValues(h, fv) == nvml.SUCCESS {
			if v, ok := fieldUint(fv[0]); ok {
				lk.TxBytes = metric.Some(v * 1024) // KiB
			}
			if v, ok := fieldUint(fv[1]); ok {
				lk.RxBytes = metric.Some(v * 1024)
			}
		}
		links = append(links, lk)
	}
	return links, nil
}

func linkState(v uint32) gpu.LinkState {
	switch v {
	case 0:
		return gpu.LinkInactive
	case 1:
		return gpu.LinkActive
	case 2:
		return gpu.LinkSleep
	case 3:
		return gpu.LinkDisabled
	}
	return gpu.LinkUnknown
}

// Partitions implements gpu.Provider.
func (p *Provider) Partitions(ctx context.Context, id gpu.ID) ([]gpu.Partition, error) {
	api, st, err := p.state(id)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	h := st.handle
	cur, _, r := api.DeviceGetMigMode(h)
	if r != nvml.SUCCESS {
		if r == nvml.ERROR_NOT_SUPPORTED || r == nvml.ERROR_FUNCTION_NOT_FOUND {
			return nil, nil
		}
		return nil, mapErr(r)
	}
	if cur != nvml.DEVICE_MIG_ENABLE {
		return nil, nil
	}
	n, r := api.DeviceGetMaxMigDeviceCount(h)
	if r != nvml.SUCCESS {
		return nil, mapErr(r)
	}
	var parts []gpu.Partition
	for i := 0; i < n; i++ {
		mh, r := api.DeviceGetMigDeviceHandleByIndex(h, i)
		if r != nvml.SUCCESS {
			continue // slot not populated
		}
		uuid, r := api.DeviceGetUUID(mh)
		if r != nvml.SUCCESS {
			continue
		}
		pt := gpu.Partition{ID: gpu.ID(uuid), ParentID: id, Index: i}
		if name, r := api.DeviceGetName(mh); r == nvml.SUCCESS {
			pt.Name = name
			pt.Profile = MIGProfileFromName(name)
		}
		if v, r := api.DeviceGetGpuInstanceId(mh); r == nvml.SUCCESS {
			pt.InstanceID = metric.Some(v)
		}
		if v, r := api.DeviceGetComputeInstanceId(mh); r == nvml.SUCCESS {
			pt.ComputeInstanceID = metric.Some(v)
		}
		if m, r := memoryInfo(api, mh); r == nvml.SUCCESS {
			pt.MemTotal = metric.Some(m.Total)
			pt.MemUsed = metric.Some(m.Used)
		}
		parts = append(parts, pt)
	}
	return parts, nil
}

// MIGProfileFromName extracts the profile ("1g.10gb") from a MIG device name
// such as "NVIDIA A100-SXM4-40GB MIG 1g.5gb".
func MIGProfileFromName(name string) string {
	if i := strings.LastIndex(name, "MIG "); i >= 0 {
		return strings.TrimSpace(name[i+4:])
	}
	return ""
}

// Topology implements gpu.TopologyProvider.
func (p *Provider) Topology(ctx context.Context, a, b gpu.ID) (gpu.TopologyLevel, error) {
	api, sa, err := p.state(a)
	if err != nil {
		return gpu.TopoUnknown, err
	}
	_, sb, err := p.state(b)
	if err != nil {
		return gpu.TopoUnknown, err
	}
	sa.mu.Lock()
	ha := sa.handle
	sa.mu.Unlock()
	sb.mu.Lock()
	hb := sb.handle
	sb.mu.Unlock()
	v, r := api.DeviceGetTopologyCommonAncestor(ha, hb)
	if r != nvml.SUCCESS {
		return gpu.TopoUnknown, mapErr(r)
	}
	switch v {
	case nvml.TOPOLOGY_INTERNAL:
		return gpu.TopoSame, nil
	case nvml.TOPOLOGY_SINGLE:
		return gpu.TopoSingle, nil
	case nvml.TOPOLOGY_MULTIPLE:
		return gpu.TopoMultiple, nil
	case nvml.TOPOLOGY_HOSTBRIDGE:
		return gpu.TopoHostBridge, nil
	case nvml.TOPOLOGY_NODE:
		return gpu.TopoNode, nil
	case nvml.TOPOLOGY_SYSTEM:
		return gpu.TopoSystem, nil
	}
	return gpu.TopoUnknown, nil
}

// WatchEvents implements gpu.EventSource using NVML event sets.
func (p *Provider) WatchEvents(ctx context.Context, emit func(gpu.DeviceEvent)) error {
	api, err := p.lib()
	if err != nil {
		return err
	}
	set, r := api.EventSetCreate()
	if r != nvml.SUCCESS {
		return mapErr(r)
	}
	defer func() { _ = api.EventSetFree(set) }()

	const wanted = nvml.EventTypeXidCriticalError | nvml.EventTypeSingleBitEccError |
		nvml.EventTypeDoubleBitEccError | nvml.EventMigConfigChange |
		nvml.EventTypeGpuUnavailableError | nvml.EventTypeGpuRecoveryAction

	p.mu.RLock()
	registered := 0
	for _, st := range p.devices {
		supported, r := api.DeviceGetSupportedEventTypes(st.handle)
		if r != nvml.SUCCESS {
			continue
		}
		if api.DeviceRegisterEvents(st.handle, supported&wanted, set) == nvml.SUCCESS {
			registered++
		}
	}
	p.mu.RUnlock()
	if registered == 0 {
		return fmt.Errorf("%w: no device supports event registration", gpu.ErrNotSupported)
	}

	for ctx.Err() == nil {
		data, r := api.EventSetWait(set, 1000)
		switch r {
		case nvml.SUCCESS:
		case nvml.ERROR_TIMEOUT:
			continue
		case nvml.ERROR_GPU_IS_LOST:
			// Keep waiting on the remaining devices.
			continue
		default:
			return mapErr(r)
		}
		p.mu.RLock()
		id := p.byHand[data.Device]
		p.mu.RUnlock()
		ev := gpu.DeviceEvent{Time: p.now(), DeviceID: id, Code: data.EventData}
		switch {
		case data.EventType&nvml.EventTypeXidCriticalError != 0:
			ev.Kind = "xid"
		case data.EventType&nvml.EventTypeDoubleBitEccError != 0:
			ev.Kind = "ecc_double_bit"
		case data.EventType&nvml.EventTypeSingleBitEccError != 0:
			ev.Kind = "ecc_single_bit"
		case data.EventType&nvml.EventMigConfigChange != 0:
			ev.Kind = "partition_config_change"
		case data.EventType&nvml.EventTypeGpuUnavailableError != 0:
			ev.Kind = "gpu_unavailable"
		case data.EventType&nvml.EventTypeGpuRecoveryAction != 0:
			ev.Kind = "recovery_action"
		default:
			ev.Kind = fmt.Sprintf("nvml_event_%#x", data.EventType)
		}
		switch ev.Kind {
		case "xid":
			ev.Detail = XIDDescription(data.EventData)
			ev.Severity = XIDSeverity(data.EventData)
		case "ecc_double_bit", "gpu_unavailable":
			ev.Severity = "critical"
		case "recovery_action", "ecc_single_bit":
			ev.Severity = "warning"
		default:
			ev.Severity = "info"
		}
		emit(ev)
	}
	return nil
}
