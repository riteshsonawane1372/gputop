// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

func keysOverview(m *Model, a keymap.Action) (bool, tea.Cmd) {
	if len(m.view().GPUs) == 0 {
		return false, nil
	}
	return keysGPUSelect(m, a)
}

func hintsOverview(m *Model) []hint {
	if len(m.view().GPUs) == 0 {
		return nil
	}
	return []hint{{keymap.Up, "select"}, {keymap.Select, "GPU detail"}, {keymap.History, "history"}, {keymap.Pause, "pause"}}
}

func viewOverview(m *Model, w, h int) widgets.Block {
	s := m.view()
	if len(s.GPUs) == 0 {
		return m.viewNoGPU(w, h)
	}
	th := m.th
	compact := h < 30 || w < 100

	fleetLines := m.fleetLines(s, w)
	topH := len(fleetLines) + 2
	if compact {
		topH = min(topH, 6)
	}
	// The GPU table has priority: lower panels only get leftover space.
	tableH := min(len(s.GPUs)+3, max(4, h-topH))
	if h-topH-tableH < 6 {
		tableH = max(4, h-topH)
	}
	bottomH := h - topH - tableH

	var top widgets.Block
	if w >= 110 {
		ws := widgets.Split(w, 11, 9)
		fleet := widgets.Box(th, widgets.BoxOpts{Title: "Fleet", RightTitle: fleetRight(s)}, ws[0], topH, fleetLines)
		top = widgets.HJoin(th, fleet, m.fleetCharts(ws[1], topH))
	} else {
		top = widgets.Box(th, widgets.BoxOpts{Title: "Fleet", RightTitle: fleetRight(s)}, w, topH, fleetLines)
	}

	table := m.gpuTable(s, w, tableH)

	out := widgets.VJoin(top, table)
	if bottomH >= 5 {
		if w >= 120 {
			ws := widgets.Split(w, 1, 1)
			out = append(out, widgets.HJoin(th, m.topProcesses(s, ws[0], bottomH), m.alertsAndEvents(s, ws[1], bottomH))...)
		} else {
			out = append(out, m.alertsAndEvents(s, w, bottomH)...)
		}
	}
	return out
}

func fleetRight(s *model.Snapshot) string {
	if s.Node.Demo {
		return "simulated"
	}
	return ""
}

func (m *Model) fleetLines(s *model.Snapshot, w int) []string {
	th := m.th
	f := s.Fleet
	lw := 11
	var lines []string

	states := fmt.Sprintf("%s  %s  %s",
		th.OK.Render(fmt.Sprintf("● %d busy", f.Busy)),
		th.Accent.Render(fmt.Sprintf("◐ %d active", f.Active-f.Busy)),
		th.Muted.Render(fmt.Sprintf("○ %d idle", f.Idle)))
	if f.Unavailable > 0 {
		states += "  " + th.Crit.Render(fmt.Sprintf("✖ %d down", f.Unavailable))
	}
	lines = append(lines, m.kv("GPUs", th.Bold.Render(fmt.Sprint(f.GPUs))+"   "+states, lw))

	// Allocation is a scheduling concept; a Mac's GPU always serves the desktop.
	if !m.apple() {
		alloc := th.Text.Render(fmt.Sprintf("%d/%d", f.Allocated, f.GPUs))
		if f.IdleAllocated > 0 {
			alloc += th.Warn.Render(fmt.Sprintf("  %d allocated but idle", f.IdleAllocated))
		}
		if f.Throttled > 0 {
			alloc += th.Warn.Render(fmt.Sprintf("  %d throttled", f.Throttled))
		}
		lines = append(lines, m.kv("Allocated", alloc, lw))
	}

	if f.HealthAvg.OK {
		hs := m.healthStyle(int(f.HealthAvg.V))
		txt := hs.Render(fmt.Sprintf("%.0f", f.HealthAvg.V)) + th.Dim.Render(" avg")
		if f.HealthMin.OK && f.HealthMin.V < 100 {
			worst := ""
			for _, g := range s.GPUs {
				if g.Health.Score == f.HealthMin.V {
					worst = " (" + m.gpuName(&g) + ")"
					break
				}
			}
			txt += th.Dim.Render(" · min ") + m.healthStyle(f.HealthMin.V).Render(fmt.Sprint(f.HealthMin.V)) + th.Dim.Render(worst)
		}
		lines = append(lines, m.kv("Health", txt+"  "+widgets.Pips(th, f.HealthAvg.V/100, 10), lw))
	}

	barW := max(8, min(24, w/6))
	if f.PowerW.OK {
		txt := th.Text.Render(fmtWatts(f.PowerW.V))
		frac, ok := 0.0, false
		if f.PowerLimitW.OK && f.PowerLimitW.V > 0 {
			frac, ok = f.PowerW.V/f.PowerLimitW.V, true
			txt += th.Dim.Render(" / "+fmtWatts(f.PowerLimitW.V)) + th.Text.Render(fmt.Sprintf(" %.0f%%", frac*100))
		}
		if ok {
			txt = widgets.Bar(th, frac, barW) + th.Base.Render(" ") + txt
		}
		lines = append(lines, m.kv("Power", txt, lw))
	}
	if f.VRAMUsed.OK {
		txt := th.Text.Render(fmt.Sprintf("%s / %s", fmtBytes(f.VRAMUsed.V), fmtBytes(f.VRAMTotal.V)))
		if f.VRAMFraction.OK {
			txt = widgets.Bar(th, f.VRAMFraction.V, barW) + th.Base.Render(" ") + txt + th.Text.Render(fmt.Sprintf(" %.0f%%", f.VRAMFraction.V*100))
		}
		label := "VRAM"
		if m.apple() {
			label = "GPU mem"
		}
		lines = append(lines, m.kv(label, txt, lw))
	}
	if f.TempAvgC.OK {
		lines = append(lines, m.kv("Temp", th.Text.Render(m.temp(f.TempAvgC))+th.Dim.Render(" avg · max ")+
			th.Level(f.TempMaxC.V, 80, 88).Render(m.temp(f.TempMaxC)), lw))
	}

	im := f.Imbalance
	switch {
	case im.Valid && len(im.Outliers) > 0:
		var outs []string
		for _, id := range im.Outliers {
			if g, ok := s.GPUByID(id); ok {
				outs = append(outs, fmt.Sprintf("%s %+.0fpp", m.gpuName(g), g.Derived.OutlierDelta.V))
			}
		}
		lines = append(lines, m.kv("Balance", th.Warn.Render("⚠ outlier "+strings.Join(outs, ", "))+
			th.Dim.Render(fmt.Sprintf(" · spread %.0fpp across %d GPUs", im.Spread, im.Members)), lw))
	case im.Valid:
		lines = append(lines, m.kv("Balance", th.OK.Render("balanced")+th.Dim.Render(fmt.Sprintf(" · spread %.0fpp across %d GPUs (%s)", im.Spread, im.Members, im.Cohort)), lw))
	}

	var extra []string
	if f.UnusedAllocated.OK {
		extra = append(extra, th.Text.Render(fmt.Sprintf("%.1f GPU-eq", f.UnusedAllocated.V))+th.Dim.Render(" unused of allocated (5m)"))
	}
	if len(extra) > 0 {
		lines = append(lines, m.kv("Capacity", strings.Join(extra, "  "), lw))
	}
	if hs := s.Host; hs != nil {
		txt := th.Dim.Render("CPU ") + m.naOr(optPct(hs.CPU.UtilPercent), th.Gradient(hs.CPU.UtilPercent.V/100))
		if f := hs.Memory.UsedFraction(); f.OK {
			txt += th.Dim.Render(" · RAM ") + th.Gradient(f.V).Render(fmt.Sprintf("%.0f%%", f.V*100))
		}
		rx := m.live.values("host/netrx", 1)
		tx := m.live.values("host/nettx", 1)
		if len(rx) == 1 && rx[0] == rx[0] {
			txt += th.Dim.Render(" · net ") + th.Accent.Render("↓"+fmtRate(rx[0])) + th.Dim.Render(" ") + th.Accent.Render("↑"+fmtRate(tx[0]))
		}
		lines = append(lines, m.kv("Host", txt, lw))
	}
	return lines
}

func (m *Model) fleetCharts(w, h int) widgets.Block {
	th := m.th
	inner := h - 2
	cw := w - 2
	util := m.live.values("fleet/util", 0)
	power := m.live.values("fleet/power", 0)
	var lines []string
	uh := (inner - 2) / 2
	ph := inner - 2 - uh
	cur := func(vals []float64, f func(float64) string) string {
		if len(vals) == 0 || vals[len(vals)-1] != vals[len(vals)-1] {
			return na
		}
		return f(vals[len(vals)-1])
	}
	lines = append(lines, th.Dim.Render("avg utilization ")+th.Primary.Render(cur(util, func(v float64) string { return fmt.Sprintf("%.0f%%", v) })))
	lines = append(lines, widgets.Chart(th, util, cw, max(1, uh), widgets.ChartOpts{Min: 0, Max: 100, Cursor: -1})...)
	lines = append(lines, th.Dim.Render("total power ")+th.Primary.Render(cur(power, fmtWatts)))
	lines = append(lines, widgets.Chart(th, power, cw, max(1, ph), widgets.ChartOpts{Cursor: -1})...)
	return widgets.Box(th, widgets.BoxOpts{Title: "Live", RightTitle: fmtDuration(m.refresh * liveCap)}, w, h, lines)
}

func (m *Model) gpuTable(s *model.Snapshot, w, h int) widgets.Block {
	th := m.th
	tb := &widgets.Table{
		Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "NAME", Width: 18, Min: 8, Flex: true, Priority: 4},
			{Title: "UTIL", Width: 18, Min: 10},
			{Title: m.memTitle(), Width: 22, Min: 12},
			{Title: "TEMP", Width: 5, Align: widgets.Right},
			{Title: "POWER", Width: 10, Align: widgets.Right, Priority: 1},
			{Title: "HEALTH", Width: 6, Align: widgets.Right, Priority: 2},
			{Title: "STATE", Width: 8},
			{Title: "UTIL 60s", Width: 20, Priority: 3},
			{Title: "WORKLOAD", Width: 22, Min: 10, Flex: true, Priority: 5},
		},
		SortCol: -1,
	}
	inner := w - 2
	widths := tb.Layout(inner)
	workloads := gpuWorkloads(s)
	_, sel := m.selected()
	tb.Selected = sel
	tb.RowMark = m.gpuRowMark()
	for i := range s.GPUs {
		g := &s.GPUs[i]
		smp := g.Sample
		row := make([]widgets.Cell, len(tb.Columns))
		row[0] = widgets.C(th.Dim, fmt.Sprint(g.Device.Index))
		name := shortName(g.Device.Name)
		if g.Derived.Outlier {
			name = "⚠ " + name
		}
		row[1] = widgets.C(th.Text, name)
		if !g.Available {
			row[2] = widgets.C(th.Crit, "unavailable")
			row[3] = widgets.C(th.NA, g.Error)
		} else {
			row[2] = widgets.R(m.pctBar(smp.UtilPercent, widths[2]))
			if smp.MemUsed.OK && smp.MemTotal.OK {
				row[3] = widgets.R(m.fracBar(g.Derived.VRAMFraction.V, true, fmtGiBPair(smp.MemUsed.V, smp.MemTotal.V), widths[3]))
			} else {
				row[3] = widgets.C(th.NA, na)
			}
		}
		row[4] = widgets.R(m.naOr(m.temp(smp.TempC), th.Level(smp.TempC.V, 80, 88)))
		pw := na
		if smp.PowerW.OK {
			pw = strings.ReplaceAll(fmtWatts(smp.PowerW.V), " ", "")
			if smp.PowerLimitW.OK {
				pw = fmt.Sprintf("%.0f/%.0fW", smp.PowerW.V, smp.PowerLimitW.V)
			}
		}
		row[5] = widgets.R(m.naOr(pw, th.Text))
		row[6] = widgets.C(m.healthStyle(g.Health.Score), fmt.Sprint(g.Health.Score))
		row[7] = widgets.R(m.stateCell(g))
		if widths[8] > 0 {
			row[8] = widgets.R(widgets.Sparkline(th, m.live.values("util/"+string(g.Device.ID), widths[8]), widths[8], 100))
		}
		row[9] = widgets.C(th.Dim, workloads[g.Device.ID])
		tb.Rows = append(tb.Rows, row)
	}
	right := fmt.Sprintf("%d GPUs", len(s.GPUs))
	return widgets.Box(th, widgets.BoxOpts{Title: "GPUs", RightTitle: right, Focus: true}, w, h, tb.Render(th, inner, h-2))
}

// gpuWorkloads summarizes the workload(s) on each GPU.
func gpuWorkloads(s *model.Snapshot) map[gpu.ID]string {
	names := map[gpu.ID][]string{}
	for _, p := range s.Processes {
		_, n := p.WorkloadKey()
		if p.Kube.WorkloadName != "" {
			n = p.Kube.WorkloadName
		} else if p.Kube.PodName != "" {
			n = p.Kube.PodName
		} else if p.Name != "" {
			n = p.Name
		}
		list := names[p.DeviceID]
		dup := false
		for _, x := range list {
			if x == n {
				dup = true
			}
		}
		if !dup {
			names[p.DeviceID] = append(list, n)
		}
	}
	out := map[gpu.ID]string{}
	for id, list := range names {
		out[id] = strings.Join(list, ", ")
	}
	return out
}

func (m *Model) topProcesses(s *model.Snapshot, w, h int) widgets.Block {
	if m.apple() {
		return m.topProcessesApple(s, w, h)
	}
	th := m.th
	procs := append([]model.Process(nil), s.Processes...)
	sort.SliceStable(procs, func(i, j int) bool { return procs[i].MemUsed.Or(0) > procs[j].MemUsed.Or(0) })
	tb := &widgets.Table{
		Columns: []widgets.Column{
			{Title: "PID", Width: 7, Align: widgets.Right},
			{Title: "PROCESS", Width: 12, Min: 6, Flex: true},
			{Title: "GPU", Width: 3, Align: widgets.Right},
			{Title: "VRAM", Width: 9, Align: widgets.Right},
			{Title: "SM%", Width: 4, Align: widgets.Right, Priority: 1},
			{Title: "POD", Width: 20, Min: 8, Flex: true, Priority: 2},
		},
		Selected: -1, SortCol: 3, SortDesc: true,
	}
	for _, p := range procs {
		gpuLabel := fmt.Sprint(p.DeviceIndex)
		if p.PartitionID != "" {
			gpuLabel = fmt.Sprintf("%d:%d", p.DeviceIndex, p.PartitionIndex)
		}
		name := p.Name
		if name == "" {
			name = "?"
		}
		pod := p.Kube.PodName
		if pod == "" {
			pod = "—"
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, fmt.Sprint(p.PID)), widgets.C(th.Text, name), widgets.C(th.Dim, gpuLabel),
			widgets.R(m.naOr(optBytes(p.MemUsed), th.Text)), widgets.R(m.naOr(optF(p.SMUtil, "%.0f"), th.Gradient(p.SMUtil.V/100))),
			widgets.C(th.Dim, pod),
		})
	}
	return widgets.Box(th, widgets.BoxOpts{Title: "Top processes", RightTitle: "by VRAM"}, w, h, tb.Render(th, w-2, h-2))
}

// topProcessesApple ranks processes by GPU time: Apple reports no
// per-process GPU memory.
func (m *Model) topProcessesApple(s *model.Snapshot, w, h int) widgets.Block {
	th := m.th
	procs := append([]model.Process(nil), s.Processes...)
	sort.SliceStable(procs, func(i, j int) bool { return procs[i].SMUtil.Or(-1) > procs[j].SMUtil.Or(-1) })
	tb := &widgets.Table{
		Columns: []widgets.Column{
			{Title: "PID", Width: 7, Align: widgets.Right},
			{Title: "PROCESS", Width: 20, Min: 8, Flex: true},
			{Title: "GPU%", Width: 5, Align: widgets.Right},
			{Title: "USER", Width: 12, Min: 6, Priority: 1},
		},
		Selected: -1, SortCol: 2, SortDesc: true,
	}
	for _, p := range procs {
		name := p.Name
		if name == "" {
			name = "?"
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, fmt.Sprint(p.PID)), widgets.C(th.Text, name),
			widgets.R(m.naOr(optF(p.SMUtil, "%.0f"), th.Gradient(p.SMUtil.V/100))), widgets.C(th.Dim, p.User),
		})
	}
	return widgets.Box(th, widgets.BoxOpts{Title: "Top processes", RightTitle: "by GPU time"}, w, h, tb.Render(th, w-2, h-2))
}

func (m *Model) alertsAndEvents(s *model.Snapshot, w, h int) widgets.Block {
	th := m.th
	inner := w - 2
	var lines []string
	for _, a := range s.Alerts {
		st, glyph := m.sevStyle(a.Severity)
		who := ""
		if a.DeviceIndex >= 0 {
			who = fmt.Sprintf("GPU %d ", a.DeviceIndex)
		}
		lines = append(lines, st.Render(glyph+" "+who)+th.Text.Render(a.Title)+th.Dim.Render(" · "+fmtDuration(m.now().Sub(a.Since))))
		if len(lines) >= (h-2)/2 {
			break
		}
	}
	if len(s.Alerts) == 0 {
		lines = append(lines, th.OK.Render("✔ no active alerts"))
	}
	lines = append(lines, widgets.Rule(th, "recent events", inner))
	for i := len(s.Events) - 1; i >= 0 && len(lines) < h-2; i-- {
		lines = append(lines, m.eventLine(s.Events[i], inner, false))
	}
	right := fmt.Sprintf("%d active", len(s.Alerts))
	return widgets.Box(th, widgets.BoxOpts{Title: "Alerts & events", RightTitle: right}, w, h, lines)
}

func (m *Model) eventLine(e model.Event, w int, withDate bool) string {
	th := m.th
	st, glyph := m.sevStyle(e.Severity)
	ts := e.Time.Local().Format("15:04:05")
	if withDate {
		ts = e.Time.Local().Format("01-02 15:04:05")
	}
	who := "     "
	if e.DeviceIndex >= 0 {
		who = fmt.Sprintf("GPU %-2d", e.DeviceIndex)
	}
	return th.Dim.Render(ts+" ") + st.Render(glyph+" ") + th.Accent.Render(who+" ") + th.Text.Render(e.Message)
}

// viewNoGPU explains why no accelerator is visible.
func (m *Model) viewNoGPU(w, h int) widgets.Block {
	th := m.th
	s := m.view()
	var lines []string
	lines = append(lines, th.Warn.Bold(true).Render("No supported GPU detected"), "")
	if len(s.Providers) == 0 {
		lines = append(lines, th.Dim.Render("No accelerator providers are enabled (gpu.providers)."))
	}
	for _, p := range s.Providers {
		title := p.Diagnostics.Provider
		if title == "" {
			title = p.Name
		}
		lines = append(lines, th.Title.Render(title))
		for _, c := range p.Diagnostics.Checks {
			mark := th.OK.Render("✔")
			if !c.OK {
				mark = th.Crit.Render("✖")
			}
			lines = append(lines, "  "+mark+" "+th.Text.Render(fmt.Sprintf("%-18s", c.Name))+th.Dim.Render(c.Detail))
		}
		if len(p.Diagnostics.Hints) > 0 {
			lines = append(lines, "", th.Title.Render("Troubleshooting"))
			for _, hnt := range p.Diagnostics.Hints {
				for i, l := range wrap(hnt, min(90, w-12)) {
					prefix := "    "
					if i == 0 {
						prefix = "  → "
					}
					lines = append(lines, th.Accent.Render(prefix)+th.Text.Render(l))
				}
			}
		}
		lines = append(lines, "")
	}
	lines = append(lines,
		th.Dim.Render("Host monitoring is still available in the Nodes and Network tabs."),
		th.Dim.Render("Explore the interface with simulated GPUs: ")+th.Primary.Render("gputop --demo"))

	bw := min(w, max(70, min(110, w-4)))
	bh := min(h, len(lines)+4)
	box := widgets.Box(th, widgets.BoxOpts{Title: "Accelerators", Focus: true}, bw, bh, append([]string{""}, indent(lines, " ")...))
	out := widgets.Blank(th, w, h)
	top := max(0, (h-bh)/3)
	left := (w - bw) / 2
	for i, l := range box {
		if top+i < h {
			out[top+i] = widgets.Space(th, left) + l + widgets.Space(th, w-left-bw)
		}
	}
	return out
}

func indent(lines []string, prefix string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = prefix + l
	}
	return out
}
