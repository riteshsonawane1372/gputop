// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"fmt"
	"math"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/theme"
	"github.com/gputop/gputop/internal/tui/widgets"
)

// The Dashboard tab mirrors the NVIDIA DCGM exporter Grafana dashboard:
// stat tiles, one time-series panel per metric with a line per GPU, and a
// reliability table. Series come from the history store (with a selectable
// time range) or, when history is off, from the in-memory live buffers.

type dashState struct {
	window int // index into windows
	init   bool
	focus  int
	zoom   bool
	only   int // isolated GPU index, -1 for all
	scroll int

	lastFocus int
	maxScroll int
	res       *history.Result
	err       error
	loading   bool
	lastAt    time.Time
	lastKey   string
}

type dashMsg struct {
	key string
	res *history.Result
	err error
}

// dashPanel is one time-series panel. dcgm names the equivalent DCGM
// exporter field so Grafana users recognize the metric.
type dashPanel struct {
	title  string
	dcgm   string
	metric history.Metric
	live   string
	scale  float64 // live value multiplier to the panel unit
	unit   string
	max    float64
	agg    string // headline aggregate: avg, sum or max
}

var dashPanels = []dashPanel{
	{"GPU Utilization", "DCGM_FI_DEV_GPU_UTIL", history.Util, "util", 1, "%", 100, "avg"},
	{"GPU Temperature", "DCGM_FI_DEV_GPU_TEMP", history.TempC, "temp", 1, "°C", 0, "avg"},
	{"GPU Power Usage", "DCGM_FI_DEV_POWER_USAGE", history.PowerW, "power", 1, "W", 0, "sum"},
	{"Framebuffer Used", "DCGM_FI_DEV_FB_USED", history.VRAMUsedGiB, "fbused", 1, "GiB", 0, "sum"},
	{"Memory Copy Utilization", "DCGM_FI_DEV_MEM_COPY_UTIL", history.MemBandwidth, "membw", 1, "%", 100, "avg"},
	{"GPU SM Clock", "", history.ClockCoreMHz, "clock", 1, "MHz", 0, "avg"},
	{"Memory Temperature", "DCGM_FI_DEV_MEMORY_TEMP", history.MemTempC, "memtemp", 1, "°C", 0, "max"},
	{"PCIe RX", "DCGM_FI_PROF_PCIE_RX_BYTES", history.PCIeRxMBps, "pcierx", 1.0 / (1 << 20), "MB/s", 0, "sum"},
	{"PCIe TX", "DCGM_FI_PROF_PCIE_TX_BYTES", history.PCIeTxMBps, "pcietx", 1.0 / (1 << 20), "MB/s", 0, "sum"},
	{"NVLink RX", "DCGM_FI_PROF_NVLINK_RX_BYTES", history.NVLinkRxMBps, "nvlrx", 1.0 / (1 << 20), "MB/s", 0, "sum"},
	{"NVLink TX", "DCGM_FI_PROF_NVLINK_TX_BYTES", history.NVLinkTxMBps, "nvltx", 1.0 / (1 << 20), "MB/s", 0, "sum"},
	{"Encoder Utilization", "DCGM_FI_DEV_ENC_UTIL", history.EncoderPct, "enc", 1, "%", 100, "avg"},
	{"Decoder Utilization", "DCGM_FI_DEV_DEC_UTIL", history.DecoderPct, "dec", 1, "%", 100, "avg"},
	{"Fan Speed", "DCGM_FI_DEV_FAN_SPEED", history.FanPct, "fan", 1, "%", 100, "avg"},
}

// grafanaPalette is Grafana's classic series palette.
var grafanaPalette = []string{
	"#7EB26D", "#EAB839", "#6ED0E0", "#EF843C", "#E24D42", "#1F78C1", "#BA43A9", "#705DA0",
	"#508642", "#CCA300", "#447EBC", "#C15C17", "#890F02", "#0A437C", "#6D1F62", "#584477",
}

func (m *Model) seriesStyle(gpuIndex int) lipgloss.Style {
	return m.th.Base.Foreground(lipgloss.Color(grafanaPalette[(gpuIndex%len(grafanaPalette)+len(grafanaPalette))%len(grafanaPalette)]))
}

func (m *Model) dashUsesHistory() bool {
	return m.src.History() != nil && m.view().History.Enabled
}

func (m *Model) dashQueryKey() string { return fmt.Sprint(m.dash.window) }

// maybeQueryDash refreshes the dashboard's history query when stale.
func (m *Model) maybeQueryDash(force bool) tea.Cmd {
	d := &m.dash
	if !m.dashUsesHistory() || d.loading || m.paused && d.res != nil && !force {
		return nil
	}
	res := m.view().History.Resolution
	if res <= 0 {
		res = 5 * time.Second
	}
	key := m.dashQueryKey()
	if !force && key == d.lastKey && d.res != nil && m.now().Sub(d.lastAt) < res {
		return nil
	}
	d.loading, d.lastKey, d.lastAt = true, key, m.now()
	reader := m.src.History()
	window := windows[d.window]
	points := max(120, m.width*2)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := reader.Query(ctx, history.Query{Since: time.Now().Add(-window), MaxPoints: points})
		return dashMsg{key: key, res: r, err: err}
	}
}

func (m *Model) onDash(msg dashMsg) {
	d := &m.dash
	d.loading = false
	if msg.key == m.dashQueryKey() {
		d.res, d.err = msg.res, msg.err
	}
}

// panelSeries returns one value slice per GPU (in snapshot order) and the
// covered time span.
func (m *Model) panelSeries(p dashPanel) ([][]float64, time.Time, time.Time) {
	s := m.view()
	out := make([][]float64, len(s.GPUs))
	now := m.now()
	if m.dashUsesHistory() {
		res := m.dash.res
		if res == nil || len(res.Times) == 0 {
			return out, now.Add(-windows[m.dash.window]), now
		}
		for i := range s.GPUs {
			col := res.Column(string(s.GPUs[i].Device.ID), p.metric)
			vals := make([]float64, len(res.Times))
			for j := range vals {
				vals[j] = math.NaN()
				if j < len(col) {
					vals[j] = float64(col[j])
				}
			}
			out[i] = vals
		}
		return out, time.UnixMilli(res.Times[0]), time.UnixMilli(res.Times[len(res.Times)-1])
	}
	n := 0
	for i := range s.GPUs {
		vals := m.live.values(p.live+"/"+string(s.GPUs[i].Device.ID), 0)
		for j := range vals {
			vals[j] *= p.scale
		}
		out[i] = vals
		n = max(n, len(vals))
	}
	return out, now.Add(-time.Duration(n) * m.refresh), now
}

// visiblePanels drops panels without data for any GPU (for example fan
// speed on passively cooled GPUs, NVLink on PCIe-only systems).
func (m *Model) visiblePanels() []int {
	var out []int
	for i, p := range dashPanels {
		series, _, _ := m.panelSeries(p)
		if hasData(series) {
			out = append(out, i)
		}
	}
	return out
}

func hasData(series [][]float64) bool {
	for _, s := range series {
		for _, v := range s {
			if !math.IsNaN(v) {
				return true
			}
		}
	}
	return false
}

func lastValue(vals []float64) float64 {
	for i := len(vals) - 1; i >= 0; i-- {
		if !math.IsNaN(vals[i]) {
			return vals[i]
		}
	}
	return math.NaN()
}

func keysDashboard(m *Model, a keymap.Action) (bool, tea.Cmd) {
	d := &m.dash
	panels := m.visiblePanels()
	n := len(panels) + 1 // + reliability table
	switch a {
	case keymap.Up:
		d.focus = max(0, d.focus-1)
	case keymap.Down:
		d.focus = min(n-1, d.focus+1)
	case keymap.PageUp:
		d.scroll = max(0, d.scroll-10)
	case keymap.PageDown:
		d.scroll = min(d.maxScroll, d.scroll+10)
	case keymap.Home:
		d.focus, d.scroll = 0, 0
	case keymap.End:
		d.focus, d.scroll = n-1, d.maxScroll
	case keymap.Select:
		if d.focus < len(panels) {
			d.zoom = !d.zoom
		}
	case keymap.Back:
		switch {
		case d.zoom:
			d.zoom = false
		case d.only >= 0:
			d.only = -1
		default:
			return false, nil
		}
	case keymap.ZoomIn:
		if d.window > 0 {
			d.window--
			return true, m.maybeQueryDash(true)
		}
	case keymap.ZoomOut:
		if d.window < m.maxWindow() {
			d.window++
			return true, m.maybeQueryDash(true)
		}
		m.setToast("window limited by history.retention", 2*time.Second)
	case keymap.NextGPU, keymap.PrevGPU:
		m.cycleIsolate(a == keymap.NextGPU)
	default:
		return false, nil
	}
	return true, nil
}

// cycleIsolate steps the isolated GPU: all → 0 → 1 → … → all.
func (m *Model) cycleIsolate(forward bool) {
	s := m.view()
	d := &m.dash
	pos := -1
	for i := range s.GPUs {
		if s.GPUs[i].Device.Index == d.only {
			pos = i
		}
	}
	n := len(s.GPUs) + 1
	step := 1
	if !forward {
		step = -1
	}
	pos = ((pos+1+step)%n+n)%n - 1
	d.only = -1
	if pos >= 0 {
		d.only = s.GPUs[pos].Device.Index
	}
}

func hintsDashboard(m *Model) []hint {
	if m.dash.zoom {
		return []hint{{keymap.Back, "back"}, {keymap.NextGPU, "isolate GPU"}, {keymap.ZoomOut, "range"}}
	}
	return []hint{{keymap.Down, "panel"}, {keymap.Select, "zoom"}, {keymap.NextGPU, "isolate GPU"}, {keymap.ZoomOut, "range"}}
}

func viewDashboard(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	d := &m.dash
	if !d.init {
		d.init, d.window, d.only = true, 3, -1 // Grafana's default: last 15 minutes
	}
	d.window = min(d.window, m.maxWindow())

	toolbar := m.dashToolbar(w)
	panels := m.visiblePanels()
	if d.zoom && d.focus < len(panels) {
		return widgets.VJoin(widgets.Block{toolbar}, m.renderPanel(dashPanels[panels[d.focus]], d.focus, w, h-1, true, true))
	}
	d.zoom = false

	cols := 1
	switch {
	case w >= 180:
		cols = 3
	case w >= 100:
		cols = 2
	}
	ph := min(16, max(10, (h-6)/2))
	var canvas widgets.Block
	tiles := m.statTiles(w)
	canvas = append(canvas, tiles...)
	focusTop, focusBottom := 0, 0
	for row := 0; row*cols < len(panels); row++ {
		widths := widgets.Split(w, repeat(1, cols)...)
		var blocks []widgets.Block
		for c := 0; c < cols; c++ {
			i := row*cols + c
			if i >= len(panels) {
				blocks = append(blocks, widgets.Blank(th, widths[c], ph))
				continue
			}
			if i == d.focus {
				focusTop, focusBottom = len(canvas), len(canvas)+ph
			}
			blocks = append(blocks, m.renderPanel(dashPanels[panels[i]], i, widths[c], ph, i == d.focus, false))
		}
		canvas = append(canvas, widgets.HJoin(th, blocks...)...)
	}
	relH := len(s.GPUs) + 3
	if d.focus >= len(panels) {
		focusTop, focusBottom = len(canvas), len(canvas)+relH
	}
	canvas = append(canvas, m.reliabilityPanel(w, relH, d.focus >= len(panels), len(panels))...)

	body := h - 1
	d.maxScroll = max(0, len(canvas)-body)
	// Bring a newly focused panel into view; free scrolling is kept
	// otherwise.
	if d.focus != d.lastFocus {
		d.lastFocus = d.focus
		if focusBottom > d.scroll+body {
			d.scroll = focusBottom - body
		}
		if focusTop < d.scroll {
			d.scroll = focusTop
		}
	}
	d.scroll = max(0, min(d.scroll, d.maxScroll))
	visible := canvas[d.scroll:min(len(canvas), d.scroll+body)]
	visible = widgets.Scrollbar(th, widgets.FitBlock(th, visible, w, body), d.scroll, d.maxScroll)
	if m.mouse {
		for i, l := range visible {
			visible[i] = m.zones.markWheel("dash:canvas", l, nil, func(delta int) tea.Cmd {
				d.scroll = max(0, min(d.maxScroll, d.scroll+delta))
				return nil
			})
		}
	}
	return widgets.VJoin(widgets.Block{toolbar}, visible)
}

func (m *Model) dashToolbar(w int) string {
	th := m.th
	d := &m.dash
	s := m.view()
	out := th.Title.Render(" NVIDIA DCGM") + th.Dim.Render(" dashboard  ")
	if m.apple() {
		out = th.Title.Render(" GPU metrics") + th.Dim.Render(" dashboard  ")
	}
	if m.dashUsesHistory() {
		for i, win := range windows[:m.maxWindow()+1] {
			label := " " + fmtDurationShort(win) + " "
			if i == d.window {
				label = th.TabActive.Render(label)
			} else {
				label = th.Dim.Render(label)
			}
			idx := i
			out += m.zone("dash:window:"+fmt.Sprint(i), label, func(bool) tea.Cmd {
				d.window = idx
				return m.maybeQueryDash(true)
			})
		}
		if d.loading && d.res == nil {
			out += th.Muted.Render("  loading…")
		}
		if d.err != nil {
			out += th.Crit.Render("  " + d.err.Error())
		}
	} else {
		out += th.Dim.Render("live ") + th.TabActive.Render(" "+fmtDurationShort(m.refresh*liveCap)+" ") + th.Muted.Render("  (history disabled)")
	}
	gpus := "all GPUs"
	if d.only >= 0 {
		gpus = fmt.Sprintf("GPU %d only", d.only)
	}
	out += th.Dim.Render("   series ") + th.Accent.Render(gpus)
	if len(s.GPUs) > 0 && len(s.GPUs) != 1 {
		out += th.Dim.Render("  [ ] isolate · click legend")
	}
	return widgets.Fit(th, out, w)
}

// statTiles renders Grafana singlestat-like tiles.
func (m *Model) statTiles(w int) widgets.Block {
	th := m.th
	s := m.view()
	f := s.Fleet
	type tile struct {
		label, value string
		style        lipgloss.Style
	}
	var energy float64
	energyOK := false
	var eccU, xids uint64
	eccOK := false
	for i := range s.GPUs {
		g := &s.GPUs[i]
		if g.Sample.EnergyJ.OK {
			energy += g.Sample.EnergyJ.V
			energyOK = true
		}
		if g.Counters.ECCUncorrectedVolatile.OK {
			eccU += g.Counters.ECCUncorrectedVolatile.V
			eccOK = true
		}
	}
	cutoff := m.now().Add(-time.Hour)
	for _, e := range s.Events {
		if e.Kind == "xid" && e.Time.After(cutoff) {
			xids++
		}
	}
	opt := func(ok bool, v string) string {
		if !ok {
			return na
		}
		return v
	}
	tiles := []tile{
		{"GPUs", fmt.Sprint(len(s.GPUs)), th.Primary},
		{"Utilization", opt(f.UtilAvg.OK, fmt.Sprintf("%.0f%%", f.UtilAvg.V)), th.Gradient(f.UtilAvg.V / 100)},
		{"Power", opt(f.PowerW.OK, fmtWatts(f.PowerW.V)), th.Primary},
		{"Avg temp", opt(f.TempAvgC.OK, m.temp(f.TempAvgC)), th.Level(f.TempMaxC.V, 80, 88)},
		{"Max temp", opt(f.TempMaxC.OK, m.temp(f.TempMaxC)), th.Level(f.TempMaxC.V, 80, 88)},
		{"FB used", opt(f.VRAMUsed.OK, fmtBytes(f.VRAMUsed.V)+" / "+fmtBytes(f.VRAMTotal.V)), th.Gradient(f.VRAMFraction.V)},
		{"Energy", opt(energyOK, fmtEnergy(energy)), th.Primary},
		{"XID (1h)", fmt.Sprint(xids), levelIf(th, xids > 0, th.Warn)},
		{"ECC DBE", opt(eccOK, fmt.Sprint(eccU)), levelIf(th, eccU > 0, th.Crit)},
		{"Throttled", fmt.Sprintf("%d / %d", f.Throttled, len(s.GPUs)), levelIf(th, f.Throttled > 0, th.Warn)},
	}
	const minW = 18
	perRow := max(1, min(len(tiles), w/minW))
	var out widgets.Block
	for start := 0; start < len(tiles); start += perRow {
		row := tiles[start:min(len(tiles), start+perRow)]
		widths := widgets.Split(w, repeat(1, perRow)...)
		var blocks []widgets.Block
		for i := 0; i < perRow; i++ {
			if i >= len(row) {
				blocks = append(blocks, widgets.Blank(th, widths[i], 4))
				continue
			}
			t := row[i]
			blocks = append(blocks, widgets.Box(th, widgets.BoxOpts{Title: t.label}, widths[i], 3,
				[]string{widgets.Center(th, t.style.Bold(true).Render(t.value), widths[i]-2)}))
		}
		out = append(out, widgets.HJoin(th, blocks...)...)
	}
	return out
}

// levelIf returns st when cond holds, the OK style otherwise.
func levelIf(th *theme.Theme, cond bool, st lipgloss.Style) lipgloss.Style {
	if cond {
		return st
	}
	return th.OK
}

// renderPanel draws one time-series panel.
func (m *Model) renderPanel(p dashPanel, idx, w, h int, focus, zoom bool) widgets.Block {
	th := m.th
	s := m.view()
	d := &m.dash
	series, t0, t1 := m.panelSeries(p)
	inner := w - 2

	// Headline aggregate over GPUs' latest values.
	var sum, mx float64
	cnt := 0
	mx = math.Inf(-1)
	for i, vals := range series {
		if d.only >= 0 && s.GPUs[i].Device.Index != d.only {
			continue
		}
		if v := lastValue(vals); !math.IsNaN(v) {
			sum += v
			mx = math.Max(mx, v)
			cnt++
		}
	}
	headline := ""
	if cnt > 0 {
		switch p.agg {
		case "sum":
			headline = "total " + fmtVal(sum, p.unit)
		case "max":
			headline = "max " + fmtVal(mx, p.unit)
		default:
			headline = "avg " + fmtVal(sum/float64(cnt), p.unit)
		}
	}
	right := headline
	if p.dcgm != "" && !m.apple() && len(headline)+len(p.dcgm)+len(p.title)+12 < w {
		right = headline + " · " + p.dcgm
	}

	// Legend: compact entries in the grid, a stats table when zoomed.
	var legend []string
	type entry struct {
		gpuIndex int
		text     string
	}
	var entries []entry
	for i, vals := range series {
		g := s.GPUs[i]
		cur := lastValue(vals)
		txt := fmtVal(cur, p.unit)
		if zoom {
			st := statsOf(vals)
			txt = fmt.Sprintf("%-22s now %9s   min %9s   avg %9s   max %9s", truncate(shortName(g.Device.Name), 22),
				fmtVal(cur, p.unit), fmtVal(st.min, p.unit), fmtVal(st.avg, p.unit), fmtVal(st.max, p.unit))
		}
		entries = append(entries, entry{g.Device.Index, txt})
	}
	renderEntry := func(e entry) string {
		mark := "■"
		label := fmt.Sprintf("GPU%d ", e.gpuIndex)
		style := m.seriesStyle(e.gpuIndex)
		if d.only >= 0 && d.only != e.gpuIndex {
			style, mark = th.Muted, "□"
		}
		out := style.Render(mark) + th.Text.Render(" "+label) + th.Dim.Render(e.text)
		gi := e.gpuIndex
		return m.zone(fmt.Sprintf("dash:legend:%d:%d", idx, gi), out, func(bool) tea.Cmd {
			if d.only == gi {
				d.only = -1
			} else {
				d.only = gi
			}
			return nil
		})
	}
	if zoom {
		for _, e := range entries {
			legend = append(legend, renderEntry(e))
		}
	} else {
		line, lineW := "", 0
		for _, e := range entries {
			r := renderEntry(e)
			rw := widgets.Width(r) + 2
			if lineW > 0 && lineW+rw > inner {
				legend = append(legend, line)
				line, lineW = "", 0
			}
			line += r + th.Base.Render("  ")
			lineW += rw
		}
		if line != "" {
			legend = append(legend, line)
		}
		if maxLegend := max(1, (h-2)/4); len(legend) > maxLegend {
			legend = legend[:maxLegend]
		}
	}

	chartH := h - 2 - 1 - len(legend)
	if chartH < 2 {
		legend = nil
		chartH = max(1, h-3)
	}
	var ss []widgets.Series
	for i, vals := range series {
		gi := s.GPUs[i].Device.Index
		if d.only >= 0 && gi != d.only {
			continue
		}
		ss = append(ss, widgets.Series{Values: vals, Style: m.seriesStyle(gi)})
	}
	// The scale is known only after drawing, so size the axis for the
	// widest label a 1000x range could need, then draw.
	_, lo, hi := widgets.MultiChart(th, ss, max(1, inner-8), chartH, 0, p.max)
	axisW := max(widgets.Width(fmtAxis(hi, p.unit)), widgets.Width(fmtAxis((hi+lo)/2, p.unit)), widgets.Width(fmtAxis(lo, p.unit))) + 1
	chart, _, _ := widgets.MultiChart(th, ss, max(1, inner-axisW), chartH, lo, hi)
	lines := make([]string, 0, h-2)
	for i, l := range chart {
		axis := ""
		switch i {
		case 0:
			axis = fmtAxis(hi, p.unit)
		case len(chart) - 1:
			axis = fmtAxis(lo, p.unit)
		case len(chart) / 2:
			if len(chart) >= 5 {
				axis = fmtAxis((hi+lo)/2, p.unit)
			}
		}
		lines = append(lines, th.Muted.Render(fmt.Sprintf("%*s ", axisW-1, axis))+l)
	}
	start, end := t0.Local().Format("15:04"), t1.Local().Format("15:04")
	lines = append(lines, widgets.Space(th, axisW)+th.Muted.Render(start)+widgets.Space(th, max(1, inner-axisW-len(start)-len(end)))+th.Muted.Render(end))
	lines = append(lines, legend...)

	box := widgets.Box(th, widgets.BoxOpts{Title: p.title, RightTitle: right, Focus: focus}, w, h, lines)
	if m.mouse && !zoom {
		i := idx
		for li, l := range box {
			box[li] = m.zone(fmt.Sprintf("dash:panel:%d", i), l, func(dbl bool) tea.Cmd {
				d.focus = i
				if dbl {
					d.zoom = true
				}
				return nil
			})
		}
	}
	return box
}

// reliabilityPanel is the DCGM dashboard's counter table.
func (m *Model) reliabilityPanel(w, h int, focus bool, idx int) widgets.Block {
	th := m.th
	s := m.view()
	now := m.now()
	xids := map[gpu.ID]int{}
	for _, e := range s.Events {
		if e.Kind == "xid" && e.Time.After(now.Add(-time.Hour)) {
			xids[e.DeviceID]++
		}
	}
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "GPU", Width: 3, Align: widgets.Right},
		{Title: "NAME", Width: 18, Min: 8, Flex: true, Priority: 3},
		{Title: "XID 1h", Width: 6, Align: widgets.Right},
		{Title: "ECC SBE", Width: 7, Align: widgets.Right},
		{Title: "ECC DBE", Width: 7, Align: widgets.Right},
		{Title: "REMAP C/U", Width: 9, Align: widgets.Right, Priority: 1},
		{Title: "REMAP FAIL", Width: 10, Align: widgets.Right, Priority: 4},
		{Title: "PCIe REPLAY", Width: 11, Align: widgets.Right, Priority: 2},
		{Title: "PWR VIOL", Width: 8, Align: widgets.Right, Priority: 5},
		{Title: "THRM VIOL", Width: 9, Align: widgets.Right, Priority: 5},
		{Title: "ENERGY", Width: 9, Align: widgets.Right, Priority: 6},
	}, SortCol: -1, Selected: -1}
	for i := range s.GPUs {
		g := &s.GPUs[i]
		c := g.Counters
		energy := na
		if g.Sample.EnergyJ.OK {
			energy = fmtEnergy(g.Sample.EnergyJ.V)
		}
		x := xids[g.Device.ID]
		remap := na
		if c.RemappedCorrectable.OK {
			remap = fmt.Sprintf("%d/%d", c.RemappedCorrectable.V, c.RemappedUncorrectable.V)
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.R(m.seriesStyle(g.Device.Index).Render(fmt.Sprint(g.Device.Index))),
			widgets.C(th.Text, shortName(g.Device.Name)),
			widgets.C(levelIf(th, x > 0, th.Warn), fmt.Sprint(x)),
			widgets.R(m.naOr(optU(c.ECCCorrectedVolatile), th.Text)),
			widgets.R(m.naOr(optU(c.ECCUncorrectedVolatile), nonZero(m, c.ECCUncorrectedVolatile.V))),
			widgets.R(m.naOr(remap, th.Text)),
			widgets.R(m.naOr(optBool(c.RemapFailure, "yes", "no"), levelIf(th, c.RemapFailure.V, th.Crit))),
			widgets.R(m.naOr(optU(c.PCIeReplays), th.Text)),
			widgets.R(m.naOr(durOpt(c.ViolationPower), th.Text)),
			widgets.R(m.naOr(durOpt(c.ViolationThermal), th.Text)),
			widgets.R(m.naOr(energy, th.Text)),
		})
	}
	box := widgets.Box(th, widgets.BoxOpts{Title: "Reliability", RightTitle: "ECC · row remapping · XID · PCIe · violations", Focus: focus}, w, h, tb.Render(th, w-2, h-2))
	if m.mouse {
		for li, l := range box {
			box[li] = m.zone("dash:reliability", l, func(bool) tea.Cmd { m.dash.focus = idx; return nil })
		}
	}
	return box
}
