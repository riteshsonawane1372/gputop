// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/riteshsonawane1372/gputop/internal/keymap"
	"github.com/riteshsonawane1372/gputop/internal/kube"
	"github.com/riteshsonawane1372/gputop/internal/tui/widgets"
)

// tabDef describes one tab. Tabs are hidden when they have nothing
// meaningful to show for the current snapshot.
type tabDef struct {
	id, title  string
	visible    func(m *Model) bool
	keys       func(m *Model, a keymap.Action) (bool, tea.Cmd)
	view       func(m *Model, w, h int) widgets.Block
	hints      func(m *Model) []hint
	searchable bool
}

func hasGPUs(m *Model) bool { return len(m.view().GPUs) > 0 }

func allTabs() []*tabDef {
	return []*tabDef{
		{id: "overview", title: "Overview", view: viewOverview, keys: keysOverview, hints: hintsOverview},
		{id: "gpus", title: "GPUs", visible: hasGPUs, view: viewGPUs, keys: keysGPUs, hints: hintsGPUs},
		{id: "processes", title: "Processes", visible: hasGPUs, view: viewProcesses, keys: keysProcesses, hints: hintsProcesses, searchable: true},
		{id: "memory", title: "Memory", visible: hasGPUs, view: viewMemory, keys: keysGPUSelect, hints: hintsGPUSelect},
		{id: "power", title: "Power", visible: hasGPUs, view: viewPower, keys: keysGPUSelect, hints: hintsGPUSelect},
		{id: "thermals", title: "Thermals", visible: hasGPUs, view: viewThermals, keys: keysGPUSelect, hints: hintsGPUSelect},
		{id: "nvlink", title: "NVLink", visible: func(m *Model) bool { return m.view().HasLinks() }, view: viewNVLink, keys: keysGPUSelect, hints: hintsGPUSelect},
		{id: "pcie", title: "PCIe", visible: hasPCIe, view: viewPCIe, keys: keysGPUSelect, hints: hintsGPUSelect},
		{id: "mig", title: "MIG", visible: func(m *Model) bool { return m.view().HasPartitioning() }, view: viewMIG, keys: keysGPUSelect, hints: hintsGPUSelect},
		{id: "nodes", title: "Nodes", view: viewNodes, keys: keysNodes},
		{id: "kubernetes", title: "Kubernetes", visible: kubeVisible, view: viewKube, keys: keysKube, hints: hintsKube, searchable: true},
		{id: "workloads", title: "Workloads", visible: func(m *Model) bool { return len(m.view().Processes) > 0 }, view: viewWorkloads, keys: keysWorkloads, searchable: true},
		{id: "network", title: "Network", visible: func(m *Model) bool { return m.view().Host != nil }, view: viewNetwork, keys: keysNetwork, searchable: true},
		{id: "dashboard", title: "Dashboard", visible: hasGPUs, view: viewDashboard, keys: keysDashboard, hints: hintsDashboard},
		{id: "history", title: "History", view: viewHistory, keys: keysHistory, hints: hintsHistory},
		{id: "events", title: "Events", view: viewEvents, keys: keysEvents, hints: hintsEvents, searchable: true},
		{id: "health", title: "Health", view: viewHealth, keys: keysHealth, hints: hintsGPUSelect},
	}
}

func kubeVisible(m *Model) bool {
	s := m.view()
	if len(s.Kubernetes.Pods) > 0 || s.Kubernetes.Environment.Mode != "" && s.Kubernetes.Environment.Mode != kube.ModeNone {
		return true
	}
	for _, p := range s.Processes {
		if p.Kube.PodName != "" || p.Kube.PodUID != "" {
			return true
		}
	}
	return false
}
