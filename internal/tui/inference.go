// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/riteshsonawane1372/gputop/internal/inference"
	"github.com/riteshsonawane1372/gputop/internal/keymap"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/tui/widgets"
)

// The Inference tab shows serving metrics scraped from LLM inference
// servers: a table of servers and, for the selected one, its latency
// percentiles, load and live charts.

func inferenceVisible(m *Model) bool { return len(m.view().Inference) > 0 }

func (m *Model) filteredServers() []inference.Server {
	q := m.q("inference")
	var out []inference.Server
	for _, sv := range m.view().Inference {
		fields := map[string]string{"name": sv.Name, "engine": sv.Engine, "model": strings.Join(sv.Models, ","), "url": sv.URL, "origin": sv.Origin}
		if matchesQuery(q, fields) {
			out = append(out, sv)
		}
	}
	return out
}

func keysInference(m *Model, a keymap.Action) (bool, tea.Cmd) {
	return listKeys(&m.infSel, len(m.filteredServers()), 5, a), nil
}

func hintsInference(m *Model) []hint {
	return []hint{{keymap.Down, "server"}}
}

// fmtLatency renders seconds compactly: 850µs, 182ms, 1.24s.
func fmtLatency(o metric.Opt[float64]) string {
	if !o.OK || math.IsNaN(o.V) {
		return na
	}
	switch v := o.V; {
	case v < 0.001:
		return fmt.Sprintf("%.0fµs", v*1e6)
	case v < 0.1:
		return fmt.Sprintf("%.1fms", v*1e3)
	case v < 1:
		return fmt.Sprintf("%.0fms", v*1e3)
	case v < 100:
		return fmt.Sprintf("%.2fs", v)
	default:
		return fmt.Sprintf("%.0fs", v)
	}
}

// fmtCount renders a rate or count with k/M suffixes.
func fmtCount(o metric.Opt[float64]) string {
	if !o.OK {
		return na
	}
	switch v := o.V; {
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e4:
		return fmt.Sprintf("%.0fk", v/1e3)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	case v >= 10 || v == math.Trunc(v):
		return fmt.Sprintf("%.0f", v)
	default:
		return fmt.Sprintf("%.1f", v)
	}
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, x := range v {
		s[i] = fmt.Sprint(x)
	}
	return strings.Join(s, ",")
}

// kvCell renders KV-cache usage as a bar.
func (m *Model) kvCell(o metric.Opt[float64], w int) string {
	if !o.OK {
		return m.th.NA.Render(na)
	}
	return m.fracBar(o.V, true, fmt.Sprintf("%.0f%%", o.V*100), w)
}

func viewInference(m *Model, w, h int) widgets.Block {
	th := m.th
	servers := m.filteredServers()
	m.infSel = widgets.Scroll(m.infSel, 0, len(servers))

	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "SERVER", Width: 26, Min: 10, Flex: true},
		{Title: "ENGINE", Width: 9, Priority: 3},
		{Title: "GPU", Width: 5, Priority: 4},
		{Title: "STATE", Width: 6},
		{Title: "RUN", Width: 4, Align: widgets.Right, Priority: 2},
		{Title: "WAIT", Width: 4, Align: widgets.Right},
		{Title: "KV CACHE", Width: 12, Min: 5, Priority: 1},
		{Title: "TTFT p50", Width: 8, Align: widgets.Right},
		{Title: "TTFT p99", Width: 8, Align: widgets.Right},
		{Title: "ITL p50", Width: 7, Align: widgets.Right, Priority: 1},
		{Title: "E2E p50", Width: 7, Align: widgets.Right, Priority: 2},
		{Title: "REQ/S", Width: 6, Align: widgets.Right, Priority: 3},
		{Title: "TOK/S", Width: 7, Align: widgets.Right},
	}, Selected: m.infSel, SortCol: -1, RowMark: m.listRowMark("inference:", &m.infSel)}
	widths := tb.Layout(w - 2)
	up := 0
	for _, sv := range servers {
		mt := sv.Metrics
		state := th.OK.Render("● up")
		if sv.Up {
			up++
		} else {
			state = th.Crit.Render("✖ down")
		}
		wait := th.Text
		if mt.Waiting.OK && mt.Waiting.V > 0 {
			wait = th.Warn
		}
		engine := sv.Engine
		if engine == "" {
			engine = "?"
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Text, sv.Name), widgets.C(th.Dim, engine), widgets.C(th.Accent, orDash(joinInts(sv.GPUs))), widgets.R(state),
			widgets.R(m.naOr(fmtCount(mt.Running), th.Text)), widgets.R(m.naOr(fmtCount(mt.Waiting), wait)),
			widgets.R(m.kvCell(mt.KVCacheUsage, widths[6])),
			widgets.R(m.naOr(fmtLatency(mt.TTFT.P50), th.Text)), widgets.R(m.naOr(fmtLatency(mt.TTFT.P99), th.Text)),
			widgets.R(m.naOr(fmtLatency(mt.ITL.P50), th.Text)), widgets.R(m.naOr(fmtLatency(mt.E2E.P50), th.Text)),
			widgets.R(m.naOr(fmtCount(mt.RequestsPerSec), th.Text)), widgets.R(m.naOr(fmtCount(mt.GenTokensPerSec), th.Primary)),
		})
	}
	tableH := min(max(5, len(servers)+3), max(5, h/3))
	if len(servers) == 0 || h < 16 {
		tableH = h
	}
	right := fmt.Sprintf("%d/%d up", up, len(servers))
	out := widgets.Box(th, widgets.BoxOpts{Title: "Inference servers", RightTitle: right, Focus: true}, w, tableH, tb.Render(th, w-2, tableH-2))
	if tableH >= h || len(servers) == 0 {
		return out
	}
	return widgets.VJoin(out, m.serverDetail(servers[m.infSel], w, h-tableH))
}

func (m *Model) serverDetail(sv inference.Server, w, h int) widgets.Block {
	th := m.th
	mt := sv.Metrics
	lw := 12
	win := "window filling"
	if mt.Window > 0 {
		win = "last " + fmtDurationShortAny(mt.Window.Round(1e9))
	}

	var info []string
	info = append(info, m.kv("URL", th.Text.Render(sv.URL), lw))
	if len(sv.Models) > 0 {
		info = append(info, m.kv("Model", th.Bold.Render(strings.Join(sv.Models, ", ")), lw))
	}
	origin := sv.Origin
	if sv.PID > 0 {
		origin += fmt.Sprintf(" · pid %d", sv.PID)
	}
	if len(sv.GPUs) > 0 {
		origin += " · GPU " + joinInts(sv.GPUs)
	}
	info = append(info, m.kv("Source", th.Dim.Render(origin), lw))
	if !sv.Up {
		msg := sv.Error
		if msg == "" {
			msg = "not scraped yet"
		}
		info = append(info, m.kv("Error", th.Crit.Render(msg), lw))
		if sv.Origin == inference.OriginDiscovered {
			info = append(info, th.Dim.Render("SGLang needs --enable-metrics and llama-server --metrics to serve /metrics;"),
				th.Dim.Render("configure inference.endpoints for servers on other hosts or ports."))
		}
		return widgets.Box(th, widgets.BoxOpts{Title: sv.Name}, w, h, info)
	}

	barW := max(10, min(30, w/2-lw-8))
	load := []string{
		m.kv("Requests", th.Text.Render(fmtCount(mt.Running))+th.Dim.Render(" running · ")+
			m.naOr(fmtCount(mt.Waiting), th.Level(mt.Waiting.V, 1, 20))+th.Dim.Render(" waiting"), lw),
		m.kv("KV cache", m.kvCell(mt.KVCacheUsage, barW+6), lw),
		m.kv("Prefix hit", m.naOr(optPct(scalePct(mt.PrefixCacheHitRate)), th.Text), lw),
		m.kv("Throughput", m.naOr(fmtCount(mt.RequestsPerSec), th.Text)+th.Dim.Render(" req/s · ")+
			m.naOr(fmtCount(mt.GenTokensPerSec), th.Primary)+th.Dim.Render(" gen tok/s · ")+
			m.naOr(fmtCount(mt.PromptTokensPerSec), th.Text)+th.Dim.Render(" prompt tok/s"), lw),
	}
	if mt.PreemptionsPerSec.OK {
		st := th.Text
		if mt.PreemptionsPerSec.V > 0 {
			st = th.Warn
		}
		load = append(load, m.kv("Preemptions", st.Render(fmt.Sprintf("%.2f/s", mt.PreemptionsPerSec.V)), lw))
	}

	lat := &widgets.Table{Columns: []widgets.Column{
		{Title: "", Width: 16, Min: 5},
		{Title: "MEAN", Width: 8, Align: widgets.Right},
		{Title: "p50", Width: 8, Align: widgets.Right},
		{Title: "p90", Width: 8, Align: widgets.Right},
		{Title: "p99", Width: 8, Align: widgets.Right},
		{Title: "COUNT", Width: 7, Align: widgets.Right, Priority: 1},
	}, Selected: -1, SortCol: -1}
	for _, r := range []struct {
		name string
		l    inference.Latency
	}{
		{"Time to 1st tok", mt.TTFT}, {"Inter-token", mt.ITL}, {"End-to-end", mt.E2E}, {"Queue", mt.Queue},
	} {
		cnt := na
		if r.l.Count > 0 {
			cnt = fmtCount(metric.Some(r.l.Count))
		}
		lat.Rows = append(lat.Rows, []widgets.Cell{
			widgets.C(th.Text, r.name),
			widgets.R(m.naOr(fmtLatency(r.l.Mean), th.Text)), widgets.R(m.naOr(fmtLatency(r.l.P50), th.Text)),
			widgets.R(m.naOr(fmtLatency(r.l.P90), th.Text)), widgets.R(m.naOr(fmtLatency(r.l.P99), th.Warn)),
			widgets.R(m.naOr(cnt, th.Dim)),
		})
	}

	ws := widgets.Split(w, 1, 1)
	key := "inf/" + sv.URL + "/"
	latLines := lat.Render(th, ws[0]-4, len(lat.Rows)+1)
	load = append(load, info...)
	leftSecs := []section{{"Latency · " + win, latLines}, {"Load", load}}
	leftH := min(h, len(latLines)+len(load)+4)
	left, _ := m.flowSections(leftSecs, ws[0], leftH, 1, 0, "")
	if reqH := h - leftH; reqH >= 5 {
		run, wait := m.live.values(key+"run", 0), m.live.values(key+"wait", 0)
		reqLines, _, _ := widgets.MultiChart(th, []widgets.Series{
			{Values: run, Style: th.Primary}, {Values: wait, Style: th.Warn},
		}, ws[0]-2, reqH-2, 0, 0)
		title := "Requests " + th.Primary.Render("running "+fmtCount(mt.Running)) + th.Dim.Render(" · ") + th.Warn.Render("waiting "+fmtCount(mt.Waiting))
		left = widgets.VJoin(left, widgets.Box(th, widgets.BoxOpts{Title: title}, ws[0], reqH, reqLines))
	} else {
		left = widgets.FitBlock(th, left, ws[0], h)
	}

	// Charts share the remaining height.
	ch := max(4, h/3)
	cw := ws[1] - 2
	ttft50, ttft99 := m.live.values(key+"ttft50", 0), m.live.values(key+"ttft99", 0)
	ttftLines, _, hi := widgets.MultiChart(th, []widgets.Series{
		{Values: ttft99, Style: th.Warn}, {Values: ttft50, Style: th.Primary},
	}, cw, ch-2, 0, 0)
	ttftTitle := "TTFT " + th.Primary.Render("p50 "+fmtLatency(mt.TTFT.P50)) + th.Dim.Render(" · ") + th.Warn.Render("p99 "+fmtLatency(mt.TTFT.P99))
	ttftBox := widgets.Box(th, widgets.BoxOpts{Title: ttftTitle, RightTitle: "max " + fmtLatency(metric.Some(hi))}, ws[1], ch, ttftLines)

	gen := m.live.values(key+"gen", 0)
	genBox := widgets.Box(th, widgets.BoxOpts{Title: "Generation · " + fmtCount(mt.GenTokensPerSec) + " tok/s"}, ws[1], ch,
		widgets.Chart(th, gen, cw, ch-2, widgets.ChartOpts{Cursor: -1}))

	restH := h - 2*ch
	kv := m.live.values(key+"kv", 0)
	kvTitle := "KV cache"
	if mt.KVCacheUsage.OK {
		kvTitle += fmt.Sprintf(" · %.0f%%", mt.KVCacheUsage.V*100)
	}
	kvBox := widgets.Box(th, widgets.BoxOpts{Title: kvTitle, RightTitle: "waiting " + fmtCount(mt.Waiting)}, ws[1], restH,
		widgets.Chart(th, kv, cw, restH-2, widgets.ChartOpts{Min: 0, Max: 100, Cursor: -1}))
	right := widgets.VJoin(ttftBox, genBox, kvBox)
	return widgets.HJoin(th, left, right)
}

func scalePct(o metric.Opt[float64]) metric.Opt[float64] {
	if o.OK {
		o.V *= 100
	}
	return o
}

// servingLine summarizes inference servers for the Overview fleet box:
// the worst TTFT p99 and ITL p50 and the total generation throughput.
func (m *Model) servingLine(servers []inference.Server) string {
	th := m.th
	up := 0
	var ttft99, itl50, gen, waiting metric.Opt[float64]
	worst := func(o *metric.Opt[float64], v metric.Opt[float64]) {
		if v.OK && (!o.OK || v.V > o.V) {
			*o = v
		}
	}
	add := func(o *metric.Opt[float64], v metric.Opt[float64]) {
		if v.OK {
			*o = metric.Some(o.V + v.V)
		}
	}
	for _, sv := range servers {
		if !sv.Up {
			continue
		}
		up++
		mt := sv.Metrics
		worst(&ttft99, mt.TTFT.P99)
		worst(&itl50, mt.ITL.P50)
		add(&gen, mt.GenTokensPerSec)
		add(&waiting, mt.Waiting)
	}
	out := th.Text.Render(fmt.Sprintf("%d/%d up", up, len(servers)))
	if up == 0 {
		return out
	}
	out += th.Dim.Render(" · TTFT p99 ") + m.naOr(fmtLatency(ttft99), th.Text) +
		th.Dim.Render(" · ITL p50 ") + m.naOr(fmtLatency(itl50), th.Text) +
		th.Dim.Render(" · ") + m.naOr(fmtCount(gen), th.Primary) + th.Dim.Render(" tok/s")
	if waiting.OK && waiting.V > 0 {
		out += th.Dim.Render(" · ") + th.Warn.Render(fmtCount(waiting)+" waiting")
	}
	return out
}
