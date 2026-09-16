// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

var zoneMarker = regexp.MustCompile(`\x1b\[\d+z`)

func TestZoneScan(t *testing.T) {
	var z zoneSet
	hits := map[string]int{}
	fn := func(key string) clickFn { return func(bool) tea.Cmd { hits[key]++; return nil } }
	styled := "\x1b[1;32m界x\x1b[0m" // wide rune: 3 cells
	frame := z.mark("a", "ab", fn("a")) + "--" + z.mark("b", styled, fn("b")) + "\n" +
		z.mark("outer", "12"+z.mark("inner", "34", fn("inner"))+"56", fn("outer"))

	out := z.scan(frame)
	if zoneMarker.MatchString(out) {
		t.Fatalf("markers left in frame: %q", out)
	}
	if want := "ab--" + styled + "\n123456"; out != want {
		t.Fatalf("scan changed content: %q, want %q", out, want)
	}
	for _, c := range []struct {
		x, y int
		key  string
	}{{0, 0, "a"}, {1, 0, "a"}, {2, 0, ""}, {4, 0, "b"}, {6, 0, "b"}, {7, 0, ""}, {0, 1, "outer"}, {2, 1, "inner"}, {3, 1, "inner"}, {5, 1, "outer"}, {6, 1, ""}} {
		e, ok := z.at(c.x, c.y, func(zoneEntry) bool { return true })
		if (c.key == "") == ok || (ok && e.key != c.key) {
			t.Errorf("at(%d,%d) = %q %v, want %q", c.x, c.y, e.key, ok, c.key)
		}
	}

	z.reset()
	if out := z.scan("plain"); out != "plain" {
		t.Fatal("frames without zones must pass through")
	}
}

func newMouseModel(src Source, w, h int) *Model {
	m := newTestModel(src, w, h)
	m.mouse = true
	return m
}

// clickZone clicks the centre of the first zone with key (rendering first).
func clickZone(t *testing.T, m *Model, key string) {
	t.Helper()
	m.View()
	for _, r := range m.zones.rects {
		if m.zones.entries[r.entry].key == key {
			m.Update(tea.MouseMsg{X: (r.x0 + r.x1) / 2, Y: r.y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			return
		}
	}
	t.Fatalf("no zone %q in frame", key)
}

func TestMouseNavigation(t *testing.T) {
	src := simSource(t, 8)
	m := newMouseModel(src, 160, 50)
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	for _, tab := range m.visibleTabs() {
		m.activeID = tab.id
		frame := m.View()
		checkFrame(t, "mouse-"+tab.id, frame, 160, 50)
		if zoneMarker.MatchString(frame) {
			t.Fatalf("%s: markers leaked to the terminal", tab.id)
		}
	}
	m.activeID = "overview"

	// Tabs.
	clickZone(t, m, "tab:processes")
	if m.activeID != "processes" {
		t.Fatalf("tab click: active %s", m.activeID)
	}

	// Row click selects; a double-click opens the detail; clicking beside
	// the popup closes it.
	procs := m.filteredProcesses()
	target := procs[2].PID
	clickZone(t, m, "proc:"+strconv.Itoa(target))
	if m.procs.selPID != target || m.procs.detail {
		t.Fatalf("single click: sel %d detail %v", m.procs.selPID, m.procs.detail)
	}
	now = now.Add(time.Second) // too slow for a double-click
	clickZone(t, m, "proc:"+strconv.Itoa(target))
	if m.procs.detail {
		t.Fatal("slow second click opened the detail")
	}
	now = now.Add(100 * time.Millisecond)
	clickZone(t, m, "proc:"+strconv.Itoa(target))
	if !m.procs.detail || m.procs.selPID != target {
		t.Fatalf("double click: detail %v sel %d", m.procs.detail, m.procs.selPID)
	}
	m.View()
	m.Update(tea.MouseMsg{X: 0, Y: 10, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.procs.detail {
		t.Fatal("click outside the popup did not close it")
	}

	// Header click sorts; clicking the sorted column reverses it.
	clickZone(t, m, "sort:PID")
	if m.procs.sortKey != sortPID || m.procs.sortDesc {
		t.Fatalf("sort click: key %v desc %v", m.procs.sortKey, m.procs.sortDesc)
	}
	clickZone(t, m, "sort:PID")
	if !m.procs.sortDesc {
		t.Fatal("second header click did not reverse the sort")
	}

	// Wheel moves the selection like the arrow keys.
	sel := m.procs.sel
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.procs.sel != sel+1 {
		t.Fatalf("wheel down: sel %d, want %d", m.procs.sel, sel+1)
	}

	// GPU rows on a hardware tab: double-click opens the GPU detail.
	m.activeID = "power"
	id := src.snap.GPUs[5].Device.ID
	clickZone(t, m, "gpu:"+string(id))
	now = now.Add(50 * time.Millisecond)
	clickZone(t, m, "gpu:"+string(id))
	if m.selGPU != id || m.activeID != "gpus" || !m.gpus.detail {
		t.Fatalf("gpu double click: sel %s tab %s detail %v", m.selGPU, m.activeID, m.gpus.detail)
	}

	// Footer hints run their action.
	m.gpus.detail = false
	clickZone(t, m, "hint:help")
	if !m.help {
		t.Fatal("help hint click did not open help")
	}
	m.Update(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.help {
		t.Fatal("click did not close help")
	}
}

func TestMouseDisabled(t *testing.T) {
	src := simSource(t, 2)
	m := newTestModel(src, 120, 40)
	m.View()
	if len(m.zones.entries) != 0 {
		t.Fatal("zones registered with mouse disabled")
	}
}

// appleSnapshot rewrites a simulated snapshot into what the Apple provider
// reports: one integrated GPU with unified memory and no PCIe/NVLink/MIG.
func appleSnapshot(sim *model.Snapshot) *model.Snapshot {
	s := *sim
	g := sim.GPUs[0]
	g.Device.Vendor = gpu.VendorApple
	g.Device.Name = "Apple M4 10-core GPU"
	g.Device.PCI = gpu.PCIAddress{}
	g.Device.LinkCount = 0
	g.Device.MIG = gpu.MIGMode{}
	g.Device.ClockCoreMaxMHz = metric.Some(1578.0)
	g.Links, g.Partitions = nil, nil
	g.Sample.PCIeGen, g.Sample.PCIeWidth = metric.None[int](), metric.None[int]()
	g.Sample.PowerLimitW, g.Sample.Throttle = metric.None[float64](), metric.None[gpu.ThrottleReasons]()
	g.Sample.ClockCoreMHz = metric.Some(800.0)
	s.GPUs = []model.GPU{g}
	s.Providers = []model.ProviderStatus{{Name: g.Provider, Vendor: gpu.VendorApple, Available: true,
		System: gpu.SystemInfo{DriverVersion: "340.26.3", RuntimeName: "macOS", RuntimeVersion: "26.0"}}}
	s.Topology = nil
	var procs []model.Process
	for _, p := range sim.Processes {
		p.DeviceID, p.DeviceIndex, p.PartitionID = g.Device.ID, 0, ""
		p.MemUsed = metric.None[uint64]()
		procs = append(procs, p)
	}
	s.Processes = procs
	return &s
}

func TestAppleUI(t *testing.T) {
	src := &staticSource{snap: appleSnapshot(simSource(t, 8).snap)}
	for _, sz := range sizes {
		m := newMouseModel(src, sz[0], sz[1])
		m.Update(snapshotMsg{src.snap})
		if !m.apple() || m.procs.sortKey != sortUtil {
			t.Fatalf("apple mode %v, process sort %v", m.apple(), m.procs.sortKey)
		}
		for _, tab := range m.visibleTabs() {
			switch tab.id {
			case "pcie", "nvlink", "mig":
				t.Fatalf("tab %s shown for an integrated GPU", tab.id)
			}
			m.activeID = tab.id
			checkFrame(t, "apple-"+tab.id, m.View(), sz[0], sz[1])
		}
		m.activeID, m.gpus.detail = "gpus", true
		detail := ansi.Strip(m.View())
		checkFrame(t, "apple-gpu-detail", m.View(), sz[0], sz[1])
		if sz[0] >= 120 && (!strings.Contains(detail, "unified") || strings.Contains(detail, "CUDA") || strings.Contains(detail, "ECC mode")) {
			t.Fatalf("apple detail shows NVIDIA fields:\n%s", detail)
		}
		m.gpus.detail = false
		m.activeID = "memory"
		if frame := ansi.Strip(m.View()); !strings.Contains(frame, "unified") || strings.Contains(frame, "ECC") {
			t.Fatalf("apple memory tab:\n%s", frame)
		}
		m.activeID = "processes"
		if frame := ansi.Strip(m.View()); !strings.Contains(frame, "GPU%") || strings.Contains(frame, "VRAM") || strings.Contains(frame, "POD") {
			t.Fatalf("apple processes tab:\n%s", frame)
		}
	}
}

func TestGPUsTabScrolling(t *testing.T) {
	src := simSource(t, 8)
	m := newMouseModel(src, 120, 30)
	m.activeID = "gpus"
	frame := m.View()
	if m.gpus.maxScroll == 0 {
		t.Fatal("detail pane should overflow at 120x30")
	}
	if !strings.Contains(ansi.Strip(frame), "┃") {
		t.Fatal("no scrollbar on an overflowing pane")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.gpus.scroll != min(10, m.gpus.maxScroll) {
		t.Fatalf("pgdown scrolled to %d", m.gpus.scroll)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.gpus.scroll != 0 {
		t.Fatalf("pgup scrolled to %d", m.gpus.scroll)
	}

	// The wheel scrolls the pane under the pointer, but moves the GPU
	// selection over the list.
	var paneX, paneY, listX, listY int
	m.View()
	for _, r := range m.zones.rects {
		switch e := m.zones.entries[r.entry]; {
		case e.key == "gpus:pane" && paneX == 0:
			paneX, paneY = r.x0+2, r.y
		case strings.HasPrefix(e.key, "gpu:") && listX == 0:
			listX, listY = r.x0+2, r.y
		}
	}
	if paneX == 0 || listX == 0 {
		t.Fatal("pane or list zones missing")
	}
	sel := m.selGPU
	m.Update(tea.MouseMsg{X: paneX, Y: paneY, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.gpus.scroll != min(wheelLines, m.gpus.maxScroll) || m.selGPU != sel {
		t.Fatalf("wheel over pane: scroll %d sel changed %v", m.gpus.scroll, m.selGPU != sel)
	}
	m.Update(tea.MouseMsg{X: listX, Y: listY, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.selGPU == sel {
		t.Fatal("wheel over the list did not move the GPU selection")
	}

	// Double-clicking the pane opens the full detail, which also scrolls.
	clickZone(t, m, "gpus:pane")
	clickZone(t, m, "gpus:pane")
	if !m.gpus.detail || m.gpus.scroll != 0 {
		t.Fatalf("pane double click: detail %v scroll %d", m.gpus.detail, m.gpus.scroll)
	}
	checkFrame(t, "gpu-detail-scrolled", m.View(), 120, 30)
}

func TestArrowKeysSwitchTabs(t *testing.T) {
	src := simSource(t, 2)
	m := newTestModel(src, 120, 40)
	vis := m.visibleTabs()
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.activeID != vis[1].id {
		t.Fatalf("right: active %s, want %s", m.activeID, vis[1].id)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.activeID != vis[len(vis)-1].id {
		t.Fatalf("left twice from the second tab: active %s, want wrap to %s", m.activeID, vis[len(vis)-1].id)
	}
	if !strings.Contains(ansi.Strip(m.View()), "←→ tabs") {
		t.Fatal("footer does not advertise arrow-key tab switching")
	}
}
