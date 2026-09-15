// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

type hint struct {
	action keymap.Action
	label  string
}

func (m *Model) viewHeader(w int) string {
	th := m.th
	s := m.view()
	left := th.Surface.Bold(true).Foreground(th.Primary.GetForeground()).Render(" ◆ gputop ")
	host := s.Node.Hostname
	if host == "" && s.Host != nil {
		host = s.Host.Info.Hostname
	}
	if host != "" {
		left += th.Surface.Render("▸ " + host + " ")
	}
	if s.Node.Demo {
		left += th.TabActive.Background(th.Warn.GetForeground()).Render(" DEMO · SIMULATED ") + th.Surface.Render(" ")
	}
	if m.remote != "" {
		left += th.TabActive.Background(th.Accent.GetForeground()).Render(" REMOTE "+m.remote+" ") + th.Surface.Render(" ")
	}

	var mid []string
	for _, p := range s.Providers {
		if !p.Available {
			continue
		}
		if p.System.DriverVersion != "" {
			mid = append(mid, "driver "+p.System.DriverVersion)
		}
		if p.System.RuntimeVersion != "" {
			mid = append(mid, p.System.RuntimeName+" "+p.System.RuntimeVersion)
		}
	}
	if len(s.GPUs) > 0 {
		counts := map[string]int{}
		var names []string
		for _, g := range s.GPUs {
			n := shortName(g.Device.Name)
			if counts[n] == 0 {
				names = append(names, n)
			}
			counts[n]++
		}
		var parts []string
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("%d× %s", counts[n], n))
		}
		mid = append(mid, strings.Join(parts, ", "))
	}

	right := ""
	if m.paused {
		right += th.TabActive.Background(th.Warn.GetForeground()).Render(" ⏸ PAUSED ") + th.Surface.Render(" ")
	}
	if !s.Time.IsZero() {
		age := m.now().Sub(s.Time)
		stale := ""
		if !m.paused && m.refresh > 0 && age > 3*m.refresh+time.Second {
			stale = th.Warn.Render(fmt.Sprintf(" stale %s ", fmtDuration(age)))
		}
		right += stale + th.Surface.Render(fmt.Sprintf("⟳ %s  %s ", fmtDuration(m.refresh), s.Time.Format("15:04:05")))
	}

	midStr := th.Surface.Render(strings.Join(mid, " · "))
	space := w - widgets.Width(left) - widgets.Width(right)
	if space < 4 {
		return widgets.Fit(th, left+right, w)
	}
	mw := widgets.Width(midStr)
	if mw > space-2 {
		midStr = widgets.Fit(th, midStr, space-2)
		mw = widgets.Width(midStr)
	}
	pad := space - mw
	return left + th.Surface.Render(strings.Repeat(" ", pad/2)) + midStr + th.Surface.Render(strings.Repeat(" ", pad-pad/2)) + right
}

func (m *Model) viewTabBar(w int) string {
	th := m.th
	vis := m.visibleTabs()
	active := m.activeTab()
	labels := make([]string, len(vis))
	activeIdx := 0
	for i, t := range vis {
		num := ""
		if i < 9 {
			num = fmt.Sprintf("%d ", i+1)
		}
		label := " " + num + t.title + " "
		if t == active {
			labels[i] = th.TabActive.Render(label)
			activeIdx = i
		} else {
			labels[i] = th.TabInactive.Render(" ") + th.Key.Render(num) + th.TabInactive.Render(t.title+" ")
		}
		id := t.id
		labels[i] = m.zone("tab:"+id, labels[i], func(bool) tea.Cmd {
			if m.activeID == id {
				return nil
			}
			m.activeID = id
			m.onEnterTab()
			return m.enterCmd()
		})
	}
	// Fit a window of tabs around the active one.
	total := 0
	for _, l := range labels {
		total += widgets.Width(l)
	}
	lo, hi := 0, len(labels)
	for total > w-4 && hi-lo > 1 {
		if activeIdx-lo > hi-1-activeIdx {
			total -= widgets.Width(labels[lo])
			lo++
		} else {
			hi--
			total -= widgets.Width(labels[hi])
		}
	}
	var b strings.Builder
	if lo > 0 {
		b.WriteString(th.Muted.Render("‹ "))
	}
	for _, l := range labels[lo:hi] {
		b.WriteString(l)
	}
	if hi < len(labels) {
		b.WriteString(th.Muted.Render(" ›"))
	}
	line := b.String()
	if pad := w - widgets.Width(line); pad > 0 {
		line += th.Surface.Render(strings.Repeat(" ", pad))
	}
	return widgets.Fit(th, line, w)
}

func (m *Model) viewFooter(w int) string {
	th := m.th
	if m.input != nil {
		prompt := "/"
		help := "  enter apply · esc cancel · ctrl+u clear"
		if m.input.mode == "filter" {
			prompt = "filter "
			help = "  key:value (gpu: user: pod: ns: name: kind: sev:) · enter apply · esc cancel"
		}
		line := th.Key.Render(" "+prompt) + th.Surface.Foreground(th.Text.GetForeground()).Render(m.input.value) +
			th.TabActive.Render(" ") + th.Surface.Render(help)
		return widgets.Fit(th, line+th.Surface.Render(strings.Repeat(" ", w)), w)
	}

	var parts []string
	add := func(a keymap.Action, label string) {
		k := m.keys.Label(a)
		if k == "" {
			return
		}
		part := th.Key.Render(k) + th.Surface.Render(" "+label)
		// Hints run their action when clicked; quitting and plain
		// movement stay keyboard-only.
		if a != keymap.Quit && a != keymap.Up && a != keymap.Down {
			part = m.zone("hint:"+string(a), part, func(bool) tea.Cmd { return m.dispatch(a, "") })
		}
		parts = append(parts, part)
	}
	t := m.activeTab()
	if !m.help && t.hints != nil {
		for _, h := range t.hints(m) {
			add(h.action, h.label)
		}
	}
	if !m.help && m.keys.Lookup("left") == keymap.PrevTab && m.keys.Lookup("right") == keymap.NextTab {
		parts = append(parts, th.Key.Render("←→")+th.Surface.Render(" tabs"))
	}
	if t.searchable {
		add(keymap.Search, "search")
		add(keymap.Filter, "filter")
	}
	add(keymap.Help, "help")
	add(keymap.Quit, "quit")
	left := th.Surface.Render(" ") + strings.Join(parts, th.Surface.Render("  "))

	s := m.view()
	var status []string
	if q := m.q(t.id); q.search != "" || q.filter != "" {
		status = append(status, th.Accent.Render("⌕ "+strings.TrimSpace(q.search+" "+q.filter)))
	}
	if m.toast != "" && m.now().Before(m.toastTill) {
		status = append(status, th.Accent.Render(m.toast))
	}
	crit, warn := 0, 0
	for _, a := range s.Alerts {
		if a.Severity == model.SevCritical {
			crit++
		} else {
			warn++
		}
	}
	if crit > 0 {
		status = append(status, th.Crit.Render(fmt.Sprintf("✖ %d", crit)))
	}
	if warn > 0 {
		status = append(status, th.Warn.Render(fmt.Sprintf("▲ %d", warn)))
	}
	if s.History.Enabled {
		label := "hist " + fmtDuration(s.History.Retention)
		if s.History.Error != "" {
			status = append(status, th.Warn.Render(label+" (mem)"))
		} else {
			status = append(status, th.Muted.Render(label))
		}
	}
	right := strings.Join(status, th.Surface.Render("  ")) + th.Surface.Render(" ")
	space := w - widgets.Width(right)
	if space < 10 {
		return widgets.Fit(th, left, w)
	}
	left = widgets.Fit(th, left, space)
	return strings.ReplaceAll(left, th.Base.Render(" "), th.Surface.Render(" ")) + right
}

func (m *Model) viewLoading(w, h int) widgets.Block {
	th := m.th
	s := m.view()
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	f := frames[int(m.now().UnixMilli()/100)%len(frames)]
	lines := []string{"", th.Primary.Render(f) + th.Text.Render(" Initializing accelerator providers…")}
	if m.remote != "" {
		lines[1] = th.Primary.Render(f) + th.Text.Render(" Connecting to remote agent "+m.remote+"…")
	}
	for _, p := range s.Providers {
		if p.Error != "" {
			lines = append(lines, th.Dim.Render(p.Name+": "+p.Error))
		}
	}
	out := widgets.Blank(th, w, h)
	for i, l := range lines {
		if h/3+i < h {
			out[h/3+i] = widgets.Center(th, l, w)
		}
	}
	return out
}

func (m *Model) viewHelp(w, h int) widgets.Block {
	th := m.th
	var keys []string
	seen := map[keymap.Action]bool{}
	for _, b := range m.keys.Bindings() {
		if _, isTab := keymap.TabIndex(b.Action); isTab {
			if b.Action != keymap.Tab1 {
				continue
			}
			keys = append(keys, m.fmtKeyLine(th.Key.Render("1…9"), th.Text.Render("jump to tab")))
			continue
		}
		if seen[b.Action] || len(b.Keys) == 0 {
			continue
		}
		seen[b.Action] = true
		var ks []string
		for _, k := range b.Keys {
			ks = append(ks, keymap.DisplayKey(k))
		}
		keys = append(keys, m.fmtKeyLine(th.Key.Render(strings.Join(ks, " ")), th.Text.Render(b.Help)))
	}

	legend := []string{
		th.Title.Render("States"),
		th.OK.Render("● busy") + th.Dim.Render("   ≥60% utilization"),
		th.Accent.Render("◐ active") + th.Dim.Render(" above idle threshold"),
		th.Muted.Render("○ idle") + th.Dim.Render("   below idle threshold"),
		th.Crit.Render("✖ down") + th.Dim.Render("   not readable"),
		"",
		th.Title.Render("Values"),
		th.NA.Render("N/A") + th.Dim.Render("  not supported by this GPU/driver"),
		th.Dim.Render("Health and efficiency scores are gputop-derived"),
		th.Dim.Render("heuristics, not vendor metrics (see docs/)."),
	}
	if m.mouse {
		legend = append(legend, "",
			th.Title.Render("Mouse"),
			th.Dim.Render("click tab · row · header (sort) · hint"),
			th.Dim.Render("double-click row: detail · wheel: scroll"),
		)
	}
	legend = append(legend, "", th.Title.Render("Tabs"))
	vis := m.visibleTabs()
	names := make([]string, len(vis))
	for i, t := range vis {
		names[i] = t.title
	}
	sort.Strings(names)
	legend = append(legend, th.Dim.Render(strings.Join(names, " · ")))

	cols := widgets.Split(w, 1, 1)
	left := widgets.Box(th, widgets.BoxOpts{Title: "Keys", Focus: true}, cols[0], h, keys)
	right := widgets.Box(th, widgets.BoxOpts{Title: "Legend"}, cols[1], h, legend)
	return widgets.HJoin(th, left, right)
}

// fmtKeyLine aligns a key label and its description in two columns.
func (m *Model) fmtKeyLine(k, desc string) string {
	return k + widgets.Space(m.th, max(1, 16-widgets.Width(k))) + desc
}
