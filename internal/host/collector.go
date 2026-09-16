// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package host

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	ghost "github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

// Collector gathers host telemetry. Rates are computed from consecutive
// counter readings, so the first reading of each rate is unavailable.
// Methods are safe for concurrent use.
type Collector struct {
	mu sync.Mutex

	info     Info
	infoAt   time.Time
	prevCPU  []cpu.TimesStat
	prevAll  *cpu.TimesStat
	prevNet  map[string]counterSample
	prevDisk map[string]diskSample
	ib       ibReader
}

type counterSample struct {
	at                     time.Time
	rx, tx, rxPkts, txPkts uint64
}

type diskSample struct {
	at                       time.Time
	rb, wb, rc, wc, ioTimeMs uint64
}

// NewCollector creates a host collector.
func NewCollector() *Collector {
	return &Collector{prevNet: map[string]counterSample{}, prevDisk: map[string]diskSample{}}
}

// Info returns host identity, cached for a minute.
func (c *Collector) Info(ctx context.Context) Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.infoAt.IsZero() && time.Since(c.infoAt) < time.Minute {
		return c.info
	}
	in := Info{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if hi, err := ghost.InfoWithContext(ctx); err == nil {
		in.Hostname = hi.Hostname
		in.Platform = strings.TrimSpace(hi.Platform + " " + hi.PlatformVersion)
		in.Kernel = hi.KernelVersion
		in.Uptime = time.Duration(hi.Uptime) * time.Second
		if hi.VirtualizationRole == "guest" {
			in.Virtualization = hi.VirtualizationSystem
		}
	}
	if c.info.CPUModel == "" {
		if ci, err := cpu.InfoWithContext(ctx); err == nil && len(ci) > 0 {
			in.CPUModel = strings.TrimSpace(ci[0].ModelName)
		}
	} else {
		in.CPUModel = c.info.CPUModel
	}
	c.info, c.infoAt = in, time.Now()
	return in
}

func busy(t cpu.TimesStat) (busyT, total float64) {
	total = t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal
	busyT = total - t.Idle - t.Iowait
	return
}

func pct(num, den float64) metric.Opt[float64] {
	if den <= 0 {
		return metric.None[float64]()
	}
	v := 100 * num / den
	return metric.Some(max(0, min(100, v)))
}

// CPU samples processor utilization, load and frequency (fast tier).
func (c *Collector) CPU(ctx context.Context) CPU {
	out := CPU{}
	out.Threads, _ = cpu.CountsWithContext(ctx, true)
	out.Cores, _ = cpu.CountsWithContext(ctx, false)

	all, errAll := cpu.TimesWithContext(ctx, false)
	per, errPer := cpu.TimesWithContext(ctx, true)

	c.mu.Lock()
	if errAll == nil && len(all) == 1 {
		if c.prevAll != nil {
			b1, t1 := busy(*c.prevAll)
			b2, t2 := busy(all[0])
			dt := t2 - t1
			out.UtilPercent = pct(b2-b1, dt)
			out.IOWaitPct = pct(all[0].Iowait-c.prevAll.Iowait, dt)
			out.StealPct = pct(all[0].Steal-c.prevAll.Steal, dt)
		}
		cur := all[0]
		c.prevAll = &cur
	}
	if errPer == nil {
		if len(c.prevCPU) == len(per) {
			out.PerCore = make([]float64, len(per))
			for i := range per {
				b1, t1 := busy(c.prevCPU[i])
				b2, t2 := busy(per[i])
				out.PerCore[i] = pct(b2-b1, t2-t1).Or(0)
			}
		}
		c.prevCPU = per
	}
	c.mu.Unlock()

	if l, err := load.AvgWithContext(ctx); err == nil {
		out.Load1, out.Load5, out.Load15 = metric.Some(l.Load1), metric.Some(l.Load5), metric.Some(l.Load15)
	}
	out.FreqMHz = cpuFrequency()
	return out
}

// Memory samples RAM and swap (fast tier).
func (c *Collector) Memory(ctx context.Context) Memory {
	var m Memory
	if v, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		m.Total, m.Used, m.Available = metric.Some(v.Total), metric.Some(v.Used), metric.Some(v.Available)
		m.Cached, m.Buffers = metric.Some(v.Cached), metric.Some(v.Buffers)
	}
	if s, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.SwapTotal, m.SwapUsed = metric.Some(s.Total), metric.Some(s.Used)
	}
	return m
}

// Network samples interface counters (normal tier).
func (c *Collector) Network(ctx context.Context) []NetIf {
	now := time.Now()
	counters, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil
	}
	flags := map[string][]string{}
	if ifs, err := gnet.InterfacesWithContext(ctx); err == nil {
		for _, i := range ifs {
			flags[i.Name] = i.Flags
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]NetIf, 0, len(counters))
	seen := map[string]bool{}
	for _, ct := range counters {
		n := NetIf{Name: ct.Name, Kind: classifyInterface(ct.Name, flags[ct.Name])}
		for _, f := range flags[ct.Name] {
			if f == "up" {
				n.Up = true
			}
		}
		n.RxErrors, n.TxErrors = metric.Some(ct.Errin), metric.Some(ct.Errout)
		n.RxDrops, n.TxDrops = metric.Some(ct.Dropin), metric.Some(ct.Dropout)
		n.SpeedMbps = linkSpeed(ct.Name)
		key := "eth/" + ct.Name
		seen[key] = true
		cur := counterSample{at: now, rx: ct.BytesRecv, tx: ct.BytesSent, rxPkts: ct.PacketsRecv, txPkts: ct.PacketsSent}
		if prev, ok := c.prevNet[key]; ok {
			n.RxBps, n.TxBps, n.RxPps, n.TxPps = rates(prev, cur)
		}
		c.prevNet[key] = cur
		out = append(out, n)
	}
	for _, port := range c.ib.read() {
		key := "ib/" + port.name
		seen[key] = true
		n := NetIf{Name: port.name, Kind: "infiniband", Up: port.up, SpeedMbps: port.speed}
		cur := counterSample{at: now, rx: port.rxBytes, tx: port.txBytes, rxPkts: port.rxPkts, txPkts: port.txPkts}
		if prev, ok := c.prevNet[key]; ok {
			n.RxBps, n.TxBps, n.RxPps, n.TxPps = rates(prev, cur)
		}
		n.RxErrors = port.rxErrors
		c.prevNet[key] = cur
		out = append(out, n)
	}
	for k := range c.prevNet {
		if !seen[k] {
			delete(c.prevNet, k)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if kindRank(out[i].Kind) != kindRank(out[j].Kind) {
			return kindRank(out[i].Kind) < kindRank(out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func kindRank(k string) int {
	switch k {
	case "infiniband":
		return 0
	case "ethernet":
		return 1
	case "virtual":
		return 2
	}
	return 3
}

func rates(prev, cur counterSample) (rx, tx, rxp, txp metric.Opt[float64]) {
	dt := cur.at.Sub(prev.at).Seconds()
	if dt <= 0 {
		return
	}
	f := func(a, b uint64) metric.Opt[float64] {
		if b < a { // counter reset / wrap
			return metric.None[float64]()
		}
		return metric.Some(float64(b-a) / dt)
	}
	return f(prev.rx, cur.rx), f(prev.tx, cur.tx), f(prev.rxPkts, cur.rxPkts), f(prev.txPkts, cur.txPkts)
}

var virtualPrefixes = []string{"veth", "docker", "br-", "cali", "flannel", "cni", "vxlan", "tun", "tap", "virbr", "kube", "cilium", "lxc", "weave", "genev", "utun", "awdl", "llw", "bridge", "gif", "stf", "anpi", "ap"}

func classifyInterface(name string, flags []string) string {
	for _, f := range flags {
		if f == "loopback" {
			return "loopback"
		}
	}
	if name == "lo" || strings.HasPrefix(name, "lo0") {
		return "loopback"
	}
	if strings.HasPrefix(name, "ib") {
		return "infiniband"
	}
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return "virtual"
		}
	}
	return "ethernet"
}

// Disks samples block device throughput (normal tier).
func (c *Collector) Disks(ctx context.Context) []BlockDevice {
	now := time.Now()
	stats, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]BlockDevice, 0, len(stats))
	seen := map[string]bool{}
	for name, s := range stats {
		if !physicalDisk(name) {
			continue
		}
		seen[name] = true
		d := BlockDevice{Name: name}
		cur := diskSample{at: now, rb: s.ReadBytes, wb: s.WriteBytes, rc: s.ReadCount, wc: s.WriteCount, ioTimeMs: s.IoTime}
		if prev, ok := c.prevDisk[name]; ok {
			dt := now.Sub(prev.at).Seconds()
			if dt > 0 && cur.rb >= prev.rb && cur.wb >= prev.wb && cur.rc >= prev.rc && cur.wc >= prev.wc {
				d.ReadBps = metric.Some(float64(cur.rb-prev.rb) / dt)
				d.WriteBps = metric.Some(float64(cur.wb-prev.wb) / dt)
				d.ReadIOPS = metric.Some(float64(cur.rc-prev.rc) / dt)
				d.WriteIOPS = metric.Some(float64(cur.wc-prev.wc) / dt)
				if runtime.GOOS == "linux" && cur.ioTimeMs >= prev.ioTimeMs {
					d.BusyPct = metric.Some(min(100, float64(cur.ioTimeMs-prev.ioTimeMs)/(dt*10)))
				}
			}
		}
		c.prevDisk[name] = cur
		out = append(out, d)
	}
	for k := range c.prevDisk {
		if !seen[k] {
			delete(c.prevDisk, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func physicalDisk(name string) bool {
	for _, p := range []string{"loop", "ram", "zram", "dm-", "sr", "fd", "nbd"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	// Skip partitions when the whole device is also reported.
	if strings.HasPrefix(name, "nvme") && strings.Contains(name, "p") && strings.LastIndex(name, "p") > strings.Index(name, "n1") {
		return false
	}
	if (strings.HasPrefix(name, "sd") || strings.HasPrefix(name, "vd") || strings.HasPrefix(name, "xvd")) &&
		len(name) > 0 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' {
		return false
	}
	return true
}

var ignoredFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "overlay": true, "squashfs": true, "proc": true, "sysfs": true,
	"cgroup": true, "cgroup2": true, "devfs": true, "autofs": true, "nsfs": true, "tracefs": true,
	"debugfs": true, "securityfs": true, "pstore": true, "bpf": true, "mqueue": true, "hugetlbfs": true,
	"fusectl": true, "configfs": true, "binfmt_misc": true, "efivarfs": true, "ramfs": true, "nullfs": true,
}

// Filesystems samples mounted filesystem usage (slow tier).
func (c *Collector) Filesystems(ctx context.Context) []Filesystem {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Filesystem
	for _, p := range parts {
		if ignoredFS[p.Fstype] || seen[p.Device] || strings.HasPrefix(p.Mountpoint, "/System/Volumes/") && p.Mountpoint != "/System/Volumes/Data" {
			continue
		}
		seen[p.Device] = true
		fs := Filesystem{Mount: p.Mountpoint, Device: p.Device, FSType: p.Fstype}
		uctx, cancel := context.WithTimeout(ctx, time.Second) // hung NFS mounts must not block
		if u, err := disk.UsageWithContext(uctx, p.Mountpoint); err == nil && u.Total > 0 {
			fs.Total, fs.Used = metric.Some(u.Total), metric.Some(u.Used)
		}
		cancel()
		out = append(out, fs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}
