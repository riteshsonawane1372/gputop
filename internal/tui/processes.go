// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/kube"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

type sortKey int

const (
	sortVRAM sortKey = iota
	sortUtil
	sortPID
	sortRuntime
	sortName
	sortGPU
	numSortKeys
)

var sortNames = map[sortKey]string{sortVRAM: "VRAM", sortUtil: "SM%", sortPID: "PID", sortRuntime: "RUNTIME", sortName: "PROCESS", sortGPU: "GPU"}

type procsState struct {
	sel      int
	detail   bool
	sortKey  sortKey
	sortDesc bool
	selPID   int
	// sortChosen is set once the default sort was adapted to the vendor.
	sortChosen bool
}

func (m *Model) filteredProcesses() []model.Process {
	s := m.view()
	q := m.q("processes")
	now := m.now()
	var out []model.Process
	for _, p := range s.Processes {
		fields := map[string]string{
			"pid": fmt.Sprint(p.PID), "name": p.Name, "user": p.User, "gpu": fmt.Sprint(p.DeviceIndex),
			"pod": p.Kube.PodName, "namespace": p.Kube.Namespace, "workload": p.Kube.WorkloadName,
			"container": p.Kube.ContainerID, "command": p.Command, "type": string(p.Type),
		}
		if p.PartitionID != "" {
			fields["mig"] = fmt.Sprint(p.PartitionIndex)
		}
		if matchesQuery(q, fields) {
			out = append(out, p)
		}
	}
	st := m.procs
	less := func(a, b model.Process) bool {
		switch st.sortKey {
		case sortUtil:
			return a.SMUtil.Or(-1) < b.SMUtil.Or(-1)
		case sortPID:
			return a.PID < b.PID
		case sortRuntime:
			return runtimeOf(a, now) < runtimeOf(b, now)
		case sortName:
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case sortGPU:
			return a.DeviceIndex < b.DeviceIndex
		}
		return a.MemUsed.Or(0) < b.MemUsed.Or(0)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if st.sortDesc {
			return less(out[j], out[i])
		}
		return less(out[i], out[j])
	})
	return out
}

func runtimeOf(p model.Process, now time.Time) time.Duration {
	if p.StartTime.IsZero() {
		return -1
	}
	return now.Sub(p.StartTime)
}

func keysProcesses(m *Model, a keymap.Action) (bool, tea.Cmd) {
	st := &m.procs
	procs := m.filteredProcesses()
	if st.detail {
		if a == keymap.Back || a == keymap.Select {
			st.detail = false
			return true, nil
		}
		return false, nil
	}
	page := max(1, m.height-8)
	switch a {
	case keymap.Up:
		st.sel = widgets.Scroll(st.sel, -1, len(procs))
	case keymap.Down:
		st.sel = widgets.Scroll(st.sel, 1, len(procs))
	case keymap.PageUp:
		st.sel = widgets.Scroll(st.sel, -page, len(procs))
	case keymap.PageDown:
		st.sel = widgets.Scroll(st.sel, page, len(procs))
	case keymap.Home:
		st.sel = 0
	case keymap.End:
		st.sel = max(0, len(procs)-1)
	case keymap.SortNext, keymap.Right:
		st.sortKey = (st.sortKey + 1) % numSortKeys
		if st.sortKey == sortVRAM && m.apple() {
			st.sortKey++ // Apple reports no per-process GPU memory
		}
		st.sortDesc = st.sortKey == sortVRAM || st.sortKey == sortUtil || st.sortKey == sortRuntime
		m.setToast("sort: "+m.sortLabel(st.sortKey), 1500*time.Millisecond)
	case keymap.Left:
		st.sortKey = (st.sortKey + numSortKeys - 1) % numSortKeys
		if st.sortKey == sortVRAM && m.apple() {
			st.sortKey = numSortKeys - 1
		}
		st.sortDesc = st.sortKey == sortVRAM || st.sortKey == sortUtil || st.sortKey == sortRuntime
		m.setToast("sort: "+m.sortLabel(st.sortKey), 1500*time.Millisecond)
	case keymap.SortReverse:
		st.sortDesc = !st.sortDesc
	case keymap.Select:
		if len(procs) > 0 {
			st.detail = true
			st.selPID = procs[min(st.sel, len(procs)-1)].PID
		}
	default:
		return false, nil
	}
	if st.sel < len(procs) && len(procs) > 0 {
		st.selPID = procs[st.sel].PID
	}
	return true, nil
}

func hintsProcesses(m *Model) []hint {
	if m.procs.detail {
		return []hint{{keymap.Back, "close"}}
	}
	return []hint{{keymap.Up, "select"}, {keymap.SortNext, "sort"}, {keymap.SortReverse, "reverse"}, {keymap.Select, "detail"}}
}

func viewProcesses(m *Model, w, h int) widgets.Block {
	th := m.th
	st := &m.procs
	procs := m.filteredProcesses()
	// Keep the selection on the same PID when the list reorders.
	if st.selPID != 0 {
		for i, p := range procs {
			if p.PID == st.selPID {
				st.sel = i
				break
			}
		}
	}
	st.sel = widgets.Scroll(st.sel, 0, len(procs))
	now := m.now()

	specs := m.processColumns(now)
	cols := make([]widgets.Column, len(specs))
	sortCol := -1
	for i, c := range specs {
		cols[i] = c.col
		if c.sort == st.sortKey && c.sortable {
			sortCol = i
		}
	}
	tb := &widgets.Table{Columns: cols, Selected: st.sel, SortCol: sortCol, SortDesc: st.sortDesc}
	if len(procs) == 0 {
		tb.Selected = -1
	}
	if m.mouse {
		tb.RowMark = func(r int, line string) string {
			pid := procs[r].PID
			return m.zone("proc:"+strconv.Itoa(pid), line, func(dbl bool) tea.Cmd {
				st.sel, st.selPID = r, pid
				if dbl {
					return m.dispatch(keymap.Select, "")
				}
				return nil
			})
		}
		tb.HeaderMark = func(col int, title string) string {
			c := specs[col]
			if !c.sortable {
				return title
			}
			return m.zone("sort:"+c.col.Title, title, func(bool) tea.Cmd {
				if st.sortKey == c.sort {
					st.sortDesc = !st.sortDesc
				} else {
					st.sortKey = c.sort
					st.sortDesc = c.sort == sortVRAM || c.sort == sortUtil || c.sort == sortRuntime
				}
				m.setToast("sort: "+m.sortLabel(st.sortKey), 1500*time.Millisecond)
				return nil
			})
		}
	}
	for _, p := range procs {
		row := make([]widgets.Cell, len(specs))
		for i, c := range specs {
			row[i] = c.cell(p)
		}
		tb.Rows = append(tb.Rows, row)
	}

	right := fmt.Sprintf("%d of %d · sort %s", len(procs), len(m.view().Processes), m.sortLabel(st.sortKey))
	content := tb.Render(th, w-2, h-2)
	if len(procs) == 0 {
		msg := "No GPU processes"
		if len(m.view().Processes) > 0 {
			msg = "No processes match the current search/filter (esc clears)"
		}
		content = widgets.FitBlock(th, []string{content[0], "", th.Dim.Render("  " + msg)}, w-2, h-2)
	}
	box := widgets.Box(th, widgets.BoxOpts{Title: "GPU processes", RightTitle: right, Focus: true}, w, h, content)
	if st.detail {
		for _, p := range procs {
			if p.PID == st.selPID {
				return m.overlay(box, m.processDetail(p, min(w-4, 100)), w, h)
			}
		}
		st.detail = false
	}
	return box
}

// sortLabel is the display name of a sort key.
func (m *Model) sortLabel(k sortKey) string {
	if k == sortUtil && m.apple() {
		return "GPU%"
	}
	return sortNames[k]
}

type procColumn struct {
	col      widgets.Column
	sort     sortKey
	sortable bool
	cell     func(p model.Process) widgets.Cell
}

// processColumns lists the process table columns for the GPUs in view.
func (m *Model) processColumns(now time.Time) []procColumn {
	th := m.th
	dash := func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	}
	col := func(c widgets.Column, cell func(p model.Process) widgets.Cell) procColumn {
		return procColumn{col: c, cell: cell}
	}
	sortBy := func(k sortKey, c widgets.Column, cell func(p model.Process) widgets.Cell) procColumn {
		return procColumn{col: c, sort: k, sortable: true, cell: cell}
	}
	pid := sortBy(sortPID, widgets.Column{Title: "PID", Width: 8, Align: widgets.Right}, func(p model.Process) widgets.Cell {
		return widgets.C(th.Dim, fmt.Sprint(p.PID))
	})
	name := sortBy(sortName, widgets.Column{Title: "PROCESS", Width: 16, Min: 8}, func(p model.Process) widgets.Cell {
		if !p.Visible && p.Name == "" {
			return widgets.C(th.NA, "?")
		}
		return widgets.C(th.Text, p.Name)
	})
	user := col(widgets.Column{Title: "USER", Width: 10, Min: 6, Priority: 3}, func(p model.Process) widgets.Cell {
		return widgets.C(th.Dim, dash(p.User))
	})
	util := func(title string) procColumn {
		return sortBy(sortUtil, widgets.Column{Title: title, Width: max(4, len(title)+1), Align: widgets.Right}, func(p model.Process) widgets.Cell {
			return widgets.R(m.naOr(optF(p.SMUtil, "%.0f"), th.Gradient(p.SMUtil.V/100)))
		})
	}
	runtime := sortBy(sortRuntime, widgets.Column{Title: "RUNTIME", Width: 8, Align: widgets.Right, Priority: 4}, func(p model.Process) widgets.Cell {
		if p.StartTime.IsZero() {
			return widgets.C(th.Dim, na)
		}
		return widgets.C(th.Dim, fmtDuration(now.Sub(p.StartTime)))
	})
	command := col(widgets.Column{Title: "COMMAND", Width: 30, Min: 10, Priority: 10, Flex: true}, func(p model.Process) widgets.Cell {
		return widgets.C(th.Dim, p.Command)
	})

	if m.apple() {
		// One integrated GPU, no per-process memory and no containers.
		cols := []procColumn{pid, name, user, util("GPU%"), runtime}
		cols[1].col.Width, cols[1].col.Flex = 28, true
		if m.showCmd {
			cols = append(cols, command)
		}
		return cols
	}

	cols := []procColumn{
		pid, name, user,
		sortBy(sortGPU, widgets.Column{Title: "GPU", Width: 4, Align: widgets.Right}, func(p model.Process) widgets.Cell {
			return widgets.C(th.Accent, fmt.Sprint(p.DeviceIndex))
		}),
		col(widgets.Column{Title: "MIG", Width: 4, Align: widgets.Right, Priority: 6}, func(p model.Process) widgets.Cell {
			if p.PartitionID == "" {
				return widgets.C(th.Dim, "—")
			}
			return widgets.C(th.Dim, fmt.Sprint(p.PartitionIndex))
		}),
		sortBy(sortVRAM, widgets.Column{Title: "VRAM", Width: 10, Align: widgets.Right}, func(p model.Process) widgets.Cell {
			return widgets.R(m.naOr(optBytes(p.MemUsed), th.Text))
		}),
		util("SM%"),
		col(widgets.Column{Title: "MEM%", Width: 4, Align: widgets.Right, Priority: 7}, func(p model.Process) widgets.Cell {
			return widgets.R(m.naOr(optF(p.MemUtil, "%.0f"), th.Text))
		}),
		runtime,
		col(widgets.Column{Title: "CONTAINER", Width: 12, Priority: 8}, func(p model.Process) widgets.Cell {
			return widgets.C(th.Dim, dash(kube.ShortID(p.Kube.ContainerID)))
		}),
		col(widgets.Column{Title: "POD", Width: 24, Min: 10, Priority: 2, Flex: true}, func(p model.Process) widgets.Cell {
			return widgets.C(th.Text, dash(p.Kube.PodName))
		}),
		col(widgets.Column{Title: "NAMESPACE", Width: 12, Priority: 5}, func(p model.Process) widgets.Cell {
			return widgets.C(th.Dim, dash(p.Kube.Namespace))
		}),
		col(widgets.Column{Title: "WORKLOAD", Width: 22, Min: 10, Priority: 9, Flex: true}, func(p model.Process) widgets.Cell {
			if p.Kube.WorkloadName == "" {
				return widgets.C(th.Dim, "—")
			}
			wl := p.Kube.WorkloadKind + "/" + p.Kube.WorkloadName
			if p.Kube.Inferred {
				wl += "?"
			}
			return widgets.C(th.Dim, wl)
		}),
	}
	if m.showCmd {
		cols = append(cols, command)
	}
	return cols
}

func (m *Model) processDetail(p model.Process, w int) widgets.Block {
	th := m.th
	lw := 14
	kv := func(k, v string) string {
		if v == "" {
			v = th.NA.Render("—")
		}
		return m.kv(k, v, lw)
	}
	t := func(s string) string {
		if s == "" {
			return ""
		}
		return th.Text.Render(s)
	}
	lines := []string{
		kv("PID", t(fmt.Sprint(p.PID))),
		kv("Process", t(p.Name)),
		kv("User", t(p.User)),
		kv("GPU", t(fmt.Sprintf("%d (%s)", p.DeviceIndex, p.DeviceID))),
	}
	if p.PartitionID != "" {
		lines = append(lines, kv("MIG instance", t(fmt.Sprintf("%d (%s)", p.PartitionIndex, p.PartitionID))))
	}
	lines = append(lines,
		kv("Type", t(string(p.Type))),
	)
	if m.apple() {
		lines = append(lines, kv("GPU time", m.naOr(optPct(p.SMUtil), th.Text)+th.Dim.Render(" of wall time")))
	} else {
		lines = append(lines,
			kv("VRAM", m.naOr(optBytes(p.MemUsed), th.Text)),
			kv("SM / MEM util", m.naOr(optPct(p.SMUtil), th.Text)+th.Dim.Render(" / ")+m.naOr(optPct(p.MemUtil), th.Text)),
			kv("ENC / DEC util", m.naOr(optPct(p.EncUtil), th.Text)+th.Dim.Render(" / ")+m.naOr(optPct(p.DecUtil), th.Text)),
		)
	}
	if !p.StartTime.IsZero() {
		lines = append(lines, kv("Started", t(p.StartTime.Local().Format("2006-01-02 15:04:05")+" ("+fmtDuration(m.now().Sub(p.StartTime))+" ago)")))
	}
	if p.Reason != "" {
		lines = append(lines, kv("Note", th.Warn.Render(p.Reason)))
	}
	// Container correlation is meaningless for a Mac's desktop processes.
	if !m.apple() || p.Kube.ContainerID != "" {
		lines = append(lines, "", th.Title.Render("Correlation"),
			kv("Container", t(p.Kube.ContainerID)),
			kv("Runtime", t(p.Kube.Runtime)),
			kv("Pod", t(p.Kube.PodName)),
			kv("Pod UID", t(p.Kube.PodUID)),
			kv("Namespace", t(p.Kube.Namespace)),
			kv("Container name", t(p.Kube.Container)),
		)
		wl := ""
		if p.Kube.WorkloadName != "" {
			wl = p.Kube.WorkloadKind + "/" + p.Kube.WorkloadName
			if p.Kube.Inferred {
				wl += th.Dim.Render("  (inferred from pod name)")
			}
		}
		lines = append(lines, kv("Workload", t(wl)))
	}
	if p.Command != "" {
		lines = append(lines, "", th.Title.Render("Command"))
		for _, l := range wrap(p.Command, w-4) {
			lines = append(lines, th.Dim.Render(l))
		}
	}
	return widgets.Box(th, widgets.BoxOpts{Title: fmt.Sprintf("Process %d", p.PID), Focus: true}, w, len(lines)+2, lines)
}

// overlay centers a block over a background block.
func (m *Model) overlay(bg, fg widgets.Block, w, h int) widgets.Block {
	out := make(widgets.Block, len(bg))
	copy(out, bg)
	fw := 0
	if len(fg) > 0 {
		fw = widgets.Width(fg[0])
	}
	top := max(0, (h-len(fg))/2)
	left := max(0, (w-fw)/2)
	for i, l := range fg {
		if top+i >= len(out) {
			break
		}
		out[top+i] = widgets.Space(m.th, left) + l + widgets.Space(m.th, max(0, w-left-fw))
	}
	return out
}
