// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

var windows = []time.Duration{time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}

type histState struct {
	metric  int
	window  int
	cursor  int // points back from the newest (0 = now)
	host    bool
	res     *history.Result
	err     error
	loading bool
	stale   bool
	lastKey string
	lastAt  time.Time
}

var histGPUMetrics = []history.Metric{
	history.Util, history.VRAMPercent, history.PowerW, history.TempC, history.MemTempC, history.MemBandwidth,
	history.ClockCoreMHz, history.PCIeRxMBps, history.PCIeTxMBps, history.NVLinkRxMBps, history.NVLinkTxMBps,
	history.ProcVRAMGiB, history.ProcCount, history.HealthScore,
}

var histHostMetrics = []history.Metric{
	history.HostCPU, history.HostMemPercent, history.HostNetRxMBps, history.HostNetTxMBps,
	history.HostDiskReadMBps, history.HostDiskWriteMBps, history.HostLoad1,
}

func (h *histState) metrics() []history.Metric {
	if h.host {
		return histHostMetrics
	}
	return histGPUMetrics
}

func (h *histState) current() history.Metric {
	ms := h.metrics()
	return ms[(h.metric%len(ms)+len(ms))%len(ms)]
}

func (h *histState) queryKey() string { return fmt.Sprintf("%d", h.window) }

func (m *Model) maxWindow() int {
	s := m.view()
	ret := s.History.Retention
	idx := 0
	for i, w := range windows {
		if w <= ret || i == 0 {
			idx = i
		}
	}
	return idx
}

// maybeQueryHistory issues an async query when data is stale.
func (m *Model) maybeQueryHistory() tea.Cmd {
	h := &m.hist
	reader := m.src.History()
	if reader == nil || h.loading {
		return nil
	}
	s := m.view()
	res := s.History.Resolution
	if res <= 0 {
		res = 5 * time.Second
	}
	key := h.queryKey()
	if !h.stale && key == h.lastKey && m.now().Sub(h.lastAt) < res && h.res != nil {
		return nil
	}
	if m.paused && h.res != nil && !h.stale {
		return nil
	}
	h.loading, h.stale, h.lastKey, h.lastAt = true, false, key, m.now()
	window := windows[h.window]
	width := max(60, m.width*2)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		r, err := reader.Query(ctx, history.Query{Since: time.Now().Add(-window), MaxPoints: width})
		return historyMsg{key: key, res: r, err: err}
	}
}

func keysHistory(m *Model, a keymap.Action) (bool, tea.Cmd) {
	h := &m.hist
	n := 0
	if h.res != nil {
		n = len(h.res.Times)
	}
	switch a {
	case keymap.NextMetric, keymap.Down:
		h.metric = (h.metric + 1) % len(h.metrics())
	case keymap.PrevMetric, keymap.Up:
		h.metric = (h.metric - 1 + len(h.metrics())) % len(h.metrics())
	case keymap.NextGPU, keymap.Right:
		m.histMoveSeries(1)
	case keymap.PrevGPU, keymap.Left:
		m.histMoveSeries(-1)
	case keymap.ZoomIn:
		if h.window > 0 {
			h.window--
			h.cursor, h.stale = 0, true
		}
	case keymap.ZoomOut:
		if h.window < m.maxWindow() {
			h.window++
			h.cursor, h.stale = 0, true
		} else {
			m.setToast("window limited by history.retention ("+fmtDuration(m.view().History.Retention)+")", 2*time.Second)
		}
	case keymap.ScrubBack:
		h.cursor = min(max(0, n-1), h.cursor+max(1, n/60))
	case keymap.ScrubFwd:
		h.cursor = max(0, h.cursor-max(1, n/60))
	case keymap.ScrubNow, keymap.Home:
		h.cursor = 0
	case keymap.End:
		h.cursor = max(0, n-1)
	default:
		return false, nil
	}
	return true, m.maybeQueryHistory()
}

// histMoveSeries cycles GPU 0..N-1 then the host series.
func (m *Model) histMoveSeries(delta int) {
	s := m.view()
	h := &m.hist
	n := len(s.GPUs)
	_, idx := m.selected()
	pos := idx
	if h.host || n == 0 {
		pos = n
	}
	pos = (pos + delta + n + 1) % (n + 1)
	if pos == n {
		h.host = true
	} else {
		h.host = false
		m.selGPU = s.GPUs[pos].Device.ID
	}
	h.metric = 0
}

func hintsHistory(m *Model) []hint {
	return []hint{{keymap.NextGPU, "GPU/host"}, {keymap.NextMetric, "metric"}, {keymap.ZoomOut, "zoom"}, {keymap.ScrubBack, "scrub"}, {keymap.ScrubNow, "now"}}
}

func viewHistory(m *Model, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	hs := &m.hist
	if m.src.History() == nil || !s.History.Enabled {
		lines := []string{"", th.Warn.Render("History is disabled."),
			th.Dim.Render("Enable it with history.enabled: true in the config (or drop --no-history)."),
			th.Dim.Render("Live charts for the last few minutes remain available in the Overview and GPU detail views.")}
		return widgets.Box(th, widgets.BoxOpts{Title: "History"}, w, h, indent(lines, "  "))
	}
	if hs.window > m.maxWindow() {
		hs.window = m.maxWindow()
	}
	res := hs.res
	key := history.HostKey
	label := "Host"
	var g *model.GPU
	if !hs.host {
		if g, _ = m.selected(); g != nil {
			key = string(g.Device.ID)
			label = fmt.Sprintf("GPU %d · %s", g.Device.Index, shortName(g.Device.Name))
		} else {
			hs.host = true
		}
	}
	metric := hs.current()
	info := history.Describe(metric)

	// Controls line.
	var tabs []string
	for i, win := range windows[:m.maxWindow()+1] {
		lbl := fmtDurationShort(win)
		if i == hs.window {
			tabs = append(tabs, th.TabActive.Render(" "+lbl+" "))
		} else {
			tabs = append(tabs, th.Dim.Render(" "+lbl+" "))
		}
	}
	controls := th.Title.Render(label) + th.Dim.Render("  ▸ ") + th.Primary.Render(info.Label) + th.Dim.Render("  window ") + strings.Join(tabs, "")
	if hs.loading && res == nil {
		controls += th.Muted.Render("  loading…")
	}
	if hs.err != nil {
		controls += th.Crit.Render("  " + hs.err.Error())
	}

	var values []float64
	var times []int64
	if res != nil {
		times = res.Times
		col := res.Column(key, metric)
		values = make([]float64, len(times))
		for i := range values {
			if i < len(col) {
				values[i] = float64(col[i])
			} else {
				values[i] = math.NaN()
			}
		}
	}
	n := len(values)
	hs.cursor = min(hs.cursor, max(0, n-1))
	cursorIdx := n - 1 - hs.cursor
	chartCursor := cursorIdx
	if hs.cursor == 0 {
		chartCursor = -1 // "now" needs no marker
	}

	// Events within the window mark the chart baseline.
	markers := map[int]lipgloss.Style{}
	var windowEvents []model.Event
	if n > 0 {
		t0, t1 := times[0], times[n-1]
		for _, e := range s.Events {
			ms := e.Time.UnixMilli()
			if ms < t0 || ms > t1 || (e.Severity == model.SevInfo && strings.HasPrefix(e.Kind, "process_")) {
				continue
			}
			if !hs.host && g != nil && e.DeviceID != "" && e.DeviceID != g.Device.ID {
				continue
			}
			idx := nearestIdx(times, ms)
			st, _ := m.sevStyle(e.Severity)
			if prev, ok := markers[idx]; !ok || e.Severity != model.SevInfo || prev.GetForeground() == th.Accent.GetForeground() {
				markers[idx] = st
			}
			windowEvents = append(windowEvents, e)
		}
	}

	compareH := 0
	if !hs.host && len(s.GPUs) > 1 {
		compareH = min(len(s.GPUs)+2, h/3)
	}
	detailH := min(12, max(7, h/4))
	chartH := h - 1 - compareH - detailH
	if chartH < 5 {
		detailH = max(0, detailH-(5-chartH))
		chartH = h - 1 - compareH - detailH
	}

	// Main chart.
	cw := w - 2 - 8
	stats := statsOf(values)
	chartLines := []string{}
	top := info.Max
	if top <= 0 {
		top = stats.max
	}
	body := widgets.Chart(th, values, cw, max(1, chartH-3), widgets.ChartOpts{Min: 0, Max: info.Max, Cursor: chartCursor, Markers: markers})
	for i, l := range body {
		axis := ""
		switch i {
		case 0:
			axis = fmtAxis(top, info.Unit)
		case len(body) - 1:
			axis = fmtAxis(0, info.Unit)
		case len(body) / 2:
			axis = fmtAxis(top/2, info.Unit)
		}
		chartLines = append(chartLines, th.Muted.Render(fmt.Sprintf("%7s ", axis))+l)
	}
	if n > 0 {
		start := time.UnixMilli(times[0]).Local().Format("15:04:05")
		end := time.UnixMilli(times[n-1]).Local().Format("15:04:05")
		chartLines = append(chartLines, th.Muted.Render(widgets.Space(th, 8)+start)+widgets.Space(th, max(1, cw-16))+th.Muted.Render(end))
	}
	statLine := th.Dim.Render("min ") + th.Text.Render(fmtVal(stats.min, info.Unit)) +
		th.Dim.Render("  avg ") + th.Text.Render(fmtVal(stats.avg, info.Unit)) +
		th.Dim.Render("  max ") + th.Text.Render(fmtVal(stats.max, info.Unit))
	if n > 0 {
		statLine += th.Dim.Render("  now ") + th.Primary.Render(fmtVal(values[n-1], info.Unit))
	}
	chartLines = append([]string{statLine}, chartLines...)
	right := fmt.Sprintf("%d points · %s resolution", n, fmtDuration(s.History.Resolution))
	chart := widgets.Box(th, widgets.BoxOpts{Title: info.Label, RightTitle: right, Focus: true}, w, chartH, chartLines)

	out := widgets.Block{widgets.Fit(th, " "+controls, w)}
	out = append(out, chart...)

	if compareH > 0 && res != nil {
		out = append(out, m.historyCompare(res, metric, cursorIdx, w, compareH)...)
	}
	if detailH > 0 {
		out = append(out, m.historyCursor(res, key, g, cursorIdx, windowEvents, w, detailH)...)
	}
	return out
}

func (m *Model) historyCompare(res *history.Result, metric history.Metric, cursor, w, h int) widgets.Block {
	th := m.th
	s := m.view()
	info := history.Describe(metric)
	var lines []string
	sparkW := max(10, w-2-32)
	for _, g := range s.GPUs {
		col := res.Column(string(g.Device.ID), metric)
		vals := make([]float64, len(col))
		for i, v := range col {
			vals[i] = float64(v)
		}
		st := statsOf(vals)
		at := math.NaN()
		if cursor >= 0 && cursor < len(vals) {
			at = vals[cursor]
		}
		name := fmt.Sprintf("GPU %-2d", g.Device.Index)
		nameSt := th.Dim
		if g.Device.ID == m.selGPU {
			nameSt = th.Primary.Bold(true)
		}
		flag := " "
		if g.Derived.Outlier {
			flag = th.Warn.Render("⚠")
		}
		lines = append(lines, nameSt.Render(name)+flag+" "+widgets.Sparkline(th, vals, sparkW, info.Max)+
			th.Text.Render(fmt.Sprintf(" %8s", fmtVal(at, info.Unit)))+th.Dim.Render(fmt.Sprintf(" avg %7s", fmtVal(st.avg, info.Unit))))
	}
	return widgets.Box(th, widgets.BoxOpts{Title: "Compare GPUs · " + info.Label, RightTitle: "value at cursor"}, w, h, lines)
}

func (m *Model) historyCursor(res *history.Result, key string, g *model.GPU, cursor int, evs []model.Event, w, h int) widgets.Block {
	th := m.th
	if res == nil || cursor < 0 || cursor >= len(res.Times) {
		return widgets.Box(th, widgets.BoxOpts{Title: "At cursor"}, w, h, []string{th.Dim.Render("No history in this window yet. Data is recorded every resolution interval.")})
	}
	at := time.UnixMilli(res.Times[cursor])
	ago := m.now().Sub(at)
	title := "At " + at.Local().Format("15:04:05")
	if m.hist.cursor == 0 {
		title += " (latest)"
	} else {
		title += " (" + fmtDuration(ago) + " ago)"
	}
	ws := widgets.Split(w, 1, 1)
	var vals []string
	metrics := histGPUMetrics
	if key == history.HostKey {
		metrics = histHostMetrics
	}
	var cells []string
	for _, mt := range metrics {
		col := res.Column(key, mt)
		if cursor >= len(col) {
			continue
		}
		v := float64(col[cursor])
		info := history.Describe(mt)
		cells = append(cells, m.kv(shortLabel(info.Label), m.naOr(fmtVal(v, info.Unit), th.Text), 12))
	}
	if key != history.HostKey {
		if col := res.Column(key, history.ThrottleMask); cursor < len(col) && !math.IsNaN(float64(col[cursor])) {
			mask := gpu.ThrottleReasons(uint32(col[cursor])) &^ gpu.ThrottleIdle
			st := th.OK
			if mask&gpu.ThrottlePerformance != 0 {
				st = th.Warn
			}
			cells = append(cells, m.kv("Throttle", st.Render(mask.String()), 12))
		}
	}
	half := (len(cells) + 1) / 2
	inner := ws[0] - 2
	colW := inner / 2
	for i := 0; i < half; i++ {
		l := widgets.Fit(th, cells[i], colW)
		if i+half < len(cells) {
			l += widgets.Fit(th, cells[i+half], inner-colW)
		}
		vals = append(vals, l)
	}
	left := widgets.Box(th, widgets.BoxOpts{Title: title}, ws[0], h, vals)

	// Events near the cursor (± 2 resolution buckets), otherwise the window's latest.
	res2 := res.Resolution
	var near []string
	for i := len(evs) - 1; i >= 0; i-- {
		d := evs[i].Time.Sub(at)
		if d < 0 {
			d = -d
		}
		if d <= 2*res2+time.Second {
			near = append(near, m.eventLine(evs[i], ws[1]-2, false))
		}
	}
	title2 := "Events near cursor"
	if len(near) == 0 {
		title2 = "Events in window"
		for i := len(evs) - 1; i >= 0 && len(near) < h-2; i-- {
			near = append(near, m.eventLine(evs[i], ws[1]-2, false))
		}
	}
	if len(near) == 0 {
		near = []string{th.Dim.Render("none")}
	}
	right := widgets.Box(th, widgets.BoxOpts{Title: title2}, ws[1], h, near)
	return widgets.HJoin(th, left, right)
}

func shortLabel(s string) string {
	r := strings.NewReplacer("GPU utilization", "Util", "Memory bandwidth util", "Mem bandw.", "VRAM used", "VRAM",
		"Power draw", "Power", "GPU temperature", "Temp", "Memory temperature", "Mem temp", "Core clock", "Clock",
		"Process VRAM", "Proc VRAM", "GPU processes", "Processes", "Health score", "Health", "Load average (1m)", "Load 1m",
		"Host memory", "Memory", "Host CPU", "CPU")
	return r.Replace(s)
}

type seriesStats struct{ min, max, avg float64 }

func statsOf(v []float64) seriesStats {
	st := seriesStats{min: math.NaN(), max: math.NaN(), avg: math.NaN()}
	var sum float64
	n := 0
	for _, x := range v {
		if math.IsNaN(x) {
			continue
		}
		if n == 0 || x < st.min {
			st.min = x
		}
		if n == 0 || x > st.max {
			st.max = x
		}
		sum += x
		n++
	}
	if n > 0 {
		st.avg = sum / float64(n)
	}
	return st
}

func nearestIdx(times []int64, ms int64) int {
	lo, hi := 0, len(times)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if times[mid] < ms {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo > 0 && ms-times[lo-1] < times[lo]-ms {
		return lo - 1
	}
	return lo
}

func fmtVal(v float64, unit string) string {
	if math.IsNaN(v) {
		return na
	}
	switch unit {
	case "%":
		return fmt.Sprintf("%.0f%%", v)
	case "°C":
		return fmt.Sprintf("%.0f°C", v)
	case "W":
		return fmt.Sprintf("%.0f W", v)
	case "MHz":
		return fmt.Sprintf("%.0f MHz", v)
	case "GiB":
		return fmt.Sprintf("%.1f GiB", v)
	case "MB/s":
		return fmtRate(v * (1 << 20))
	case "B/s":
		return fmtRate(v)
	case "":
		if v == math.Trunc(v) {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%.2f", v)
	}
	return fmt.Sprintf("%.1f %s", v, unit)
}

func fmtAxis(v float64, unit string) string {
	if math.IsNaN(v) {
		return ""
	}
	if unit == "MB/s" {
		return fmtRate(v * (1 << 20))
	}
	return fmtVal(v, unit)
}

func fmtDurationShort(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
}
