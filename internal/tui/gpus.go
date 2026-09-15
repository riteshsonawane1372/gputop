// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

type gpusState struct {
	detail    bool
	scroll    int
	maxScroll int
}

func keysGPUs(m *Model, a keymap.Action) (bool, tea.Cmd) {
	st := &m.gpus
	if st.detail {
		switch a {
		case keymap.Back:
			st.detail, st.scroll = false, 0
		case keymap.Up:
			st.scroll = max(0, st.scroll-1)
		case keymap.Down:
			st.scroll = min(st.maxScroll, st.scroll+1)
		case keymap.PageUp:
			st.scroll = max(0, st.scroll-10)
		case keymap.PageDown:
			st.scroll = min(st.maxScroll, st.scroll+10)
		case keymap.PrevGPU, keymap.Left:
			m.moveGPU(-1)
		case keymap.NextGPU, keymap.Right:
			m.moveGPU(1)
		default:
			return false, nil
		}
		return true, nil
	}
	if a == keymap.Select {
		st.detail, st.scroll = true, 0
		return true, nil
	}
	return keysGPUSelect(m, a)
}

func hintsGPUs(m *Model) []hint {
	if m.gpus.detail {
		return []hint{{keymap.Back, "back"}, {keymap.NextGPU, "next GPU"}, {keymap.Down, "scroll"}, {keymap.History, "history"}}
	}
	return []hint{{keymap.Up, "select"}, {keymap.Select, "full detail"}, {keymap.History, "history"}}
}

func viewGPUs(m *Model, w, h int) widgets.Block {
	g, _ := m.selected()
	if g == nil {
		return nil
	}
	if m.gpus.detail {
		cols := 1
		switch {
		case w >= 165:
			cols = 3
		case w >= 105:
			cols = 2
		}
		secs := m.gpuSections(g, w/cols-2, true)
		block, maxScroll := m.flowSections(secs, w, h, cols, m.gpus.scroll, "")
		m.gpus.maxScroll = maxScroll
		return block
	}
	s := m.view()
	if w < 110 {
		listH := min(len(s.GPUs)+3, h/2)
		list := m.gpuList(s, w, listH)
		secs := m.gpuSections(g, w-2, false)
		detail, _ := m.flowSections(secs, w, h-listH, 1, 0, "")
		return widgets.VJoin(list, detail)
	}
	ws := widgets.Split(w, 9, 11)
	list := m.gpuList(s, ws[0], h)
	cols := 1
	if ws[1] >= 110 {
		cols = 2
	}
	secs := m.gpuSections(g, ws[1]/cols-2, false)
	detail, _ := m.flowSections(secs, ws[1], h, cols, 0, "")
	return widgets.HJoin(m.th, list, detail)
}

func (m *Model) gpuList(s *model.Snapshot, w, h int) widgets.Block {
	th := m.th
	tb := &widgets.Table{
		Columns: []widgets.Column{
			{Title: "#", Width: 2, Align: widgets.Right},
			{Title: "NAME", Width: 14, Min: 6, Flex: true, Priority: 3},
			{Title: "UTIL", Width: 12, Min: 8},
			{Title: "VRAM", Width: 10, Min: 8},
			{Title: "TEMP", Width: 5, Align: widgets.Right, Priority: 1},
			{Title: "HLTH", Width: 4, Align: widgets.Right, Priority: 2},
			{Title: "STATE", Width: 8},
		},
		SortCol: -1,
	}
	widths := tb.Layout(w - 2)
	_, tb.Selected = m.selected()
	for i := range s.GPUs {
		g := &s.GPUs[i]
		frac := g.Derived.VRAMFraction
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, fmt.Sprint(g.Device.Index)),
			widgets.C(th.Text, shortName(g.Device.Name)),
			widgets.R(m.pctBar(g.Sample.UtilPercent, widths[2])),
			widgets.R(m.pctBar(fracPct(frac.V, frac.OK), widths[3])),
			widgets.R(m.naOr(m.temp(g.Sample.TempC), th.Level(g.Sample.TempC.V, 80, 88))),
			widgets.C(m.healthStyle(g.Health.Score), fmt.Sprint(g.Health.Score)),
			widgets.R(m.stateCell(g)),
		})
	}
	return widgets.Box(th, widgets.BoxOpts{Title: "GPUs", Focus: true}, w, h, tb.Render(th, w-2, h-2))
}

// gpuSections builds the detail view of one GPU.
func (m *Model) gpuSections(g *model.GPU, cw int, full bool) []section {
	th := m.th
	s := m.view()
	d := g.Device
	smp := g.Sample
	lw := 15
	barW := max(10, min(30, cw-lw-14))
	kv := func(label, value string) string { return m.kv(label, value, lw) }
	txt := func(v string) string { return m.naOr(v, th.Text) }
	str := func(v string) string {
		if v == "" {
			return th.NA.Render(na)
		}
		return th.Text.Render(v)
	}
	var secs []section

	title := fmt.Sprintf("GPU %d · %s", d.Index, shortName(d.Name))
	sys := gpu.SystemInfo{}
	for _, p := range s.Providers {
		if p.Name == g.Provider {
			sys = p.System
		}
	}
	ident := []string{
		kv("Name", th.Bold.Render(d.Name)),
		kv("UUID", th.Text.Render(string(d.ID))),
		kv("State", m.stateCell(g)+func() string {
			if g.Error != "" {
				return th.Crit.Render("  " + g.Error)
			}
			return ""
		}()),
		kv("Architecture", str(d.Architecture)),
		kv("Compute cap.", str(d.ComputeCapability)),
		kv("PCI bus", str(d.PCI.BusID)),
		kv("NUMA node", txt(optI(d.NUMANode, "%d"))),
	}
	if full {
		ident = append(ident,
			kv("Brand", str(d.Brand)),
			kv("Serial", str(d.Serial)),
			kv("Part number", str(d.PartNumber)),
			kv("VBIOS", str(d.FirmwareVersion)),
		)
	}
	ident = append(ident,
		kv("Driver", str(sys.DriverVersion)),
		kv("CUDA (max)", str(sys.RuntimeVersion)),
		kv("Persistence", txt(optBool(d.PersistenceMode, "enabled", "disabled"))),
		kv("Compute mode", str(d.ComputeMode)),
	)
	secs = append(secs, section{title, ident})

	eff := g.Derived.Efficiency
	effStr := th.NA.Render(eff.Note)
	if eff.Score.OK {
		effStr = th.Gradient(1-float64(eff.Score.V)/100).Render(fmt.Sprintf("%d", eff.Score.V)) + th.Dim.Render(" "+eff.Grade+" · 5m, derived")
	}
	compute := []string{
		kv("Utilization", m.pctBar(smp.UtilPercent, barW)),
		kv("Mem bandwidth", m.pctBar(smp.MemBandwidthPercent, barW)),
		kv("Encoder", txt(optPct(smp.EncoderPercent))+th.Dim.Render("  decoder ")+txt(optPct(smp.DecoderPercent))),
		kv("JPEG / OFA", txt(optPct(smp.JPEGPercent))+th.Dim.Render(" / ")+txt(optPct(smp.OFAPercent))),
		kv("Core clock", txt(optF(smp.ClockCoreMHz, "%.0f MHz"))+th.Dim.Render(" max ")+txt(optF(d.ClockCoreMaxMHz, "%.0f"))),
		kv("Mem clock", txt(optF(smp.ClockMemMHz, "%.0f MHz"))+th.Dim.Render(" max ")+txt(optF(d.ClockMemMaxMHz, "%.0f"))),
		kv("P-state", txt(optI(smp.PState, "P%d"))),
		kv("Avg util (5m)", txt(optPct(g.Derived.UtilAvg))),
		kv("Efficiency", effStr),
	}
	if g.Derived.Outlier {
		compute = append(compute, kv("Outlier", th.Warn.Render(fmt.Sprintf("%+.0fpp vs cohort median", g.Derived.OutlierDelta.V))))
	}
	if g.Derived.IdleAllocated {
		compute = append(compute, kv("Idle", th.Warn.Render("allocated but idle for "+fmtDuration(g.Derived.IdleFor))))
	}
	secs = append(secs, section{"Compute", compute})

	var procVRAM uint64
	var procs []model.Process
	for _, p := range s.Processes {
		if p.DeviceID == d.ID {
			procs = append(procs, p)
			procVRAM += p.MemUsed.Or(0)
		}
	}
	memLabel := na
	if smp.MemUsed.OK && smp.MemTotal.OK {
		memLabel = fmt.Sprintf("%s / %s", fmtBytes(smp.MemUsed.V), fmtBytes(smp.MemTotal.V))
	}
	secs = append(secs, section{"Memory", []string{
		kv("VRAM", m.fracBar(g.Derived.VRAMFraction.V, g.Derived.VRAMFraction.OK, fmt.Sprintf("%.0f%%", g.Derived.VRAMFraction.V*100), barW)),
		kv("Used / total", txt(memLabel)),
		kv("Free", txt(optBytes(smp.MemFree))),
		kv("Reserved", txt(optBytes(smp.MemReserved))),
		kv("Headroom", txt(optBytes(g.Derived.VRAMHeadroom))),
		kv("Processes", th.Text.Render(fmt.Sprintf("%d using %s", len(procs), fmtBytes(procVRAM)))),
		kv("ECC mode", txt(optBool(d.ECCEnabled, "enabled", "disabled"))),
	}})

	hotspot := th.NA.Render("N/A (not exposed by the driver API)")
	if d.Capabilities.Has(gpu.CapHotspotTemp) {
		hotspot = th.Text.Render("supported")
	}
	thermal := []string{
		kv("GPU temp", m.tempBar(smp.TempC, d, barW)),
		kv("Memory temp", m.naOr(m.temp(smp.MemTempC), th.Level(smp.MemTempC.V, d.MemTempMaxC.Or(95)-8, d.MemTempMaxC.Or(95)-3))),
		kv("Hotspot", hotspot),
		kv("Slowdown at", txt(m.temp(d.TempSlowdownC))+th.Dim.Render("  shutdown ")+txt(m.temp(d.TempShutdownC))),
		kv("Max operating", txt(m.temp(d.TempMaxOpC))),
		kv("Fan", txt(optPct(smp.FanPct))),
		kv("Thermal state", m.thermalState(g)),
	}
	secs = append(secs, section{"Thermals", thermal})

	var powerBar string
	if smp.PowerW.OK && smp.PowerLimitW.OK && smp.PowerLimitW.V > 0 {
		powerBar = m.fracBar(smp.PowerW.V/smp.PowerLimitW.V, true, fmt.Sprintf("%.0f / %.0f W", smp.PowerW.V, smp.PowerLimitW.V), barW+6)
	} else {
		powerBar = txt(optF(smp.PowerW, "%.0f W"))
	}
	reasons := th.NA.Render(na)
	if smp.Throttle.OK {
		if smp.Throttle.V&gpu.ThrottlePerformance == 0 {
			reasons = th.OK.Render("none")
			if smp.Throttle.V&gpu.ThrottleIdle != 0 {
				reasons = th.Muted.Render("idle")
			}
		} else {
			reasons = th.Warn.Render((smp.Throttle.V &^ gpu.ThrottleIdle).String())
		}
	}
	energy := na
	if smp.EnergyJ.OK {
		energy = fmt.Sprintf("%.1f kWh", smp.EnergyJ.V/3.6e6)
	}
	pw := []string{
		kv("Power", powerBar),
		kv("Default limit", txt(optF(d.PowerLimitDefaultW, "%.0f W"))+th.Dim.Render("  range ")+txt(optF(d.PowerLimitMinW, "%.0f"))+th.Dim.Render("–")+txt(optF(d.PowerLimitMaxW, "%.0f W"))),
		kv("Energy", txt(energy)+th.Dim.Render(" since driver load")),
		kv("Throttle", reasons),
	}
	if full {
		pw = append(pw,
			kv("Power viol.", txt(durOpt(g.Counters.ViolationPower))),
			kv("Thermal viol.", txt(durOpt(g.Counters.ViolationThermal))),
		)
	}
	secs = append(secs, section{"Power", pw})

	c := g.Counters
	pcie := []string{
		kv("Link", m.pcieLink(g)),
		kv("TX / RX", txt(optRate(smp.PCIeTxBps))+th.Dim.Render(" / ")+txt(optRate(smp.PCIeRxBps))),
		kv("Replays", txt(optU(c.PCIeReplays))),
		kv("AER errors", th.Dim.Render("corr ")+txt(optU(c.PCIeCorrectableErrors))+th.Dim.Render(" nonfatal ")+txt(optU(c.PCIeNonFatalErrors))+th.Dim.Render(" fatal ")+txt(optU(c.PCIeFatalErrors))),
	}
	secs = append(secs, section{"PCIe", pcie})

	if d.LinkCount > 0 {
		var glyphs strings.Builder
		errs := uint64(0)
		for _, l := range g.Links {
			switch l.State {
			case gpu.LinkActive:
				glyphs.WriteString(th.OK.Render("●"))
			case gpu.LinkSleep:
				glyphs.WriteString(th.Muted.Render("◌"))
			default:
				glyphs.WriteString(th.Crit.Render("○"))
			}
			errs += l.ErrorTotal()
		}
		errStyle := th.Text
		if errs > 0 {
			errStyle = th.Warn
		}
		secs = append(secs, section{"NVLink", []string{
			kv("Links", th.Text.Render(fmt.Sprintf("%d/%d active ", g.Derived.LinksActive, d.LinkCount))+glyphs.String()),
			kv("TX / RX", txt(optRate(g.Derived.NVLinkTxBps))+th.Dim.Render(" / ")+txt(optRate(g.Derived.NVLinkRxBps))),
			kv("Errors", errStyle.Render(fmt.Sprint(errs))+th.Dim.Render(" (replay+recovery+CRC)")),
		}})
	}

	hl := []string{
		kv("Score", m.healthStyle(g.Health.Score).Bold(true).Render(fmt.Sprint(g.Health.Score))+th.Dim.Render(" "+string(g.Health.Band)+" · gputop-derived")),
	}
	for _, r := range g.Health.Reasons {
		st, _ := m.sevStyle(model.Severity(r.Severity))
		hl = append(hl, st.Render(fmt.Sprintf("  −%-3d", r.Penalty))+th.Text.Render(r.Text))
	}
	hl = append(hl,
		kv("ECC volatile", th.Dim.Render("corr ")+txt(optU(c.ECCCorrectedVolatile))+th.Dim.Render(" uncorr ")+m.naOr(optU(c.ECCUncorrectedVolatile), nonZero(m, c.ECCUncorrectedVolatile.V))),
		kv("ECC lifetime", th.Dim.Render("corr ")+txt(optU(c.ECCCorrectedAggregate))+th.Dim.Render(" uncorr ")+txt(optU(c.ECCUncorrectedAggregate))),
	)
	if c.RemappedCorrectable.OK || !c.RetiredPagesSBE.OK {
		hl = append(hl, kv("Row remap", th.Dim.Render("corr ")+txt(optU(c.RemappedCorrectable))+th.Dim.Render(" uncorr ")+txt(optU(c.RemappedUncorrectable))+
			th.Dim.Render(" pending ")+txt(optBool(c.RemapPending, "yes", "no"))+th.Dim.Render(" failed ")+txt(optBool(c.RemapFailure, "yes", "no"))))
	}
	if c.RetiredPagesSBE.OK {
		hl = append(hl, kv("Retired pages", th.Dim.Render("sbe ")+txt(optU(c.RetiredPagesSBE))+th.Dim.Render(" dbe ")+txt(optU(c.RetiredPagesDBE))+th.Dim.Render(" pending ")+txt(optBool(c.RetiredPending, "yes", "no"))))
	}
	hl = append(hl, kv("Recovery", txt(orNA(c.RecoveryAction.V, c.RecoveryAction.OK))))
	var xids []string
	for i := len(s.Events) - 1; i >= 0 && len(xids) < 3; i-- {
		e := s.Events[i]
		if e.Kind == "xid" && e.DeviceID == d.ID {
			xids = append(xids, fmt.Sprintf("%s %s", e.Time.Local().Format("15:04"), e.Message))
		}
	}
	if len(xids) > 0 {
		hl = append(hl, kv("Recent Xids", th.Warn.Render(xids[0])))
		for _, x := range xids[1:] {
			hl = append(hl, widgets.Space(th, lw)+th.Warn.Render(x))
		}
	}
	secs = append(secs, section{"Health", hl})

	if len(procs) > 0 {
		sort.SliceStable(procs, func(i, j int) bool { return procs[i].MemUsed.Or(0) > procs[j].MemUsed.Or(0) })
		var pl []string
		for i, p := range procs {
			if i >= 6 {
				pl = append(pl, th.Dim.Render(fmt.Sprintf("… %d more (Processes tab)", len(procs)-i)))
				break
			}
			owner := p.Kube.PodName
			if owner == "" {
				owner = p.User
			}
			pl = append(pl, th.Dim.Render(fmt.Sprintf("%7d ", p.PID))+th.Text.Render(fmt.Sprintf("%-14s", truncate(p.Name, 14)))+
				m.naOr(optBytes(p.MemUsed), th.Text)+th.Dim.Render("  "+owner))
		}
		secs = append(secs, section{"Processes", pl})
	}

	if full {
		id := string(d.ID)
		chW := max(10, cw-2)
		var ch []string
		ch = append(ch, th.Dim.Render("utilization % (live)"))
		ch = append(ch, widgets.Chart(th, m.live.values("util/"+id, 0), chW, 3, widgets.ChartOpts{Min: 0, Max: 100, Cursor: -1})...)
		ch = append(ch, th.Dim.Render("power W (live)"))
		ch = append(ch, widgets.Chart(th, m.live.values("power/"+id, 0), chW, 3, widgets.ChartOpts{Min: 0, Max: smp.PowerLimitW.Or(0), Cursor: -1})...)
		ch = append(ch, th.Dim.Render("temperature °C (live)"))
		ch = append(ch, widgets.Chart(th, m.live.values("temp/"+id, 0), chW, 2, widgets.ChartOpts{Cursor: -1})...)
		secs = append(secs, section{"Trends", ch})

		var caps []string
		keys := make([]string, 0, len(d.Capabilities))
		for k := range d.Capabilities {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			st := d.Capabilities[gpu.Capability(k)]
			style := th.OK
			switch st {
			case gpu.CapUnsupported:
				style = th.NA
			case gpu.CapNoPermission:
				style = th.Warn
			case gpu.CapUnknown:
				style = th.Muted
			}
			caps = append(caps, m.kv(k, style.Render(string(st)), 22))
		}
		if len(caps) > 0 {
			secs = append(secs, section{"Capabilities", caps})
		}
	}
	return secs
}

func nonZero(m *Model, v uint64) lipgloss.Style {
	if v > 0 {
		return m.th.Crit
	}
	return m.th.Text
}

func orNA(v string, ok bool) string {
	if !ok || v == "" {
		return na
	}
	return v
}

func durOpt(o metric.Opt[time.Duration]) string {
	if !o.OK {
		return na
	}
	return fmtDuration(o.V)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}

func fracPct(frac float64, ok bool) metric.Opt[float64] {
	if !ok {
		return metric.Opt[float64]{}
	}
	return metric.Opt[float64]{V: frac * 100, OK: true}
}

func (m *Model) tempBar(t metric.Opt[float64], d gpu.Device, w int) string {
	th := m.th
	if !t.OK {
		return th.NA.Render(na)
	}
	limit := d.TempShutdownC.Or(100)
	slow := d.TempSlowdownC.Or(limit - 5)
	label := m.temp(t)
	bw := w - widgets.Width(label) - 1
	return widgets.LevelBar(th, t.V/limit, max(3, bw), (slow-10)/limit, (slow-3)/limit) + th.Base.Render(" ") + th.Level(t.V, slow-10, slow-3).Render(label)
}

func (m *Model) thermalState(g *model.GPU) string {
	th := m.th
	smp := g.Sample
	switch {
	case !smp.Throttle.OK && !smp.TempC.OK:
		return th.NA.Render(na)
	case smp.Throttle.OK && smp.Throttle.V&(gpu.ThrottleHWThermal|gpu.ThrottleHWSlowdown) != 0:
		return th.Crit.Render("hardware slowdown")
	case smp.Throttle.OK && smp.Throttle.V&gpu.ThrottleSWThermal != 0:
		return th.Warn.Render("software thermal slowdown")
	case smp.TempC.OK && g.Device.TempSlowdownC.OK && smp.TempC.V >= g.Device.TempSlowdownC.V-5:
		return th.Warn.Render(fmt.Sprintf("near slowdown (%.0f°C headroom)", g.Device.TempSlowdownC.V-smp.TempC.V))
	}
	return th.OK.Render("normal")
}

func (m *Model) pcieLink(g *model.GPU) string {
	th := m.th
	smp, d := g.Sample, g.Device
	if !smp.PCIeGen.OK && !smp.PCIeWidth.OK {
		return th.NA.Render(na)
	}
	link := fmt.Sprintf("Gen%s x%s", optI(smp.PCIeGen, "%d"), optI(smp.PCIeWidth, "%d"))
	max := fmt.Sprintf(" (max Gen%s x%s)", optI(d.PCIeMaxGen, "%d"), optI(d.PCIeMaxWidth, "%d"))
	st := th.OK
	note := ""
	if smp.PCIeWidth.OK && d.PCIeMaxWidth.OK && smp.PCIeWidth.V < d.PCIeMaxWidth.V {
		st, note = th.Warn, " DEGRADED WIDTH"
	} else if smp.PCIeGen.OK && d.PCIeMaxGen.OK && smp.PCIeGen.V < d.PCIeMaxGen.V {
		if smp.UtilPercent.Or(0) >= 50 {
			st, note = th.Warn, " below max gen under load"
		} else {
			note = " (power saving at idle)"
		}
	}
	return st.Render(link) + th.Dim.Render(max) + st.Render(note)
}
