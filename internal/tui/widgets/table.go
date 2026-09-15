// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/theme"
)

// Align is column alignment.
type Align int

const (
	Left Align = iota
	Right
)

// Column describes a table column.
type Column struct {
	Title string
	// Width is the preferred width; Min the minimum when shrinking.
	Width, Min int
	Align      Align
	// Flex columns absorb extra width.
	Flex bool
	// Priority: when space is short, higher numbers are dropped first
	// (0 = never dropped).
	Priority int
}

// Cell is a pre-styled cell. Raw renders the text as-is (already styled).
type Cell struct {
	Text  string
	Style *lipgloss.Style
}

// C builds a cell styled with s.
func C(s lipgloss.Style, text string) Cell { return Cell{Text: text, Style: &s} }

// R builds a raw (pre-rendered) cell.
func R(text string) Cell { return Cell{Text: text} }

// Table renders rows with a header.
type Table struct {
	Columns  []Column
	Rows     [][]Cell
	Selected int // -1 for none
	Offset   int // first visible row
	// SortCol marks the sorted column title (-1 none); SortDesc its order.
	SortCol  int
	SortDesc bool
	// Widths is filled by Render with the resolved column widths (0 = hidden).
	Widths []int
	// RowMark, when set, decorates each rendered row line (row is the index
	// into Rows); HeaderMark decorates each rendered column title. Both must
	// return strings of the same display width (used for mouse zones).
	RowMark    func(row int, line string) string
	HeaderMark func(col int, title string) string
}

// Layout resolves column widths for the available width.
func (t *Table) Layout(width int) []int {
	n := len(t.Columns)
	widths := make([]int, n)
	visible := make([]bool, n)
	for i := range visible {
		visible[i] = true
	}
	total := func() int {
		sum, cnt := 0, 0
		for i, c := range t.Columns {
			if visible[i] {
				sum += max(c.Width, c.Min)
				cnt++
			}
		}
		return sum + max(0, cnt-1) // single-space gaps
	}
	// Drop low-priority columns until preferred widths fit.
	for total() > width {
		worst, wp := -1, 0
		for i, c := range t.Columns {
			if visible[i] && c.Priority > wp {
				worst, wp = i, c.Priority
			}
		}
		if worst < 0 {
			break
		}
		visible[worst] = false
	}
	used := 0
	cnt := 0
	for i, c := range t.Columns {
		if visible[i] {
			widths[i] = max(c.Width, c.Min)
			used += widths[i]
			cnt++
		}
	}
	used += max(0, cnt-1)
	// Shrink flex/any columns down to Min if still too wide.
	for i := n - 1; i >= 0 && used > width; i-- {
		if !visible[i] {
			continue
		}
		minW := max(1, t.Columns[i].Min)
		if t.Columns[i].Min == 0 {
			minW = min(widths[i], max(3, len(t.Columns[i].Title)))
		}
		cut := min(widths[i]-minW, used-width)
		if cut > 0 {
			widths[i] -= cut
			used -= cut
		}
	}
	// Distribute extra space to flex columns.
	if extra := width - used; extra > 0 {
		var flex []int
		for i, c := range t.Columns {
			if visible[i] && c.Flex {
				flex = append(flex, i)
			}
		}
		for k := 0; extra > 0 && len(flex) > 0; k = (k + 1) % len(flex) {
			widths[flex[k]]++
			extra--
		}
	}
	return widths
}

// Render draws the header and up to h-1 rows in w cells.
func (t *Table) Render(th *theme.Theme, w, h int) Block {
	if h <= 0 || w <= 0 {
		return nil
	}
	widths := t.Layout(w)
	t.Widths = widths
	out := make(Block, 0, h)

	var hdr strings.Builder
	first := true
	for i, c := range t.Columns {
		if widths[i] == 0 {
			continue
		}
		if !first {
			hdr.WriteString(th.Base.Render(" "))
		}
		first = false
		title := c.Title
		st := th.Dim.Bold(true)
		if i == t.SortCol {
			arrow := "▲"
			if t.SortDesc {
				arrow = "▼"
			}
			title += arrow
			st = th.Primary.Bold(true)
		}
		cell := align(th, st.Render(title), widths[i], c.Align)
		if t.HeaderMark != nil {
			cell = t.HeaderMark(i, cell)
		}
		hdr.WriteString(cell)
	}
	out = append(out, Fit(th, hdr.String(), w))

	rowsH := h - 1
	if t.Selected >= 0 {
		if t.Selected < t.Offset {
			t.Offset = t.Selected
		}
		if t.Selected >= t.Offset+rowsH {
			t.Offset = t.Selected - rowsH + 1
		}
	}
	t.Offset = max(0, min(t.Offset, max(0, len(t.Rows)-rowsH)))
	for r := t.Offset; r < len(t.Rows) && len(out) < h; r++ {
		row := t.Rows[r]
		selected := r == t.Selected
		var sb strings.Builder
		first := true
		for i := range t.Columns {
			if widths[i] == 0 {
				continue
			}
			if !first {
				if selected {
					sb.WriteString(th.Selected.Render(" "))
				} else {
					sb.WriteString(th.Base.Render(" "))
				}
			}
			first = false
			var cell Cell
			if i < len(row) {
				cell = row[i]
			}
			text := cell.Text
			if selected {
				text = th.Selected.Render(stripForSelection(cell))
			} else if cell.Style != nil {
				text = cell.Style.Render(cell.Text)
			}
			if selected {
				sb.WriteString(alignSel(th, text, widths[i], t.Columns[i].Align))
			} else {
				sb.WriteString(align(th, text, widths[i], t.Columns[i].Align))
			}
		}
		line := sb.String()
		if selected {
			if pad := w - Width(line); pad > 0 {
				line += th.Selected.Render(strings.Repeat(" ", pad))
			}
		}
		line = Fit(th, line, w)
		if t.RowMark != nil {
			line = t.RowMark(r, line)
		}
		out = append(out, line)
	}
	for len(out) < h {
		out = append(out, Space(th, w))
	}
	return out
}

// stripForSelection returns plain text for styled cells so the selection
// highlight is uniform; raw (pre-rendered) cells keep their rendering.
func stripForSelection(c Cell) string {
	if c.Style != nil {
		return c.Text
	}
	return c.Text
}

func align(th *theme.Theme, s string, w int, a Align) string {
	if a == Right {
		return FitRight(th, s, w)
	}
	return Fit(th, s, w)
}

func alignSel(th *theme.Theme, s string, w int, a Align) string {
	sw := Width(s)
	if sw > w {
		return Fit(th, s, w)
	}
	pad := th.Selected.Render(strings.Repeat(" ", w-sw))
	if a == Right {
		return pad + s
	}
	return s + pad
}

// Scroll clamps a selection index into [0, n).
func Scroll(sel, delta, n int) int {
	if n == 0 {
		return 0
	}
	return max(0, min(n-1, sel+delta))
}
