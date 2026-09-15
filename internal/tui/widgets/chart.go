// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/theme"
)

// ChartOpts configure a braille area chart.
type ChartOpts struct {
	// Min/Max fix the scale; when Max <= Min the scale is automatic.
	Min, Max float64
	// Cursor draws a vertical marker at this data index (-1 disables).
	Cursor int
	// Markers colors individual columns' baseline (event markers); keyed
	// by data index.
	Markers map[int]lipgloss.Style
	// Line draws only the top dots instead of a filled area.
	Line bool
}

// Braille dot bits for a 2×4 cell: [column][row], row 0 at the top.
var dots = [2][4]rune{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// Resample maps values onto n points, averaging when shrinking and
// repeating when stretching. NaN values are ignored in averages.
func Resample(values []float64, n int) []float64 {
	out := make([]float64, n)
	if len(values) == 0 {
		for i := range out {
			out[i] = math.NaN()
		}
		return out
	}
	for i := 0; i < n; i++ {
		lo := i * len(values) / n
		hi := (i + 1) * len(values) / n
		if hi <= lo {
			hi = lo + 1
		}
		var sum float64
		cnt := 0
		for _, v := range values[lo:min(hi, len(values))] {
			if !math.IsNaN(v) {
				sum += v
				cnt++
			}
		}
		if cnt == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}

// Chart renders values as a w×h braille area chart. The most recent value
// is on the right. Returned lines have exactly w cells.
func Chart(th *theme.Theme, values []float64, w, h int, o ChartOpts) Block {
	if w <= 0 || h <= 0 {
		return nil
	}
	lo, hi := o.Min, o.Max
	if hi <= lo {
		lo, hi = math.Inf(1), math.Inf(-1)
		for _, v := range values {
			if !math.IsNaN(v) {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
		if math.IsInf(lo, 0) {
			lo, hi = 0, 1
		}
		if lo > 0 && lo < hi*0.3 {
			lo = 0
		}
		if hi-lo < 1e-9 {
			hi = lo + math.Max(1, math.Abs(lo)*0.1)
		}
	}
	px := w * 2
	// Right-align: when there are fewer values than pixels, pad the left.
	var pts []float64
	if len(values) >= px {
		pts = Resample(values, px)
	} else {
		pts = make([]float64, px)
		pad := px - len(values)
		for i := range pts {
			if i < pad {
				pts[i] = math.NaN()
			} else {
				pts[i] = values[i-pad]
			}
		}
	}
	rows := h * 4
	heights := make([]int, px) // dots filled from bottom, -1 for NaN
	for i, v := range pts {
		if math.IsNaN(v) {
			heights[i] = -1
			continue
		}
		f := (v - lo) / (hi - lo)
		f = math.Max(0, math.Min(1, f))
		heights[i] = int(math.Round(f * float64(rows)))
		if v > lo && heights[i] == 0 {
			heights[i] = 1 // keep non-zero values visible
		}
	}

	cursorCol := -1
	if o.Cursor >= 0 && len(values) > 0 {
		if len(values) >= px {
			cursorCol = o.Cursor * w / len(values)
		} else {
			cursorCol = (px - len(values) + o.Cursor) / 2
		}
	}

	out := make(Block, h)
	var sb strings.Builder
	for r := 0; r < h; r++ {
		sb.Reset()
		// Row r covers dot rows [rowTop, rowTop+4) counted from the top.
		rowTop := r * 4
		style := th.Gradient(1 - (float64(r)+0.5)/float64(h))
		run := strings.Builder{}
		flush := func(st lipgloss.Style) {
			if run.Len() > 0 {
				sb.WriteString(st.Render(run.String()))
				run.Reset()
			}
		}
		for c := 0; c < w; c++ {
			if c == cursorCol {
				flush(style)
				sb.WriteString(th.Accent.Render("│"))
				continue
			}
			var cell rune
			for dc := 0; dc < 2; dc++ {
				hgt := heights[c*2+dc]
				if hgt < 0 {
					continue
				}
				for dr := 0; dr < 4; dr++ {
					dotFromBottom := rows - (rowTop + dr) // 1..rows
					if o.Line {
						if dotFromBottom == hgt || (hgt == 0 && dotFromBottom == 1) {
							cell |= dots[dc][dr]
						}
					} else if dotFromBottom <= hgt {
						cell |= dots[dc][dr]
					}
				}
			}
			if cell == 0 {
				if r == h-1 && o.Markers != nil {
					if st, ok := markerAt(o.Markers, c, w, len(values), px); ok {
						flush(style)
						sb.WriteString(st.Render("▴"))
						continue
					}
				}
				run.WriteRune(' ')
				continue
			}
			run.WriteRune(0x2800 + cell)
		}
		flush(style)
		out[r] = Fit(th, sb.String(), w)
	}
	return out
}

func markerAt(m map[int]lipgloss.Style, col, w, n, px int) (lipgloss.Style, bool) {
	for idx, st := range m {
		var c int
		if n >= px {
			c = idx * w / max(1, n)
		} else {
			c = (px - n + idx) / 2
		}
		if c == col {
			return st, true
		}
	}
	return lipgloss.Style{}, false
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders values in w cells using block elements, colored by
// value along the theme gradient. max <= 0 autoscales.
func Sparkline(th *theme.Theme, values []float64, w int, maxV float64) string {
	if w <= 0 {
		return ""
	}
	pts := values
	if len(values) > w {
		pts = Resample(values, w)
	}
	if maxV <= 0 {
		for _, v := range pts {
			if !math.IsNaN(v) {
				maxV = math.Max(maxV, v)
			}
		}
		if maxV <= 0 {
			maxV = 1
		}
	}
	var sb strings.Builder
	pad := w - len(pts)
	if pad > 0 {
		sb.WriteString(Space(th, pad))
	}
	for _, v := range pts {
		if math.IsNaN(v) {
			sb.WriteString(th.NA.Render("·"))
			continue
		}
		f := math.Max(0, math.Min(1, v/maxV))
		i := int(math.Round(f * float64(len(sparkRunes)-1)))
		sb.WriteString(th.Gradient(f).Render(string(sparkRunes[i])))
	}
	return sb.String()
}
