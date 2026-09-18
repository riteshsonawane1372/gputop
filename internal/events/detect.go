// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package events

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/health"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

// Detector derives events from consecutive snapshots. It keeps debounce
// state so flapping conditions do not flood the timeline. Not safe for
// concurrent use; the collector calls it from one goroutine.
type Detector struct {
	started    bool
	throttle   map[gpu.ID]map[string]*debounce
	linkErrAt  map[string]time.Time
	alertSince map[string]time.Time
}

type debounce struct {
	active   bool
	onSince  time.Time
	offSince time.Time
	reasons  gpu.ThrottleReasons
}

// Debounce windows for throttle events.
const (
	throttleOnAfter  = 2 * time.Second
	throttleOffAfter = 5 * time.Second
	linkErrInterval  = time.Minute
	processBurst     = 5
)

// NewDetector creates a detector.
func NewDetector() *Detector {
	return &Detector{
		throttle:   map[gpu.ID]map[string]*debounce{},
		linkErrAt:  map[string]time.Time{},
		alertSince: map[string]time.Time{},
	}
}

func ev(t time.Time, kind string, sev model.Severity, g *model.GPU, src metric.Source, format string, a ...any) model.Event {
	e := model.Event{Time: t, Kind: kind, Severity: sev, DeviceIndex: -1, Source: src, Message: fmt.Sprintf(format, a...)}
	if g != nil {
		e.DeviceID, e.DeviceIndex = g.Device.ID, g.Device.Index
	}
	return e
}

func deviceSource(g *model.GPU) metric.Source {
	if g.Device.Vendor == gpu.VendorSimulated {
		return metric.SourceSimulated
	}
	if g.Device.Vendor == gpu.VendorNVIDIA {
		return metric.SourceNVML
	}
	return metric.Source(g.Provider)
}

// Diff returns events describing what changed from prev to cur. prev may be
// nil for the first snapshot.
func (d *Detector) Diff(prev, cur *model.Snapshot) []model.Event {
	if cur == nil || !cur.Ready {
		return nil
	}
	now := cur.Time
	var out []model.Event

	if prev == nil || !prev.Ready || !d.started {
		d.started = true
		if len(cur.GPUs) > 0 {
			names := map[string]int{}
			for _, g := range cur.GPUs {
				names[g.Device.Name]++
			}
			var parts []string
			for n, c := range names {
				parts = append(parts, fmt.Sprintf("%d× %s", c, n))
			}
			sort.Strings(parts)
			out = append(out, model.Event{Time: now, Kind: "gpu_discovered", Severity: model.SevInfo, DeviceIndex: -1,
				Source: metric.SourceDerived, Message: "Discovered " + strings.Join(parts, ", ")})
		}
		for i := range cur.GPUs {
			g := &cur.GPUs[i]
			out = append(out, d.throttleEvents(g, now)...)
			if s, m := g.Sample.PCIeWidth, g.Device.PCIeMaxWidth; s.OK && m.OK && s.V < m.V {
				out = append(out, ev(now, "pcie_degraded", model.SevWarning, g, deviceSource(g), "PCIe link at x%d (max x%d)", s.V, m.V))
			}
			if !g.Available {
				out = append(out, ev(now, "gpu_unavailable", model.SevCritical, g, deviceSource(g), "GPU unavailable: %s", g.Error))
			}
		}
		return out
	}

	prevByID := make(map[gpu.ID]*model.GPU, len(prev.GPUs))
	for i := range prev.GPUs {
		prevByID[prev.GPUs[i].Device.ID] = &prev.GPUs[i]
	}
	curIDs := make(map[gpu.ID]bool, len(cur.GPUs))

	for i := range cur.GPUs {
		g := &cur.GPUs[i]
		curIDs[g.Device.ID] = true
		src := deviceSource(g)
		p, existed := prevByID[g.Device.ID]
		if !existed {
			out = append(out, ev(now, "gpu_discovered", model.SevInfo, g, src, "GPU discovered: %s (%s)", g.Device.Name, g.Device.ID))
			continue
		}
		switch {
		case p.Available && !g.Available:
			out = append(out, ev(now, "gpu_unavailable", model.SevCritical, g, src, "GPU became unavailable: %s", g.Error))
		case !p.Available && g.Available:
			out = append(out, ev(now, "gpu_recovered", model.SevWarning, g, src, "GPU recovered and is readable again"))
		}
		if !g.Available {
			continue
		}

		// Energy is cumulative since driver load; a large drop indicates
		// a driver reload or GPU reset.
		if pe, ce := p.Sample.EnergyJ, g.Sample.EnergyJ; pe.OK && ce.OK && ce.V+1000 < pe.V {
			out = append(out, ev(now, "gpu_reset", model.SevWarning, g, metric.SourceDerived,
				"Energy counter reset: probable GPU reset or driver reload (inferred)"))
		}

		out = append(out, counterEvents(p, g, now)...)
		out = append(out, d.throttleEvents(g, now)...)
		out = append(out, d.linkEvents(p, g, now)...)
		out = append(out, partitionEvents(p, g, now)...)

		ps, pm := p.Sample.PCIeWidth, p.Device.PCIeMaxWidth
		cs := g.Sample.PCIeWidth
		if ps.OK && cs.OK && pm.OK {
			if ps.V >= pm.V && cs.V < pm.V {
				out = append(out, ev(now, "pcie_degraded", model.SevWarning, g, src, "PCIe link width dropped to x%d (max x%d)", cs.V, pm.V))
			} else if ps.V < pm.V && cs.V >= pm.V {
				out = append(out, ev(now, "pcie_restored", model.SevInfo, g, src, "PCIe link width restored to x%d", cs.V))
			}
		}
		if p.Health.Band != g.Health.Band && g.Health.Band.Rank() > p.Health.Band.Rank() && g.Health.Band.Rank() >= 2 {
			out = append(out, ev(now, "health_degraded", severityForBand(g.Health.Band), g, metric.SourceDerived,
				"Health score fell to %d (%s)", g.Health.Score, g.Health.Band))
		}
	}
	for id, p := range prevByID {
		if !curIDs[id] {
			out = append(out, ev(now, "gpu_disappeared", model.SevCritical, p, deviceSource(p), "GPU disappeared from enumeration: %s", p.Device.Name))
			delete(d.throttle, id)
		}
	}

	out = append(out, processEvents(prev, cur, now)...)
	out = append(out, collectorEvents(prev, cur, now)...)
	return out
}

func severityForBand(b health.Band) model.Severity {
	if b == health.Critical || b == health.Unhealthy {
		return model.SevCritical
	}
	return model.SevWarning
}

func counterEvents(p, g *model.GPU, now time.Time) []model.Event {
	var out []model.Event
	src := deviceSource(g)
	inc := func(a, b metric.Opt[uint64]) uint64 {
		if a.OK && b.OK && b.V > a.V {
			return b.V - a.V
		}
		return 0
	}
	pc, cc := p.Counters, g.Counters
	if n := inc(pc.ECCUncorrectedVolatile, cc.ECCUncorrectedVolatile); n > 0 {
		out = append(out, ev(now, "ecc_uncorrectable", model.SevCritical, g, src, "%d new uncorrectable ECC error(s)", n))
	}
	if n := inc(pc.ECCCorrectedVolatile, cc.ECCCorrectedVolatile); n > 0 {
		out = append(out, ev(now, "ecc_corrected", model.SevInfo, g, src, "%d new corrected ECC error(s)", n))
	}
	if n := inc(pc.RemappedUncorrectable, cc.RemappedUncorrectable) + inc(pc.RemappedCorrectable, cc.RemappedCorrectable); n > 0 {
		out = append(out, ev(now, "row_remap", model.SevWarning, g, src, "%d memory row(s) remapped", n))
	}
	if n := inc(pc.RetiredPagesDBE, cc.RetiredPagesDBE) + inc(pc.RetiredPagesSBE, cc.RetiredPagesSBE); n > 0 {
		out = append(out, ev(now, "pages_retired", model.SevWarning, g, src, "%d memory page(s) retired", n))
	}
	if cc.RemapFailure.OK && cc.RemapFailure.V && (!pc.RemapFailure.OK || !pc.RemapFailure.V) {
		out = append(out, ev(now, "row_remap_failure", model.SevCritical, g, src, "Row remapping failure"))
	}
	if n := inc(pc.PCIeFatalErrors, cc.PCIeFatalErrors); n > 0 {
		out = append(out, ev(now, "pcie_fatal", model.SevCritical, g, src, "%d fatal PCIe error(s)", n))
	}
	if cc.RecoveryAction.OK && cc.RecoveryAction.V != "none" && cc.RecoveryAction != pc.RecoveryAction {
		out = append(out, ev(now, "recovery_action", model.SevCritical, g, src, "Driver recommends recovery action: %s", cc.RecoveryAction.V))
	}
	return out
}

var throttleGroups = []struct {
	name     string
	mask     gpu.ThrottleReasons
	severity model.Severity
	label    string
}{
	{"thermal", gpu.ThrottleThermal, model.SevWarning, "thermal throttling"},
	{"hardware", gpu.ThrottleHWSlowdown | gpu.ThrottleHWPowerBrake, model.SevWarning, "hardware slowdown"},
	{"power", gpu.ThrottleSWPowerCap | gpu.ThrottleBoardLimit, model.SevInfo, "power capping"},
}

func (d *Detector) throttleEvents(g *model.GPU, now time.Time) []model.Event {
	if !g.Sample.Throttle.OK {
		return nil
	}
	states := d.throttle[g.Device.ID]
	if states == nil {
		states = map[string]*debounce{}
		d.throttle[g.Device.ID] = states
	}
	var out []model.Event
	for _, grp := range throttleGroups {
		st := states[grp.name]
		if st == nil {
			st = &debounce{}
			states[grp.name] = st
		}
		bits := g.Sample.Throttle.V & grp.mask
		if bits != 0 {
			st.offSince = time.Time{}
			if st.onSince.IsZero() {
				st.onSince = now
			}
			st.reasons = bits
			if !st.active && now.Sub(st.onSince) >= throttleOnAfter {
				st.active = true
				detail := ""
				if grp.name == "thermal" && g.Sample.TempC.OK {
					detail = fmt.Sprintf(" at %.0f°C", g.Sample.TempC.V)
				}
				if grp.name == "power" && g.Sample.PowerW.OK {
					detail = fmt.Sprintf(" at %.0f W", g.Sample.PowerW.V)
				}
				e := ev(now, "throttle_start", grp.severity, g, deviceSource(g), "%s started%s (%s)", upperFirst(grp.label), detail, bits)
				e.Attrs = map[string]string{"group": grp.name}
				out = append(out, e)
			}
		} else {
			st.onSince = time.Time{}
			if st.active {
				if st.offSince.IsZero() {
					st.offSince = now
				}
				if now.Sub(st.offSince) >= throttleOffAfter {
					st.active = false
					e := ev(now, "throttle_end", model.SevInfo, g, deviceSource(g), "%s ended", upperFirst(grp.label))
					e.Attrs = map[string]string{"group": grp.name}
					out = append(out, e)
				}
			}
		}
	}
	return out
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func (d *Detector) linkEvents(p, g *model.GPU, now time.Time) []model.Event {
	if len(p.Links) == 0 || len(g.Links) == 0 {
		return nil
	}
	prev := make(map[int]gpu.Link, len(p.Links))
	for _, l := range p.Links {
		prev[l.Index] = l
	}
	var out []model.Event
	src := deviceSource(g)
	for _, l := range g.Links {
		pl, ok := prev[l.Index]
		if !ok {
			continue
		}
		if pl.State == gpu.LinkActive && (l.State == gpu.LinkInactive || l.State == gpu.LinkDisabled) {
			out = append(out, ev(now, "nvlink_down", model.SevWarning, g, src, "Link %d went %s", l.Index, l.State))
		} else if pl.State != gpu.LinkActive && pl.State != gpu.LinkSleep && l.State == gpu.LinkActive {
			out = append(out, ev(now, "nvlink_up", model.SevInfo, g, src, "Link %d is active", l.Index))
		}
		if l.ErrorTotal() > pl.ErrorTotal() {
			key := fmt.Sprintf("%s/%d", g.Device.ID, l.Index)
			if now.Sub(d.linkErrAt[key]) >= linkErrInterval {
				d.linkErrAt[key] = now
				out = append(out, ev(now, "nvlink_errors", model.SevWarning, g, src, "Link %d error counters increased by %d", l.Index, l.ErrorTotal()-pl.ErrorTotal()))
			}
		}
	}
	return out
}

func partitionEvents(p, g *model.GPU, now time.Time) []model.Event {
	prev := map[gpu.ID]gpu.Partition{}
	for _, pt := range p.Partitions {
		prev[pt.ID] = pt
	}
	var out []model.Event
	src := deviceSource(g)
	for _, pt := range g.Partitions {
		if _, ok := prev[pt.ID]; !ok {
			out = append(out, ev(now, "mig_created", model.SevInfo, g, src, "MIG instance created: %s (GI %d)", pt.Profile, pt.InstanceID.V))
		}
		delete(prev, pt.ID)
	}
	for _, pt := range prev {
		out = append(out, ev(now, "mig_destroyed", model.SevInfo, g, src, "MIG instance destroyed: %s (GI %d)", pt.Profile, pt.InstanceID.V))
	}
	if p.Device.MIG.Enabled != g.Device.MIG.Enabled {
		state := "disabled"
		if g.Device.MIG.Enabled {
			state = "enabled"
		}
		out = append(out, ev(now, "mig_mode", model.SevInfo, g, src, "MIG mode %s", state))
	}
	return out
}

type procKey struct {
	pid int
	dev gpu.ID
}

func processEvents(prev, cur *model.Snapshot, now time.Time) []model.Event {
	before := map[procKey]model.Process{}
	for _, p := range prev.Processes {
		before[procKey{p.PID, p.DeviceID}] = p
	}
	after := map[procKey]model.Process{}
	for _, p := range cur.Processes {
		after[procKey{p.PID, p.DeviceID}] = p
	}
	var started, stopped []model.Process
	for k, p := range after {
		if _, ok := before[k]; !ok {
			started = append(started, p)
		}
	}
	for k, p := range before {
		if _, ok := after[k]; !ok {
			stopped = append(stopped, p)
		}
	}
	var out []model.Event
	emit := func(list []model.Process, kind, verb string) {
		sort.Slice(list, func(i, j int) bool { return list[i].PID < list[j].PID })
		if len(list) > processBurst {
			out = append(out, model.Event{Time: now, Kind: kind, Severity: model.SevInfo, DeviceIndex: -1, Source: metric.SourceDerived,
				Message: fmt.Sprintf("%d GPU processes %s", len(list), verb)})
			return
		}
		for _, p := range list {
			name := p.Name
			if name == "" {
				name = "pid " + fmt.Sprint(p.PID)
			}
			e := model.Event{Time: now, Kind: kind, Severity: model.SevInfo, DeviceID: p.DeviceID, DeviceIndex: p.DeviceIndex,
				Source: metric.SourceDerived, Message: fmt.Sprintf("Process %s (%d) %s", name, p.PID, verb)}
			if p.Kube.PodName != "" {
				e.Attrs = map[string]string{"pod": p.Kube.Namespace + "/" + p.Kube.PodName}
			}
			out = append(out, e)
		}
	}
	emit(started, "process_start", "started")
	emit(stopped, "process_stop", "stopped")
	return out
}

func collectorEvents(prev, cur *model.Snapshot, now time.Time) []model.Event {
	before := map[string]model.CollectorStatus{}
	for _, c := range prev.Collectors {
		before[c.Name] = c
	}
	var out []model.Event
	for _, c := range cur.Collectors {
		p, ok := before[c.Name]
		if !ok || p.Runs == 0 {
			continue
		}
		if p.Healthy && !c.Healthy {
			out = append(out, model.Event{Time: now, Kind: "collector_failed", Severity: model.SevWarning, DeviceIndex: -1,
				Source: metric.SourceDerived, Message: fmt.Sprintf("Collector %s failing: %s", c.Name, c.LastError)})
		} else if !p.Healthy && c.Healthy {
			out = append(out, model.Event{Time: now, Kind: "collector_recovered", Severity: model.SevInfo, DeviceIndex: -1,
				Source: metric.SourceDerived, Message: fmt.Sprintf("Collector %s recovered", c.Name)})
		}
	}
	return out
}

// Alerts computes the currently active alerts from a snapshot: warning and
// critical health reasons, failing collectors and inference servers whose
// KV cache is full while requests wait. Since is when the
// condition was first observed by this detector.
func (d *Detector) Alerts(s *model.Snapshot) []model.Alert {
	seen := map[string]bool{}
	var out []model.Alert
	add := func(a model.Alert) {
		seen[a.Key] = true
		since, ok := d.alertSince[a.Key]
		if !ok {
			since = s.Time
			d.alertSince[a.Key] = since
		}
		a.Since = since
		out = append(out, a)
	}
	for _, g := range s.GPUs {
		for _, r := range g.Health.Reasons {
			if r.Severity == "info" {
				continue
			}
			sev := model.SevWarning
			if r.Severity == "critical" {
				sev = model.SevCritical
			}
			add(model.Alert{Key: string(g.Device.ID) + "/" + r.Code, Severity: sev, DeviceID: g.Device.ID,
				DeviceIndex: g.Device.Index, Title: r.Text, Detail: fmt.Sprintf("-%d health", r.Penalty)})
		}
	}
	for _, c := range s.Collectors {
		if !c.Healthy && c.Runs > 0 {
			add(model.Alert{Key: "collector/" + c.Name, Severity: model.SevWarning, DeviceIndex: -1,
				Title: "Collector " + c.Name + " failing", Detail: c.LastError})
		}
	}
	for _, sv := range s.Inference {
		mt := sv.Metrics
		if !sv.Up || !mt.KVCacheUsage.OK || mt.KVCacheUsage.V < 0.95 || !mt.Waiting.OK || mt.Waiting.V <= 0 {
			continue
		}
		detail := fmt.Sprintf("%.0f%% used, %.0f waiting", mt.KVCacheUsage.V*100, mt.Waiting.V)
		if mt.PreemptionsPerSec.OK && mt.PreemptionsPerSec.V > 0 {
			detail += fmt.Sprintf(", %.1f preemptions/s", mt.PreemptionsPerSec.V)
		}
		add(model.Alert{Key: "inference/" + sv.URL + "/kv", Severity: model.SevWarning, DeviceIndex: -1,
			Title: "Inference " + sv.Name + " KV cache full", Detail: detail})
	}
	for k := range d.alertSince {
		if !seen[k] {
			delete(d.alertSince, k)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() > out[j].Severity.Rank()
		}
		return out[i].DeviceIndex < out[j].DeviceIndex
	})
	return out
}
