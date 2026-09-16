// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/keymap"
)

// Mouse support, k9s style: click a tab to open it, click a row to select
// it, double-click a row to open its detail, click a column header to sort,
// click a footer hint to run it, and scroll with the wheel (panes such as the
// GPU detail scroll under the pointer; elsewhere the wheel moves the
// selection).
//
// Views wrap clickable text in zero-width markers (a private CSI sequence,
// "ESC [ <id> z", placed at both ends). Layout code measures and truncates
// strings with ANSI-aware functions, so markers survive composition. View
// scans the finished frame, records where each zone landed and strips the
// markers before the frame reaches the terminal.

// doubleClickWindow is the maximum delay between the clicks of a double-click.
const doubleClickWindow = 400 * time.Millisecond

// clickFn handles a click on a zone; dbl is true for the second click of a
// double-click.
type clickFn func(dbl bool) tea.Cmd

// wheelFn handles the mouse wheel over a zone; delta is in lines (negative
// scrolls up).
type wheelFn func(delta int) tea.Cmd

// wheelLines is how far one wheel notch scrolls a pane.
const wheelLines = 3

type zoneEntry struct {
	key   string
	fn    clickFn
	wheel wheelFn
}

type zoneRect struct {
	entry  int
	y      int
	x0, x1 int // x1 exclusive
}

// zoneSet holds the clickable regions of the current frame.
type zoneSet struct {
	entries []zoneEntry
	rects   []zoneRect
}

func (z *zoneSet) reset() {
	z.entries = z.entries[:0]
	z.rects = z.rects[:0]
}

// mark wraps s in markers for a zone. key identifies the target across
// frames (for double-click detection).
func (z *zoneSet) mark(key, s string, fn clickFn) string {
	tag := "\x1b[" + strconv.Itoa(len(z.entries)) + "z"
	z.entries = append(z.entries, zoneEntry{key: key, fn: fn})
	return tag + s + tag
}

// markWheel is mark for a zone that also handles the mouse wheel.
func (z *zoneSet) markWheel(key, s string, fn clickFn, wheel wheelFn) string {
	out := z.mark(key, s, fn)
	z.entries[len(z.entries)-1].wheel = wheel
	return out
}

// scan records zone positions in frame and returns it without markers.
func (z *zoneSet) scan(frame string) string {
	if len(z.entries) == 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	open := map[int]int{} // entry -> start column
	for y, line := range lines {
		lines[y] = z.scanLine(line, y, open)
	}
	return strings.Join(lines, "\n")
}

func (z *zoneSet) scanLine(line string, y int, open map[int]int) string {
	var b strings.Builder
	x, last, found := 0, 0, false
	for i := 0; i < len(line); {
		j := strings.Index(line[i:], "\x1b[")
		if j < 0 {
			break
		}
		j += i
		k := j + 2
		for k < len(line) && line[k] >= '0' && line[k] <= '9' {
			k++
		}
		if k == j+2 || k >= len(line) || line[k] != 'z' {
			i = j + 2
			continue
		}
		id, err := strconv.Atoi(line[j+2 : k])
		i = k + 1
		if err != nil || id >= len(z.entries) {
			continue
		}
		found = true
		part := line[last:j]
		b.WriteString(part)
		x += ansi.StringWidth(part)
		last = i
		if x0, ok := open[id]; ok {
			z.add(id, y, x0, x)
			delete(open, id)
		} else {
			open[id] = x
		}
	}
	if !found && len(open) == 0 {
		return line
	}
	part := line[last:]
	b.WriteString(part)
	x += ansi.StringWidth(part)
	// Zones still open continue on the next line.
	for id, x0 := range open {
		z.add(id, y, x0, x)
		open[id] = 0
	}
	return b.String()
}

func (z *zoneSet) add(entry, y, x0, x1 int) {
	if x1 > x0 {
		z.rects = append(z.rects, zoneRect{entry: entry, y: y, x0: x0, x1: x1})
	}
}

// at returns the innermost zone under (x, y) accepted by want.
func (z *zoneSet) at(x, y int, want func(zoneEntry) bool) (zoneEntry, bool) {
	best, bestW := -1, 0
	for _, r := range z.rects {
		if r.y == y && x >= r.x0 && x < r.x1 && (best < 0 || r.x1-r.x0 < bestW) && want(z.entries[r.entry]) {
			best, bestW = r.entry, r.x1-r.x0
		}
	}
	if best < 0 {
		return zoneEntry{}, false
	}
	return z.entries[best], true
}

// zone marks s as clickable.
func (m *Model) zone(key, s string, fn clickFn) string {
	if !m.mouse {
		return s
	}
	return m.zones.mark(key, s, fn)
}

type lastClick struct {
	key string
	at  time.Time
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.input != nil {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		if m.help || msg.Action != tea.MouseActionPress {
			return m, nil
		}
		a, delta := keymap.Down, wheelLines
		if msg.Button == tea.MouseButtonWheelUp {
			a, delta = keymap.Up, -wheelLines
		}
		// Scrollable panes take the wheel; elsewhere it moves the selection.
		if z, ok := m.zones.at(msg.X, msg.Y, func(e zoneEntry) bool { return e.wheel != nil }); ok {
			return m, z.wheel(delta)
		}
		return m, m.dispatch(a, "")
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
	default:
		return m, nil
	}
	if m.help {
		m.help = false
		return m, nil
	}
	z, ok := m.zones.at(msg.X, msg.Y, func(e zoneEntry) bool { return e.fn != nil })
	if !ok {
		m.click = lastClick{}
		// Clicking beside a detail popup closes it.
		if m.procs.detail || m.events.detail {
			return m, m.dispatch(keymap.Back, "")
		}
		return m, nil
	}
	now := m.now()
	dbl := m.click.key == z.key && now.Sub(m.click.at) <= doubleClickWindow
	m.click = lastClick{key: z.key, at: now}
	if dbl {
		m.click = lastClick{} // a third click starts a new double-click
	}
	return m, z.fn(dbl)
}

// gpuRowMark makes table row r select the r-th GPU; a double-click opens the
// active tab's detail for it.
func (m *Model) gpuRowMark() func(r int, line string) string {
	if !m.mouse {
		return nil
	}
	s := m.view()
	return func(r int, line string) string {
		if r >= len(s.GPUs) {
			return line
		}
		id := s.GPUs[r].Device.ID
		return m.zone("gpu:"+string(id), line, func(dbl bool) tea.Cmd {
			return m.clickGPU(id, dbl)
		})
	}
}

func (m *Model) clickGPU(id gpu.ID, dbl bool) tea.Cmd {
	m.selGPU = id
	if dbl {
		return m.dispatch(keymap.Select, "")
	}
	return nil
}

// listRowMark makes table row r set *sel; a double-click sends Select.
func (m *Model) listRowMark(prefix string, sel *int) func(r int, line string) string {
	if !m.mouse {
		return nil
	}
	return func(r int, line string) string {
		return m.zone(prefix+strconv.Itoa(r), line, func(dbl bool) tea.Cmd {
			*sel = r
			if dbl {
				return m.dispatch(keymap.Select, "")
			}
			return nil
		})
	}
}
