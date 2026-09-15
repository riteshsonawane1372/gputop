// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package health computes the gputop-derived GPU health score.
//
// The score is a transparent heuristic, NOT an NVIDIA metric and not a
// scientifically validated reliability predictor. Every point deducted is
// reported as a Reason so users can see exactly why a GPU scored what it
// did. The rules are documented in docs/health-score.md.
package health

import (
	"fmt"
	"sort"
	"time"

	"github.com/gputop/gputop/internal/gpu"
)

// Band is the qualitative interpretation of a score.
type Band string

const (
	Healthy   Band = "healthy"   // 90-100
	Good      Band = "good"      // 75-89
	Degraded  Band = "degraded"  // 50-74
	Unhealthy Band = "unhealthy" // 25-49
	Critical  Band = "critical"  // 0-24
	Unknown   Band = "unknown"
)

// Rank orders bands from best (0) to worst (4).
func (b Band) Rank() int {
	switch b {
	case Good:
		return 1
	case Degraded:
		return 2
	case Unhealthy:
		return 3
	case Critical:
		return 4
	}
	return 0
}

// BandFor maps a score to a band.
func BandFor(score int) Band {
	switch {
	case score >= 90:
		return Healthy
	case score >= 75:
		return Good
	case score >= 50:
		return Degraded
	case score >= 25:
		return Unhealthy
	}
	return Critical
}

// Reason is one deduction.
type Reason struct {
	Penalty  int    `json:"penalty"`
	Severity string `json:"severity"` // info, warning, critical
	Code     string `json:"code"`
	Text     string `json:"text"`
}

// Result is a scored device.
type Result struct {
	Score   int      `json:"score"`
	Band    Band     `json:"band"`
	Reasons []Reason `json:"reasons,omitempty"`
	// Source is always "derived": computed by gputop.
	Source string `json:"source"`
}

// XID is a recent vendor error event.
type XID struct {
	Code     uint64
	Severity string // from the vendor catalog: info, warning, critical
	Time     time.Time
}

// Input is everything the score considers.
type Input struct {
	Now       time.Time
	Available bool
	Error     string
	Device    gpu.Device
	Sample    gpu.Sample
	Counters  gpu.HealthCounters
	// Baseline holds counters from the start of the evaluation window
	// (typically ~15 minutes ago) to detect increases.
	Baseline *gpu.HealthCounters
	Links    []gpu.Link
	// LinkErrBaseline maps link index to its error total at window start.
	LinkErrBaseline map[int]uint64
	XIDs            []XID
}

const (
	windowRecent = 10 * time.Minute
	windowXID    = time.Hour
)

// Score computes the health score.
func Score(in Input) Result {
	res := Result{Source: "derived"}
	if !in.Available {
		msg := "GPU unavailable"
		if in.Error != "" {
			msg += ": " + in.Error
		}
		res.Reasons = []Reason{{Penalty: 100, Severity: "critical", Code: "unavailable", Text: msg}}
		res.Band = Critical
		return res
	}

	var reasons []Reason
	critical := false
	add := func(p int, sev, code, text string) {
		reasons = append(reasons, Reason{Penalty: p, Severity: sev, Code: code, Text: text})
		if sev == "critical" {
			critical = true
		}
	}
	c := in.Counters

	// Memory reliability.
	if c.RemapFailure.OK && c.RemapFailure.V {
		add(60, "critical", "remap_failure", "Row remapping failure: GPU requires service")
	}
	if c.ECCUncorrectedVolatile.OK && c.ECCUncorrectedVolatile.V > 0 {
		add(40, "critical", "ecc_uncorrectable", fmt.Sprintf("%d uncorrectable ECC error(s) since driver load", c.ECCUncorrectedVolatile.V))
	}
	if (c.RemapPending.OK && c.RemapPending.V) || (c.RetiredPending.OK && c.RetiredPending.V) {
		add(25, "warning", "memory_retirement_pending", "Memory remap/retirement pending: reset the GPU to apply")
	}
	if c.RetiredPagesDBE.OK && c.RetiredPagesDBE.V > 0 {
		add(10, "warning", "retired_pages_dbe", fmt.Sprintf("%d page(s) retired after double-bit ECC errors", c.RetiredPagesDBE.V))
	}
	if in.Baseline != nil && c.ECCCorrectedVolatile.OK && in.Baseline.ECCCorrectedVolatile.OK {
		if d := c.ECCCorrectedVolatile.V - min(c.ECCCorrectedVolatile.V, in.Baseline.ECCCorrectedVolatile.V); d >= 100 {
			add(5, "warning", "ecc_corrected_rate", fmt.Sprintf("%d corrected ECC errors in the recent window", d))
		}
	}
	if c.RecoveryAction.OK && c.RecoveryAction.V != "none" && c.RecoveryAction.V != "" {
		add(40, "critical", "recovery_action", "Driver recommends recovery action: "+c.RecoveryAction.V)
	}

	// Xid events: the worst recent critical Xid dominates; lesser Xids add
	// small, capped penalties. Older events count half.
	xids := append([]XID(nil), in.XIDs...)
	sort.Slice(xids, func(i, j int) bool { return xids[i].Time.After(xids[j].Time) })
	critXID, warnPenalty, infoPenalty := false, 0, 0
	for _, x := range xids {
		age := in.Now.Sub(x.Time)
		if age > windowXID || age < -time.Minute {
			continue
		}
		weight := 1.0
		if age > windowRecent {
			weight = 0.5
		}
		switch x.Severity {
		case "critical":
			if !critXID {
				critXID = true
				sev := "critical"
				if weight < 1 {
					sev = "warning"
				}
				add(int(35*weight), sev, "xid_critical", fmt.Sprintf("Critical Xid %d %s", x.Code, window(age)))
			}
		case "warning":
			if warnPenalty < 20 {
				p := min(int(10*weight), 20-warnPenalty)
				warnPenalty += p
				add(p, "warning", "xid", fmt.Sprintf("Xid %d %s", x.Code, window(age)))
			}
		default:
			if infoPenalty < 4 {
				infoPenalty += 2
				add(2, "info", "xid_info", fmt.Sprintf("Informational Xid %d %s", x.Code, window(age)))
			}
		}
	}

	// Thermal and power.
	s := in.Sample
	thr := s.Throttle.Or(0)
	switch {
	case thr&(gpu.ThrottleHWThermal|gpu.ThrottleHWSlowdown) != 0:
		add(20, "warning", "hw_slowdown", "Hardware slowdown active ("+(thr&(gpu.ThrottleHWThermal|gpu.ThrottleHWSlowdown)).String()+")")
	case thr&gpu.ThrottleSWThermal != 0:
		add(10, "warning", "sw_thermal", "Software thermal slowdown active")
	case s.TempC.OK && in.Device.TempSlowdownC.OK && s.TempC.V >= in.Device.TempSlowdownC.V-3:
		add(10, "warning", "temp_near_slowdown", fmt.Sprintf("GPU temperature %.0f°C is within 3°C of slowdown (%.0f°C)", s.TempC.V, in.Device.TempSlowdownC.V))
	}
	if s.MemTempC.OK && in.Device.MemTempMaxC.OK && s.MemTempC.V >= in.Device.MemTempMaxC.V-3 {
		add(10, "warning", "mem_temp_high", fmt.Sprintf("Memory temperature %.0f°C near limit (%.0f°C)", s.MemTempC.V, in.Device.MemTempMaxC.V))
	}
	if thr&gpu.ThrottleHWPowerBrake != 0 {
		add(15, "warning", "power_brake", "External power brake asserted")
	}

	// PCIe link.
	if s.PCIeWidth.OK && in.Device.PCIeMaxWidth.OK && s.PCIeWidth.V < in.Device.PCIeMaxWidth.V {
		add(15, "warning", "pcie_width_degraded", fmt.Sprintf("PCIe link width x%d below maximum x%d", s.PCIeWidth.V, in.Device.PCIeMaxWidth.V))
	}
	// Link generation drops at idle for power saving; only flag under load.
	if s.PCIeGen.OK && in.Device.PCIeMaxGen.OK && s.PCIeGen.V < in.Device.PCIeMaxGen.V && s.UtilPercent.Or(0) >= 50 {
		add(5, "warning", "pcie_gen_degraded", fmt.Sprintf("PCIe Gen%d below maximum Gen%d under load", s.PCIeGen.V, in.Device.PCIeMaxGen.V))
	}
	if c.PCIeFatalErrors.OK && c.PCIeFatalErrors.V > 0 {
		add(20, "critical", "pcie_fatal", fmt.Sprintf("%d fatal PCIe error(s)", c.PCIeFatalErrors.V))
	}
	if in.Baseline != nil && c.PCIeReplays.OK && in.Baseline.PCIeReplays.OK {
		if d := c.PCIeReplays.V - min(c.PCIeReplays.V, in.Baseline.PCIeReplays.V); d >= 10 {
			add(5, "warning", "pcie_replays", fmt.Sprintf("%d PCIe replays in the recent window", d))
		}
	}

	// Interconnect links.
	active, down := 0, 0
	for _, l := range in.Links {
		switch l.State {
		case gpu.LinkActive:
			active++
		case gpu.LinkInactive, gpu.LinkDisabled:
			down++
		}
	}
	if active > 0 && down > 0 {
		add(min(30, 10*down), "warning", "nvlink_down", fmt.Sprintf("%d interconnect link(s) down while %d active", down, active))
	}
	if in.LinkErrBaseline != nil {
		growing := 0
		for _, l := range in.Links {
			if base, ok := in.LinkErrBaseline[l.Index]; ok && l.ErrorTotal() > base {
				growing++
			}
		}
		if growing > 0 {
			add(min(15, 5*growing), "warning", "nvlink_errors", fmt.Sprintf("Error counters increasing on %d link(s)", growing))
		}
	}

	total := 0
	for _, r := range reasons {
		total += r.Penalty
	}
	score := max(0, 100-total)
	if critical && score > 49 {
		score = 49
	}
	sort.SliceStable(reasons, func(i, j int) bool { return reasons[i].Penalty > reasons[j].Penalty })
	res.Score, res.Band, res.Reasons = score, BandFor(score), reasons
	return res
}

// window describes an event age coarsely so alert titles stay stable.
func window(age time.Duration) string {
	if age <= windowRecent {
		return "in the last 10 minutes"
	}
	return "in the last hour"
}
