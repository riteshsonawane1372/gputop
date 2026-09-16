// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package sim implements a simulated gpu.Provider used by `gputop --demo`,
// by tests and by benchmarks. Every value it produces is fabricated and is
// tagged with metric.SourceSimulated / gpu.VendorSimulated so it can never
// be mistaken for hardware telemetry.
//
// The simulation tells a story useful for exploring the UI: a distributed
// training job where one GPU is a straggler (its PCIe link trained at x8),
// a MIG-partitioned inference GPU, an allocated-but-idle notebook GPU,
// periodic thermal throttling and occasional Xid events.
package sim

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// Options configure the simulation.
type Options struct {
	GPUs  int
	Model string // display name, default "NVIDIA H100 80GB HBM" style
	Now   func() time.Time
	// Speed multiplies simulated time (tests).
	Speed float64
}

// Provider is a simulated accelerator provider.
type Provider struct {
	opts  Options
	start time.Time
	now   func() time.Time

	mu      sync.Mutex
	role    []role
	energy  []float64
	lastE   []time.Time
	nvlTx   []float64
	eccCorr []uint64
	pcieTx  []float64
}

type role int

const (
	roleTrain role = iota
	roleStraggler
	roleMIG
	roleIdle
)

var (
	_ gpu.Provider         = (*Provider)(nil)
	_ gpu.EventSource      = (*Provider)(nil)
	_ gpu.TopologyProvider = (*Provider)(nil)
)

const (
	memTotal   = 80 << 30
	powerLimit = 700.0
	linkCount  = 18
)

// New creates a simulated provider.
func New(opts Options) *Provider {
	if opts.GPUs <= 0 {
		opts.GPUs = 8
	}
	if opts.GPUs > 64 {
		opts.GPUs = 64
	}
	if opts.Model == "" {
		opts.Model = "NVIDIA H100 80GB HBM (simulated)"
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Speed <= 0 {
		opts.Speed = 1
	}
	n := opts.GPUs
	p := &Provider{
		opts: opts, now: opts.Now, start: opts.Now(),
		role: make([]role, n), energy: make([]float64, n), lastE: make([]time.Time, n),
		nvlTx: make([]float64, n), eccCorr: make([]uint64, n), pcieTx: make([]float64, n),
	}
	for i := range p.role {
		switch {
		case n >= 4 && i == 3:
			p.role[i] = roleStraggler
		case n >= 3 && i == n-2:
			p.role[i] = roleMIG
		case n >= 2 && i == n-1:
			p.role[i] = roleIdle
		default:
			p.role[i] = roleTrain
		}
		p.energy[i] = 3.6e9 + float64(i)*1e8 // joules since "driver load"
	}
	return p
}

func (p *Provider) elapsed() float64 {
	return p.now().Sub(p.start).Seconds() * p.opts.Speed
}

// ID returns the simulated UUID of device i.
func ID(i int) gpu.ID {
	return gpu.ID(fmt.Sprintf("GPU-5e1a7ed0-0000-4000-8000-%012x", i))
}

func (p *Provider) index(id gpu.ID) (int, error) {
	for i := 0; i < p.opts.GPUs; i++ {
		if ID(i) == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%w: %s", gpu.ErrNotFound, id)
}

// Name implements gpu.Provider.
func (p *Provider) Name() string { return "simulated" }

// Vendor implements gpu.Provider.
func (p *Provider) Vendor() gpu.Vendor { return gpu.VendorSimulated }

// Open implements gpu.Provider.
func (p *Provider) Open(ctx context.Context) (gpu.Diagnostics, error) {
	return gpu.Diagnostics{
		Provider: "Simulated (--demo)",
		Checks:   []gpu.Check{{Name: "Simulation", OK: true, Detail: fmt.Sprintf("%d simulated GPUs — no real hardware is being read", p.opts.GPUs)}},
	}, nil
}

// Close implements gpu.Provider.
func (p *Provider) Close() error { return nil }

// System implements gpu.Provider.
func (p *Provider) System(ctx context.Context) (gpu.SystemInfo, error) {
	return gpu.SystemInfo{DriverVersion: "simulated", LibraryVersion: "simulated", RuntimeName: "CUDA", RuntimeVersion: "12.6"}, nil
}

// Devices implements gpu.Provider.
func (p *Provider) Devices(ctx context.Context) ([]gpu.Device, error) {
	out := make([]gpu.Device, p.opts.GPUs)
	for i := range out {
		caps := gpu.Capabilities{
			gpu.CapNVLink: gpu.CapSupported, gpu.CapECC: gpu.CapSupported, gpu.CapMemoryTemp: gpu.CapSupported,
			gpu.CapEnergy: gpu.CapSupported, gpu.CapEncoder: gpu.CapSupported, gpu.CapDecoder: gpu.CapSupported,
			gpu.CapProcessUtil: gpu.CapSupported, gpu.CapPCIeThroughput: gpu.CapSupported,
			gpu.CapClockReasons: gpu.CapSupported, gpu.CapRemappedRows: gpu.CapSupported,
			gpu.CapXIDEvents: gpu.CapSupported, gpu.CapPowerLimit: gpu.CapSupported,
			gpu.CapMIG: gpu.CapSupported, gpu.CapFan: gpu.CapUnsupported, gpu.CapHotspotTemp: gpu.CapUnsupported,
			gpu.CapJPEG: gpu.CapUnsupported, gpu.CapOFA: gpu.CapUnsupported, gpu.CapViolationCounter: gpu.CapSupported,
		}
		out[i] = gpu.Device{
			ID: ID(i), Vendor: gpu.VendorSimulated, Index: i,
			Name: p.opts.Model, Brand: "NVIDIA", Architecture: "Hopper", ComputeCapability: "9.0",
			Serial: fmt.Sprintf("SIM%010d", 1650000000+i), PartNumber: "SIM-0000-0000",
			FirmwareVersion: "96.00.99.00.01",
			PCI:             gpu.PCIAddress{BusID: busID(i), DeviceID: 0x233010de},
			NUMANode:        metric.Some(i * 2 / p.opts.GPUs),
			Memory:          metric.Some(uint64(memTotal)),
			PersistenceMode: metric.Some(true), ComputeMode: "default", ECCEnabled: metric.Some(true),
			PowerLimitDefaultW: metric.Some(powerLimit), PowerLimitMinW: metric.Some(200.0), PowerLimitMaxW: metric.Some(powerLimit),
			TempSlowdownC: metric.Some(92.0), TempShutdownC: metric.Some(97.0), TempMaxOpC: metric.Some(87.0), MemTempMaxC: metric.Some(95.0),
			ClockCoreMaxMHz: metric.Some(2890.0), ClockMemMaxMHz: metric.Some(3200.0),
			PCIeMaxGen: metric.Some(5), PCIeMaxWidth: metric.Some(16), PCIeDeviceMaxGen: metric.Some(5),
			MIG:          gpu.MIGMode{Supported: true, Enabled: p.role[i] == roleMIG, Pending: p.role[i] == roleMIG, MaxInstances: 7},
			LinkCount:    linkCount,
			Capabilities: caps,
		}
	}
	return out, nil
}

func busID(i int) string { return fmt.Sprintf("0000:%02x:00.0", 0x18+i*0x10) }

// utilAt returns the simulated utilization of device i at time t.
func (p *Provider) utilAt(i int, t float64) float64 {
	// Training step rhythm: ~6s steps with a short dip for data loading
	// and an all-reduce, identical across ranks.
	step := math.Mod(t, 6.0)
	train := 97.0 - 1.5*math.Sin(t/7)
	if step > 5.2 {
		train = 62
	}
	if math.Mod(t, 300) > 280 { // checkpoint every 5 minutes
		train = 8
	}
	switch p.role[i] {
	case roleTrain:
		return clamp(train-float64(i%3)*0.7+jitter(i, t, 1.2), 0, 100)
	case roleStraggler:
		return clamp(train*0.55+jitter(i, t, 3), 0, 100)
	case roleMIG:
		return clamp(48+22*math.Sin(t/45)+jitter(i, t, 6), 0, 100)
	default:
		return clamp(0.4+math.Max(0, jitter(i, t, 0.8)), 0, 100)
	}
}

// thermalEpisode is true during a 45s window every 7 minutes on one GPU.
func (p *Provider) thermalEpisode(i int, t float64) bool {
	target := 5
	if p.opts.GPUs <= 5 {
		target = 0
	}
	return i == target && math.Mod(t+300, 420) < 45
}

func jitter(i int, t, amp float64) float64 {
	x := math.Sin(t*1.7+float64(i)*3.1) + 0.6*math.Sin(t*4.3+float64(i)*1.3) + 0.3*math.Sin(t*11.9+float64(i))
	return amp * x / 1.9
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// Sample implements gpu.Provider.
func (p *Provider) Sample(ctx context.Context, id gpu.ID) (gpu.Sample, error) {
	i, err := p.index(id)
	if err != nil {
		return gpu.Sample{}, err
	}
	now := p.now()
	t := p.elapsed()
	u := p.utilAt(i, t)
	hot := p.thermalEpisode(i, t)

	var memUsed float64
	switch p.role[i] {
	case roleTrain, roleStraggler:
		memUsed = 71.2e9 + 0.4e9*math.Sin(t/30+float64(i))
	case roleMIG:
		memUsed = 38e9 + 3e9*math.Sin(t/90)
	default:
		memUsed = 17.6e9
	}
	power := 72 + (powerLimit-110)*u/100 + jitter(i, t, 8)
	var reasons gpu.ThrottleReasons
	if u < 5 {
		reasons |= gpu.ThrottleIdle
	}
	if power > powerLimit-40 {
		reasons |= gpu.ThrottleSWPowerCap
	}
	temp := 34 + 0.38*u + jitter(i, t/3, 1.5)
	clock := 2890.0
	if hot {
		temp = 89 + jitter(i, t, 1)
		reasons |= gpu.ThrottleSWThermal
		clock = 1650
		u *= 0.8
		power *= 0.78
	}
	if u < 5 {
		clock = 180
	}

	p.mu.Lock()
	if !p.lastE[i].IsZero() {
		dt := now.Sub(p.lastE[i]).Seconds()
		p.energy[i] += power * dt
		p.pcieTx[i] += dt
	}
	p.lastE[i] = now
	energy := p.energy[i]
	p.mu.Unlock()

	width := 16
	if p.role[i] == roleStraggler {
		width = 8
	}
	pcie := 1.2e9 * u / 100
	if p.role[i] == roleStraggler {
		pcie = 0.62e9 * u / 100
	}
	return gpu.Sample{
		Time:                now,
		UtilPercent:         metric.Some(math.Round(u)),
		MemBandwidthPercent: metric.Some(math.Round(clamp(u*0.46+jitter(i, t, 2), 0, 100))),
		EncoderPercent:      metric.Some(0.0),
		DecoderPercent:      metric.Some(0.0),
		MemTotal:            metric.Some(uint64(memTotal)),
		MemUsed:             metric.Some(uint64(memUsed)),
		MemFree:             metric.Some(uint64(memTotal - memUsed)),
		MemReserved:         metric.Some(uint64(562 << 20)),
		TempC:               metric.Some(math.Round(temp)),
		MemTempC:            metric.Some(math.Round(temp + 6)),
		PowerW:              metric.Some(math.Round(power*10) / 10),
		PowerLimitW:         metric.Some(powerLimit),
		EnergyJ:             metric.Some(energy),
		ClockCoreMHz:        metric.Some(clock),
		ClockMemMHz:         metric.Some(3200.0),
		PState:              metric.Some(map[bool]int{true: 8, false: 0}[u < 5]),
		Throttle:            metric.Some(reasons),
		PCIeGen:             metric.Some(5),
		PCIeWidth:           metric.Some(width),
		PCIeTxBps:           metric.Some(pcie * (0.8 + 0.2*math.Sin(t))),
		PCIeRxBps:           metric.Some(pcie * 1.6 * (0.8 + 0.2*math.Cos(t))),
	}, nil
}

type simProc struct {
	pid       int
	name, cmd string
	user      string
	mem       float64
	part      int // -1 for none
	sm        float64
	pod, ns   string
	kind, wl  string
	container string
}

func (p *Provider) procs(i int, t float64) []simProc {
	switch p.role[i] {
	case roleTrain, roleStraggler:
		u := p.utilAt(i, t)
		return []simProc{{
			pid: 210400 + i, name: "python", user: "mlops", part: -1,
			cmd: fmt.Sprintf("python -m torch.distributed.run train.py --model llama-70b --rank %d", i),
			mem: 70.4e9, sm: u, pod: fmt.Sprintf("llama-70b-pretrain-worker-%d", i), ns: "ml-training",
			kind: "Job", wl: "llama-70b-pretrain", container: "trainer",
		}}
	case roleMIG:
		u := p.utilAt(i, t)
		return []simProc{
			{pid: 188100, name: "vllm", user: "svc-infer", part: 0, cmd: "python -m vllm.entrypoints.openai.api_server --model mistral-7b",
				mem: 26e9, sm: u * 0.6, pod: "chat-api-7d9f8b6c5-x2kqp", ns: "inference", kind: "Deployment", wl: "chat-api", container: "vllm"},
			{pid: 188240, name: "tritonserver", user: "svc-infer", part: 1, cmd: "tritonserver --model-repository=/models",
				mem: 12e9, sm: u * 0.4, pod: "embed-svc-6c7d9f5b8-p4mzn", ns: "inference", kind: "Deployment", wl: "embed-svc", container: "triton"},
		}
	default:
		return []simProc{{
			pid: 99321, name: "python", user: "alice", part: -1, cmd: "/opt/conda/bin/python -m ipykernel_launcher",
			mem: 17.2e9, sm: 0, pod: "notebook-alice-0", ns: "research", kind: "StatefulSet", wl: "notebook-alice", container: "notebook",
		}}
	}
}

// Processes implements gpu.Provider.
func (p *Provider) Processes(ctx context.Context, id gpu.ID) ([]gpu.Process, error) {
	i, err := p.index(id)
	if err != nil {
		return nil, err
	}
	t := p.elapsed()
	start := p.start.Add(-3*time.Hour - time.Duration(i)*time.Minute)
	var out []gpu.Process
	for _, sp := range p.procs(i, t) {
		pr := gpu.Process{
			PID: sp.pid, DeviceID: id, Type: gpu.ProcessCompute,
			MemUsed: metric.Some(uint64(sp.mem)), SMUtil: metric.Some(math.Round(sp.sm)),
			MemUtil: metric.Some(math.Round(sp.sm * 0.45)), EncUtil: metric.Some(0.0), DecUtil: metric.Some(0.0),
			Meta: &gpu.ProcessMeta{
				Name: sp.name, User: sp.user, Command: sp.cmd,
				ContainerID: fmt.Sprintf("%064x", sp.pid), PodUID: fmt.Sprintf("5e1a7ed0-0000-4000-8000-%012x", sp.pid),
				PodName: sp.pod, Namespace: sp.ns, WorkloadKind: sp.kind, WorkloadName: sp.wl, StartTime: start,
			},
		}
		if sp.part >= 0 {
			pr.PartitionID = migID(i, sp.part)
		}
		out = append(out, pr)
	}
	return out, nil
}

func migID(gpuIndex, part int) gpu.ID {
	return gpu.ID(fmt.Sprintf("MIG-5e1a7ed0-%04x-4000-8000-%012x", gpuIndex, part))
}

// Health implements gpu.Provider.
func (p *Provider) Health(ctx context.Context, id gpu.ID) (gpu.HealthCounters, error) {
	i, err := p.index(id)
	if err != nil {
		return gpu.HealthCounters{}, err
	}
	t := p.elapsed()
	p.mu.Lock()
	if i == 1 && math.Mod(t, 240) < 5 && p.eccCorr[i] < uint64(t/240)+1 {
		p.eccCorr[i]++
	}
	corr := p.eccCorr[i]
	p.mu.Unlock()
	replays := uint64(0)
	if p.role[i] == roleStraggler {
		replays = 37 + uint64(t/20)
	}
	return gpu.HealthCounters{
		Time:                    p.now(),
		ECCCorrectedVolatile:    metric.Some(corr),
		ECCUncorrectedVolatile:  metric.Some(uint64(0)),
		ECCCorrectedAggregate:   metric.Some(corr + 12),
		ECCUncorrectedAggregate: metric.Some(uint64(0)),
		RemappedCorrectable:     metric.Some(uint64(0)),
		RemappedUncorrectable:   metric.Some(uint64(0)),
		RemapPending:            metric.Some(false),
		RemapFailure:            metric.Some(false),
		PCIeReplays:             metric.Some(replays),
		PCIeCorrectableErrors:   metric.Some(replays / 4),
		PCIeNonFatalErrors:      metric.Some(uint64(0)),
		PCIeFatalErrors:         metric.Some(uint64(0)),
		ViolationPower:          metric.Some(time.Duration(t*0.02*1e9) * time.Nanosecond),
		ViolationThermal:        metric.Some(time.Duration(0)),
		RecoveryAction:          metric.Some("none"),
	}, nil
}

// Links implements gpu.Provider.
func (p *Provider) Links(ctx context.Context, id gpu.ID) ([]gpu.Link, error) {
	i, err := p.index(id)
	if err != nil {
		return nil, err
	}
	t := p.elapsed()
	u := p.utilAt(i, t)
	links := make([]gpu.Link, linkCount)
	for l := range links {
		base := t * 2.4e9 * (0.2 + u/100) / linkCount
		st := gpu.LinkActive
		if p.role[i] == roleIdle && l >= 12 {
			st = gpu.LinkSleep
		}
		links[l] = gpu.Link{
			Index: l, Kind: "nvlink", Version: metric.Some(4), State: st,
			RemoteType: gpu.EndpointSwitch, RemoteBusID: fmt.Sprintf("0000:%02x:00.0", 0xa0+l%4),
			TxBytes: metric.Some(uint64(base * (1 + 0.01*float64(l)))), RxBytes: metric.Some(uint64(base * 0.98)),
			ErrReplay: metric.Some(uint64(0)), ErrRecovery: metric.Some(uint64(0)),
			ErrCRCFlit: metric.Some(uint64(0)), ErrCRCData: metric.Some(uint64(0)),
		}
		if p.role[i] == roleStraggler && l == 7 {
			links[l].ErrCRCFlit = metric.Some(uint64(t / 30))
		}
	}
	return links, nil
}

// Partitions implements gpu.Provider.
func (p *Provider) Partitions(ctx context.Context, id gpu.ID) ([]gpu.Partition, error) {
	i, err := p.index(id)
	if err != nil {
		return nil, err
	}
	if p.role[i] != roleMIG {
		return nil, nil
	}
	t := p.elapsed()
	mk := func(idx int, profile string, total, used float64, gi int) gpu.Partition {
		return gpu.Partition{
			ID: migID(i, idx), ParentID: id, Index: idx, Profile: profile,
			Name:       "NVIDIA H100 80GB MIG " + profile + " (simulated)",
			InstanceID: metric.Some(gi), ComputeInstanceID: metric.Some(0),
			MemTotal: metric.Some(uint64(total)), MemUsed: metric.Some(uint64(used)),
		}
	}
	return []gpu.Partition{
		mk(0, "3g.40gb", 40e9, 26e9+2e9*math.Sin(t/90), 2),
		mk(1, "2g.20gb", 20e9, 12e9, 3),
		mk(2, "1g.10gb", 10e9, 0.03e9, 9),
	}, nil
}

// Topology implements gpu.TopologyProvider.
func (p *Provider) Topology(ctx context.Context, a, b gpu.ID) (gpu.TopologyLevel, error) {
	ia, err := p.index(a)
	if err != nil {
		return gpu.TopoUnknown, err
	}
	ib, err := p.index(b)
	if err != nil {
		return gpu.TopoUnknown, err
	}
	half := max(1, p.opts.GPUs/2)
	switch {
	case ia == ib:
		return gpu.TopoSame, nil
	case ia/2 == ib/2:
		return gpu.TopoSingle, nil
	case ia/half == ib/half:
		return gpu.TopoNode, nil
	}
	return gpu.TopoSystem, nil
}

// WatchEvents implements gpu.EventSource. It emits a simulated Xid 13 on
// GPU 2 two minutes after start and every 15 minutes afterwards.
func (p *Provider) WatchEvents(ctx context.Context, emit func(gpu.DeviceEvent)) error {
	target := min(2, p.opts.GPUs-1)
	next := 120.0
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if p.elapsed() >= next {
				next += 900
				emit(gpu.DeviceEvent{
					Time: p.now(), DeviceID: ID(target), Kind: "xid", Code: 13,
					Detail: "Graphics Engine Exception (simulated)", Severity: "warning",
				})
			}
		}
	}
}
