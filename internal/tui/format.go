// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gputop/gputop/internal/metric"
)

const na = "N/A"

func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	v := float64(b) / float64(div)
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}[exp]
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, suffix)
	}
	return fmt.Sprintf("%.1f %s", v, suffix)
}

// fmtGiB renders a used/total pair compactly: "62.1/80G".
func fmtGiBPair(used, total uint64) string {
	u, t := float64(used)/(1<<30), float64(total)/(1<<30)
	if t >= 100 {
		return fmt.Sprintf("%.0f/%.0fG", u, t)
	}
	return fmt.Sprintf("%.1f/%.0fG", u, t)
}

func fmtRate(bps float64) string {
	if math.IsNaN(bps) {
		return na
	}
	units := []string{"B/s", "KiB/s", "MiB/s", "GiB/s", "TiB/s"}
	i := 0
	for bps >= 1024 && i < len(units)-1 {
		bps /= 1024
		i++
	}
	if bps >= 100 || i == 0 {
		return fmt.Sprintf("%.0f %s", bps, units[i])
	}
	return fmt.Sprintf("%.1f %s", bps, units[i])
}

func optRate(o metric.Opt[float64]) string {
	if !o.OK {
		return na
	}
	return fmtRate(o.V)
}

func optBytes(o metric.Opt[uint64]) string {
	if !o.OK {
		return na
	}
	return fmtBytes(o.V)
}

func optPct(o metric.Opt[float64]) string {
	if !o.OK {
		return na
	}
	return fmt.Sprintf("%.0f%%", o.V)
}

func optF(o metric.Opt[float64], format string) string {
	if !o.OK {
		return na
	}
	return fmt.Sprintf(format, o.V)
}

func optI(o metric.Opt[int], format string) string {
	if !o.OK {
		return na
	}
	return fmt.Sprintf(format, o.V)
}

func optU(o metric.Opt[uint64]) string {
	if !o.OK {
		return na
	}
	return fmt.Sprint(o.V)
}

func optBool(o metric.Opt[bool], yes, no string) string {
	if !o.OK {
		return na
	}
	if o.V {
		return yes
	}
	return no
}

func (m *Model) temp(o metric.Opt[float64]) string {
	if !o.OK {
		return na
	}
	if m.fahrenheit {
		return fmt.Sprintf("%.0f°F", o.V*9/5+32)
	}
	return fmt.Sprintf("%.0f°C", o.V)
}

func fmtWatts(w float64) string {
	if w >= 10000 {
		return fmt.Sprintf("%.2f kW", w/1000)
	}
	if w < 10 {
		return fmt.Sprintf("%.1f W", w)
	}
	return fmt.Sprintf("%.0f W", w)
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		if int(d.Seconds())%60 == 0 {
			return fmt.Sprintf("%dm", int(d.Minutes()))
		}
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 48*time.Hour:
		if int(d.Minutes())%60 == 0 {
			return fmt.Sprintf("%dh", int(d.Hours()))
		}
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
}

func fmtAgo(now, t time.Time) string {
	if t.IsZero() {
		return na
	}
	d := now.Sub(t)
	if d < time.Second {
		return "now"
	}
	return fmtDuration(d) + " ago"
}

// shortName trims vendor prefixes from device names for dense tables.
func shortName(name string) string {
	name = strings.TrimSuffix(name, " (simulated)")
	for _, p := range []string{"NVIDIA ", "GeForce ", "Apple "} {
		name = strings.TrimPrefix(name, p)
	}
	return name
}
