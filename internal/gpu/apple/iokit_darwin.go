// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package apple

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"golang.org/x/sys/unix"
)

type iokitBackend struct {
	mu    sync.Mutex
	accel uint32 // retained IOAccelerator registry entry

	// IOReport subscription; prev is the last raw sample.
	channels uintptr
	sub      uintptr
	subbed   uintptr
	prev     uintptr
	prevAt   time.Time
	freqs    []float64 // MHz of the active performance states P1..Pn

	smc      *smc
	tempKeys []smcKey

	hid     uintptr // HID event system client (temperature fallback)
	hidSvcs uintptr // CFArray of GPU temperature services
}

func newIOKitBackend() backend { return &iokitBackend{} }

func (b *iokitBackend) Open() (staticInfo, []gpu.Check, error) {
	var info staticInfo
	var checks []gpu.Check
	add := func(name string, ok bool, detail string) {
		checks = append(checks, gpu.Check{Name: name, OK: ok, Detail: detail})
	}
	if err := loadFrameworks(); err != nil {
		add("IOKit", false, err.Error())
		return info, checks, err
	}
	eachService("IOAccelerator", func(e uint32) bool {
		props := properties(e)
		defer func() {
			if props != 0 {
				cfRelease(props)
			}
		}()
		model := dictString(props, "model")
		if !strings.HasPrefix(model, "Apple") || dictGet(props, "PerformanceStatistics") == 0 {
			return true
		}
		info.Model = model
		if n, ok := dictInt(props, "gpu-core-count"); ok {
			info.Cores = int(n)
		}
		info.Driver = dictString(props, "IOSourceVersion")
		if cfg := dictGet(props, "GPUConfigurationVariable"); cfg != 0 {
			if gen, ok := dictInt(cfg, "gpu_gen"); ok {
				info.Architecture = fmt.Sprintf("AGX G%d%s", gen, dictString(cfg, "gpu_var"))
			}
		}
		ioRegistryEntryGetRegistryEntryID(e, &info.RegistryID)
		// Keep a reference: eachService releases e after we return.
		ioObjectRetain(e)
		b.accel = e
		return false
	})
	if b.accel == 0 {
		add("GPU", false, "no Apple silicon IOAccelerator found")
		return info, checks, errors.New("no Apple silicon GPU found")
	}
	detail := info.Model
	if info.Cores > 0 {
		detail += fmt.Sprintf(" · %d GPU cores", info.Cores)
	}
	add("GPU", true, detail)

	info.OSVersion, _ = unix.Sysctl("kern.osproductversion")
	info.MemTotal, _ = unix.SysctlUint64("hw.memsize")

	b.freqs = gpuFrequencies()
	if n := len(b.freqs); n > 0 {
		info.MaxFreqMHz = b.freqs[n-1]
	}
	if b.subscribe() {
		add("IOReport", true, fmt.Sprintf("GPU power and residency (%d performance states)", len(b.freqs)))
	} else {
		add("IOReport", false, "libIOReport unavailable: power and frequency are N/A")
	}

	switch {
	case b.openSMC():
		add("Temperature", true, fmt.Sprintf("%d SMC GPU sensors", len(b.tempKeys)))
	case b.openHID():
		add("Temperature", true, "HID GPU sensors")
	default:
		add("Temperature", false, "no GPU temperature sensors found")
	}
	return info, checks, nil
}

// gpuFrequencies reads the GPU DVFS table from the power manager: pairs of
// little-endian uint32 (frequency, voltage). The first (zero) entry is the
// powered-off state.
func gpuFrequencies() []float64 {
	var raw []uint32
	eachService("AppleARMIODevice", func(e uint32) bool {
		if entryName(e) != "pmgr" {
			return true
		}
		props := properties(e)
		defer cfRelease(props)
		for _, key := range []string{"voltage-states9", "voltage-states9-sram"} {
			data := dictBytes(props, key)
			for i := 0; i+8 <= len(data); i += 8 {
				if f := binary.LittleEndian.Uint32(data[i:]); f > 0 {
					raw = append(raw, f)
				}
			}
			if len(raw) > 0 {
				break
			}
		}
		return false
	})
	return scaleFrequencies(raw)
}

func (b *iokitBackend) subscribe() bool {
	if !haveIOReport {
		return false
	}
	energy, stats := cfstr("Energy Model"), cfstr("GPU Stats")
	pstates := cfstr("GPU Performance States")
	defer cfRelease(energy)
	defer cfRelease(stats)
	defer cfRelease(pstates)
	chans := ioReportCopyChannelsInGroup(energy, 0, 0, 0, 0)
	if chans == 0 {
		return false
	}
	if gpuChans := ioReportCopyChannelsInGroup(stats, pstates, 0, 0, 0); gpuChans != 0 {
		ioReportMergeChannels(chans, gpuChans, 0)
		cfRelease(gpuChans)
	}
	var subbed uintptr
	sub := ioReportCreateSubscription(0, chans, &subbed, 0, 0)
	if sub == 0 || subbed == 0 {
		cfRelease(chans)
		return false
	}
	b.channels, b.sub, b.subbed = chans, sub, subbed
	b.prev = ioReportCreateSamples(sub, subbed, 0)
	b.prevAt = time.Now()
	return b.prev != 0
}

func (b *iokitBackend) openSMC() bool {
	s, err := openSMC()
	if err != nil {
		return false
	}
	keys := s.keysWithPrefix("Tg")
	if len(keys) == 0 {
		s.Close()
		return false
	}
	b.smc, b.tempKeys = s, keys
	return true
}

// openHID matches the "GPU MTR Temp Sensor" services that M1/M2 machines
// publish through the HID event system.
func (b *iokitBackend) openHID() bool {
	if !haveHID {
		return false
	}
	client := ioHIDEventSystemClientCreate(0)
	if client == 0 {
		return false
	}
	match := cfDictionaryCreateMutable(0, 2, cfTypeDictionaryKeyCallBacks, cfTypeDictionaryValueCallBacks)
	for k, v := range map[string]int32{"PrimaryUsagePage": 0xff00, "PrimaryUsage": 5} {
		ks, vn := cfstr(k), cfNumber32(v)
		cfDictionarySetValue(match, ks, vn)
		cfRelease(ks)
		cfRelease(vn)
	}
	ioHIDEventSystemClientSetMatching(client, match)
	cfRelease(match)
	svcs := ioHIDEventSystemClientCopyServices(client)
	found := false
	product := cfstr("Product")
	defer cfRelease(product)
	arrayEach(svcs, func(svc uintptr) {
		name := ioHIDServiceClientCopyProperty(svc, product)
		if strings.HasPrefix(goString(name), "GPU MTR Temp Sensor") {
			found = true
		}
		if name != 0 {
			cfRelease(name)
		}
	})
	if !found {
		if svcs != 0 {
			cfRelease(svcs)
		}
		cfRelease(client)
		return false
	}
	b.hid, b.hidSvcs = client, svcs
	return true
}

func (b *iokitBackend) Read() reading {
	b.mu.Lock()
	defer b.mu.Unlock()
	var r reading
	if b.accel == 0 {
		return r
	}
	if stats := property(b.accel, "PerformanceStatistics"); stats != 0 {
		if v, ok := dictInt(stats, "Device Utilization %"); ok {
			r.UtilPercent = metric.Some(float64(v))
		}
		if v, ok := dictInt(stats, "In use system memory"); ok && v >= 0 {
			r.MemInUse = metric.Some(uint64(v))
		}
		if v, ok := dictInt(stats, "Alloc system memory"); ok && v >= 0 {
			r.MemAlloc = metric.Some(uint64(v))
		}
		cfRelease(stats)
	}
	b.readIOReport(&r)
	r.TempC = b.readTemp()
	return r
}

func (b *iokitBackend) readIOReport(r *reading) {
	if b.sub == 0 || b.prev == 0 {
		return
	}
	cur := ioReportCreateSamples(b.sub, b.subbed, 0)
	if cur == 0 {
		return
	}
	now := time.Now()
	delta := ioReportCreateSamplesDelta(b.prev, cur, 0)
	cfRelease(b.prev)
	b.prev, r.Interval = cur, now.Sub(b.prevAt)
	b.prevAt = now
	if delta == 0 {
		return
	}
	defer cfRelease(delta)
	var states []stateResidency
	arrayEach(dictGet(delta, "IOReportChannels"), func(ch uintptr) {
		name := goString(ioReportChannelGetChannelName(ch))
		switch goString(ioReportChannelGetGroup(ch)) {
		case "Energy Model":
			if name == "GPU Energy" {
				if j, ok := energyJoules(ioReportSimpleGetIntegerValue(ch, 0), goString(ioReportChannelGetUnitLabel(ch))); ok {
					r.EnergyJ = metric.Some(j)
				}
			}
		case "GPU Stats":
			if name != "GPUPH" {
				return
			}
			n := ioReportStateGetCount(ch)
			for i := int32(0); i < n; i++ {
				states = append(states, stateResidency{goString(ioReportStateGetNameForIndex(ch, i)), ioReportStateGetResidency(ch, i)})
			}
		}
	})
	if len(states) > 0 {
		_, r.FreqMHz = residency(states, b.freqs)
	}
}

func (b *iokitBackend) readTemp() metric.Opt[float64] {
	var vals []float64
	if b.smc != nil {
		for _, k := range b.tempKeys {
			if v, ok := b.smc.readFloat(k); ok {
				vals = append(vals, v)
			}
		}
	} else if b.hidSvcs != 0 {
		product := cfstr("Product")
		arrayEach(b.hidSvcs, func(svc uintptr) {
			name := ioHIDServiceClientCopyProperty(svc, product)
			gpuSensor := strings.HasPrefix(goString(name), "GPU MTR Temp Sensor")
			if name != 0 {
				cfRelease(name)
			}
			if !gpuSensor {
				return
			}
			if ev := ioHIDServiceClientCopyEvent(svc, ioHIDEventTypeTemperature, 0, 0); ev != 0 {
				vals = append(vals, ioHIDEventGetFloatValue(ev, ioHIDEventTypeTemperature<<16))
				cfRelease(ev)
			}
		})
		cfRelease(product)
	}
	return averageTemp(vals)
}

func (b *iokitBackend) Clients() []client {
	b.mu.Lock()
	accel := b.accel
	b.mu.Unlock()
	if accel == 0 {
		return nil
	}
	byPID := map[int]*client{}
	var order []int
	eachChild(accel, func(child uint32) {
		creator := property(child, "IOUserClientCreator")
		if creator == 0 {
			return
		}
		pid, ok := parseCreator(goString(creator))
		cfRelease(creator)
		if !ok {
			return
		}
		var total int64
		if usage := property(child, "AppUsage"); usage != 0 {
			arrayEach(usage, func(u uintptr) {
				if v, ok := dictInt(u, "accumulatedGPUTime"); ok && v > 0 {
					total += v
				}
			})
			cfRelease(usage)
		}
		c := byPID[pid]
		if c == nil {
			c = &client{PID: pid}
			byPID[pid] = c
			order = append(order, pid)
		}
		c.GPUTime += time.Duration(total)
	})
	out := make([]client, 0, len(order))
	for _, pid := range order {
		out = append(out, *byPID[pid])
	}
	return out
}

func (b *iokitBackend) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ref := range []*uintptr{&b.prev, &b.subbed, &b.sub, &b.channels, &b.hidSvcs, &b.hid} {
		if *ref != 0 {
			cfRelease(*ref)
			*ref = 0
		}
	}
	if b.accel != 0 {
		ioObjectRelease(b.accel)
		b.accel = 0
	}
	b.smc.Close()
	b.smc = nil
}
