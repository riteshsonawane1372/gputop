// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/collector"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/gpu/sim"
	"github.com/riteshsonawane1372/gputop/internal/history"
	"github.com/riteshsonawane1372/gputop/internal/host"
	"github.com/riteshsonawane1372/gputop/internal/keymap"
	"github.com/riteshsonawane1372/gputop/internal/model"
	"github.com/riteshsonawane1372/gputop/internal/theme"
	"github.com/muesli/termenv"
)

type staticSource struct {
	snap  *model.Snapshot
	store *history.Store
}

func (s *staticSource) Subscribe() (<-chan *model.Snapshot, func()) {
	ch := make(chan *model.Snapshot, 1)
	return ch, func() {}
}
func (s *staticSource) Latest() *model.Snapshot { return s.snap }
func (s *staticSource) RefreshNow()             {}
func (s *staticSource) History() history.Reader {
	if s.store == nil {
		return nil
	}
	return s.store
}

func testTheme() *theme.Theme {
	lipgloss.SetColorProfile(termenv.TrueColor)
	p, _ := theme.Resolve("green", "", nil)
	return theme.New("green", p, false)
}

// simSource runs a simulated engine long enough to populate rates and history.
func simSource(t testing.TB, gpus int) *staticSource {
	store, _, err := history.Open(history.Options{Retention: 30 * time.Minute, Resolution: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	e := collector.New(collector.Options{
		Providers: []gpu.Provider{sim.New(sim.Options{GPUs: gpus})},
		Intervals: collector.Intervals{Fast: 30 * time.Millisecond, Normal: 60 * time.Millisecond, Slow: 120 * time.Millisecond, Inventory: time.Second, Timeout: time.Second},
		Host:      host.NewCollector(), History: store, Demo: true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	_ = e.Run(ctx)
	return &staticSource{snap: e.Latest(), store: store}
}

func newTestModel(src Source, w, h int) *Model {
	m := New(Options{Source: src, Theme: testTheme(), Keys: keymap.Default(), Refresh: time.Second, DefaultTab: "overview"})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m.live.observe(src.Latest())
	m.ensureSelection()
	return m
}

func checkFrame(t *testing.T, name, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("%s: %d lines, want %d", name, len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("%s: line %d has width %d, want %d:\n%s", name, i, got, w, ansi.Strip(l))
		}
	}
}

var sizes = [][2]int{{80, 24}, {100, 30}, {120, 40}, {160, 50}, {220, 60}}

func TestAllTabsRenderWithinBounds(t *testing.T) {
	src := simSource(t, 8)
	for _, sz := range sizes {
		m := newTestModel(src, sz[0], sz[1])
		vis := m.visibleTabs()
		if len(vis) < 14 {
			var ids []string
			for _, v := range vis {
				ids = append(ids, v.id)
			}
			t.Fatalf("expected most tabs visible in demo, got %v", ids)
		}
		for _, tab := range vis {
			m.activeID = tab.id
			if tab.id == "history" {
				if cmd := m.maybeQueryHistory(); cmd != nil {
					m.Update(cmd())
				}
			}
			checkFrame(t, tab.id, m.View(), sz[0], sz[1])
		}
		// Detail views and overlays.
		m.activeID = "gpus"
		m.gpus.detail = true
		checkFrame(t, "gpu-detail", m.View(), sz[0], sz[1])
		m.activeID = "processes"
		m.procs.detail = true
		m.procs.selPID = src.snap.Processes[0].PID
		checkFrame(t, "process-detail", m.View(), sz[0], sz[1])
		m.activeID = "events"
		m.events.detail = true
		checkFrame(t, "event-detail", m.View(), sz[0], sz[1])
		m.help = true
		checkFrame(t, "help", m.View(), sz[0], sz[1])
	}
}

func TestNoGPUAndLoadingStates(t *testing.T) {
	snap := &model.Snapshot{Ready: true, Time: time.Now(), Providers: []model.ProviderStatus{{
		Name: "nvml", Diagnostics: gpu.Diagnostics{Provider: "NVIDIA (NVML)", Checks: []gpu.Check{
			{Name: "Kernel driver", OK: false, Detail: "/proc/driver/nvidia/version not found"},
			{Name: "NVML library", OK: false, Detail: "unable to load libnvidia-ml.so.1"},
		}, Hints: []string{"Install the NVIDIA driver (it ships libnvidia-ml.so.1)."}},
	}}}
	src := &staticSource{snap: snap}
	for _, sz := range sizes {
		m := newTestModel(src, sz[0], sz[1])
		frame := m.View()
		checkFrame(t, "no-gpu", frame, sz[0], sz[1])
		if !strings.Contains(ansi.Strip(frame), "No supported GPU detected") {
			t.Fatal("no-GPU screen must explain the situation")
		}
		for _, tab := range m.visibleTabs() {
			if tab.id == "gpus" || tab.id == "nvlink" || tab.id == "mig" {
				t.Fatalf("GPU tab %s must be hidden without GPUs", tab.id)
			}
			m.activeID = tab.id
			checkFrame(t, "no-gpu/"+tab.id, m.View(), sz[0], sz[1])
		}
	}
	loading := &staticSource{snap: &model.Snapshot{}}
	checkFrame(t, "loading", newTestModel(loading, 80, 24).View(), 80, 24)
	checkFrame(t, "tiny", newTestModel(loading, 40, 10).View(), 40, 10)
}

func TestKeyboardNavigation(t *testing.T) {
	src := simSource(t, 4)
	m := newTestModel(src, 160, 50)
	press := func(keys ...string) {
		for _, k := range keys {
			var msg tea.KeyMsg
			switch k {
			case "tab":
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case "shift+tab":
				msg = tea.KeyMsg{Type: tea.KeyShiftTab}
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case "down":
				msg = tea.KeyMsg{Type: tea.KeyDown}
			default:
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
			}
			m.Update(msg)
		}
	}
	press("tab")
	if m.activeID != "gpus" {
		t.Fatalf("tab -> %s", m.activeID)
	}
	press("shift+tab")
	if m.activeID != "overview" {
		t.Fatalf("shift+tab -> %s", m.activeID)
	}
	press("3")
	if m.activeID != "processes" {
		t.Fatalf("3 -> %s", m.activeID)
	}
	press("/", "t", "r", "a", "i", "n", "enter")
	if m.q("processes").search != "train" || m.input != nil {
		t.Fatalf("search state: %+v", m.q("processes"))
	}
	for _, p := range m.filteredProcesses() {
		if !strings.Contains(p.Command+p.Name, "train") {
			t.Fatalf("search leaked %+v", p.Name)
		}
	}
	press("esc")
	if m.q("processes").search != "" {
		t.Fatal("esc should clear filters")
	}
	press("f", "g", "p", "u", ":", "1", "enter")
	if procs := m.filteredProcesses(); len(procs) != 1 || procs[0].DeviceIndex != 1 {
		t.Fatalf("filter gpu:1 -> %d processes", len(procs))
	}
	press("esc", "s")
	if m.procs.sortKey != sortUtil {
		t.Fatalf("sort key %v", m.procs.sortKey)
	}
	press("h")
	if m.activeID != "history" {
		t.Fatal("h opens history")
	}
	press("m", "-", ",", "n", "]")
	if m.hist.metric != 0 || !m.hist.host && m.selGPU == src.snap.GPUs[0].Device.ID {
		// ']' moves to the next series and resets the metric.
		t.Fatalf("history navigation: %+v", m.hist)
	}
	press("p")
	if !m.paused {
		t.Fatal("p pauses")
	}
	press("?")
	if !m.help {
		t.Fatal("? opens help")
	}
	press("?")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q must quit")
	}
}

func TestCustomKeybindings(t *testing.T) {
	keys, err := keymap.New(map[string][]string{"next_tab": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	src := simSource(t, 2)
	m := New(Options{Source: src, Theme: testTheme(), Keys: keys, Refresh: time.Second})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.activeID != "gpus" {
		t.Fatalf("custom binding: active %s", m.activeID)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.activeID != "gpus" {
		t.Fatal("rebound default key must no longer switch tabs")
	}
}

func BenchmarkRenderOverview(b *testing.B) {
	for _, n := range []int{1, 4, 8, 16} {
		src := simSource(b, n)
		m := newTestModel(src, 200, 60)
		b.Run(strings.Repeat("gpu", 0)+itoa(n)+"gpus", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.View()
			}
		})
	}
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+n/10)) + string(rune('0'+n%10)))
}
