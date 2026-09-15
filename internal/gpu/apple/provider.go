// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package apple implements gpu.Provider for the integrated GPU of Apple
// silicon Macs (M1 and later).
//
// Everything is read without root privileges and without cgo:
//
//   - IOKit (IOAccelerator "PerformanceStatistics"): device utilization and
//     GPU memory drawn from the unified memory pool;
//   - IOReport ("Energy Model" and "GPU Stats" groups): GPU power, active
//     residency and the average frequency of the active performance states;
//   - the SMC ("Tg??" keys): GPU die temperature;
//   - AGXDeviceUserClient registry entries: per-process accumulated GPU time.
//
// Apple exposes no ECC, throttle-reason, power-limit, PCIe or partitioning
// telemetry for its GPUs; those metrics are reported as not supported.
package apple

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/metric"
)

// staticInfo is the inventory read once when the backend opens.
type staticInfo struct {
	Model        string // "Apple M4"
	Cores        int
	RegistryID   uint64
	Architecture string // "G16G"
	Driver       string // AGX kext source version
	OSVersion    string
	MemTotal     uint64 // unified memory
	MaxFreqMHz   float64
}

// reading is one fast-tier sample from the backend.
type reading struct {
	UtilPercent metric.Opt[float64]
	MemInUse    metric.Opt[uint64]
	MemAlloc    metric.Opt[uint64]
	// EnergyJ is the GPU energy consumed over Interval.
	EnergyJ  metric.Opt[float64]
	Interval time.Duration
	FreqMHz  metric.Opt[float64]
	TempC    metric.Opt[float64]
}

// client is a process's accumulated GPU time across its Metal user clients.
type client struct {
	PID     int
	GPUTime time.Duration
}

// backend reads the hardware. The darwin implementation uses IOKit; tests
// substitute a fake.
type backend interface {
	Open() (staticInfo, []gpu.Check, error)
	Read() reading
	Clients() []client
	Close()
}

// Options configure the provider.
type Options struct {
	// Backend overrides hardware access (tests).
	Backend func() backend
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Provider is the Apple silicon gpu.Provider.
type Provider struct {
	newBackend func() backend
	now        func() time.Time

	mu       sync.Mutex
	be       backend
	info     staticInfo
	energyJ  float64
	procPrev map[int]procMark
}

type procMark struct {
	gpuTime time.Duration
	at      time.Time
}

var _ gpu.Provider = (*Provider)(nil)

// New creates an Apple provider. Call Open before use.
func New(opts Options) *Provider {
	p := &Provider{newBackend: opts.Backend, now: opts.Now, procPrev: map[int]procMark{}}
	if p.newBackend == nil {
		p.newBackend = newIOKitBackend
	}
	if p.now == nil {
		p.now = time.Now
	}
	return p
}

// Name implements gpu.Provider.
func (p *Provider) Name() string { return "iokit" }

// Vendor implements gpu.Provider.
func (p *Provider) Vendor() gpu.Vendor { return gpu.VendorApple }

// Open implements gpu.Provider.
func (p *Provider) Open(ctx context.Context) (gpu.Diagnostics, error) {
	diag := gpu.Diagnostics{Provider: "Apple silicon (IOKit)"}
	be := p.newBackend()
	info, checks, err := be.Open()
	diag.Checks = append([]gpu.Check{{Name: "Platform", OK: err == nil || runtime.GOOS == "darwin", Detail: runtime.GOOS + "/" + runtime.GOARCH}}, checks...)
	if err != nil {
		be.Close()
		if runtime.GOOS != "darwin" {
			diag.Hints = append(diag.Hints, "Apple GPUs are monitored on the Mac itself. On Linux, gputop uses NVIDIA NVML (gpu.providers: [nvidia]).")
		} else {
			diag.Hints = append(diag.Hints, "Apple GPU monitoring requires an Apple silicon Mac (M1 or later).")
		}
		diag.Hints = append(diag.Hints, "Try `gputop --demo` to explore the interface with simulated GPUs.")
		return diag, fmt.Errorf("%w: %w", gpu.ErrUnavailable, err)
	}
	p.mu.Lock()
	if p.be != nil {
		p.be.Close()
	}
	p.be, p.info = be, info
	p.mu.Unlock()
	return diag, nil
}

// Close implements gpu.Provider.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.be != nil {
		p.be.Close()
		p.be = nil
	}
	return nil
}

func (p *Provider) backendFor(id gpu.ID) (backend, staticInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.be == nil {
		return nil, staticInfo{}, gpu.ErrUnavailable
	}
	if id != "" && id != deviceID(p.info) {
		return nil, staticInfo{}, fmt.Errorf("%w: %s", gpu.ErrNotFound, id)
	}
	return p.be, p.info, nil
}

func deviceID(info staticInfo) gpu.ID {
	return gpu.ID(fmt.Sprintf("apple-gpu-%x", info.RegistryID))
}

// System implements gpu.Provider.
func (p *Provider) System(ctx context.Context) (gpu.SystemInfo, error) {
	_, info, err := p.backendFor("")
	if err != nil {
		return gpu.SystemInfo{}, err
	}
	return gpu.SystemInfo{DriverVersion: info.Driver, RuntimeName: "macOS", RuntimeVersion: info.OSVersion}, nil
}

// Devices implements gpu.Provider. Apple silicon has exactly one GPU.
func (p *Provider) Devices(ctx context.Context) ([]gpu.Device, error) {
	_, info, err := p.backendFor("")
	if err != nil {
		return nil, err
	}
	name := info.Model
	if name == "" {
		name = "Apple GPU"
	}
	if info.Cores > 0 {
		name = fmt.Sprintf("%s %d-core GPU", name, info.Cores)
	}
	d := gpu.Device{
		ID: deviceID(info), Vendor: gpu.VendorApple, Index: 0,
		Name: name, Brand: "Apple", Architecture: info.Architecture, FirmwareVersion: info.Driver,
		Memory:          metric.Some(info.MemTotal),
		PersistenceMode: metric.None[bool](), ECCEnabled: metric.None[bool](), NUMANode: metric.None[int](),
		Capabilities: gpu.Capabilities{
			gpu.CapEnergy: gpu.CapSupported, gpu.CapProcessUtil: gpu.CapSupported,
			gpu.CapECC: gpu.CapUnsupported, gpu.CapFan: gpu.CapUnsupported, gpu.CapMIG: gpu.CapUnsupported,
			gpu.CapNVLink: gpu.CapUnsupported, gpu.CapPowerLimit: gpu.CapUnsupported,
			gpu.CapClockReasons: gpu.CapUnsupported, gpu.CapPCIeThroughput: gpu.CapUnsupported,
			gpu.CapEncoder: gpu.CapUnsupported, gpu.CapDecoder: gpu.CapUnsupported,
		},
	}
	if info.MemTotal == 0 {
		d.Memory = metric.None[uint64]()
	}
	if info.MaxFreqMHz > 0 {
		d.ClockCoreMaxMHz = metric.Some(info.MaxFreqMHz)
	}
	return []gpu.Device{d}, nil
}

// Sample implements gpu.Provider.
func (p *Provider) Sample(ctx context.Context, id gpu.ID) (gpu.Sample, error) {
	be, info, err := p.backendFor(id)
	if err != nil {
		return gpu.Sample{}, err
	}
	r := be.Read()
	s := gpu.Sample{
		Time:         p.now(),
		UtilPercent:  r.UtilPercent,
		MemUsed:      r.MemInUse,
		MemReserved:  r.MemAlloc,
		TempC:        r.TempC,
		ClockCoreMHz: r.FreqMHz,
	}
	if info.MemTotal > 0 {
		s.MemTotal = metric.Some(info.MemTotal)
	}
	if r.EnergyJ.OK && r.Interval > 0 {
		s.PowerW = metric.Some(r.EnergyJ.V / r.Interval.Seconds())
		p.mu.Lock()
		p.energyJ += r.EnergyJ.V
		s.EnergyJ = metric.Some(p.energyJ)
		p.mu.Unlock()
	}
	return s, nil
}

// Processes implements gpu.Provider. Utilization is the share of wall time
// the process occupied the GPU since the previous call.
func (p *Provider) Processes(ctx context.Context, id gpu.ID) ([]gpu.Process, error) {
	be, info, err := p.backendFor(id)
	if err != nil {
		return nil, err
	}
	clients := be.Clients()
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := make(map[int]procMark, len(clients))
	out := make([]gpu.Process, 0, len(clients))
	for _, c := range clients {
		seen[c.PID] = procMark{gpuTime: c.GPUTime, at: now}
		if c.GPUTime <= 0 {
			continue
		}
		proc := gpu.Process{PID: c.PID, DeviceID: deviceID(info), Type: gpu.ProcessGraphics}
		if prev, ok := p.procPrev[c.PID]; ok && now.After(prev.at) && c.GPUTime >= prev.gpuTime {
			pct := float64(c.GPUTime-prev.gpuTime) / float64(now.Sub(prev.at)) * 100
			proc.SMUtil = metric.Some(min(100, pct))
		}
		out = append(out, proc)
	}
	p.procPrev = seen
	return out, nil
}

// Health implements gpu.Provider. Apple exposes no reliability counters.
func (p *Provider) Health(ctx context.Context, id gpu.ID) (gpu.HealthCounters, error) {
	return gpu.HealthCounters{}, gpu.ErrNotSupported
}

// Links implements gpu.Provider.
func (p *Provider) Links(ctx context.Context, id gpu.ID) ([]gpu.Link, error) { return nil, nil }

// Partitions implements gpu.Provider.
func (p *Provider) Partitions(ctx context.Context, id gpu.ID) ([]gpu.Partition, error) {
	return nil, nil
}

// parseCreator extracts the PID from an IOUserClientCreator value
// ("pid 407, WindowServer"). The name is truncated by the kernel; the
// collector resolves full process metadata from the PID.
func parseCreator(s string) (int, bool) {
	rest, ok := strings.CutPrefix(s, "pid ")
	if !ok {
		return 0, false
	}
	num, _, _ := strings.Cut(rest, ",")
	pid, err := strconv.Atoi(num)
	return pid, err == nil && pid > 0
}
