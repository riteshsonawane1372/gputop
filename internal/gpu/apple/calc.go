// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package apple

import (
	"strings"

	"github.com/gputop/gputop/internal/metric"
)

// stateResidency is one IOReport state and the time spent in it.
type stateResidency struct {
	name      string
	residency int64
}

// residency derives the active percentage and the residency-weighted average
// frequency of the active performance states. States named "P<n>" are the
// active states in DVFS table order; everything else (OFF, IDLE, DOWN) is
// inactive. The frequency is 0 when the GPU was never active.
func residency(states []stateResidency, freqs []float64) (active, freq metric.Opt[float64]) {
	var total, busy, weighted float64
	p := 0
	for _, s := range states {
		r := float64(max(0, s.residency))
		total += r
		if !strings.HasPrefix(s.name, "P") {
			continue
		}
		busy += r
		if p < len(freqs) {
			weighted += r * freqs[p]
		}
		p++
	}
	if total <= 0 {
		return metric.None[float64](), metric.None[float64]()
	}
	active = metric.Some(busy / total * 100)
	if len(freqs) == 0 {
		return active, metric.None[float64]()
	}
	if busy == 0 {
		return active, metric.Some(0.0)
	}
	return active, metric.Some(weighted / busy)
}

// energyJoules converts an IOReport energy value to joules.
func energyJoules(v int64, unit string) (float64, bool) {
	if v < 0 {
		return 0, false
	}
	switch unit {
	case "mJ":
		return float64(v) / 1e3, true
	case "uJ", "µJ":
		return float64(v) / 1e6, true
	case "nJ":
		return float64(v) / 1e9, true
	}
	return 0, false
}

// scaleFrequencies converts a DVFS table to MHz. Depending on the SoC the
// power manager stores Hz or kHz.
func scaleFrequencies(raw []uint32) []float64 {
	var top uint32
	for _, f := range raw {
		top = max(top, f)
	}
	div := 1.0
	switch {
	case top >= 1e8:
		div = 1e6
	case top >= 1e5:
		div = 1e3
	}
	out := make([]float64, len(raw))
	for i, f := range raw {
		out[i] = float64(f) / div
	}
	return out
}

// averageTemp averages plausible sensor readings (°C).
func averageTemp(vals []float64) metric.Opt[float64] {
	var sum float64
	n := 0
	for _, v := range vals {
		if v > 0 && v < 150 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return metric.None[float64]()
	}
	return metric.Some(sum / float64(n))
}
