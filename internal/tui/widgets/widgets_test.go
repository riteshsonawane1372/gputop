// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/riteshsonawane1372/gputop/internal/theme"
)

func th() *theme.Theme {
	lipgloss.SetColorProfile(termenv.TrueColor)
	p, _ := theme.Resolve("green", "", nil)
	return theme.New("green", p, false)
}

func assertWidth(t *testing.T, lines []string, w int) {
	t.Helper()
	for i, l := range lines {
		if got := Width(l); got != w {
			t.Fatalf("line %d width %d, want %d: %q", i, got, w, l)
		}
	}
}

func TestFit(t *testing.T) {
	tt := th()
	if got := Width(Fit(tt, "hello world", 5)); got != 5 {
		t.Fatalf("truncate width %d", got)
	}
	if got := Width(Fit(tt, tt.Crit.Render("hi"), 10)); got != 10 {
		t.Fatalf("pad width %d", got)
	}
	if got := Width(Center(tt, "ab", 7)); got != 7 {
		t.Fatal("center")
	}
	if s := Split(10, 1, 1, 1); s[0]+s[1]+s[2] != 10 {
		t.Fatalf("split %v", s)
	}
}

func TestBoxDimensions(t *testing.T) {
	tt := th()
	for _, w := range []int{4, 10, 40, 120} {
		b := Box(tt, BoxOpts{Title: "A very long title that must be clipped", RightTitle: "right"}, w, 5, []string{"content that is very long and must be clipped at the border"})
		if len(b) != 5 {
			t.Fatalf("height %d", len(b))
		}
		assertWidth(t, b, w)
	}
}

func TestBars(t *testing.T) {
	tt := th()
	for _, f := range []float64{0, 0.33, 0.5, 0.999, 1, 1.5, -1, math.NaN()} {
		for _, w := range []int{1, 7, 20} {
			if got := Width(Bar(tt, f, w)); got != w {
				t.Fatalf("Bar(%v,%d) width %d", f, w, got)
			}
			if got := Width(LevelBar(tt, f, w, 0.8, 0.95)); got != w {
				t.Fatalf("LevelBar(%v,%d) width %d", f, w, got)
			}
		}
	}
	if Width(Pips(tt, 0.7, 10)) != 10 {
		t.Fatal("pips")
	}
}

func TestChart(t *testing.T) {
	tt := th()
	vals := []float64{0, 10, 50, math.NaN(), 100, 75}
	for _, dims := range [][2]int{{10, 3}, {3, 1}, {80, 8}} {
		b := Chart(tt, vals, dims[0], dims[1], ChartOpts{Min: 0, Max: 100, Cursor: 2, Markers: map[int]lipgloss.Style{1: tt.Warn}})
		if len(b) != dims[1] {
			t.Fatalf("rows %d", len(b))
		}
		assertWidth(t, b, dims[0])
	}
	// A full-scale value fills the top row of the last column.
	b := Chart(tt, []float64{100}, 1, 2, ChartOpts{Min: 0, Max: 100, Cursor: -1})
	if !strings.ContainsRune(b[0], 0x2800|0x08|0x10|0x20|0x80) {
		t.Fatalf("top row not filled: %q", b[0])
	}
	if Width(Sparkline(tt, vals, 12, 100)) != 12 || Width(Sparkline(tt, nil, 4, 0)) != 4 {
		t.Fatal("sparkline width")
	}
	r := Resample([]float64{1, 2, 3, 4}, 2)
	if r[0] != 1.5 || r[1] != 3.5 {
		t.Fatalf("resample %v", r)
	}
}

func TestTableLayoutDropsColumnsByPriority(t *testing.T) {
	tt := th()
	tb := &Table{
		Columns: []Column{
			{Title: "PID", Width: 7, Align: Right},
			{Title: "NAME", Width: 20, Min: 8, Flex: true},
			{Title: "POD", Width: 30, Priority: 2},
			{Title: "CMD", Width: 40, Priority: 3},
		},
		Rows:     [][]Cell{{R("1"), C(tt.Text, "python"), R("pod-a"), R("python train.py")}, {R("2"), R("x"), R("y"), R("z")}},
		Selected: 1, SortCol: 0, SortDesc: true,
	}
	b := tb.Render(tt, 40, 4)
	assertWidth(t, b, 40)
	if tb.Widths[3] != 0 || tb.Widths[2] != 0 {
		t.Fatalf("expected low-priority columns dropped: %v", tb.Widths)
	}
	b = tb.Render(tt, 120, 4)
	assertWidth(t, b, 120)
	if tb.Widths[3] == 0 {
		t.Fatalf("wide terminal should show all columns: %v", tb.Widths)
	}
	if !strings.Contains(b[0], "▼") {
		t.Fatal("sort indicator")
	}
	tiny := tb.Render(tt, 12, 3)
	assertWidth(t, tiny, 12)
}

func TestMultiChart(t *testing.T) {
	t0 := th()
	rising := []float64{0, 25, 50, 75, 100}
	flat := []float64{math.NaN(), 10, 10, 10, 10}
	block, lo, hi := MultiChart(t0, []Series{{Values: rising, Style: t0.OK}, {Values: flat, Style: t0.Warn}}, 20, 5, 0, 0)
	if len(block) != 5 || lo != 0 || hi != 200 {
		t.Fatalf("lines %d scale %v..%v (want autoscaled 0..200)", len(block), lo, hi)
	}
	dotsSeen := 0
	for _, l := range block {
		if Width(l) != 20 {
			t.Fatalf("line width %d: %q", Width(l), l)
		}
		for _, r := range ansi.Strip(l) {
			if r >= 0x2801 && r <= 0x28FF {
				dotsSeen++
			}
		}
	}
	if dotsSeen == 0 {
		t.Fatal("nothing drawn")
	}
	if _, lo, hi = MultiChart(t0, nil, 10, 3, 0, 100); lo != 0 || hi != 100 {
		t.Fatalf("fixed scale %v..%v", lo, hi)
	}
	if b, _, _ := MultiChart(t0, nil, 0, 3, 0, 0); b != nil {
		t.Fatal("zero width must render nothing")
	}
}
