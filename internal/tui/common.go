// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/riteshsonawane1372/gputop/internal/health"
	"github.com/riteshsonawane1372/gputop/internal/keymap"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
	"github.com/riteshsonawane1372/gputop/internal/tui/widgets"
)

// kv renders an aligned "label  value" line.
func (m *Model) kv(label, value string, lw int) string {
	return m.th.Dim.Render(label) + widgets.Space(m.th, max(1, lw-widgets.Width(label))) + value
}

// naOr renders N/A in the unavailable style, otherwise text in style.
func (m *Model) naOr(text string, st lipgloss.Style) string {
	if text == na {
		return m.th.NA.Render(na)
	}
	return st.Render(text)
}

func (m *Model) gpuName(g *model.GPU) string {
	return fmt.Sprintf("GPU %d", g.Device.Index)
}

func (m *Model) stateCell(g *model.GPU) string {
	th := m.th
	d := g.Derived
	switch d.State {
	case model.StateUnavailable:
		return th.Crit.Render("✖ DOWN")
	case model.StateBusy:
		if d.Throttled {
			return th.Warn.Render("● THRTL")
		}
		return th.OK.Render("● BUSY")
	case model.StateActive:
		if d.Throttled {
			return th.Warn.Render("◐ THRTL")
		}
		return th.Accent.Render("◐ ACTIVE")
	case model.StateIdle:
		if d.IdleAllocated {
			return th.Warn.Render("○ IDLE*")
		}
		return th.Muted.Render("○ IDLE")
	}
	return th.NA.Render("? N/A")
}

func (m *Model) healthStyle(score int) lipgloss.Style {
	switch health.BandFor(score) {
	case health.Healthy:
		return m.th.OK
	case health.Good:
		return m.th.Accent
	case health.Degraded:
		return m.th.Warn
	}
	return m.th.Crit
}

func (m *Model) sevStyle(s model.Severity) (lipgloss.Style, string) {
	switch s {
	case model.SevCritical:
		return m.th.Crit, "✖"
	case model.SevWarning:
		return m.th.Warn, "▲"
	}
	return m.th.Accent, "•"
}

// pctBar renders "▰bar▱ 94%" into w cells.
func (m *Model) pctBar(o metric.Opt[float64], w int) string {
	if !o.OK {
		return m.th.NA.Render(widgets.Fit(m.th, na, w))
	}
	label := fmt.Sprintf("%3.0f%%", o.V)
	bw := w - len(label) - 1
	if bw < 3 {
		return widgets.FitRight(m.th, m.th.Gradient(o.V/100).Render(label), w)
	}
	return widgets.Bar(m.th, o.V/100, bw) + m.th.Base.Render(" ") + m.th.Gradient(o.V/100).Render(label)
}

// fracBar renders a bar for a fraction with a free-form label.
func (m *Model) fracBar(frac float64, ok bool, label string, w int) string {
	if !ok {
		return widgets.Fit(m.th, m.th.NA.Render(na), w)
	}
	bw := w - widgets.Width(label) - 1
	if bw < 3 {
		return widgets.FitRight(m.th, m.th.Text.Render(label), w)
	}
	return widgets.Bar(m.th, frac, bw) + m.th.Base.Render(" ") + m.th.Text.Render(label)
}

func (m *Model) selected() (*model.GPU, int) {
	s := m.view()
	for i := range s.GPUs {
		if s.GPUs[i].Device.ID == m.selGPU {
			return &s.GPUs[i], i
		}
	}
	if len(s.GPUs) > 0 {
		m.selGPU = s.GPUs[0].Device.ID
		return &s.GPUs[0], 0
	}
	return nil, -1
}

func (m *Model) moveGPU(delta int) {
	s := m.view()
	if len(s.GPUs) == 0 {
		return
	}
	_, i := m.selected()
	i = widgets.Scroll(i, delta, len(s.GPUs))
	m.selGPU = s.GPUs[i].Device.ID
}

func keysGPUSelect(m *Model, a keymap.Action) (bool, tea.Cmd) {
	switch a {
	case keymap.Up, keymap.PrevGPU, keymap.Left:
		m.moveGPU(-1)
	case keymap.Down, keymap.NextGPU, keymap.Right:
		m.moveGPU(1)
	case keymap.Home:
		m.moveGPU(-math.MaxInt32)
	case keymap.End:
		m.moveGPU(math.MaxInt32)
	case keymap.Select:
		m.activeID = "gpus"
		m.gpus.detail = true
		return true, nil
	default:
		return false, nil
	}
	return true, nil
}

func hintsGPUSelect(m *Model) []hint {
	return []hint{{keymap.Up, "select GPU"}, {keymap.Select, "detail"}, {keymap.History, "history"}}
}

// wrap splits text into lines of at most w cells (by words).
func wrap(text string, w int) []string {
	if w <= 0 {
		return nil
	}
	var lines []string
	var cur strings.Builder
	for _, word := range strings.Fields(text) {
		if cur.Len() > 0 && cur.Len()+1+len(word) > w {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(word)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// matchesQuery applies a tab's search text and key:value filter to a
// record described by fields (lower-case keys).
func matchesQuery(q *query, fields map[string]string) bool {
	if q == nil {
		return true
	}
	if s := strings.ToLower(strings.TrimSpace(q.search)); s != "" {
		found := false
		for _, v := range fields {
			if strings.Contains(strings.ToLower(v), s) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, tok := range strings.Fields(q.filter) {
		k, v, ok := strings.Cut(tok, ":")
		v = strings.ToLower(v)
		if !ok {
			// Bare words behave like search terms.
			hit := false
			for _, fv := range fields {
				if strings.Contains(strings.ToLower(fv), strings.ToLower(tok)) {
					hit = true
					break
				}
			}
			if !hit {
				return false
			}
			continue
		}
		k = strings.ToLower(k)
		if k == "ns" {
			k = "namespace"
		}
		if k == "severity" {
			k = "sev"
		}
		fv, has := fields[k]
		if !has {
			return false
		}
		fv = strings.ToLower(fv)
		if k == "gpu" || k == "pid" || k == "mig" {
			if fv != v {
				return false
			}
			continue
		}
		if !strings.Contains(fv, v) {
			return false
		}
	}
	return true
}

// sections flows titled line groups into n columns of boxes.
type section struct {
	title string
	lines []string
}

func (m *Model) flowSections(secs []section, w, h, cols, scroll int, focusTitle string) (widgets.Block, int) {
	if cols < 1 {
		cols = 1
	}
	widths := widgets.Split(w, repeat(1, cols)...)
	colSecs := make([][]section, cols)
	heights := make([]int, cols)
	for _, s := range secs {
		best := 0
		for c := 1; c < cols; c++ {
			if heights[c] < heights[best] {
				best = c
			}
		}
		colSecs[best] = append(colSecs[best], s)
		heights[best] += len(s.lines) + 2
	}
	total := 0
	for _, hh := range heights {
		total = max(total, hh)
	}
	blocks := make([]widgets.Block, cols)
	for c := 0; c < cols; c++ {
		var col widgets.Block
		target := max(total, h)
		for i, s := range colSecs[c] {
			bh := len(s.lines) + 2
			if i == len(colSecs[c])-1 {
				bh = max(bh, target-len(col)) // stretch the last box to the bottom
			}
			col = append(col, widgets.Box(m.th, widgets.BoxOpts{Title: s.title, Focus: s.title == focusTitle}, widths[c], bh, s.lines)...)
		}
		blocks[c] = widgets.FitBlock(m.th, col, widths[c], max(total, h))
	}
	canvas := widgets.HJoin(m.th, blocks...)
	maxScroll := max(0, len(canvas)-h)
	scroll = max(0, min(scroll, maxScroll))
	end := min(len(canvas), scroll+h)
	return canvas[scroll:end], maxScroll
}

func repeat(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}
