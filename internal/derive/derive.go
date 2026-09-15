// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package derive computes gputop-derived metrics: per-GPU state and
// efficiency, fleet summaries, utilization imbalance and health scores.
//
// Every value produced here has source "derived" and is documented in
// docs/derived-metrics.md. Nothing here fabricates precision: when inputs
// are missing the outputs are unavailable.
package derive

import (
	"math"
	"sort"
	"time"

	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/health"
	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
)

// Options configure derivation.
type Options struct {
	IdleThreshold   float64       // percent
	IdleAfter       time.Duration // idle duration before flagging allocated GPUs
	OutlierMinDelta float64       // percentage points
	Window          time.Duration // efficiency / averages window
}

// DefaultOptions mirrors the configuration defaults.
func DefaultOptions() Options {
	return Options{IdleThreshold: 5, IdleAfter: 5 * time.Minute, OutlierMinDelta: 20, Window: 5 * time.Minute}
}

// BusyThreshold is the utilization at which a GPU is "busy".
const BusyThreshold = 60.0

// Minimum window coverage before efficiency is reported.
const minCoverage = time.Minute

// Tracker holds the rolling state needed for derived metrics. Not safe for
// concurrent use.
type Tracker struct {
	opts Options
	gpus map[gpu.ID]*track
}

type point struct {
	t         time.Time
	util      float32
	membw     float32
	vram      float32
	throttled bool
	hasUtil   bool
	hasMembw  bool
	hasVRAM   bool
}

type counterMark struct {
	t        time.Time
	counters gpu.HealthCounters
	linkErrs map[int]uint64
}

type track struct {
	points     []point // ring
	head, size int
	firstSeen  time.Time
	lastActive time.Time
	marks      []counterMark
	prevLinks  map[int]gpu.Link
	prevLinkAt time.Time
}

const ringCap = 1024

// NewTracker creates a tracker.
func NewTracker(o Options) *Tracker {
	def := DefaultOptions()
	if o.Window <= 0 {
		o.Window = def.Window
	}
	if o.IdleAfter <= 0 {
		o.IdleAfter = def.IdleAfter
	}
	if o.OutlierMinDelta <= 0 {
		o.OutlierMinDelta = def.OutlierMinDelta
	}
	if o.IdleThreshold <= 0 {
		o.IdleThreshold = def.IdleThreshold
	}
	return &Tracker{opts: o, gpus: map[gpu.ID]*track{}}
}

func (t *track) push(p point) {
	if t.points == nil {
		t.points = make([]point, ringCap)
	}
	t.points[t.head] = p
	t.head = (t.head + 1) % ringCap
	if t.size < ringCap {
		t.size++
	}
}

// each iterates points newer than since, oldest first.
func (t *track) each(since time.Time, f func(point)) {
	for i := 0; i < t.size; i++ {
		p := t.points[(t.head-t.size+i+ringCap)%ringCap]
		if !p.t.Before(since) {
			f(p)
		}
	}
}

// Apply fills derived fields, link rates and health on s in place. s must
// not have been published yet. xids provides recent Xid events per device.
func (tr *Tracker) Apply(s *model.Snapshot, xids map[gpu.ID][]health.XID) {
	now := s.Time
	seen := map[gpu.ID]bool{}
	for i := range s.GPUs {
		g := &s.GPUs[i]
		seen[g.Device.ID] = true
		t := tr.gpus[g.Device.ID]
		if t == nil {
			t = &track{firstSeen: now, lastActive: now}
			tr.gpus[g.Device.ID] = t
		}
		tr.applyGPU(g, t, now, xids[g.Device.ID])
	}
	for id := range tr.gpus {
		if !seen[id] {
			delete(tr.gpus, id)
		}
	}
	tr.imbalance(s)
	s.Fleet = Fleet(s)
}

func (tr *Tracker) applyGPU(g *model.GPU, t *track, now time.Time, xids []health.XID) {
	d := &g.Derived
	smp := g.Sample
	d.Allocated = g.Processes > 0

	thr := smp.Throttle.OK && smp.Throttle.V&gpu.ThrottlePerformance != 0
	d.Throttled = thr
	d.VRAMFraction = smp.VRAMUsedFraction()
	if smp.MemTotal.OK && smp.MemUsed.OK {
		d.VRAMHeadroom = metric.Some(smp.MemTotal.V - min(smp.MemUsed.V, smp.MemTotal.V))
	}
	if smp.PowerW.OK && smp.PowerLimitW.OK && smp.PowerLimitW.V > 0 {
		d.PowerFraction = metric.Some(smp.PowerW.V / smp.PowerLimitW.V)
	}

	switch {
	case !g.Available:
		d.State = model.StateUnavailable
	case !smp.UtilPercent.OK:
		d.State = model.StateUnknown
	case smp.UtilPercent.V >= BusyThreshold:
		d.State = model.StateBusy
	case smp.UtilPercent.V >= tr.opts.IdleThreshold:
		d.State = model.StateActive
	default:
		d.State = model.StateIdle
	}

	if g.Available {
		p := point{t: now, throttled: thr}
		if smp.UtilPercent.OK {
			p.util, p.hasUtil = float32(smp.UtilPercent.V), true
			if smp.UtilPercent.V >= tr.opts.IdleThreshold {
				t.lastActive = now
			}
		}
		if smp.MemBandwidthPercent.OK {
			p.membw, p.hasMembw = float32(smp.MemBandwidthPercent.V), true
		}
		if d.VRAMFraction.OK {
			p.vram, p.hasVRAM = float32(d.VRAMFraction.V*100), true
		}
		t.push(p)
	}
	if d.State == model.StateIdle || d.State == model.StateActive && smp.UtilPercent.V < tr.opts.IdleThreshold {
		d.IdleFor = now.Sub(t.lastActive)
	}
	d.IdleAllocated = d.Allocated && d.State == model.StateIdle && d.IdleFor >= tr.opts.IdleAfter

	tr.efficiency(g, t, now)
	tr.linkRates(g, t, now)
	tr.health(g, t, now, xids)
}

func (tr *Tracker) efficiency(g *model.GPU, t *track, now time.Time) {
	d := &g.Derived
	since := now.Add(-tr.opts.Window)
	var n, nb, nv, nt int
	var su, sb, sv float64
	var oldest time.Time
	t.each(since, func(p point) {
		if oldest.IsZero() {
			oldest = p.t
		}
		if p.hasUtil {
			su += float64(p.util)
			n++
		}
		if p.hasMembw {
			sb += float64(p.membw)
			nb++
		}
		if p.hasVRAM {
			sv += float64(p.vram)
			nv++
		}
		if p.throttled {
			nt++
		}
	})
	if n > 0 {
		d.UtilAvg = metric.Some(su / float64(n))
	}
	e := model.Efficiency{Window: tr.opts.Window}
	switch {
	case !g.Available:
		e.Note = "unavailable"
	case !d.Allocated:
		e.Note = "not allocated (no GPU processes)"
	case n == 0:
		e.Note = "utilization not supported"
	case now.Sub(oldest) < minCoverage:
		e.Note = "collecting (needs 1m of samples)"
	default:
		e.ComputePct = su / float64(n)
		if nb > 0 {
			e.MemBandwidthPct = sb / float64(nb)
		}
		if nv > 0 {
			e.VRAMPct = sv / float64(nv)
		}
		e.ThrottleFrac = float64(nt) / float64(n)
		wCompute, wBandwidth, wVRAM := 0.60, 0.25, 0.15
		if nb == 0 { // redistribute weight of missing inputs
			wCompute += wBandwidth * 0.6
			wVRAM += wBandwidth * 0.4
			wBandwidth = 0
		}
		if nv == 0 {
			wCompute += wVRAM
			wVRAM = 0
		}
		raw := (wCompute*e.ComputePct + wBandwidth*e.MemBandwidthPct + wVRAM*e.VRAMPct) * (1 - 0.5*e.ThrottleFrac)
		score := int(math.Round(raw/5) * 5) // 5-point steps: avoid false precision
		e.Score = metric.Some(max(0, min(100, score)))
		e.Grade = Grade(score)
	}
	d.Efficiency = e
}

// Grade names an efficiency score.
func Grade(score int) string {
	switch {
	case score >= 80:
		return "high"
	case score >= 50:
		return "moderate"
	case score >= 20:
		return "low"
	}
	return "very low"
}

func (tr *Tracker) linkRates(g *model.GPU, t *track, now time.Time) {
	if len(g.Links) == 0 {
		return
	}
	links := make([]gpu.Link, len(g.Links))
	copy(links, g.Links)
	dt := now.Sub(t.prevLinkAt).Seconds()
	var tx, rx float64
	haveRate := false
	active := 0
	for i := range links {
		l := &links[i]
		if l.State == gpu.LinkActive {
			active++
		}
		if prev, ok := t.prevLinks[l.Index]; ok && dt > 0 {
			if l.TxBytes.OK && prev.TxBytes.OK && l.TxBytes.V >= prev.TxBytes.V {
				l.TxBps = metric.Some(float64(l.TxBytes.V-prev.TxBytes.V) / dt)
				tx += l.TxBps.V
				haveRate = true
			}
			if l.RxBytes.OK && prev.RxBytes.OK && l.RxBytes.V >= prev.RxBytes.V {
				l.RxBps = metric.Some(float64(l.RxBytes.V-prev.RxBytes.V) / dt)
				rx += l.RxBps.V
			}
		}
	}
	g.Derived.LinksActive = active
	if haveRate {
		g.Derived.NVLinkTxBps, g.Derived.NVLinkRxBps = metric.Some(tx), metric.Some(rx)
	}
	// Counters are refreshed on the normal tier while this runs every fast
	// tick. Only advance the baseline when counters changed; otherwise carry
	// the previously computed rates.
	changed := t.prevLinks == nil
	for _, l := range g.Links {
		if p, ok := t.prevLinks[l.Index]; !ok || p.TxBytes != l.TxBytes || p.RxBytes != l.RxBytes {
			changed = true
			break
		}
	}
	if changed {
		t.prevLinks = make(map[int]gpu.Link, len(links))
		for _, l := range links {
			t.prevLinks[l.Index] = l
		}
		t.prevLinkAt = now
		g.Links = links
		return
	}
	tx, rx, haveRate = 0, 0, false
	for i := range links {
		if p, ok := t.prevLinks[links[i].Index]; ok {
			links[i].TxBps, links[i].RxBps = p.TxBps, p.RxBps
			if p.TxBps.OK {
				tx += p.TxBps.V
				haveRate = true
			}
			rx += p.RxBps.Or(0)
		}
	}
	if haveRate {
		g.Derived.NVLinkTxBps, g.Derived.NVLinkRxBps = metric.Some(tx), metric.Some(rx)
	}
	g.Links = links
}

const (
	markEvery   = time.Minute
	baselineAge = 10 * time.Minute
	maxMarks    = 16
)

func (tr *Tracker) health(g *model.GPU, t *track, now time.Time, xids []health.XID) {
	if g.Available && !g.Counters.Time.IsZero() {
		if len(t.marks) == 0 || now.Sub(t.marks[len(t.marks)-1].t) >= markEvery {
			le := map[int]uint64{}
			for _, l := range g.Links {
				le[l.Index] = l.ErrorTotal()
			}
			t.marks = append(t.marks, counterMark{t: now, counters: g.Counters, linkErrs: le})
			if len(t.marks) > maxMarks {
				t.marks = t.marks[len(t.marks)-maxMarks:]
			}
		}
	}
	in := health.Input{
		Now: now, Available: g.Available, Error: g.Error, Device: g.Device, Sample: g.Sample,
		Counters: g.Counters, Links: g.Links, XIDs: xids,
	}
	if len(t.marks) > 0 {
		base := t.marks[0]
		for _, m := range t.marks {
			if now.Sub(m.t) >= baselineAge {
				base = m
			}
		}
		in.Baseline = &base.counters
		in.LinkErrBaseline = base.linkErrs
	}
	g.Health = health.Score(in)
}

// imbalance finds the largest cohort of GPUs sharing a workload and flags
// low-utilization outliers using the median absolute deviation.
func (tr *Tracker) imbalance(s *model.Snapshot) {
	cohorts := map[string][]int{}
	for i, g := range s.GPUs {
		keys := map[string]bool{}
		for _, p := range s.Processes {
			if p.DeviceID == g.Device.ID {
				_, name := p.WorkloadKey()
				keys[name] = true
			}
		}
		for k := range keys {
			cohorts[k] = append(cohorts[k], i)
		}
	}
	var bestKey string
	var best []int
	for k, members := range cohorts {
		if len(members) > len(best) || len(members) == len(best) && k < bestKey {
			bestKey, best = k, members
		}
	}
	label := bestKey
	if len(best) < 2 {
		best, label = nil, "all active GPUs"
		for i, g := range s.GPUs {
			if g.Available && g.Derived.State != model.StateIdle && g.Derived.UtilAvg.OK {
				best = append(best, i)
			}
		}
		if len(best) < 3 {
			return
		}
	}

	type m struct {
		idx  int
		util float64
	}
	var vals []m
	for _, i := range best {
		g := s.GPUs[i]
		// Short-window average smooths training-step rhythm.
		u := g.Derived.UtilAvg
		if !u.OK || !g.Available {
			continue
		}
		vals = append(vals, m{i, u.V})
	}
	if len(vals) < 2 {
		return
	}
	sort.Slice(vals, func(a, b int) bool { return vals[a].util < vals[b].util })
	utils := make([]float64, len(vals))
	var sum float64
	for i, v := range vals {
		utils[i] = v.util
		sum += v.util
	}
	med := median(utils)
	mean := sum / float64(len(utils))
	var sq float64
	dev := make([]float64, len(utils))
	for i, u := range utils {
		sq += (u - mean) * (u - mean)
		dev[i] = math.Abs(u - med)
	}
	mad := median(dev)
	im := model.Imbalance{
		Valid: true, Cohort: label, Members: len(vals), Median: med,
		Min: utils[0], Max: utils[len(utils)-1], Spread: utils[len(utils)-1] - utils[0],
		StdDev:  math.Sqrt(sq / float64(len(utils))),
		Slowest: s.GPUs[vals[0].idx].Device.ID, Fastest: s.GPUs[vals[len(vals)-1].idx].Device.ID,
	}
	threshold := math.Max(tr.opts.OutlierMinDelta, 3*1.4826*mad)
	if med >= 30 && len(vals) >= 3 {
		for _, v := range vals {
			if med-v.util >= threshold {
				g := &s.GPUs[v.idx]
				g.Derived.Outlier = true
				g.Derived.OutlierDelta = metric.Some(v.util - med)
				im.Outliers = append(im.Outliers, g.Device.ID)
			}
		}
	}
	s.Fleet.Imbalance = im
}

func median(sorted []float64) float64 {
	c := append([]float64(nil), sorted...)
	sort.Float64s(c)
	n := len(c)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

// Fleet computes the fleet summary from per-GPU values. The imbalance
// already stored in s.Fleet is preserved.
func Fleet(s *model.Snapshot) model.Fleet {
	f := model.Fleet{GPUs: len(s.GPUs), Processes: len(s.Processes), Imbalance: s.Fleet.Imbalance}
	var utilSum, tempSum, healthSum float64
	var utilN, tempN, healthN int
	var power, limit float64
	var powerOK, limitOK bool
	var vramUsed, vramTotal uint64
	vramOK := false
	unused, unusedOK := 0.0, false
	healthMin := 101
	for _, g := range s.GPUs {
		d := g.Derived
		if !g.Available {
			f.Unavailable++
		} else {
			f.Available++
		}
		if d.Allocated {
			f.Allocated++
			if d.UtilAvg.OK && g.Available {
				unused += 1 - d.UtilAvg.V/100
				unusedOK = true
			}
		}
		switch d.State {
		case model.StateBusy:
			f.Busy++
			f.Active++
		case model.StateActive:
			f.Active++
		case model.StateIdle:
			f.Idle++
		}
		if d.IdleAllocated {
			f.IdleAllocated++
		}
		if d.Throttled {
			f.Throttled++
		}
		if !g.Available {
			healthMin = 0
			continue
		}
		smp := g.Sample
		if smp.UtilPercent.OK {
			utilSum += smp.UtilPercent.V
			utilN++
		}
		if smp.PowerW.OK {
			power += smp.PowerW.V
			powerOK = true
		}
		if smp.PowerLimitW.OK {
			limit += smp.PowerLimitW.V
			limitOK = true
		}
		if smp.TempC.OK {
			tempSum += smp.TempC.V
			tempN++
			if !f.TempMaxC.OK || smp.TempC.V > f.TempMaxC.V {
				f.TempMaxC = metric.Some(smp.TempC.V)
			}
		}
		if smp.MemUsed.OK && smp.MemTotal.OK {
			vramUsed += smp.MemUsed.V
			vramTotal += smp.MemTotal.V
			vramOK = true
		}
		healthSum += float64(g.Health.Score)
		healthN++
		healthMin = min(healthMin, g.Health.Score)
	}
	if utilN > 0 {
		f.UtilAvg = metric.Some(utilSum / float64(utilN))
	}
	if tempN > 0 {
		f.TempAvgC = metric.Some(tempSum / float64(tempN))
	}
	if powerOK {
		f.PowerW = metric.Some(power)
	}
	if limitOK {
		f.PowerLimitW = metric.Some(limit)
	}
	if vramOK {
		f.VRAMUsed, f.VRAMTotal = metric.Some(vramUsed), metric.Some(vramTotal)
		if vramTotal > 0 {
			f.VRAMFraction = metric.Some(float64(vramUsed) / float64(vramTotal))
		}
	}
	if len(s.GPUs) > 0 {
		if healthN > 0 {
			f.HealthAvg = metric.Some(healthSum / float64(len(s.GPUs)))
		} else {
			f.HealthAvg = metric.Some(0.0)
		}
		f.HealthMin = metric.Some(min(healthMin, 100))
	}
	if unusedOK {
		f.UnusedAllocated = metric.Some(unused)
	}
	return f
}
