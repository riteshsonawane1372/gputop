// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/kube"
)

// inspectSource adds canned pod logs and events to a static source.
type inspectSource struct {
	*staticSource
	logCalls int
}

func (s *inspectSource) PodLogs(ctx context.Context, pod kube.PodRef, container string, tail int) ([]string, string, error) {
	s.logCalls++
	return []string{"2026-09-15T10:00:00.000Z INFO step 1 " + pod.Name + ":" + container, "2026-09-15T10:00:01.000Z ERROR boom"}, "test", nil
}

func (s *inspectSource) PodEvents(ctx context.Context, pod kube.PodRef) ([]kube.PodEvent, error) {
	return []kube.PodEvent{{Type: "Warning", Reason: "BackOff", Message: "restarting " + pod.Name, Count: 2, Last: time.Now()}}, nil
}

// runCmd executes a command and feeds its message back, like Bubble Tea.
func runCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		if _, isBatch := msg.(tea.BatchMsg); !isBatch {
			m.Update(msg)
		}
	}
}

func key(m *Model, k string) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	_, cmd := m.Update(msg)
	runCmd(m, cmd)
}

func typeText(m *Model, s string) {
	for _, r := range s {
		key(m, string(r))
	}
}

func TestKubernetesPodViews(t *testing.T) {
	src := &inspectSource{staticSource: simSource(t, 8)}
	for _, sz := range sizes {
		m := newMouseModel(src, sz[0], sz[1])
		m.activeID = "kubernetes"
		frame := ansi.Strip(m.View())
		checkFrame(t, "pods", m.View(), sz[0], sz[1])
		for _, want := range []string{"llama-70b-pretrain-worker-3", "Pending", "Running"} {
			if !strings.Contains(frame, want) {
				t.Fatalf("%dx%d pods list lacks %q:\n%s", sz[0], sz[1], want, frame)
			}
		}

		// Select the straggler and describe it.
		pods := m.filteredPods()
		for i, p := range pods {
			if p.info.Name == "llama-70b-pretrain-worker-3" {
				m.kube.sel, m.kube.selUID = i, p.info.UID
			}
		}
		key(m, "d")
		if m.kube.view != kubeDescribe || len(m.kube.events) != 1 {
			t.Fatalf("describe: view %v events %v", m.kube.view, m.kube.events)
		}
		checkFrame(t, "describe", m.View(), sz[0], sz[1])
		if sz[1] >= 40 {
			frame = ansi.Strip(m.View())
			for _, want := range []string{"describe", "llama-70b-pretrain-worker-3", "OOMKilled", "nvidia.com/gpu"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("%dx%d describe lacks %q:\n%s", sz[0], sz[1], want, frame)
				}
			}
		}
		key(m, "j")
		if m.kube.maxScroll > 0 && m.kube.scroll != 1 {
			t.Fatalf("describe scroll = %d", m.kube.scroll)
		}

		// Logs, then back to describe, then back to the list.
		key(m, "l")
		if m.kube.view != kubeLogs || len(m.kube.logs) != 2 || m.kube.logSource != "test" {
			t.Fatalf("logs: view %v lines %v", m.kube.view, m.kube.logs)
		}
		frame = m.View()
		checkFrame(t, "logs", frame, sz[0], sz[1])
		if !strings.Contains(ansi.Strip(frame), "llama-70b-pretrain-worker-3:trainer") || sz[0] >= 120 && !strings.Contains(ansi.Strip(frame), "autoscroll on") {
			t.Fatalf("logs frame:\n%s", ansi.Strip(frame))
		}
		key(m, "s")
		key(m, "w")
		if m.kube.follow || !m.kube.wrap {
			t.Fatalf("toggles: follow %v wrap %v", m.kube.follow, m.kube.wrap)
		}
		key(m, "esc")
		if m.kube.view != kubeDescribe {
			t.Fatalf("esc from logs: view %v", m.kube.view)
		}
		key(m, "esc")
		if m.kube.view != kubePods {
			t.Fatalf("esc from describe: view %v", m.kube.view)
		}
	}
}

func TestKubernetesMouseAndFilters(t *testing.T) {
	src := &inspectSource{staticSource: simSource(t, 8)}
	m := newMouseModel(src, 160, 45)
	now := time.Unix(5000, 0)
	m.now = func() time.Time { return now }
	m.activeID = "kubernetes"

	var target podRow
	for _, p := range m.gpuPods() {
		if p.info.Name == "notebook-alice-0" {
			target = p
		}
	}
	zone := "pod:" + target.info.UID + target.info.Name
	clickZone(t, m, zone)
	clickZone(t, m, zone)
	if m.kube.view != kubeDescribe || m.kube.uid != target.info.UID {
		t.Fatalf("double click: view %v uid %s", m.kube.view, m.kube.uid)
	}
	clickZone(t, m, "crumb:pods")
	if m.kube.view != kubePods {
		t.Fatal("pods crumb did not return to the list")
	}

	m.q("kubernetes").filter = "gpu:6"
	if pods := m.filteredPods(); len(pods) != 2 {
		t.Fatalf("gpu:6 matched %d pods, want the two MIG pods", len(pods))
	}
	m.q("kubernetes").filter = "ns:research"
	if pods := m.filteredPods(); len(pods) != 1 || pods[0].info.Name != "notebook-alice-0" {
		t.Fatalf("ns filter: %+v", pods)
	}
}

func TestCommandBar(t *testing.T) {
	src := &inspectSource{staticSource: simSource(t, 8)}
	m := newTestModel(src, 160, 45)

	key(m, ":")
	if m.input == nil || m.input.mode != "command" {
		t.Fatal(": did not open the command bar")
	}
	typeText(m, "da")
	if sug := m.suggestions(m.input.value); len(sug) == 0 || sug[0] != "dashboard" {
		t.Fatalf("suggestions for da: %v", sug)
	}
	if !strings.Contains(ansi.Strip(m.View()), "dashboard") {
		t.Fatal("footer does not show suggestions")
	}
	key(m, "tab")
	key(m, "enter")
	if m.activeID != "dashboard" || m.input != nil {
		t.Fatalf(":dashboard → %s", m.activeID)
	}

	key(m, ":")
	typeText(m, "ns inference")
	key(m, "enter")
	if m.activeID != "kubernetes" || len(m.filteredPods()) != 2 {
		t.Fatalf(":ns inference → tab %s, %d pods", m.activeID, len(m.filteredPods()))
	}
	key(m, ":")
	typeText(m, "ns all")
	key(m, "enter")
	if m.q("kubernetes").filter != "" {
		t.Fatalf(":ns all left filter %q", m.q("kubernetes").filter)
	}

	key(m, ":")
	typeText(m, "worker-5")
	key(m, "enter")
	if pod, ok := m.currentPod(); m.kube.view != kubeDescribe || !ok || pod.info.Name != "llama-70b-pretrain-worker-5" {
		t.Fatalf(":worker-5 → view %v pod %s", m.kube.view, pod.info.Name)
	}

	key(m, ":")
	typeText(m, "gpus")
	key(m, "enter")
	if m.activeID != "gpus" {
		t.Fatalf(":gpus → %s", m.activeID)
	}
	key(m, ":")
	typeText(m, "nonsense-xyz")
	key(m, "enter")
	if !strings.Contains(m.toast, "unknown command") {
		t.Fatalf("toast = %q", m.toast)
	}
}

func TestDashboard(t *testing.T) {
	src := simSource(t, 8)
	for _, withHistory := range []bool{true, false} {
		s := *src
		if !withHistory {
			s.store = nil
		}
		for _, sz := range sizes {
			m := newMouseModel(&s, sz[0], sz[1])
			m.activeID = "dashboard"
			if withHistory {
				runCmd(m, m.maybeQueryDash(true))
			}
			frame := m.View()
			checkFrame(t, "dashboard", frame, sz[0], sz[1])
			plain := ansi.Strip(frame)
			for _, want := range []string{"GPU Utilization", "Utilization", "GPU0"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("history=%v %dx%d dashboard lacks %q:\n%s", withHistory, sz[0], sz[1], want, plain)
				}
			}
			// Focus the power panel, zoom it, isolate a GPU.
			key(m, "j")
			key(m, "j")
			key(m, "enter")
			if !m.dash.zoom {
				t.Fatal("enter did not zoom")
			}
			checkFrame(t, "dashboard-zoom", m.View(), sz[0], sz[1])
			key(m, "]")
			if m.dash.only != 0 {
				t.Fatalf("isolate: only=%d", m.dash.only)
			}
			key(m, "esc")
			key(m, "esc")
			if m.dash.zoom || m.dash.only != -1 {
				t.Fatalf("esc: zoom %v only %d", m.dash.zoom, m.dash.only)
			}
			checkFrame(t, "dashboard-reliability", func() string { key(m, "G"); return m.View() }(), sz[0], sz[1])
		}
	}

	m := newMouseModel(src, 160, 50)
	m.activeID = "dashboard"
	runCmd(m, m.maybeQueryDash(true))
	m.View()
	clickZone(t, m, "dash:legend:0:3")
	if m.dash.only != 3 {
		t.Fatalf("legend click: only=%d", m.dash.only)
	}
	clickZone(t, m, "dash:legend:0:3")
	if m.dash.only != -1 {
		t.Fatal("second legend click did not restore all GPUs")
	}
	win := m.dash.window
	key(m, "-")
	if m.dash.window != min(win+1, m.maxWindow()) {
		t.Fatalf("zoom out: window %d → %d", win, m.dash.window)
	}
	if len(m.visiblePanels()) == len(dashPanels) {
		t.Fatal("panels without data (fan speed) should be hidden")
	}
}
