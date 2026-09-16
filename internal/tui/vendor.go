// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/model"
	"github.com/riteshsonawane1372/gputop/internal/tui/widgets"
)

// Apple silicon GPUs share unified memory with the CPU and expose a much
// smaller metric set than NVML (no ECC, PCIe, power limits, throttle
// reasons or per-process memory). When every GPU in view is an Apple GPU the
// UI swaps NVIDIA-shaped tables for ones that only show what is measured.

// apple reports whether the snapshot's GPUs are all Apple GPUs.
func (m *Model) apple() bool {
	s := m.view()
	if len(s.GPUs) == 0 {
		return false
	}
	for i := range s.GPUs {
		if s.GPUs[i].Device.Vendor != gpu.VendorApple {
			return false
		}
	}
	return true
}

// memTitle is the column title for device memory.
func (m *Model) memTitle() string {
	if m.apple() {
		return "GPU MEM"
	}
	return "VRAM"
}

// hasPCIe reports whether any GPU sits on a PCIe bus (discrete GPUs).
func hasPCIe(m *Model) bool {
	s := m.view()
	for i := range s.GPUs {
		if s.GPUs[i].Device.PCI.BusID != "" || s.GPUs[i].Sample.PCIeGen.OK {
			return true
		}
	}
	return false
}

// fmtEnergy renders joules with a unit suited to the magnitude.
func fmtEnergy(j float64) string {
	switch {
	case j < 3600:
		return fmt.Sprintf("%.0f J", j)
	case j < 3.6e6:
		return fmt.Sprintf("%.1f Wh", j/3600)
	}
	return fmt.Sprintf("%.1f kWh", j/3.6e6)
}

// freqBar renders the average active frequency against the maximum.
func (m *Model) freqBar(g *model.GPU, w int) string {
	smp, d := g.Sample, g.Device
	if !smp.ClockCoreMHz.OK {
		return m.th.NA.Render(na)
	}
	if !d.ClockCoreMaxMHz.OK || d.ClockCoreMaxMHz.V <= 0 {
		return m.th.Text.Render(fmt.Sprintf("%.0f MHz", smp.ClockCoreMHz.V))
	}
	return m.fracBar(smp.ClockCoreMHz.V/d.ClockCoreMaxMHz.V, true, fmt.Sprintf("%.0f/%.0f MHz", smp.ClockCoreMHz.V, d.ClockCoreMaxMHz.V), w)
}

// processesOn returns the device's processes, busiest first.
func processesOn(s *model.Snapshot, id gpu.ID) []model.Process {
	var out []model.Process
	for _, p := range s.Processes {
		if p.DeviceID == id {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SMUtil.Or(-1) > out[j].SMUtil.Or(-1) })
	return out
}

// appleSections is the GPU detail view for Apple silicon.
func (m *Model) appleSections(g *model.GPU, cw int, full bool) []section {
	th := m.th
	s := m.view()
	d, smp := g.Device, g.Sample
	lw := 15
	barW := max(10, min(30, cw-lw-14))
	kv := func(label, value string) string { return m.kv(label, value, lw) }
	txt := func(v string) string { return m.naOr(v, th.Text) }
	str := func(v string) string {
		if v == "" {
			return th.NA.Render(na)
		}
		return th.Text.Render(v)
	}
	sys := gpu.SystemInfo{}
	for _, p := range s.Providers {
		if p.Name == g.Provider {
			sys = p.System
		}
	}
	var secs []section
	secs = append(secs, section{fmt.Sprintf("GPU %d · %s", d.Index, shortName(d.Name)), []string{
		kv("Name", th.Bold.Render(d.Name)),
		kv("ID", th.Text.Render(string(d.ID))),
		kv("State", m.stateCell(g)),
		kv("Architecture", str(d.Architecture)),
		kv("Driver (AGX)", str(sys.DriverVersion)),
		kv("macOS", str(sys.RuntimeVersion)),
	}})

	eff := g.Derived.Efficiency
	effStr := th.NA.Render(eff.Note)
	if eff.Score.OK {
		effStr = th.Gradient(1-float64(eff.Score.V)/100).Render(fmt.Sprintf("%d", eff.Score.V)) + th.Dim.Render(" "+eff.Grade+" · 5m, derived")
	}
	secs = append(secs, section{"Compute", []string{
		kv("Utilization", m.pctBar(smp.UtilPercent, barW)),
		kv("Frequency", m.freqBar(g, barW+8)),
		kv("Avg util (5m)", txt(optPct(g.Derived.UtilAvg))),
		kv("Efficiency", effStr),
	}})

	memLabel := na
	if smp.MemUsed.OK && smp.MemTotal.OK {
		memLabel = fmt.Sprintf("%s of %s unified", fmtBytes(smp.MemUsed.V), fmtBytes(smp.MemTotal.V))
	}
	procs := processesOn(s, d.ID)
	secs = append(secs, section{"Memory", []string{
		kv("GPU in use", m.fracBar(g.Derived.VRAMFraction.V, g.Derived.VRAMFraction.OK, fmt.Sprintf("%.0f%%", g.Derived.VRAMFraction.V*100), barW)),
		kv("In use", txt(memLabel)),
		kv("Allocated", txt(optBytes(smp.MemReserved))),
		kv("Processes", th.Text.Render(fmt.Sprintf("%d using the GPU", len(procs)))),
	}})

	energy := na
	if smp.EnergyJ.OK {
		energy = fmtEnergy(smp.EnergyJ.V)
	}
	secs = append(secs, section{"Power & thermals", []string{
		kv("GPU power", txt(optF(smp.PowerW, "%.2f W"))),
		kv("Energy", txt(energy)+th.Dim.Render(" since gputop started")),
		kv("GPU temp", m.tempBar(smp.TempC, d, barW)),
		kv("Thermal state", m.thermalState(g)),
	}})

	hl := []string{
		kv("Score", m.healthStyle(g.Health.Score).Bold(true).Render(fmt.Sprint(g.Health.Score))+th.Dim.Render(" "+string(g.Health.Band)+" · gputop-derived")),
	}
	for _, r := range g.Health.Reasons {
		st, _ := m.sevStyle(model.Severity(r.Severity))
		hl = append(hl, st.Render(fmt.Sprintf("  −%-3d", r.Penalty))+th.Text.Render(r.Text))
	}
	hl = append(hl, th.Dim.Render("Apple exposes no ECC or reliability counters."))
	secs = append(secs, section{"Health", hl})

	if len(procs) > 0 {
		var pl []string
		for i, p := range procs {
			if i >= 6 {
				pl = append(pl, th.Dim.Render(fmt.Sprintf("… %d more (Processes tab)", len(procs)-i)))
				break
			}
			pl = append(pl, th.Dim.Render(fmt.Sprintf("%7d ", p.PID))+th.Text.Render(fmt.Sprintf("%-20s", truncate(p.Name, 20)))+
				widgets.FitRight(th, m.naOr(optPct(p.SMUtil), th.Gradient(p.SMUtil.V/100)), 5)+th.Dim.Render("  "+p.User))
		}
		secs = append(secs, section{"Processes", pl})
	}
	if full {
		secs = append(secs, m.trendSections(g, cw)...)
	}
	return secs
}

func viewMemoryApple(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "GPU IN USE (of unified memory)", Width: 32, Min: 14, Flex: true},
			{Title: "IN USE", Width: 9, Align: widgets.Right},
			{Title: "ALLOCATED", Width: 9, Align: widgets.Right, Priority: 2},
			{Title: "UNIFIED", Width: 9, Align: widgets.Right, Priority: 3},
			{Title: "BUSIEST PROCESS", Width: 28, Min: 10, Priority: 1, Flex: true},
		}, SortCol: -1}
		widths := tb.Layout(w - 2)
		_, tb.Selected = m.selected()
		tb.RowMark = m.gpuRowMark()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			smp := g.Sample
			label := na
			if smp.MemUsed.OK && smp.MemTotal.OK {
				label = fmtGiBPair(smp.MemUsed.V, smp.MemTotal.V)
			}
			busiest := "—"
			if procs := processesOn(s, g.Device.ID); len(procs) > 0 {
				busiest = fmt.Sprintf("%s %s", procs[0].Name, optPct(procs[0].SMUtil))
			}
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
				widgets.R(m.fracBar(g.Derived.VRAMFraction.V, g.Derived.VRAMFraction.OK, label, widths[1])),
				widgets.R(m.naOr(optBytes(smp.MemUsed), th.Text)),
				widgets.R(m.naOr(optBytes(smp.MemReserved), th.Text)),
				widgets.R(m.naOr(optBytes(smp.MemTotal), th.Dim)),
				widgets.C(th.Dim, busiest),
			})
		}
		return widgets.Box(th, widgets.BoxOpts{Title: "GPU memory", RightTitle: "Apple silicon shares memory between CPU and GPU", Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "GPU memory % of unified", "vram", 100, "%")
}

func viewPowerApple(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "GPU POWER", Width: 10, Align: widgets.Right},
			{Title: "FREQUENCY (avg active / max)", Width: 34, Min: 14, Flex: true},
			{Title: "UTIL", Width: 16, Min: 8, Priority: 1},
			{Title: "ENERGY", Width: 9, Align: widgets.Right, Priority: 2},
		}, SortCol: -1}
		widths := tb.Layout(w - 2)
		_, tb.Selected = m.selected()
		tb.RowMark = m.gpuRowMark()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			smp := g.Sample
			energy := na
			if smp.EnergyJ.OK {
				energy = fmtEnergy(smp.EnergyJ.V)
			}
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
				widgets.R(m.naOr(optF(smp.PowerW, "%.2f W"), th.Text)),
				widgets.R(m.freqBar(g, widths[2])),
				widgets.R(m.pctBar(smp.UtilPercent, widths[3])),
				widgets.R(m.naOr(energy, th.Text)),
			})
		}
		return widgets.Box(th, widgets.BoxOpts{Title: "Power & frequency", RightTitle: "IOReport · no power limits on Apple silicon", Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "GPU power W", "power", 0, "W")
}

func viewThermalsApple(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	return m.splitTableChart(w, h, func(w, h int) widgets.Block {
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "GPU TEMP", Width: 34, Min: 14, Flex: true},
			{Title: "STATE", Width: 26, Min: 8, Flex: true},
		}, SortCol: -1}
		widths := tb.Layout(w - 2)
		_, tb.Selected = m.selected()
		tb.RowMark = m.gpuRowMark()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
				widgets.R(m.tempBar(g.Sample.TempC, g.Device, widths[1])),
				widgets.R(m.thermalState(g)),
			})
		}
		return widgets.Box(th, widgets.BoxOpts{Title: "Thermals", RightTitle: "average of the SMC GPU die sensors", Focus: true}, w, h, tb.Render(th, w-2, h-2))
	}, "GPU temperature °C", "temp", 0, "°C")
}
