// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

func listKeys(sel *int, n, page int, a keymap.Action) bool {
	switch a {
	case keymap.Up:
		*sel = widgets.Scroll(*sel, -1, n)
	case keymap.Down:
		*sel = widgets.Scroll(*sel, 1, n)
	case keymap.PageUp:
		*sel = widgets.Scroll(*sel, -page, n)
	case keymap.PageDown:
		*sel = widgets.Scroll(*sel, page, n)
	case keymap.Home:
		*sel = 0
	case keymap.End:
		*sel = max(0, n-1)
	default:
		return false
	}
	return true
}

// ---------------------------------------------------------------- Nodes

func keysNodes(m *Model, a keymap.Action) (bool, tea.Cmd) {
	n := 0
	if m.nodes != nil {
		n = len(m.nodes.Nodes())
	}
	return listKeys(&m.nodesSel, n, 5, a), nil
}

func viewNodes(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	hs := s.Host
	var remote widgets.Block
	remoteH := 0
	if m.nodes != nil {
		nodes := m.nodes.Nodes()
		remoteH = min(len(nodes)+3, h/2)
		remote = m.remoteNodes(nodes, w, remoteH)
	}
	h -= remoteH
	if hs == nil {
		return widgets.VJoin(remote, widgets.Box(th, widgets.BoxOpts{Title: "Node"}, w, h, []string{th.Dim.Render("Host metrics unavailable")}))
	}
	lw := 14
	info := hs.Info
	idLines := []string{
		m.kv("Hostname", th.Bold.Render(info.Hostname), lw),
		m.kv("OS", th.Text.Render(strings.TrimSpace(info.Platform+" ("+info.OS+"/"+info.Arch+")")), lw),
		m.kv("Kernel", m.naOr(orNA(info.Kernel, info.Kernel != ""), th.Text), lw),
		m.kv("Uptime", th.Text.Render(fmtDuration(info.Uptime)), lw),
		m.kv("CPU model", m.naOr(orNA(info.CPUModel, info.CPUModel != ""), th.Text), lw),
		m.kv("Cores/threads", th.Text.Render(fmt.Sprintf("%d / %d", hs.CPU.Cores, hs.CPU.Threads)), lw),
	}
	if info.Virtualization != "" {
		idLines = append(idLines, m.kv("Virtualized", th.Text.Render(info.Virtualization), lw))
	}
	for _, p := range s.Providers {
		if p.System.DriverVersion != "" {
			idLines = append(idLines, m.kv("GPU driver", th.Text.Render(p.System.DriverVersion+" ("+p.Name+")"), lw))
		}
	}
	idLines = append(idLines, m.kv("gputop", th.Text.Render(s.Node.Version), lw))

	barW := max(10, min(40, w/2-lw-14))
	c := hs.CPU
	cpuLines := []string{
		m.kv("Utilization", m.pctBar(c.UtilPercent, barW+5), lw),
		m.kv("iowait/steal", m.naOr(optPct(c.IOWaitPct), th.Text)+th.Dim.Render(" / ")+m.naOr(optPct(c.StealPct), th.Text), lw),
		m.kv("Load avg", m.naOr(optF(c.Load1, "%.2f"), th.Text)+th.Dim.Render(" ")+m.naOr(optF(c.Load5, "%.2f"), th.Text)+th.Dim.Render(" ")+m.naOr(optF(c.Load15, "%.2f"), th.Text), lw),
		m.kv("Frequency", m.naOr(optF(c.FreqMHz, "%.0f MHz"), th.Text), lw),
	}
	if n := len(c.PerCore); n > 0 {
		inner := w/2 - 4
		per := (inner - 1) / 1
		var b strings.Builder
		for i, v := range c.PerCore {
			if i > 0 && i%per == 0 {
				cpuLines = append(cpuLines, b.String())
				b.Reset()
			}
			b.WriteString(widgets.Sparkline(th, []float64{v}, 1, 100))
		}
		cpuLines = append(cpuLines, b.String())
	}
	cpuLines = append(cpuLines, th.Dim.Render("history"))
	cpuLines = append(cpuLines, widgets.Chart(th, m.live.values("host/cpu", 0), w/2-4, 3, widgets.ChartOpts{Min: 0, Max: 100, Cursor: -1})...)

	mem := hs.Memory
	memLabel := na
	if mem.Total.OK && mem.Available.OK {
		memLabel = fmt.Sprintf("%s / %s", fmtBytes(mem.Total.V-min(mem.Available.V, mem.Total.V)), fmtBytes(mem.Total.V))
	}
	uf := mem.UsedFraction()
	memLines := []string{
		m.kv("Used", m.fracBar(uf.V, uf.OK, memLabel, barW+14), lw),
		m.kv("Available", m.naOr(optBytes(mem.Available), th.Text), lw),
		m.kv("Cached", m.naOr(optBytes(mem.Cached), th.Text)+th.Dim.Render("  buffers ")+m.naOr(optBytes(mem.Buffers), th.Text), lw),
	}
	if mem.SwapTotal.OK && mem.SwapTotal.V > 0 {
		sf := float64(mem.SwapUsed.V) / float64(mem.SwapTotal.V)
		memLines = append(memLines, m.kv("Swap", m.fracBar(sf, true, fmt.Sprintf("%s / %s", fmtBytes(mem.SwapUsed.V), fmtBytes(mem.SwapTotal.V)), barW+14), lw))
	} else {
		memLines = append(memLines, m.kv("Swap", th.Dim.Render("none"), lw))
	}

	var diskLines []string
	for _, fs := range hs.Disks {
		if !fs.Total.OK {
			continue
		}
		f := float64(fs.Used.V) / float64(fs.Total.V)
		diskLines = append(diskLines, th.Text.Render(fmt.Sprintf("%-18s", truncate(fs.Mount, 18)))+" "+
			m.fracBar(f, true, fmt.Sprintf("%s/%s", fmtBytes(fs.Used.V), fmtBytes(fs.Total.V)), max(20, w/2-24)))
	}
	for _, d := range hs.IO {
		diskLines = append(diskLines, th.Dim.Render(fmt.Sprintf("%-18s", truncate(d.Name, 18)))+" "+
			th.Accent.Render("r ")+m.naOr(optRate(d.ReadBps), th.Text)+th.Accent.Render("  w ")+m.naOr(optRate(d.WriteBps), th.Text)+
			th.Dim.Render("  busy ")+m.naOr(optPct(d.BusyPct), th.Text))
	}
	if len(diskLines) == 0 {
		diskLines = []string{th.Dim.Render("no filesystems")}
	}

	ws := widgets.Split(w, 1, 1)
	leftSecs := []section{{"Node", idLines}, {"Memory", memLines}}
	rightSecs := []section{{"CPU", cpuLines}, {"Disks", diskLines}}
	left, _ := m.flowSections(leftSecs, ws[0], h, 1, 0, "")
	right, _ := m.flowSections(rightSecs, ws[1], h, 1, 0, "")
	return widgets.VJoin(remote, widgets.HJoin(th, left, right))
}

func (m *Model) remoteNodes(nodes []NodeSummary, w, h int) widgets.Block {
	th := m.th
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "NODE", Width: 18, Min: 8},
		{Title: "STATUS", Width: 10},
		{Title: "GPUS", Width: 5, Align: widgets.Right},
		{Title: "UTIL", Width: 5, Align: widgets.Right},
		{Title: "POWER", Width: 9, Align: widgets.Right},
		{Title: "HEALTH", Width: 6, Align: widgets.Right},
		{Title: "ALERTS", Width: 6, Align: widgets.Right},
		{Title: "ADDRESS", Width: 30, Min: 10, Flex: true, Priority: 1},
	}, Selected: m.nodesSel, SortCol: -1, RowMark: m.listRowMark("node:", &m.nodesSel)}
	for _, n := range nodes {
		row := []widgets.Cell{widgets.C(th.Text, n.Name)}
		if !n.OK || n.Snapshot == nil {
			msg := n.Error
			if msg == "" {
				msg = "connecting"
			}
			row = append(row, widgets.C(th.Crit, "✖ "+truncate(msg, 30)), widgets.R(""), widgets.R(""), widgets.R(""), widgets.R(""), widgets.R(""))
		} else {
			f := n.Snapshot.Fleet
			row = append(row, widgets.C(th.OK, "● online"), widgets.C(th.Text, fmt.Sprint(f.GPUs)),
				widgets.R(m.naOr(optPct(f.UtilAvg), th.Text)), widgets.R(m.naOr(optF(f.PowerW, "%.0f W"), th.Text)),
				widgets.R(m.naOr(optF(f.HealthAvg, "%.0f"), th.Text)), widgets.C(th.Text, fmt.Sprint(len(n.Snapshot.Alerts))))
		}
		row = append(row, widgets.C(th.Dim, n.Address))
		tb.Rows = append(tb.Rows, row)
	}
	return widgets.Box(th, widgets.BoxOpts{Title: "Remote nodes", RightTitle: "gputop --remote <name> for full view"}, w, h, tb.Render(th, w-2, h-2))
}

// ----------------------------------------------------------- Kubernetes

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ------------------------------------------------------------ Workloads

type workload struct {
	kind, name string
	gpus       map[gpu.ID]bool
	procs      int
	vram       uint64
	vramKnown  bool
}

func keysWorkloads(m *Model, a keymap.Action) (bool, tea.Cmd) {
	return listKeys(&m.workSel, 1000, 10, a), nil
}

func viewWorkloads(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	groups := map[string]*workload{}
	var order []string
	for _, p := range s.Processes {
		kind, name := p.WorkloadKey()
		key := kind + "|" + name
		wl := groups[key]
		if wl == nil {
			wl = &workload{kind: kind, name: name, gpus: map[gpu.ID]bool{}}
			groups[key] = wl
			order = append(order, key)
		}
		wl.gpus[p.DeviceID] = true
		wl.procs++
		if p.MemUsed.OK {
			wl.vram += p.MemUsed.V
			wl.vramKnown = true
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return len(groups[order[i]].gpus) > len(groups[order[j]].gpus) })

	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "KIND", Width: 11},
		{Title: "WORKLOAD", Width: 30, Min: 12, Flex: true},
		{Title: "GPUS", Width: 12, Min: 4},
		{Title: "PROCS", Width: 5, Align: widgets.Right, Priority: 3},
		{Title: m.memTitle(), Width: 10, Align: widgets.Right},
		{Title: "UTIL (5m)", Width: 14, Min: 5},
		{Title: "EFFICIENCY", Width: 14, Priority: 1},
		{Title: "SIGNALS", Width: 28, Min: 8, Flex: true, Priority: 2},
	}, SortCol: -1}
	widths := tb.Layout(w - 2)
	q := m.q("workloads")
	for _, key := range order {
		wl := groups[key]
		var ids []int
		var utilSum float64
		utilN, idle, outliers, throttled := 0, 0, 0, 0
		var effSum, effN int
		for id := range wl.gpus {
			g, ok := s.GPUByID(id)
			if !ok {
				continue
			}
			ids = append(ids, g.Device.Index)
			if g.Derived.UtilAvg.OK {
				utilSum += g.Derived.UtilAvg.V
				utilN++
			}
			if g.Derived.IdleAllocated {
				idle++
			}
			if g.Derived.Outlier {
				outliers++
			}
			if g.Derived.Throttled {
				throttled++
			}
			if g.Derived.Efficiency.Score.OK {
				effSum += g.Derived.Efficiency.Score.V
				effN++
			}
		}
		sort.Ints(ids)
		idStr := make([]string, len(ids))
		for i, v := range ids {
			idStr[i] = fmt.Sprint(v)
		}
		if !matchesQuery(q, map[string]string{"kind": wl.kind, "workload": wl.name, "gpu": strings.Join(idStr, ",")}) {
			continue
		}
		util := metric.Opt[float64]{V: utilSum / float64(max(1, utilN)), OK: utilN > 0}
		var signals []string
		if idle > 0 {
			signals = append(signals, th.Warn.Render(fmt.Sprintf("%d idle-allocated", idle)))
		}
		if outliers > 0 {
			signals = append(signals, th.Warn.Render(fmt.Sprintf("%d straggler", outliers)))
		}
		if throttled > 0 {
			signals = append(signals, th.Warn.Render(fmt.Sprintf("%d throttled", throttled)))
		}
		if len(signals) == 0 {
			signals = append(signals, th.OK.Render("ok"))
		}
		eff := th.NA.Render(na)
		if effN > 0 {
			avg := effSum / effN
			eff = th.Gradient(1-float64(avg)/100).Render(fmt.Sprintf("%d", avg)) + th.Dim.Render(" "+gradeOf(avg))
		}
		vram := na
		if wl.vramKnown {
			vram = fmtBytes(wl.vram)
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, wl.kind), widgets.C(th.Text, wl.name), widgets.C(th.Accent, strings.Join(idStr, ",")),
			widgets.C(th.Text, fmt.Sprint(wl.procs)), widgets.R(m.naOr(vram, th.Text)),
			widgets.R(m.pctBar(util, widths[5])), widgets.R(eff), widgets.R(strings.Join(signals, th.Dim.Render(" · "))),
		})
	}
	m.workSel = widgets.Scroll(m.workSel, 0, len(tb.Rows))
	tb.Selected = m.workSel
	tb.RowMark = m.listRowMark("workload:", &m.workSel)
	note := []string{th.Dim.Render(" Efficiency is a gputop-derived utilization indicator (compute, bandwidth, VRAM, throttling), not throughput.")}
	content := append(tb.Render(th, w-2, h-4), note...)
	return widgets.Box(th, widgets.BoxOpts{Title: "Workloads", RightTitle: fmt.Sprintf("%d groups", len(tb.Rows)), Focus: true}, w, h, content)
}

func gradeOf(score int) string {
	switch {
	case score >= 80:
		return "high"
	case score >= 50:
		return "moderate"
	case score >= 20:
		return "low"
	}
	return "very low"
}

// -------------------------------------------------------------- Network

func keysNetwork(m *Model, a keymap.Action) (bool, tea.Cmd) {
	return listKeys(&m.netSel, 500, 10, a), nil
}

func viewNetwork(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	if s.Host == nil {
		return nil
	}
	q := m.q("network")
	showAll := strings.Contains(q.filter, "kind:all") || strings.Contains(q.filter, "kind:virtual") || strings.Contains(q.filter, "kind:loopback")
	chartH := 0
	if h >= 20 {
		chartH = min(10, h/3)
	}
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "INTERFACE", Width: 16, Min: 6},
		{Title: "KIND", Width: 10, Priority: 3},
		{Title: "STATE", Width: 6},
		{Title: "SPEED", Width: 9, Align: widgets.Right, Priority: 2},
		{Title: "RX", Width: 22, Min: 10, Flex: true},
		{Title: "TX", Width: 22, Min: 10, Flex: true},
		{Title: "RX PPS", Width: 9, Align: widgets.Right, Priority: 4},
		{Title: "TX PPS", Width: 9, Align: widgets.Right, Priority: 4},
		{Title: "ERR RX/TX", Width: 11, Align: widgets.Right, Priority: 1},
		{Title: "DROP RX/TX", Width: 11, Align: widgets.Right, Priority: 1},
	}, SortCol: -1}
	widths := tb.Layout(w - 2)
	hidden := 0
	var maxRate float64
	for _, n := range s.Host.Net {
		maxRate = max(maxRate, n.RxBps.Or(0), n.TxBps.Or(0))
	}
	for _, n := range s.Host.Net {
		if !showAll && (n.Kind == "virtual" || n.Kind == "loopback") {
			hidden++
			continue
		}
		f := q.filter
		if showAll {
			f = strings.ReplaceAll(strings.ReplaceAll(f, "kind:all", ""), "kind:virtual", "")
		}
		if !matchesQuery(&query{search: q.search, filter: f}, map[string]string{"name": n.Name, "kind": n.Kind}) {
			continue
		}
		capRate := maxRate
		if n.SpeedMbps.OK && n.SpeedMbps.V > 0 {
			capRate = n.SpeedMbps.V * 1e6 / 8
		}
		rateBar := func(o metric.Opt[float64], w int) string {
			if !o.OK {
				return th.NA.Render(na)
			}
			return m.fracBar(o.V/max(1, capRate), true, fmtRate(o.V), w)
		}
		state := th.OK.Render("up")
		if !n.Up {
			state = th.Muted.Render("down")
		}
		errs := fmt.Sprintf("%s/%s", optU(n.RxErrors), optU(n.TxErrors))
		es := th.Text
		if n.RxErrors.Or(0)+n.TxErrors.Or(0) > 0 {
			es = th.Warn
		}
		speed := na
		if n.SpeedMbps.OK {
			speed = fmt.Sprintf("%.0fG", n.SpeedMbps.V/1000)
			if n.SpeedMbps.V < 1000 {
				speed = fmt.Sprintf("%.0fM", n.SpeedMbps.V)
			}
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Text, n.Name), widgets.C(th.Dim, n.Kind), widgets.R(state), widgets.R(m.naOr(speed, th.Dim)),
			widgets.R(rateBar(n.RxBps, widths[4])), widgets.R(rateBar(n.TxBps, widths[5])),
			widgets.R(m.naOr(optF(n.RxPps, "%.0f"), th.Text)), widgets.R(m.naOr(optF(n.TxPps, "%.0f"), th.Text)),
			widgets.C(es, errs), widgets.C(th.Text, fmt.Sprintf("%s/%s", optU(n.RxDrops), optU(n.TxDrops))),
		})
	}
	m.netSel = widgets.Scroll(m.netSel, 0, len(tb.Rows))
	tb.Selected = m.netSel
	tb.RowMark = m.listRowMark("net:", &m.netSel)
	right := ""
	if hidden > 0 {
		right = fmt.Sprintf("%d virtual/loopback hidden (f → kind:all)", hidden)
	}
	tableH := h - chartH
	out := widgets.Box(th, widgets.BoxOpts{Title: "Network", RightTitle: right, Focus: true}, w, tableH, tb.Render(th, w-2, tableH-2))
	if chartH > 0 {
		ws := widgets.Split(w, 1, 1)
		rx := m.live.values("host/netrx", 0)
		tx := m.live.values("host/nettx", 0)
		cur := func(v []float64) string {
			if len(v) == 0 {
				return na
			}
			return fmtRate(v[len(v)-1])
		}
		rxBox := widgets.Box(th, widgets.BoxOpts{Title: "RX total · " + cur(rx)}, ws[0], chartH, widgets.Chart(th, rx, ws[0]-2, chartH-2, widgets.ChartOpts{Cursor: -1}))
		txBox := widgets.Box(th, widgets.BoxOpts{Title: "TX total · " + cur(tx)}, ws[1], chartH, widgets.Chart(th, tx, ws[1]-2, chartH-2, widgets.ChartOpts{Cursor: -1}))
		out = append(out, widgets.HJoin(th, rxBox, txBox)...)
	}
	return out
}

// --------------------------------------------------------------- Events

func (m *Model) filteredEvents() []model.Event {
	s := m.view()
	q := m.q("events")
	var out []model.Event
	for i := len(s.Events) - 1; i >= 0; i-- {
		e := s.Events[i]
		fields := map[string]string{"kind": e.Kind, "sev": string(e.Severity), "message": e.Message, "source": string(e.Source)}
		if e.DeviceIndex >= 0 {
			fields["gpu"] = fmt.Sprint(e.DeviceIndex)
		}
		if matchesQuery(q, fields) {
			out = append(out, e)
		}
	}
	return out
}

func keysEvents(m *Model, a keymap.Action) (bool, tea.Cmd) {
	evs := m.filteredEvents()
	if a == keymap.Select {
		m.events.detail = !m.events.detail && len(evs) > 0
		return true, nil
	}
	if a == keymap.Back && m.events.detail {
		m.events.detail = false
		return true, nil
	}
	return listKeys(&m.events.sel, len(evs), 10, a), nil
}

func hintsEvents(m *Model) []hint {
	return []hint{{keymap.Up, "select"}, {keymap.Select, "detail"}}
}

func viewEvents(m *Model, w, h int) widgets.Block {
	th := m.th
	evs := m.filteredEvents()
	m.events.sel = widgets.Scroll(m.events.sel, 0, len(evs))
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "TIME", Width: 14},
		{Title: "SEV", Width: 8},
		{Title: "GPU", Width: 3, Align: widgets.Right},
		{Title: "KIND", Width: 18, Priority: 1},
		{Title: "MESSAGE", Width: 40, Min: 20, Flex: true},
		{Title: "SOURCE", Width: 9, Priority: 2},
	}, Selected: m.events.sel, SortCol: 0, SortDesc: true, RowMark: m.listRowMark("event:", &m.events.sel)}
	if len(evs) == 0 {
		tb.Selected = -1
	}
	for _, e := range evs {
		st, glyph := m.sevStyle(e.Severity)
		who := "—"
		if e.DeviceIndex >= 0 {
			who = fmt.Sprint(e.DeviceIndex)
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, e.Time.Local().Format("01-02 15:04:05")), widgets.C(st, glyph+" "+string(e.Severity)),
			widgets.C(th.Accent, who), widgets.C(th.Dim, e.Kind), widgets.C(th.Text, e.Message), widgets.C(th.Muted, string(e.Source)),
		})
	}
	right := fmt.Sprintf("%d events · newest first", len(evs))
	box := widgets.Box(th, widgets.BoxOpts{Title: "Event timeline", RightTitle: right, Focus: true}, w, h, tb.Render(th, w-2, h-2))
	if m.events.detail && m.events.sel < len(evs) {
		e := evs[m.events.sel]
		st, _ := m.sevStyle(e.Severity)
		lines := []string{
			m.kv("Time", th.Text.Render(e.Time.Local().Format(time.RFC3339)+" ("+fmtAgo(m.now(), e.Time)+")"), 10),
			m.kv("Severity", st.Render(string(e.Severity)), 10),
			m.kv("Kind", th.Text.Render(e.Kind), 10),
			m.kv("Source", th.Text.Render(string(e.Source)), 10),
		}
		if e.DeviceID != "" {
			lines = append(lines, m.kv("GPU", th.Text.Render(fmt.Sprintf("%d (%s)", e.DeviceIndex, e.DeviceID)), 10))
		}
		bw := min(w-4, 90)
		lines = append(lines, "")
		for _, l := range wrap(e.Message, bw-4) {
			lines = append(lines, th.Bold.Render(l))
		}
		for _, k := range sortedKeys(e.Attrs) {
			lines = append(lines, m.kv(k, th.Text.Render(e.Attrs[k]), 10))
		}
		return m.overlay(box, widgets.Box(th, widgets.BoxOpts{Title: "Event", Focus: true}, bw, len(lines)+2, lines), w, h)
	}
	return box
}

// --------------------------------------------------------------- Health

func keysHealth(m *Model, a keymap.Action) (bool, tea.Cmd) {
	return keysGPUSelect(m, a)
}

func viewHealth(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	collH := min(len(s.Collectors)+3, max(6, h/3))
	topH := h - collH

	var top widgets.Block
	if len(s.GPUs) > 0 {
		ws := widgets.Split(w, 9, 11)
		tb := &widgets.Table{Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "SCORE", Width: 18, Min: 8},
			{Title: "BAND", Width: 9},
			{Title: "TOP REASON", Width: 30, Min: 8, Flex: true, Priority: 1},
		}, SortCol: -1}
		widths := tb.Layout(ws[0] - 2)
		_, tb.Selected = m.selected()
		tb.RowMark = m.gpuRowMark()
		for i := range s.GPUs {
			g := &s.GPUs[i]
			hs := m.healthStyle(g.Health.Score)
			reason := "—"
			if len(g.Health.Reasons) > 0 {
				reason = g.Health.Reasons[0].Text
			}
			bw := max(3, widths[1]-4)
			fill := g.Health.Score * bw / 100
			bar := hs.Render(strings.Repeat("█", fill)) + th.Muted.Render(strings.Repeat("░", bw-fill))
			tb.Rows = append(tb.Rows, []widgets.Cell{
				widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
				widgets.R(hs.Render(fmt.Sprintf("%3d ", g.Health.Score)) + bar),
				widgets.C(hs, string(g.Health.Band)), widgets.C(th.Dim, reason),
			})
		}
		left := widgets.Box(th, widgets.BoxOpts{Title: "Health score", RightTitle: "gputop-derived", Focus: true}, ws[0], topH, tb.Render(th, ws[0]-2, topH-2))

		g, _ := m.selected()
		var rl []string
		rl = append(rl, th.Title.Render(fmt.Sprintf("GPU %d · score %d (%s)", g.Device.Index, g.Health.Score, g.Health.Band)))
		if len(g.Health.Reasons) == 0 {
			rl = append(rl, th.OK.Render("✔ no deductions"))
		}
		for _, r := range g.Health.Reasons {
			st, glyph := m.sevStyle(model.Severity(r.Severity))
			rl = append(rl, st.Render(fmt.Sprintf("%s −%-3d ", glyph, r.Penalty))+th.Text.Render(r.Text))
		}
		rl = append(rl, "", th.Title.Render("Active alerts"))
		if len(s.Alerts) == 0 {
			rl = append(rl, th.OK.Render("✔ none"))
		}
		for _, a := range s.Alerts {
			st, glyph := m.sevStyle(a.Severity)
			who := ""
			if a.DeviceIndex >= 0 {
				who = fmt.Sprintf("GPU %d ", a.DeviceIndex)
			}
			rl = append(rl, st.Render(glyph+" "+who)+th.Text.Render(a.Title)+th.Dim.Render(" · since "+a.Since.Local().Format("15:04:05")))
		}
		rl = append(rl, "", th.Dim.Render("The score is a transparent heuristic built from ECC, row remapping, Xid, thermal,"),
			th.Dim.Render("power-brake, PCIe and NVLink signals. It is not a vendor metric. See docs/health-score.md."))
		right := widgets.Box(th, widgets.BoxOpts{Title: "Why"}, ws[1], topH, rl)
		top = widgets.HJoin(th, left, right)
	} else {
		top = widgets.Box(th, widgets.BoxOpts{Title: "Health"}, w, topH, []string{th.Dim.Render("No GPUs to score.")})
	}

	ws := widgets.Split(w, 13, 7)
	ct := &widgets.Table{Columns: []widgets.Column{
		{Title: "COLLECTOR", Width: 12},
		{Title: "TIER", Width: 9},
		{Title: "EVERY", Width: 6, Align: widgets.Right},
		{Title: "LAST", Width: 8, Align: widgets.Right},
		{Title: "AVG", Width: 8, Align: widgets.Right, Priority: 2},
		{Title: "RUNS", Width: 6, Align: widgets.Right, Priority: 3},
		{Title: "ERR", Width: 5, Align: widgets.Right},
		{Title: "SKIP", Width: 5, Align: widgets.Right, Priority: 1},
		{Title: "STATUS", Width: 24, Min: 6, Flex: true},
	}, SortCol: -1, Selected: -1}
	for _, c := range s.Collectors {
		status := th.OK.Render("ok")
		if !c.Healthy {
			status = th.Crit.Render(c.LastError)
		} else if c.Runs == 0 {
			status = th.Muted.Render("pending")
		}
		ct.Rows = append(ct.Rows, []widgets.Cell{
			widgets.C(th.Text, c.Name), widgets.C(th.Dim, c.Tier), widgets.C(th.Dim, fmtDurationShortAny(c.Interval)),
			widgets.C(th.Text, fmtMs(c.LastDuration)), widgets.C(th.Text, fmtMs(c.AvgDuration)),
			widgets.C(th.Dim, fmt.Sprint(c.Runs)), widgets.C(errStyle(m, c.Errors), fmt.Sprint(c.Errors)),
			widgets.C(errStyle(m, c.Overruns), fmt.Sprint(c.Overruns)), widgets.R(status),
		})
	}
	coll := widgets.Box(th, widgets.BoxOpts{Title: "Collectors"}, ws[0], collH, ct.Render(th, ws[0]-2, collH-2))

	self := s.Self
	lw := 12
	sl := []string{
		m.kv("CPU", m.naOr(optF(self.CPUPercent, "%.1f%%"), th.Text), lw),
		m.kv("Memory", th.Text.Render(fmtBytes(self.SysBytes))+th.Dim.Render(" (heap "+fmtBytes(self.HeapBytes)+")"), lw),
		m.kv("Goroutines", th.Text.Render(fmt.Sprint(self.Goroutines)), lw),
		m.kv("Collect", th.Text.Render(fmtMs(self.CollectTime))+th.Dim.Render("  render ")+th.Text.Render(fmtMs(m.renderTime)), lw),
	}
	hist := s.History
	if hist.Enabled {
		mode := "disk"
		if !hist.Persistent {
			mode = "memory"
		}
		sl = append(sl, m.kv("History", th.Text.Render(fmt.Sprintf("%d pts · %s · %s", hist.Points, mode, fmtBytes(uint64(hist.DiskBytes)))), lw),
			m.kv("Write", th.Text.Render(fmtMs(hist.WriteAvg))+th.Dim.Render(" avg"), lw))
		if hist.Error != "" {
			sl = append(sl, th.Warn.Render(hist.Error))
		}
	} else {
		sl = append(sl, m.kv("History", th.Dim.Render("disabled"), lw))
	}
	if !self.StartTime.IsZero() {
		sl = append(sl, m.kv("Uptime", th.Text.Render(fmtDuration(m.now().Sub(self.StartTime))), lw))
	}
	selfBox := widgets.Box(th, widgets.BoxOpts{Title: "gputop itself"}, ws[1], collH, sl)
	return widgets.VJoin(top, widgets.HJoin(th, coll, selfBox))
}

func errStyle(m *Model, n uint64) lipgloss.Style {
	if n > 0 {
		return m.th.Warn
	}
	return m.th.Dim
}

func fmtMs(d time.Duration) string {
	switch {
	case d == 0:
		return "—"
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func fmtDurationShortAny(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%gs", d.Seconds())
	}
	return fmtDurationShort(d)
}
