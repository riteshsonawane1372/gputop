// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"math"
	"strings"

	"github.com/riteshsonawane1372/gputop/internal/theme"
)

var partials = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// Bar renders a horizontal meter of w cells for frac in [0,1]. The filled
// part is colored along the theme gradient by position; NaN renders as an
// unavailable (dotted) bar.
func Bar(th *theme.Theme, frac float64, w int) string {
	if w <= 0 {
		return ""
	}
	if math.IsNaN(frac) {
		return th.NA.Render(strings.Repeat("·", w))
	}
	frac = math.Max(0, math.Min(1, frac))
	eighths := int(math.Round(frac * float64(w*8)))
	full := eighths / 8
	part := eighths % 8

	var b strings.Builder
	// Group cells into color runs (8 gradient steps across the bar).
	const steps = 8
	run := strings.Builder{}
	runStep := -1
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(th.Gradient(float64(runStep) / float64(steps-1)).Render(run.String()))
			run.Reset()
		}
	}
	for i := 0; i < full; i++ {
		step := i * steps / w
		if step != runStep {
			flush()
			runStep = step
		}
		run.WriteString("█")
	}
	flush()
	used := full
	if part > 0 && used < w {
		step := used * steps / w
		b.WriteString(th.Gradient(float64(step) / float64(steps-1)).Render(partials[part]))
		used++
	}
	if used < w {
		b.WriteString(th.Muted.Render(strings.Repeat("░", w-used)))
	}
	return b.String()
}

// LevelBar is a single-color bar colored by value level (ok/warn/crit).
func LevelBar(th *theme.Theme, frac float64, w int, warn, crit float64) string {
	if w <= 0 {
		return ""
	}
	if math.IsNaN(frac) {
		return th.NA.Render(strings.Repeat("·", w))
	}
	frac = math.Max(0, math.Min(1, frac))
	st := th.Level(frac, warn, crit)
	eighths := int(math.Round(frac * float64(w*8)))
	full, part := eighths/8, eighths%8
	s := strings.Repeat("█", full)
	used := full
	if part > 0 && used < w {
		s += partials[part]
		used++
	}
	return st.Render(s) + th.Muted.Render(strings.Repeat("░", w-used))
}

// Pips renders a compact score like ▰▰▰▰▱ (n segments).
func Pips(th *theme.Theme, frac float64, n int) string {
	if math.IsNaN(frac) {
		return th.NA.Render(strings.Repeat("▱", n))
	}
	on := int(math.Round(math.Max(0, math.Min(1, frac)) * float64(n)))
	return th.Gradient(1-frac).Render(strings.Repeat("▰", on)) + th.Muted.Render(strings.Repeat("▱", n-on))
}
