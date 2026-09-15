// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/kube"
	"github.com/gputop/gputop/internal/metric"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/tui/widgets"
)

// The Kubernetes tab follows k9s: a pod list, a describe view (enter or d)
// and a logs view (l), with breadcrumbs to move between them.

type kubeView int

const (
	kubePods kubeView = iota
	kubeDescribe
	kubeLogs
)

const (
	logTail          = 1000
	logRefresh       = 2 * time.Second
	podEventsRefresh = 10 * time.Second
)

type kubeState struct {
	sel    int
	selUID string
	view   kubeView
	back   kubeView // view to return to from logs
	uid    string   // pod being described or tailed

	scroll, maxScroll int

	container  int
	logs       []string
	logSource  string
	logErr     error
	logLoading bool
	logAt      time.Time
	logKey     string
	follow     bool
	wrap       bool

	events    []kube.PodEvent
	evErr     error
	evLoading bool
	evUID     string
	evAt      time.Time
}

type kubeLogsMsg struct {
	key    string
	lines  []string
	source string
	err    error
}

type kubeEventsMsg struct {
	uid    string
	events []kube.PodEvent
	err    error
}

// podRow is a pod joined with the GPU processes running in it.
type podRow struct {
	info  kube.PodInfo
	procs []model.Process
	gpus  []string
	util  metric.Opt[float64]
	vram  metric.Opt[uint64]
}

func (r podRow) ref() kube.PodRef {
	return kube.PodRef{UID: r.info.UID, Name: r.info.Name, Namespace: r.info.Namespace}
}

// gpuPods lists GPU pods: those reported by the Kubernetes integration plus
// any pod that only appears through process attribution.
func (m *Model) gpuPods() []podRow {
	s := m.view()
	rows := map[string]*podRow{}
	var order []string
	key := func(uid, ns, name string) string {
		if uid != "" {
			return uid
		}
		return ns + "/" + name
	}
	byName := map[string]string{}
	for _, p := range s.Kubernetes.Pods {
		k := key(p.UID, p.Namespace, p.Name)
		rows[k] = &podRow{info: p}
		byName[p.Namespace+"/"+p.Name] = k
		order = append(order, k)
	}
	for _, p := range s.Processes {
		if p.Kube.PodName == "" && p.Kube.PodUID == "" {
			continue
		}
		k := key(p.Kube.PodUID, p.Kube.Namespace, p.Kube.PodName)
		if _, ok := rows[k]; !ok {
			if alt, ok := byName[p.Kube.Namespace+"/"+p.Kube.PodName]; ok && p.Kube.PodName != "" {
				k = alt
			}
		}
		r := rows[k]
		if r == nil {
			name := p.Kube.PodName
			if name == "" {
				name = "uid " + p.Kube.PodUID
			}
			r = &podRow{info: kube.PodInfo{UID: p.Kube.PodUID, Name: name, Namespace: p.Kube.Namespace,
				QoS: p.Kube.QoS, WorkloadKind: p.Kube.WorkloadKind, WorkloadName: p.Kube.WorkloadName, Source: "process"}}
			if p.Kube.Container != "" {
				r.info.Containers = []kube.ContainerInfo{{Name: p.Kube.Container, ID: p.Kube.ContainerID}}
			}
			rows[k] = r
			order = append(order, k)
		}
		if p.Kube.Container == "" && p.Kube.ContainerID != "" {
			for _, c := range r.info.Containers {
				if c.ID == p.Kube.ContainerID {
					p.Kube.Container = c.Name
				}
			}
		}
		r.procs = append(r.procs, p)
	}
	out := make([]podRow, 0, len(order))
	for _, k := range order {
		r := rows[k]
		seen := map[string]bool{}
		var util float64
		var vram uint64
		utilOK, vramOK := false, false
		for _, p := range r.procs {
			g := strconv.Itoa(p.DeviceIndex)
			if p.PartitionID != "" {
				g += ":" + strconv.Itoa(p.PartitionIndex)
			}
			if !seen[g] {
				seen[g] = true
				r.gpus = append(r.gpus, g)
			}
			if p.SMUtil.OK {
				util += p.SMUtil.V
				utilOK = true
			}
			if p.MemUsed.OK {
				vram += p.MemUsed.V
				vramOK = true
			}
		}
		sort.Strings(r.gpus)
		if utilOK {
			r.util = metric.Some(util)
		}
		if vramOK {
			r.vram = metric.Some(vram)
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].info.Namespace != out[j].info.Namespace {
			return out[i].info.Namespace < out[j].info.Namespace
		}
		return out[i].info.Name < out[j].info.Name
	})
	return out
}

func (m *Model) filteredPods() []podRow {
	q := m.q("kubernetes")
	var out []podRow
	for _, r := range m.gpuPods() {
		wl := ""
		if r.info.WorkloadName != "" {
			wl = r.info.WorkloadKind + "/" + r.info.WorkloadName
		}
		fields := map[string]string{
			"namespace": r.info.Namespace, "pod": r.info.Name, "name": r.info.Name, "status": r.info.Status(),
			"workload": wl, "node": r.info.Node, "gpu": strings.Join(r.gpus, ","),
		}
		rest, gpuFilters := splitGPUFilter(q)
		if matchesQuery(rest, fields) && podOnGPUs(r.gpus, gpuFilters) {
			out = append(out, r)
		}
	}
	return out
}

// splitGPUFilter separates gpu:N tokens, which match any of a pod's GPUs.
func splitGPUFilter(q *query) (*query, []string) {
	rest := &query{search: q.search}
	var gpus []string
	for _, tok := range strings.Fields(q.filter) {
		if v, ok := strings.CutPrefix(strings.ToLower(tok), "gpu:"); ok {
			gpus = append(gpus, v)
			continue
		}
		rest.filter += tok + " "
	}
	return rest, gpus
}

func podOnGPUs(gpus, want []string) bool {
	for _, w := range want {
		hit := false
		for _, g := range gpus {
			if g == w || strings.HasPrefix(g, w+":") {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

func (m *Model) currentPod() (podRow, bool) {
	for _, r := range m.gpuPods() {
		if r.info.UID == m.kube.uid {
			return r, true
		}
	}
	return podRow{}, false
}

func (m *Model) inspector() KubeInspector {
	if in, ok := m.src.(KubeInspector); ok && m.remote == "" {
		return in
	}
	return nil
}

func keysKube(m *Model, a keymap.Action) (bool, tea.Cmd) {
	st := &m.kube
	switch st.view {
	case kubeDescribe:
		switch a {
		case keymap.Back:
			st.view = kubePods
		case keymap.Logs:
			return true, m.openLogs(kubeDescribe)
		case keymap.Refresh:
			st.evAt = time.Time{}
			return true, m.fetchEvents(true)
		case keymap.Up:
			m.scrollKube(-1)
		case keymap.Down:
			m.scrollKube(1)
		case keymap.PageUp:
			m.scrollKube(-10)
		case keymap.PageDown:
			m.scrollKube(10)
		case keymap.Home:
			st.scroll = 0
		case keymap.End:
			st.scroll = st.maxScroll
		default:
			return false, nil
		}
		return true, nil
	case kubeLogs:
		switch a {
		case keymap.Back:
			st.view = st.back
		case keymap.Describe:
			st.view = kubeDescribe
			return true, m.fetchEvents(false)
		case keymap.Container:
			if pod, ok := m.currentPod(); ok && len(pod.info.Containers) > 1 {
				st.container = (st.container + 1) % len(pod.info.Containers)
				st.logs, st.logErr, st.follow = nil, nil, true
				return true, m.fetchLogs(true)
			}
		case keymap.SortNext:
			st.follow = !st.follow
		case keymap.Wrap:
			st.wrap = !st.wrap
		case keymap.Refresh:
			return true, m.fetchLogs(true)
		case keymap.Up:
			m.scrollLogs(-1)
		case keymap.Down:
			m.scrollLogs(1)
		case keymap.PageUp:
			m.scrollLogs(-20)
		case keymap.PageDown:
			m.scrollLogs(20)
		case keymap.Home:
			st.follow, st.scroll = false, 0
		case keymap.End:
			st.follow = true
		default:
			return false, nil
		}
		return true, nil
	}
	pods := m.filteredPods()
	switch a {
	case keymap.Select, keymap.Describe:
		if len(pods) > 0 {
			return true, m.openDescribe(pods[min(st.sel, len(pods)-1)])
		}
		return true, nil
	case keymap.Logs:
		if len(pods) > 0 {
			st.uid = pods[min(st.sel, len(pods)-1)].info.UID
			return true, m.openLogs(kubePods)
		}
		return true, nil
	}
	handled := listKeys(&st.sel, len(pods), 10, a)
	if handled && st.sel < len(pods) {
		st.selUID = pods[st.sel].info.UID
	}
	return handled, nil
}

func (m *Model) openDescribe(r podRow) tea.Cmd {
	st := &m.kube
	st.uid, st.selUID, st.view, st.scroll = r.info.UID, r.info.UID, kubeDescribe, 0
	return m.fetchEvents(false)
}

func (m *Model) openLogs(back kubeView) tea.Cmd {
	st := &m.kube
	st.back, st.view, st.follow, st.scroll = back, kubeLogs, true, 0
	if st.logKey != m.logKey() {
		st.logs, st.logErr, st.logSource = nil, nil, ""
	}
	return m.fetchLogs(true)
}

func (m *Model) scrollKube(delta int) {
	st := &m.kube
	st.scroll = max(0, min(st.maxScroll, st.scroll+delta))
}

func (m *Model) scrollLogs(delta int) {
	st := &m.kube
	if st.follow {
		st.scroll = st.maxScroll
	}
	st.scroll = max(0, min(st.maxScroll, st.scroll+delta))
	st.follow = st.scroll >= st.maxScroll && delta > 0
}

func (m *Model) logKey() string {
	return m.kube.uid + "/" + strconv.Itoa(m.kube.container)
}

// fetchLogs loads the selected container's log tail asynchronously.
func (m *Model) fetchLogs(force bool) tea.Cmd {
	st := &m.kube
	in := m.inspector()
	pod, ok := m.currentPod()
	if in == nil || !ok || st.logLoading || !force && m.now().Sub(st.logAt) < logRefresh {
		return nil
	}
	container := ""
	if names := pod.info.ContainerNames(); len(names) > 0 {
		st.container %= len(names)
		container = names[st.container]
	}
	key := m.logKey()
	st.logLoading, st.logAt = true, m.now()
	ref := pod.ref()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lines, source, err := in.PodLogs(ctx, ref, container, logTail)
		return kubeLogsMsg{key: key, lines: lines, source: source, err: err}
	}
}

// fetchEvents loads the described pod's events asynchronously.
func (m *Model) fetchEvents(force bool) tea.Cmd {
	st := &m.kube
	in := m.inspector()
	pod, ok := m.currentPod()
	if in == nil || !ok || st.evLoading {
		return nil
	}
	if !force && st.evUID == st.uid && m.now().Sub(st.evAt) < podEventsRefresh {
		return nil
	}
	if st.evUID != st.uid {
		st.events, st.evErr = nil, nil
	}
	st.evLoading, st.evAt, st.evUID = true, m.now(), st.uid
	ref := pod.ref()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		evs, err := in.PodEvents(ctx, ref)
		return kubeEventsMsg{uid: ref.UID, events: evs, err: err}
	}
}

// kubeTick refreshes logs (while following) and events of the open view.
func (m *Model) kubeTick() tea.Cmd {
	if m.activeID != "kubernetes" {
		return nil
	}
	switch m.kube.view {
	case kubeLogs:
		if m.kube.follow && !m.paused {
			return m.fetchLogs(false)
		}
	case kubeDescribe:
		return m.fetchEvents(false)
	}
	return nil
}

func (m *Model) onKubeLogs(msg kubeLogsMsg) {
	st := &m.kube
	st.logLoading = false
	if msg.key != m.logKey() {
		return
	}
	st.logs, st.logErr, st.logSource, st.logKey = msg.lines, msg.err, msg.source, msg.key
}

func (m *Model) onKubeEvents(msg kubeEventsMsg) {
	st := &m.kube
	st.evLoading = false
	if msg.uid != st.uid {
		return
	}
	st.events, st.evErr = msg.events, msg.err
}

func hintsKube(m *Model) []hint {
	switch m.kube.view {
	case kubeDescribe:
		return []hint{{keymap.Back, "pods"}, {keymap.Logs, "logs"}, {keymap.Down, "scroll"}, {keymap.Refresh, "refresh"}, {keymap.Command, "command"}}
	case kubeLogs:
		return []hint{{keymap.Back, "back"}, {keymap.Describe, "describe"}, {keymap.Container, "container"}, {keymap.SortNext, "autoscroll"}, {keymap.Wrap, "wrap"}, {keymap.End, "tail"}}
	}
	return []hint{{keymap.Up, "select"}, {keymap.Describe, "describe"}, {keymap.Logs, "logs"}, {keymap.Command, "command"}}
}

func viewKube(m *Model, w, h int) widgets.Block {
	st := &m.kube
	if st.view != kubePods {
		if _, ok := m.currentPod(); !ok {
			st.view = kubePods
			m.setToast("pod is gone", 2*time.Second)
		}
	}
	switch st.view {
	case kubeDescribe:
		return m.viewPodDescribe(w, h)
	case kubeLogs:
		return m.viewPodLogs(w, h)
	}
	return m.viewPodList(w, h)
}

// crumbs renders k9s-style breadcrumbs; each crumb is clickable.
func (m *Model) crumbs(w int, items []crumb) string {
	th := m.th
	var b strings.Builder
	for i, c := range items {
		st := th.TabInactive
		if i == len(items)-1 {
			st = th.TabActive
		}
		label := st.Render(" " + c.label + " ")
		if c.fn != nil {
			label = m.zone("crumb:"+c.label, label, func(bool) tea.Cmd { return c.fn() })
		}
		b.WriteString(label)
		b.WriteString(th.Base.Render(" "))
	}
	return widgets.Fit(th, b.String(), w)
}

type crumb struct {
	label string
	fn    func() tea.Cmd
}

func (m *Model) podCrumbs(r podRow, last string) []crumb {
	st := &m.kube
	items := []crumb{
		{"pods", func() tea.Cmd { st.view = kubePods; return nil }},
		{r.info.Namespace + "/" + r.info.Name, func() tea.Cmd { st.view, st.scroll = kubeDescribe, 0; return m.fetchEvents(false) }},
	}
	return append(items, crumb{last, nil})
}

func (m *Model) statusStyle(status string) lipgloss.Style {
	th := m.th
	switch status {
	case "Running":
		return th.OK
	case "Completed", "Succeeded":
		return th.Muted
	case "Pending", "ContainerCreating", "PodInitializing", "Terminating":
		return th.Warn
	case "", "—":
		return th.NA
	}
	return th.Crit
}

func (m *Model) viewPodList(w, h int) widgets.Block {
	th := m.th
	s := m.view()
	k := s.Kubernetes
	st := &m.kube

	api := th.Dim.Render("off")
	if k.APIEnabled {
		api = th.OK.Render("connected")
		if k.APIError != "" {
			api = th.Crit.Render(k.APIError)
		}
	}
	mode := string(k.Environment.Mode)
	if s.Node.Demo {
		mode = "simulated"
	}
	pods := m.filteredPods()
	info := th.Dim.Render(" Context ") + th.Text.Render(orDash(mode)) +
		th.Dim.Render("  Node ") + th.Text.Render(orDash(k.Environment.NodeName)) +
		th.Dim.Render("  API ") + api +
		th.Dim.Render("  GPU pods ") + th.Text.Render(fmt.Sprint(len(m.gpuPods()))) +
		th.Dim.Render("  Refreshed ") + th.Text.Render(fmtAgo(m.now(), k.LastRefresh))
	if k.Environment.Mode == kube.ModeKubeconfig {
		info += th.Dim.Render("  · run gputop on the GPU node for pod correlation")
	}

	if st.selUID != "" {
		for i, r := range pods {
			if r.info.UID == st.selUID {
				st.sel = i
				break
			}
		}
	}
	st.sel = widgets.Scroll(st.sel, 0, len(pods))
	now := m.now()
	tb := &widgets.Table{Columns: []widgets.Column{
		{Title: "NAMESPACE", Width: 14, Min: 6},
		{Title: "NAME", Width: 30, Min: 12, Flex: true},
		{Title: "READY", Width: 5, Align: widgets.Right},
		{Title: "STATUS", Width: 17, Min: 7},
		{Title: "RESTARTS", Width: 8, Align: widgets.Right, Priority: 3},
		{Title: "GPUS", Width: 8, Min: 4},
		{Title: "REQ", Width: 3, Align: widgets.Right, Priority: 5},
		{Title: "SM%", Width: 4, Align: widgets.Right},
		{Title: "VRAM", Width: 9, Align: widgets.Right, Priority: 1},
		{Title: "PROCS", Width: 5, Align: widgets.Right, Priority: 6},
		{Title: "WORKLOAD", Width: 26, Min: 10, Flex: true, Priority: 2},
		{Title: "IP", Width: 14, Priority: 7},
		{Title: "AGE", Width: 6, Align: widgets.Right, Priority: 4},
	}, Selected: st.sel, SortCol: 1}
	if len(pods) == 0 {
		tb.Selected = -1
	}
	for _, r := range pods {
		p := r.info
		ready, total := p.Ready()
		readyTxt, readySt := na, th.NA
		if total > 0 {
			readyTxt, readySt = fmt.Sprintf("%d/%d", ready, total), th.Text
			if ready < total {
				readySt = th.Warn
			}
		}
		status := p.Status()
		restarts := na
		restartSt := th.NA
		if p.Source == "api" {
			restarts, restartSt = fmt.Sprint(p.Restarts()), th.Dim
			if p.Restarts() > 0 {
				restartSt = th.Warn
			}
		}
		gpus := strings.Join(r.gpus, ",")
		gpuSt := th.Accent
		if gpus == "" {
			gpus, gpuSt = "—", th.Dim
		}
		req := "—"
		if p.GPURequests > 0 {
			req = fmt.Sprint(p.GPURequests)
		}
		wl := "—"
		if p.WorkloadName != "" {
			wl = p.WorkloadKind + "/" + p.WorkloadName
		}
		age := na
		if d := p.Age(now); d >= 0 {
			age = fmtDuration(d)
		}
		tb.Rows = append(tb.Rows, []widgets.Cell{
			widgets.C(th.Dim, p.Namespace), widgets.C(th.Text, p.Name), widgets.C(readySt, readyTxt),
			widgets.C(m.statusStyle(status), orDash(status)), widgets.C(restartSt, restarts),
			widgets.C(gpuSt, gpus), widgets.C(th.Dim, req),
			widgets.R(m.naOr(optF(r.util, "%.0f"), th.Gradient(r.util.V/100))), widgets.R(m.naOr(optBytes(r.vram), th.Text)),
			widgets.C(th.Dim, fmt.Sprint(len(r.procs))), widgets.C(th.Dim, wl), widgets.C(th.Dim, orDash(p.PodIP)),
			widgets.C(th.Dim, age),
		})
	}
	if m.mouse {
		tb.RowMark = func(r int, line string) string {
			pod := pods[r]
			return m.zone("pod:"+pod.info.UID+pod.info.Name, line, func(dbl bool) tea.Cmd {
				st.sel, st.selUID = r, pod.info.UID
				if dbl {
					return m.openDescribe(pod)
				}
				return nil
			})
		}
	}
	title := "Pods"
	if q := m.q("kubernetes"); q.filter != "" || q.search != "" {
		title = "Pods (" + strings.TrimSpace(q.search+" "+q.filter) + ")"
	}
	right := fmt.Sprintf("%d GPU pods · ⏎ describe · l logs", len(pods))
	content := tb.Render(th, w-2, h-3)
	if len(pods) == 0 {
		msg := "No pods are using GPUs on this node."
		if !k.Environment.Detected() && !s.Node.Demo {
			msg = "Kubernetes was not detected (see docs/kubernetes.md)."
		} else if len(m.gpuPods()) > 0 {
			msg = "No pods match the current filter (esc clears)."
		}
		content = widgets.FitBlock(th, []string{content[0], "", th.Dim.Render("  " + msg)}, w-2, h-3)
	}
	return widgets.VJoin(widgets.Block{widgets.Fit(th, info, w)}, widgets.Box(th, widgets.BoxOpts{Title: title, RightTitle: right, Focus: true}, w, h-1, content))
}

func (m *Model) viewPodDescribe(w, h int) widgets.Block {
	th := m.th
	st := &m.kube
	r, _ := m.currentPod()
	p := r.info
	now := m.now()
	cols := 1
	if w >= 150 {
		cols = 2
	}
	cw := w/cols - 2
	lw := 14
	kv := func(k, v string) string { return m.kv(k, v, lw) }
	txt := func(v string) string {
		if v == "" {
			return th.NA.Render("—")
		}
		return th.Text.Render(v)
	}
	ago := func(t time.Time) string {
		if t.IsZero() {
			return th.NA.Render("—")
		}
		return th.Text.Render(t.Local().Format("2006-01-02 15:04:05")) + th.Dim.Render(" ("+fmtAgo(now, t)+")")
	}

	status := p.Status()
	wl := ""
	if p.WorkloadName != "" {
		wl = p.WorkloadKind + "/" + p.WorkloadName
	}
	owner := ""
	if p.OwnerName != "" {
		owner = p.OwnerKind + "/" + p.OwnerName
	}
	ready, total := p.Ready()
	source := map[string]string{"api": "Kubernetes API", "node": "node pod logs (no API: status unknown)", "process": "process cgroup (no API)"}[p.Source]
	podLines := []string{
		kv("Name", th.Bold.Render(p.Name)),
		kv("Namespace", txt(p.Namespace)),
		kv("Status", m.statusStyle(status).Render(orDash(status))+th.Dim.Render(fmt.Sprintf("  ready %d/%d · restarts %d", ready, total, p.Restarts()))),
		kv("Node", txt(p.Node)),
		kv("Pod IP", txt(p.PodIP)+th.Dim.Render("  host ")+txt(p.HostIP)),
		kv("QoS", txt(p.QoS)),
		kv("Created", ago(p.Created)),
		kv("Workload", txt(wl)),
		kv("Controlled by", txt(owner)),
		kv("GPU requests", txt(orNA(fmt.Sprint(p.GPURequests), p.GPURequests > 0))),
		kv("UID", th.Dim.Render(orDash(p.UID))),
		kv("Source", th.Dim.Render(source)),
	}
	if p.Message != "" {
		for _, l := range wrap(p.Message, cw-lw-2) {
			podLines = append(podLines, kv("Message", th.Warn.Render(l)))
		}
	}
	secs := []section{{"Pod", podLines}}

	// GPUs used by the pod, with live trends.
	var gl []string
	s := m.view()
	for _, label := range r.gpus {
		idx, _ := strconv.Atoi(strings.SplitN(label, ":", 2)[0])
		for i := range s.GPUs {
			g := &s.GPUs[i]
			if g.Device.Index != idx {
				continue
			}
			spark := widgets.Sparkline(th, m.live.values("util/"+string(g.Device.ID), 24), 24, 100)
			gl = append(gl,
				th.Accent.Render(fmt.Sprintf("GPU %-4s", label))+th.Text.Render(shortName(g.Device.Name))+th.Dim.Render("  ")+m.stateCell(g),
				"  "+th.Dim.Render("util ")+m.pctBar(g.Sample.UtilPercent, 18)+th.Dim.Render("  vram ")+m.naOr(fmtPctOpt(g.Derived.VRAMFraction), th.Text)+
					th.Dim.Render("  ")+m.naOr(m.temp(g.Sample.TempC), th.Text)+th.Dim.Render("  ")+m.naOr(optF(g.Sample.PowerW, "%.0f W"), th.Text),
				"  "+th.Dim.Render("60s ")+spark,
			)
		}
	}
	if len(gl) == 0 {
		gl = []string{th.Dim.Render("No GPU processes in this pod right now.")}
	}
	secs = append(secs, section{"GPUs", gl})

	var cl []string
	for i, c := range p.Containers {
		if i > 0 {
			cl = append(cl, "")
		}
		state := c.State
		stStyle := th.OK
		switch c.State {
		case "waiting":
			stStyle = th.Warn
		case "terminated":
			stStyle = th.Crit
		case "":
			state, stStyle = "unknown", th.NA
		}
		if c.StateReason != "" {
			state += " (" + c.StateReason + ")"
		}
		readyTxt := th.OK.Render("ready")
		if !c.Ready {
			readyTxt = th.Warn.Render("not ready")
		}
		cl = append(cl, th.Bold.Render(c.Name)+"  "+stStyle.Render(state)+"  "+readyTxt)
		if c.Image != "" {
			cl = append(cl, kv("  Image", th.Text.Render(c.Image)))
		}
		if !c.StartedAt.IsZero() {
			cl = append(cl, kv("  Started", ago(c.StartedAt)))
		}
		if c.Restarts > 0 || c.LastState != "" {
			last := ""
			if c.LastState != "" {
				last = "  last " + c.LastState
				if !c.LastFinished.IsZero() {
					last += " " + fmtAgo(now, c.LastFinished)
				}
			}
			cl = append(cl, kv("  Restarts", th.Warn.Render(fmt.Sprint(c.Restarts))+th.Crit.Render(last)))
		}
		if c.StateMessage != "" {
			cl = append(cl, kv("  Message", th.Warn.Render(c.StateMessage)))
		}
		if len(c.Requests) > 0 || len(c.Limits) > 0 {
			cl = append(cl, kv("  Requests", m.resources(c.Requests)), kv("  Limits", m.resources(c.Limits)))
		}
		if c.ID != "" {
			cl = append(cl, kv("  Container ID", th.Dim.Render(kube.ShortID(c.ID))))
		}
	}
	if len(cl) == 0 {
		cl = []string{th.Dim.Render("Container details need Kubernetes API access.")}
	}
	secs = append(secs, section{"Containers", cl})

	if len(r.procs) > 0 {
		pl := []string{th.Dim.Render(fmt.Sprintf("%8s %-16s %-12s %4s %9s %4s", "PID", "PROCESS", "CONTAINER", "GPU", "VRAM", "SM%"))}
		for _, pr := range r.procs {
			g := strconv.Itoa(pr.DeviceIndex)
			if pr.PartitionID != "" {
				g += ":" + strconv.Itoa(pr.PartitionIndex)
			}
			pl = append(pl, th.Text.Render(fmt.Sprintf("%8d %-16s %-12s %4s %9s %4s", pr.PID, truncate(pr.Name, 16), truncate(orDash(pr.Kube.Container), 12), g, optBytes(pr.MemUsed), optF(pr.SMUtil, "%.0f"))))
		}
		secs = append(secs, section{"Processes", pl})
	}

	if len(p.Conditions) > 0 {
		var cond []string
		for _, c := range p.Conditions {
			cs := th.OK
			if c.Status != "True" {
				cs = th.Warn
			}
			line := th.Text.Render(fmt.Sprintf("%-16s", c.Type)) + cs.Render(fmt.Sprintf("%-6s", c.Status))
			if !c.LastTransition.IsZero() {
				line += th.Dim.Render(fmtAgo(now, c.LastTransition))
			}
			if c.Reason != "" {
				line += th.Warn.Render("  " + c.Reason)
			}
			cond = append(cond, line)
			if c.Message != "" {
				for _, l := range wrap(c.Message, cw-6) {
					cond = append(cond, th.Dim.Render("  "+l))
				}
			}
		}
		secs = append(secs, section{"Conditions", cond})
	}

	if len(p.Labels) > 0 {
		var ll []string
		for _, l := range kube.SortedLabels(p.Labels) {
			ll = append(ll, th.Dim.Render(truncate(l, cw-2)))
		}
		secs = append(secs, section{"Labels", ll})
	}

	var el []string
	switch {
	case m.inspector() == nil:
		el = []string{th.Dim.Render("Events are fetched by the local agent; not available here.")}
	case st.evErr != nil && len(st.events) == 0:
		for _, l := range wrap(st.evErr.Error(), cw-2) {
			el = append(el, th.Warn.Render(l))
		}
	case st.evLoading && len(st.events) == 0:
		el = []string{th.Muted.Render("loading…")}
	case len(st.events) == 0:
		el = []string{th.Dim.Render("No events.")}
	}
	for i := len(st.events) - 1; i >= 0; i-- {
		e := st.events[i]
		es := th.Accent
		if e.Type == "Warning" {
			es = th.Warn
		}
		count := ""
		if e.Count > 1 {
			count = fmt.Sprintf(" ×%d", e.Count)
		}
		el = append(el, th.Dim.Render(fmt.Sprintf("%6s ", fmtAgo(now, e.Last)))+es.Render(e.Reason)+th.Dim.Render(count+"  "+e.From))
		for _, l := range wrap(e.Message, cw-9) {
			el = append(el, widgets.Space(th, 7)+th.Text.Render(l))
		}
	}
	secs = append(secs, section{"Events", el})

	header := m.crumbs(w, m.podCrumbs(r, "describe"))
	block, maxScroll := m.flowSections(secs, w, h-1, cols, st.scroll, "")
	st.maxScroll = maxScroll
	st.scroll = min(st.scroll, maxScroll)
	block = widgets.Scrollbar(th, block, st.scroll, maxScroll)
	if m.mouse {
		for i, l := range block {
			block[i] = m.zones.markWheel("kube:describe", l, nil, func(delta int) tea.Cmd {
				m.scrollKube(delta)
				return nil
			})
		}
	}
	return widgets.VJoin(widgets.Block{header}, block)
}

func fmtPctOpt(o metric.Opt[float64]) string {
	if !o.OK {
		return na
	}
	return fmt.Sprintf("%.0f%%", o.V*100)
}

// resources renders a requests/limits map with GPU resources highlighted.
func (m *Model) resources(res map[string]string) string {
	th := m.th
	if len(res) == 0 {
		return th.NA.Render("—")
	}
	keys := make([]string, 0, len(res))
	for k := range res {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		st := th.Text
		if strings.HasSuffix(k, "/gpu") || strings.Contains(k, "/mig-") {
			st = th.Accent.Bold(true)
		}
		parts = append(parts, th.Dim.Render(k+"=")+st.Render(res[k]))
	}
	return strings.Join(parts, th.Dim.Render(" "))
}

func (m *Model) viewPodLogs(w, h int) widgets.Block {
	th := m.th
	st := &m.kube
	r, _ := m.currentPod()
	names := r.info.ContainerNames()
	container := "—"
	if len(names) > 0 {
		st.container %= len(names)
		container = names[st.container]
	}
	header := m.crumbs(w, m.podCrumbs(r, "logs"))
	inner := w - 2
	bodyH := h - 3

	var lines []string
	switch {
	case m.inspector() == nil:
		lines = []string{th.Dim.Render("Logs are fetched by the local agent; not available for remote nodes.")}
	case st.logErr != nil && len(st.logs) == 0:
		lines = []string{th.Warn.Render(st.logErr.Error())}
	case st.logLoading && len(st.logs) == 0:
		lines = []string{th.Muted.Render("loading…")}
	case len(st.logs) == 0:
		lines = []string{th.Dim.Render("No log lines.")}
	}
	search := strings.ToLower(strings.TrimSpace(m.q("kubernetes").search))
	matched := 0
	for _, l := range st.logs {
		if search != "" && !strings.Contains(strings.ToLower(l), search) {
			continue
		}
		matched++
		if st.wrap {
			for len([]rune(l)) > inner {
				rs := []rune(l)
				lines = append(lines, m.logLine(string(rs[:inner])))
				l = string(rs[inner:])
			}
		}
		lines = append(lines, m.logLine(l))
	}
	st.maxScroll = max(0, len(lines)-bodyH)
	if st.follow {
		st.scroll = st.maxScroll
	}
	st.scroll = max(0, min(st.scroll, st.maxScroll))
	visible := lines[st.scroll:min(len(lines), st.scroll+bodyH)]

	flag := func(name string, on bool) string {
		if on {
			return name + " on"
		}
		return name + " off"
	}
	right := flag("autoscroll", st.follow) + " · " + flag("wrap", st.wrap)
	if search != "" {
		right = fmt.Sprintf("%d match /%s · ", matched, search) + right
	}
	if st.logSource != "" {
		right += " · " + st.logSource
	}
	title := fmt.Sprintf("Logs %s/%s:%s", r.info.Namespace, r.info.Name, container)
	if len(names) > 1 {
		title += fmt.Sprintf(" (%d/%d, c next)", st.container+1, len(names))
	}
	box := widgets.Box(th, widgets.BoxOpts{Title: title, RightTitle: right, Focus: true}, w, h-1, visible)
	box = widgets.Scrollbar(th, box, st.scroll, st.maxScroll)
	if m.mouse {
		for i, l := range box {
			box[i] = m.zones.markWheel("kube:logs", l, nil, func(delta int) tea.Cmd {
				m.scrollLogs(delta)
				return nil
			})
		}
	}
	return widgets.VJoin(widgets.Block{header}, box)
}

func (m *Model) logLine(l string) string {
	th := m.th
	st := th.Text
	upper := strings.ToUpper(l)
	switch {
	case strings.Contains(upper, "ERROR") || strings.Contains(upper, "FATAL") || strings.Contains(upper, "PANIC") || strings.Contains(l, "Traceback"):
		st = th.Crit
	case strings.Contains(upper, "WARN"):
		st = th.Warn
	}
	// Dim a leading timestamp.
	if i := strings.IndexByte(l, ' '); i >= 19 && i <= 35 && (l[4] == '-' || l[0] == '[') {
		return th.Dim.Render(l[:i]) + st.Render(l[i:])
	}
	return st.Render(l)
}
