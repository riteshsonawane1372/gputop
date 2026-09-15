// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
)

// writeSummary prints a compact, script-friendly text summary.
func writeSummary(w io.Writer, s *model.Snapshot, color bool) error {
	c := func(code, text string) string {
		if !color {
			return text
		}
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	host := s.Node.Hostname
	if s.Host != nil && host == "" {
		host = s.Host.Info.Hostname
	}
	var header []string
	header = append(header, c("1;32", "gputop"), host)
	if s.Node.Demo {
		header = append(header, c("33", "[DEMO: simulated data]"))
	}
	for _, p := range s.Providers {
		if p.System.DriverVersion != "" {
			header = append(header, "driver "+p.System.DriverVersion)
		}
		if p.System.RuntimeVersion != "" {
			header = append(header, p.System.RuntimeName+" "+p.System.RuntimeVersion)
		}
	}
	fmt.Fprintln(w, strings.Join(header, "  "))

	if len(s.GPUs) == 0 {
		fmt.Fprintln(w, c("33", "No supported GPU detected."))
		for _, p := range s.Providers {
			for _, ch := range p.Diagnostics.Checks {
				mark := c("32", "ok  ")
				if !ch.OK {
					mark = c("31", "FAIL")
				}
				fmt.Fprintf(w, "  %s %-18s %s\n", mark, ch.Name, ch.Detail)
			}
			for _, h := range p.Diagnostics.Hints {
				fmt.Fprintf(w, "  -> %s\n", h)
			}
		}
		return nil
	}

	f := s.Fleet
	na := func(o metric.Opt[float64], format string) string {
		if !o.OK {
			return "N/A"
		}
		return fmt.Sprintf(format, o.V)
	}
	fmt.Fprintf(w, "GPUs %d  busy %d  active %d  idle %d  down %d  allocated %d  power %s  vram %s  health %s\n\n",
		f.GPUs, f.Busy, f.Active-f.Busy, f.Idle, f.Unavailable, f.Allocated,
		na(f.PowerW, "%.0f W"), na(metric.Map(f.VRAMFraction, func(v float64) float64 { return v * 100 }), "%.0f%%"), na(f.HealthAvg, "%.0f"))

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "GPU\tNAME\tUTIL\tVRAM\tTEMP\tPOWER\tHEALTH\tSTATE\tPROCS")
	for _, g := range s.GPUs {
		smp := g.Sample
		vram := "N/A"
		if smp.MemUsed.OK && smp.MemTotal.OK {
			vram = fmt.Sprintf("%.1f/%.0f GiB", float64(smp.MemUsed.V)/(1<<30), float64(smp.MemTotal.V)/(1<<30))
		}
		state := string(g.Derived.State)
		if g.Derived.Throttled {
			state += ",throttled"
		}
		if g.Derived.Outlier {
			state += ",outlier"
		}
		if !g.Available {
			state = c("31", "unavailable: "+g.Error)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%d\n", g.Device.Index, strings.TrimSuffix(g.Device.Name, " (simulated)"),
			na(smp.UtilPercent, "%.0f%%"), vram, na(smp.TempC, "%.0f°C"), na(smp.PowerW, "%.0f W"), g.Health.Score, state, g.Processes)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(s.Alerts) > 0 {
		fmt.Fprintln(w)
		for _, a := range s.Alerts {
			who := ""
			if a.DeviceIndex >= 0 {
				who = fmt.Sprintf("GPU %d: ", a.DeviceIndex)
			}
			code := "33"
			if a.Severity == model.SevCritical {
				code = "31"
			}
			fmt.Fprintf(w, "%s %s%s\n", c(code, strings.ToUpper(string(a.Severity))), who, a.Title)
		}
	}
	return nil
}
