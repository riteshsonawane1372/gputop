// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

// splitTableChart lays out a GPU table above a live chart of the selected GPU.
func (m *Model) splitTableChart(w, h int, table func(w, h int) widgets.Block, chartTitle, key string, maxV float64, unit string) widgets.Block {
	s := m.view()
	tableH := min(len(s.GPUs)+3, h-8)
	if h-tableH < 8 {
		tableH = h
	}
	out := table(w, tableH)
	if ch := h - tableH; ch >= 6 {
		g, _ := m.selected()
		vals := m.live.values(key+"/"+string(g.Device.ID), 0)
		cur := na
		if n := len(vals); n > 0 && vals[n-1] == vals[n-1] {
			cur = fmtVal(vals[n-1], unit)
		}
		lines := []string{m.th.Dim.Render("now ") + m.th.Primary.Render(cur)}
		lines = append(lines, widgets.Chart(m.th, vals, w-2, ch-3, widgets.ChartOpts{Min: 0, Max: maxV, Cursor: -1})...)
		title := fmt.Sprintf("%s · GPU %d (live)", chartTitle, g.Device.Index)
		out = append(out, widgets.Box(m.th, widgets.BoxOpts{Title: title, RightTitle: "[ ] or ↑↓ select GPU"}, w, ch, lines)...)
	}
	return out
}

func viewMemory(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "VRAM", Width: 26, Min: 14, Flex: true},
			{Title: "USED", Width: 9, Align: widgets.Right},
			{Title: "FREE", Width: 9, Align: widgets.Right, Priority: 2},
			{Title: "RESERVED", Width: 9, Align: widgets.Right, Priority: 4},
			{Title: "BANDWIDTH", Width: 16, Min: 8, Priority: 1},
			{Title: "TOP CONSUMER", Width: 24, Min: 10, Priority: 3, Flex: true},
			{Title: "ECC C/U", Width: 9, Align: widgets.Right, Priority: 5},
			{Title: "REMAP", Width: 8, Priority: 6},
		}, SortCol: -1}
		widths := tb.Layout(w - 2)
		_, tb.Selected = m.selected()
		top := map[gpu.ID]model.Process{}
		for _, p := range s.Processes {
			if cur, ok := top[p.DeviceID]; !ok || p.MemUsed.Or(0) > cur.MemUsed.Or(0) {
				top[p.DeviceID] = p
			}
		}
		for i := range s.GPUs {
			g := &s.GPUs[i]
			smp, c := g.Sample, g.Counters
			label := na
			if smp.MemUsed.OK && smp.MemTotal.OK {
				label = fmtGiBPair(smp.MemUsed.V, smp.MemTotal.V)
			}
			consumer := "—"
			if p, ok := top[g.Device.ID]; ok {
				consumer = fmt.Sprintf("%s %s", p.Name, optBytes(p.MemUsed))
			}
			ecc := fmt.Sprintf("%s/%s", optU(c.ECCCorrectedVolatile), optU(c.ECCUncorrectedVolatile))
			eccSt := th.Text
			if c.ECCUncorrectedVolatile.Or(0) > 0 {
				eccSt = th.Crit
			}
			remap := na
			remapSt := th.Text
			if c.RemapPending.OK {
				remap = "ok"
				if c.RemapFailure.V {
					remap, remapSt = "FAILED", th.Crit
				} else if c.RemapPending.V {
					remap, remapSt = "pending", th.Warn
				}
			}
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
				widgets.R(m.fracBar(g.Derived.VRAMFraction.V, g.Derived.VRAMFraction.OK, label, widths[1])),
				widgets.R(m.naOr(optBytes(smp.MemUsed), th.Text)),
				widgets.R(m.naOr(optBytes(smp.MemFree), th.Text)),
				widgets.R(m.naOr(optBytes(smp.MemReserved), th.Dim)),
				widgets.R(m.pctBar(smp.MemBandwidthPercent, widths[5])),
				widgets.C(th.Dim, consumer),
				widgets.R(m.naOr(ecc, eccSt)),
				widgets.R(m.naOr(remap, remapSt)),
			})
		}
		return widgets.Box(th, widgets.BoxOpts{Title: "GPU memory", RightTitle: "bandwidth = time memory was read/written", Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "VRAM used %", "vram", 100, "%")
}

func viewPower(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "POWER / LIMIT", Width: 28, Min: 14, Flex: true},
			{Title: "DEFAULT", Width: 7, Align: widgets.Right, Priority: 4},
			{Title: "RANGE", Width: 9, Align: widgets.Right, Priority: 6},
			{Title: "ENERGY", Width: 9, Align: widgets.Right, Priority: 3},
			{Title: "P", Width: 3, Align: widgets.Right, Priority: 2},
			{Title: "CLOCK", Width: 12, Align: widgets.Right, Priority: 1},
			{Title: "THROTTLE REASONS", Width: 24, Min: 8, Flex: true},
		}, SortCol: -1}
		widths := tb.Layout(w - 2)
		_, tb.Selected = m.selected()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			smp, d := g.Sample, g.Device
			bar := m.naOr(optF(smp.PowerW, "%.0f W"), th.Text)
			if smp.PowerW.OK && smp.PowerLimitW.OK && smp.PowerLimitW.V > 0 {
				bar = m.fracBar(smp.PowerW.V/smp.PowerLimitW.V, true, fmt.Sprintf("%.0f/%.0fW", smp.PowerW.V, smp.PowerLimitW.V), widths[1])
			}
			energy := na
			if smp.EnergyJ.OK {
				energy = fmt.Sprintf("%.1fkWh", smp.EnergyJ.V/3.6e6)
			}
			clock := na
			if smp.ClockCoreMHz.OK {
				clock = fmt.Sprintf("%.0f", smp.ClockCoreMHz.V)
				if d.ClockCoreMaxMHz.OK {
					clock += fmt.Sprintf("/%.0f", d.ClockCoreMaxMHz.V)
				}
			}
			reasons, rs := na, th.NA
			if smp.Throttle.OK {
				r := smp.Throttle.V &^ gpu.ThrottleIdle
				reasons, rs = "none", th.OK
				if r != 0 {
					reasons, rs = r.String(), th.Warn
					if r&gpu.ThrottleHardware != 0 {
						rs = th.Crit
					}
				} else if smp.Throttle.V&gpu.ThrottleIdle != 0 {
					reasons, rs = "idle", th.Muted
				}
			}
			rng := na
			if d.PowerLimitMinW.OK && d.PowerLimitMaxW.OK {
				rng = fmt.Sprintf("%.0f-%.0f", d.PowerLimitMinW.V, d.PowerLimitMaxW.V)
			}
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(d.Index)),
				widgets.R(bar),
				widgets.R(m.naOr(optF(d.PowerLimitDefaultW, "%.0fW"), th.Dim)),
				widgets.R(m.naOr(rng, th.Dim)),
				widgets.R(m.naOr(energy, th.Text)),
				widgets.R(m.naOr(optI(smp.PState, "P%d"), th.Text)),
				widgets.R(m.naOr(clock, th.Text)),
				widgets.C(rs, reasons),
			})
		}
		right := ""
		if s.Fleet.PowerW.OK {
			right = "total " + fmtWatts(s.Fleet.PowerW.V)
		}
		return widgets.Box(th, widgets.BoxOpts{Title: "Power & clocks", RightTitle: right, Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "Power W", "power", 0, "W")
}

func viewThermals(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "GPU TEMP (to shutdown)", Width: 28, Min: 14, Flex: true},
			{Title: "MEM", Width: 5, Align: widgets.Right},
			{Title: "HOTSPOT", Width: 7, Align: widgets.Right, Priority: 5},
			{Title: "SLOWDOWN", Width: 8, Align: widgets.Right, Priority: 2},
			{Title: "SHUTDOWN", Width: 8, Align: widgets.Right, Priority: 4},
			{Title: "HEADROOM", Width: 8, Align: widgets.Right, Priority: 1},
			{Title: "FAN", Width: 4, Align: widgets.Right, Priority: 3},
			{Title: "STATE", Width: 26, Min: 8, Flex: true},
		}, SortCol: -1}
		widths := tb.Layout(w - 2)
		_, tb.Selected = m.selected()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			smp, d := g.Sample, g.Device
			head := na
			hst := th.Text
			if smp.TempC.OK && d.TempSlowdownC.OK {
				hr := d.TempSlowdownC.V - smp.TempC.V
				head = fmt.Sprintf("%.0f°", hr)
				hst = th.Level(-hr, -10, -3)
			}
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(d.Index)),
				widgets.R(m.tempBar(smp.TempC, d, widths[1])),
				widgets.R(m.naOr(m.temp(smp.MemTempC), th.Text)),
				widgets.R(th.NA.Render(na)),
				widgets.R(m.naOr(m.temp(d.TempSlowdownC), th.Dim)),
				widgets.R(m.naOr(m.temp(d.TempShutdownC), th.Dim)),
				widgets.R(m.naOr(head, hst)),
				widgets.R(m.naOr(optPct(smp.FanPct), th.Text)),
				widgets.R(m.thermalState(g)),
			})
		}
		right := "hotspot temperature is not exposed by NVML"
		return widgets.Box(th, widgets.BoxOpts{Title: "Thermals", RightTitle: right, Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "GPU temperature °C", "temp", 0, "°C")
}

func viewPCIe(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "BUS ID", Width: 13, Priority: 3},
			{Title: "LINK (max)", Width: 34, Min: 12, Flex: true},
			{Title: "TX", Width: 11, Align: widgets.Right},
			{Title: "RX", Width: 11, Align: widgets.Right},
			{Title: "REPLAYS", Width: 7, Align: widgets.Right, Priority: 1},
			{Title: "AER C/NF/F", Width: 11, Align: widgets.Right, Priority: 2},
			{Title: "NUMA", Width: 4, Align: widgets.Right, Priority: 4},
		}, SortCol: -1}
		_, tb.Selected = m.selected()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			c := g.Counters
			aer := fmt.Sprintf("%s/%s/%s", optU(c.PCIeCorrectableErrors), optU(c.PCIeNonFatalErrors), optU(c.PCIeFatalErrors))
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
				widgets.C(th.Dim, g.Device.PCI.BusID),
				widgets.R(m.pcieLink(g)),
				widgets.R(m.naOr(optRate(g.Sample.PCIeTxBps), th.Text)),
				widgets.R(m.naOr(optRate(g.Sample.PCIeRxBps), th.Text)),
				widgets.R(m.naOr(optU(c.PCIeReplays), th.Text)),
				widgets.R(m.naOr(aer, th.Text)),
				widgets.R(m.naOr(optI(g.Device.NUMANode, "%d"), th.Dim)),
			})
		}
		return widgets.Box(th, widgets.BoxOpts{Title: "PCIe", RightTitle: "Gen downtraining at idle is normal", Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "PCIe RX", "pcierx", 0, "B/s")
}

func viewNVLink(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	g, _ := m.selected()
	if g == nil {
		return nil
	}
	var top widgets.Block
	leftW := w
	if w >= 120 {
		leftW = w * 45 / 100
	}
	// GPU summary list.
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "#", Width: 2, Align: widgets.Right},
		{Title: "LINKS", Width: 7},
		{Title: "TX", Width: 11, Align: widgets.Right},
		{Title: "RX", Width: 11, Align: widgets.Right},
		{Title: "ERRORS", Width: 6, Align: widgets.Right},
	}, SortCol: -1}
	_, tb.Selected = m.selected()
	for i := range s.GPUs {
		gg := &s.GPUs[i]
		var errs uint64
		for _, l := range gg.Links {
			errs += l.ErrorTotal()
		}
		es := th.Text
		if errs > 0 {
			es = th.Warn
		}
		links := fmt.Sprintf("%d/%d", gg.Derived.LinksActive, gg.Device.LinkCount)
		ls := th.OK
		if gg.Device.LinkCount == 0 {
			links, ls = "none", th.NA
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, fmt.Sprint(gg.Device.Index)), widgets.C(ls, links),
			widgets.R(m.naOr(optRate(gg.Derived.NVLinkTxBps), th.Text)), widgets.R(m.naOr(optRate(gg.Derived.NVLinkRxBps), th.Text)),
			widgets.C(es, fmt.Sprint(errs)),
		})
	}
	listH := min(len(s.GPUs)+3, h/2)
	if w >= 120 {
		listH = h
	}
	list := widgets.Box(th, widgets.BoxOpts{Title: "NVLink by GPU", Focus: true}, leftW, listH, tb.Render(th, leftW-2, listH-2))

	// Links of the selected GPU.
	rightW := w
	rightH := h - listH
	if w >= 120 {
		rightW, rightH = w-leftW, h
	}
	byID := map[gpu.ID]int{}
	for _, gg := range s.GPUs {
		byID[gg.Device.ID] = gg.Device.Index
	}
	lt := &widgets.Table{Columns: []widgets.Column{
		{Title: "LINK", Width: 4, Align: widgets.Right},
		{Title: "STATE", Width: 9},
		{Title: "VER", Width: 3, Align: widgets.Right, Priority: 3},
		{Title: "REMOTE", Width: 16, Min: 8, Flex: true},
		{Title: "TX", Width: 11, Align: widgets.Right},
		{Title: "RX", Width: 11, Align: widgets.Right},
		{Title: "CRC", Width: 5, Align: widgets.Right, Priority: 1},
		{Title: "REPLAY", Width: 6, Align: widgets.Right, Priority: 2},
		{Title: "RECOV", Width: 5, Align: widgets.Right, Priority: 2},
	}, SortCol: -1, Selected: -1}
	for _, l := range g.Links {
		st := th.OK
		switch l.State {
		case gpu.LinkSleep:
			st = th.Muted
		case gpu.LinkInactive, gpu.LinkDisabled:
			st = th.Crit
		}
		remote := string(l.RemoteType)
		if idx, ok := byID[l.RemoteID]; ok {
			remote = fmt.Sprintf("GPU %d", idx)
		} else if l.RemoteType == gpu.EndpointSwitch {
			remote = "NVSwitch " + l.RemoteBusID
		} else if l.RemoteBusID != "" {
			remote += " " + l.RemoteBusID
		}
		crc := l.ErrCRCData.Or(0) + l.ErrCRCFlit.Or(0)
		cs := th.Text
		if crc > 0 {
			cs = th.Warn
		}
		lt.Rows = append(lt.Rows, []widgets.Cell{
			widgets.C(th.Dim, fmt.Sprint(l.Index)), widgets.C(st, string(l.State)),
			widgets.R(m.naOr(optI(l.Version, "%d"), th.Dim)), widgets.C(th.Text, remote),
			widgets.R(m.naOr(optRate(l.TxBps), th.Text)), widgets.R(m.naOr(optRate(l.RxBps), th.Text)),
			widgets.C(cs, fmt.Sprint(crc)), widgets.R(m.naOr(optU(l.ErrReplay), th.Text)), widgets.R(m.naOr(optU(l.ErrRecovery), th.Text)),
		})
	}
	matrix := m.topologyMatrix(s, rightW-2)
	linkH := rightH
	if len(matrix) > 0 && rightH-len(matrix)-2 >= 6 {
		linkH = rightH - len(matrix) - 2
	}
	links := widgets.Box(th, widgets.BoxOpts{Title: fmt.Sprintf("GPU %d links", g.Device.Index), RightTitle: "[ ] select GPU"}, rightW, linkH, lt.Render(th, rightW-2, linkH-2))
	right := links
	if linkH < rightH {
		right = append(right, widgets.Box(th, widgets.BoxOpts{Title: "Topology", RightTitle: "NV# = NVLinks · PCIe: PIX PXB PHB NODE SYS"}, rightW, rightH-linkH, matrix)...)
	}
	if w >= 120 {
		top = widgets.HJoin(th, list, right)
	} else {
		top = widgets.VJoin(list, right)
	}
	return top
}

// topologyMatrix renders an nvidia-smi-topo-like GPU matrix.
func (m *Model) topologyMatrix(s *model.Snapshot, w int) []string {
	th := m.th
	if len(s.Topology) == 0 || len(s.GPUs) > 16 {
		return nil
	}
	edge := map[[2]gpu.ID]model.TopologyEdge{}
	for _, e := range s.Topology {
		edge[[2]gpu.ID{e.A, e.B}] = e
		edge[[2]gpu.ID{e.B, e.A}] = e
	}
	cell := 6
	if (len(s.GPUs)+1)*cell > w {
		return []string{th.Dim.Render("terminal too narrow for the topology matrix")}
	}
	header := widgets.Space(th, cell)
	for _, g := range s.GPUs {
		header += th.Dim.Render(fmt.Sprintf("%-*s", cell, fmt.Sprintf("GPU%d", g.Device.Index)))
	}
	lines := []string{header}
	for _, a := range s.GPUs {
		line := th.Dim.Render(fmt.Sprintf("%-*s", cell, fmt.Sprintf("GPU%d", a.Device.Index)))
		for _, b := range s.GPUs {
			if a.Device.ID == b.Device.ID {
				line += th.Muted.Render(fmt.Sprintf("%-*s", cell, "X"))
				continue
			}
			e, ok := edge[[2]gpu.ID{a.Device.ID, b.Device.ID}]
			label, st := "?", th.NA
			if ok {
				if e.NVLinks > 0 {
					label, st = fmt.Sprintf("NV%d", e.NVLinks), th.OK
				} else {
					label = strings.ToUpper(string(e.Level))
					st = th.Dim
					if e.Level == gpu.TopoSystem {
						st = th.Warn
					}
				}
			}
			line += st.Render(fmt.Sprintf("%-*s", cell, label))
		}
		lines = append(lines, line)
	}
	if switched := s.GPUs[0].Links; len(switched) > 0 && switched[0].RemoteType == gpu.EndpointSwitch {
		lines = append(lines, th.Dim.Render("Links terminate on NVSwitch: GPU-to-GPU paths go through the switch fabric."))
	}
	return lines
}

func viewMIG(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	var lines []string
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "GPU", Width: 3, Align: widgets.Right},
		{Title: "MIG MODE", Width: 18},
		{Title: "IDX", Width: 3, Align: widgets.Right},
		{Title: "PROFILE", Width: 9},
		{Title: "GI", Width: 3, Align: widgets.Right},
		{Title: "CI", Width: 3, Align: widgets.Right},
		{Title: "MEMORY", Width: 24, Min: 12, Flex: true},
		{Title: "PROCESSES", Width: 24, Min: 8, Flex: true, Priority: 1},
		{Title: "UUID", Width: 24, Priority: 2},
	}, SortCol: -1, Selected: -1}
	widths := tb.Layout(w - 2)
	procs := map[gpu.ID][]string{}
	for _, p := range s.Processes {
		if p.PartitionID != "" {
			procs[p.PartitionID] = append(procs[p.PartitionID], fmt.Sprintf("%s(%d)", p.Name, p.PID))
		}
	}
	for _, g := range s.GPUs {
		mode := th.NA.Render("not supported")
		if g.Device.MIG.Supported {
			mode = th.Dim.Render("disabled")
			if g.Device.MIG.Enabled {
				mode = th.OK.Render("enabled")
			}
			if g.Device.MIG.Pending != g.Device.MIG.Enabled {
				mode += th.Warn.Render(" (pending reset)")
			}
		}
		if len(g.Partitions) == 0 {
			tb.Rows = append(tb.Rows, []widgets.Cell{widgets.C(th.Accent, fmt.Sprint(g.Device.Index)), widgets.R(mode),
				widgets.C(th.Dim, "—"), widgets.C(th.Dim, "—"), widgets.C(th.Dim, ""), widgets.C(th.Dim, ""), widgets.C(th.Dim, "whole GPU"), widgets.C(th.Dim, ""), widgets.C(th.Dim, "")})
			continue
		}
		for i, p := range g.Partitions {
			gcell, mcell := widgets.C(th.Accent, fmt.Sprint(g.Device.Index)), widgets.R(mode)
			if i > 0 {
				gcell, mcell = widgets.R(""), widgets.R("")
			}
			mem := m.naOr(na, th.Text)
			if p.MemTotal.OK && p.MemUsed.OK && p.MemTotal.V > 0 {
				mem = m.fracBar(float64(p.MemUsed.V)/float64(p.MemTotal.V), true, fmtGiBPair(p.MemUsed.V, p.MemTotal.V), widths[6])
			}
			pl := strings.Join(procs[p.ID], ", ")
			if pl == "" {
				pl = "idle"
			}
			tb.Rows = append(tb.Rows, []widgets.Cell{gcell, mcell,
				widgets.C(th.Text, fmt.Sprint(p.Index)), widgets.C(th.Primary, p.Profile),
				widgets.R(m.naOr(optI(p.InstanceID, "%d"), th.Dim)), widgets.R(m.naOr(optI(p.ComputeInstanceID, "%d"), th.Dim)),
				widgets.R(mem), widgets.C(th.Text, pl), widgets.C(th.Dim, string(p.ID))})
		}
	}
	lines = tb.Render(th, w-2, max(1, h-5))
	lines = append(lines, "", th.Dim.Render(" Per-instance compute utilization is not available through NVML; it requires DCGM (planned)."))
	return widgets.Box(th, widgets.BoxOpts{Title: "Multi-Instance GPU", Focus: true}, w, h, lines)
}

// sortedKeys returns map keys in order.
func sortedKeys[V any](mp map[string]V) []string {
	out := make([]string, 0, len(mp))
	for k := range mp {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
