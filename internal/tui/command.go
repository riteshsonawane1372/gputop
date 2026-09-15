// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The command bar (":") works like k9s: type a view alias to jump to it,
// "ns NAME" to scope the Kubernetes view to a namespace, or part of a pod
// name to describe it. Tab completes the first suggestion.

type command struct {
	names []string // first is canonical
	tab   string
	help  string
}

var commands = []command{
	{[]string{"overview", "ov", "pulse"}, "overview", "fleet overview"},
	{[]string{"gpus", "gpu"}, "gpus", "GPU list and detail"},
	{[]string{"processes", "ps", "proc"}, "processes", "GPU processes"},
	{[]string{"memory", "mem", "vram"}, "memory", "GPU memory"},
	{[]string{"power", "pwr"}, "power", "power and clocks"},
	{[]string{"thermals", "temp"}, "thermals", "temperatures"},
	{[]string{"nvlink", "nvl"}, "nvlink", "NVLink"},
	{[]string{"pcie"}, "pcie", "PCIe"},
	{[]string{"mig"}, "mig", "Multi-Instance GPU"},
	{[]string{"nodes", "no"}, "nodes", "remote nodes"},
	{[]string{"pods", "po", "pod", "kubernetes", "k8s"}, "kubernetes", "GPU pods"},
	{[]string{"workloads", "wl", "deploy", "jobs"}, "workloads", "workloads"},
	{[]string{"network", "net"}, "network", "network"},
	{[]string{"dashboard", "dash", "grafana", "dcgm", "metrics"}, "dashboard", "DCGM-style metrics dashboard"},
	{[]string{"history", "hist"}, "history", "time machine"},
	{[]string{"events", "ev"}, "events", "event timeline"},
	{[]string{"health"}, "health", "health scores"},
	{[]string{"ns"}, "", "ns NAME: filter pods by namespace (ns all clears)"},
	{[]string{"help", "?"}, "", "key bindings"},
	{[]string{"quit", "q"}, "", "quit gputop"},
}

func (m *Model) gotoTab(id string) tea.Cmd {
	for _, t := range m.visibleTabs() {
		if t.id == id {
			m.activeID = id
			m.onEnterTab()
			return m.enterCmd()
		}
	}
	m.setToast(id+" is not available here", 2*time.Second)
	return nil
}

// runCommand executes a command-bar line.
func (m *Model) runCommand(line string) tea.Cmd {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	name, arg := strings.ToLower(fields[0]), strings.Join(fields[1:], " ")
	switch name {
	case "q", "quit", "q!":
		return tea.Quit
	case "help", "?":
		m.help = true
		return nil
	case "ns", "namespace":
		q := m.q("kubernetes")
		q.filter = strings.TrimSpace(removeToken(q.filter, "ns:"))
		if arg != "" && arg != "all" && arg != "-" {
			q.filter = strings.TrimSpace(q.filter + " ns:" + arg)
		}
		m.kube.view = kubePods
		return m.gotoTab("kubernetes")
	}
	for _, c := range commands {
		for _, n := range c.names {
			if n == name && c.tab != "" {
				cmd := m.gotoTab(c.tab)
				if c.tab == "kubernetes" {
					m.kube.view = kubePods
					if arg != "" {
						return m.describeByName(arg)
					}
				}
				return cmd
			}
		}
	}
	// Anything else: a pod name (or part of one).
	if cmd := m.describeByName(line); cmd != nil || m.activeID == "kubernetes" && m.kube.view == kubeDescribe {
		return cmd
	}
	m.setToast("unknown command: "+line+" (try :pods, :gpus, :dash, :ns NAME)", 3*time.Second)
	return nil
}

// describeByName opens the describe view of the best matching pod.
func (m *Model) describeByName(name string) tea.Cmd {
	name = strings.ToLower(strings.TrimSpace(name))
	var best *podRow
	pods := m.gpuPods()
	for i := range pods {
		n := strings.ToLower(pods[i].info.Name)
		full := strings.ToLower(pods[i].info.Namespace + "/" + pods[i].info.Name)
		if n == name || full == name {
			best = &pods[i]
			break
		}
		if best == nil && strings.Contains(full, name) {
			best = &pods[i]
		}
	}
	if best == nil {
		return nil
	}
	m.gotoTab("kubernetes")
	return m.openDescribe(*best)
}

func removeToken(filter, prefix string) string {
	var keep []string
	for _, tok := range strings.Fields(filter) {
		if !strings.HasPrefix(strings.ToLower(tok), prefix) {
			keep = append(keep, tok)
		}
	}
	return strings.Join(keep, " ")
}

// suggestions lists completions for the command bar input.
func (m *Model) suggestions(input string) []string {
	input = strings.ToLower(strings.TrimLeft(input, " "))
	var out []string
	if rest, ok := strings.CutPrefix(input, "ns "); ok {
		seen := map[string]bool{}
		for _, p := range m.gpuPods() {
			ns := p.info.Namespace
			if !seen[ns] && strings.HasPrefix(ns, rest) {
				seen[ns] = true
				out = append(out, "ns "+ns)
			}
		}
		sort.Strings(out)
		return append(out, "ns all")
	}
	if strings.Contains(input, " ") {
		return nil
	}
	visible := map[string]bool{}
	for _, t := range m.visibleTabs() {
		visible[t.id] = true
	}
	for _, c := range commands {
		if c.tab != "" && !visible[c.tab] {
			continue
		}
		for _, n := range c.names {
			if strings.HasPrefix(n, input) {
				out = append(out, c.names[0])
				break
			}
		}
	}
	if input != "" {
		for _, p := range m.gpuPods() {
			if strings.Contains(strings.ToLower(p.info.Name), input) {
				out = append(out, p.info.Name)
			}
		}
	}
	return out
}
