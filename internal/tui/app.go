// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package tui implements gputop's interactive terminal interface with
// Bubble Tea. It renders snapshots from a Source and never calls providers
// directly.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gputop/gputop/internal/gpu"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/keymap"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/theme"
	"github.com/gputop/gputop/internal/tui/widgets"
)

// Options configure the UI.
type Options struct {
	Source     Source
	Theme      *theme.Theme
	Keys       *keymap.Map
	DefaultTab string
	Fahrenheit bool
	ShowCmd    bool
	Refresh    time.Duration
	Remote     string
	Nodes      NodeLister
	// Notices are shown briefly at startup (e.g. config warnings).
	Notices []string
}

// Model is the Bubble Tea model.
type Model struct {
	src        Source
	th         *theme.Theme
	keys       *keymap.Map
	fahrenheit bool
	showCmd    bool
	refresh    time.Duration
	remote     string
	nodes      NodeLister

	sub       <-chan *model.Snapshot
	unsub     func()
	snap      *model.Snapshot
	frozen    *model.Snapshot
	live      *live
	width     int
	height    int
	tabs      []*tabDef
	activeID  string
	help      bool
	paused    bool
	toast     string
	toastTill time.Time
	now       func() time.Time

	input      *inputState
	queries    map[string]*query
	renderTime time.Duration

	// Shared GPU selection across GPU-centric tabs.
	selGPU gpu.ID

	gpus     gpusState
	procs    procsState
	events   listState
	hist     histState
	nodesSel int
	netSel   int
	workSel  int
	kubeSel  int
}

type query struct {
	search string
	filter string
}

type inputState struct {
	mode  string // "search" or "filter"
	value string
	prev  string
}

type listState struct {
	sel    int
	detail bool
}

type snapshotMsg struct{ s *model.Snapshot }
type historyMsg struct {
	key string
	res *history.Result
	err error
}
type tickMsg time.Time

// New creates the model.
func New(o Options) *Model {
	m := &Model{
		src: o.Source, th: o.Theme, keys: o.Keys, fahrenheit: o.Fahrenheit, showCmd: o.ShowCmd,
		refresh: o.Refresh, remote: o.Remote, nodes: o.Nodes, live: newLive(),
		queries: map[string]*query{}, now: time.Now, activeID: o.DefaultTab,
	}
	if m.keys == nil {
		m.keys = keymap.Default()
	}
	m.tabs = allTabs()
	m.snap = o.Source.Latest()
	m.procs.sortKey, m.procs.sortDesc = sortVRAM, true
	m.hist.window = 3 // 15m
	if len(o.Notices) > 0 {
		m.setToast(strings.Join(o.Notices, " · "), 8*time.Second)
	}
	return m
}

// Run starts the UI and blocks until the user quits or ctx is cancelled.
func Run(ctx context.Context, o Options) error {
	m := New(o)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	if m.unsub != nil {
		m.unsub()
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		err = nil // cancelled by signal: a normal shutdown
	}
	return err
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	m.sub, m.unsub = m.src.Subscribe()
	return tea.Batch(waitSnapshot(m.sub), tick(), tea.SetWindowTitle("gputop"))
}

func waitSnapshot(ch <-chan *model.Snapshot) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return nil
		}
		return snapshotMsg{s}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) setToast(s string, d time.Duration) {
	m.toast, m.toastTill = s, m.now().Add(d)
}

// view returns the snapshot to render (frozen while paused).
func (m *Model) view() *model.Snapshot {
	if m.paused && m.frozen != nil {
		return m.frozen
	}
	return m.snap
}

func (m *Model) q(tabID string) *query {
	q := m.queries[tabID]
	if q == nil {
		q = &query{}
		m.queries[tabID] = q
	}
	return q
}

// visibleTabs returns the tabs relevant for the current snapshot.
func (m *Model) visibleTabs() []*tabDef {
	var out []*tabDef
	for _, t := range m.tabs {
		if t.visible == nil || t.visible(m) {
			out = append(out, t)
		}
	}
	return out
}

func (m *Model) activeTab() *tabDef {
	vis := m.visibleTabs()
	for _, t := range vis {
		if t.id == m.activeID {
			return t
		}
	}
	if len(vis) == 0 {
		return m.tabs[0]
	}
	return vis[0]
}

func (m *Model) switchTab(delta int) {
	vis := m.visibleTabs()
	cur := m.activeTab()
	for i, t := range vis {
		if t == cur {
			m.activeID = vis[(i+delta+len(vis))%len(vis)].id
			m.onEnterTab()
			return
		}
	}
}

func (m *Model) onEnterTab() {
	m.input = nil
	if m.activeID == "history" {
		m.hist.stale = true
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case snapshotMsg:
		if msg.s != nil {
			m.snap = msg.s
			m.live.observe(msg.s)
			m.ensureSelection()
		}
		cmds := []tea.Cmd{waitSnapshot(m.sub)}
		if m.activeID == "history" {
			cmds = append(cmds, m.maybeQueryHistory())
		}
		return m, tea.Batch(cmds...)
	case historyMsg:
		m.hist.loading = false
		if msg.key == m.hist.queryKey() {
			m.hist.res, m.hist.err = msg.res, msg.err
		}
		return m, nil
	case tickMsg:
		if m.activeID == "history" {
			return m, tea.Batch(tick(), m.maybeQueryHistory())
		}
		return m, tick()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) ensureSelection() {
	s := m.view()
	if len(s.GPUs) == 0 {
		return
	}
	if _, ok := s.GPUByID(m.selGPU); !ok {
		m.selGPU = s.GPUs[0].Device.ID
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.input != nil {
		return m.handleInput(msg)
	}
	a := m.keys.Lookup(key)
	if m.help {
		if a == keymap.Quit && key == "ctrl+c" {
			return m, tea.Quit
		}
		if a == keymap.Help || a == keymap.Back || a == keymap.Quit {
			m.help = false
		}
		return m, nil
	}

	t := m.activeTab()
	// Tab-local handling first for Back/Select so detail views can close.
	if a == keymap.Back || a == keymap.Select {
		if t.keys != nil {
			if handled, cmd := t.keys(m, a); handled {
				return m, cmd
			}
		}
		if a == keymap.Back {
			q := m.q(t.id)
			if q.search != "" || q.filter != "" {
				q.search, q.filter = "", ""
				m.setToast("filters cleared", 2*time.Second)
			}
		}
		return m, nil
	}

	switch a {
	case keymap.Quit:
		return m, tea.Quit
	case keymap.Help:
		m.help = true
		return m, nil
	case keymap.NextTab:
		m.switchTab(1)
		return m, m.enterCmd()
	case keymap.PrevTab:
		m.switchTab(-1)
		return m, m.enterCmd()
	case keymap.History:
		m.activeID = "history"
		m.onEnterTab()
		return m, m.enterCmd()
	case keymap.Refresh:
		m.src.RefreshNow()
		m.hist.stale = true
		m.setToast("refreshing…", 1500*time.Millisecond)
		return m, m.enterCmd()
	case keymap.Pause:
		m.paused = !m.paused
		if m.paused {
			m.frozen = m.snap
			m.setToast("display paused — collection continues", 3*time.Second)
		} else {
			m.frozen = nil
			m.setToast("resumed", 1500*time.Millisecond)
		}
		return m, nil
	case keymap.Search, keymap.Filter:
		if t.searchable {
			mode := "search"
			cur := m.q(t.id).search
			if a == keymap.Filter {
				mode, cur = "filter", m.q(t.id).filter
			}
			m.input = &inputState{mode: mode, value: cur, prev: cur}
		} else {
			m.setToast("this tab has no search/filter", 2*time.Second)
		}
		return m, nil
	}
	if idx, ok := keymap.TabIndex(a); ok {
		vis := m.visibleTabs()
		if idx < len(vis) {
			m.activeID = vis[idx].id
			m.onEnterTab()
			return m, m.enterCmd()
		}
		return m, nil
	}
	if t.keys != nil {
		_, cmd := t.keys(m, a)
		return m, cmd
	}
	return m, nil
}

func (m *Model) enterCmd() tea.Cmd {
	if m.activeID == "history" {
		return m.maybeQueryHistory()
	}
	return nil
}

func (m *Model) handleInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	in := m.input
	t := m.activeTab()
	q := m.q(t.id)
	apply := func() {
		if in.mode == "search" {
			q.search = in.value
		} else {
			q.filter = in.value
		}
	}
	switch msg.Type {
	case tea.KeyEsc:
		in.value = in.prev
		apply()
		m.input = nil
	case tea.KeyEnter:
		apply()
		m.input = nil
	case tea.KeyBackspace:
		if r := []rune(in.value); len(r) > 0 {
			in.value = string(r[:len(r)-1])
		}
		apply()
	case tea.KeyCtrlU:
		in.value = ""
		apply()
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyRunes, tea.KeySpace:
		in.value += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			in.value += " "
		}
		apply()
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	start := time.Now()
	defer func() { m.renderTime = time.Since(start) }()
	w, h := m.width, m.height
	if w == 0 || h == 0 {
		return "starting gputop…"
	}
	th := m.th
	if w < 60 || h < 15 {
		lines := widgets.FitBlock(th, []string{
			th.Title.Render("gputop"),
			th.Warn.Render(fmt.Sprintf("Terminal too small: %d×%d", w, h)),
			th.Dim.Render("Resize to at least 60×15 (80×24 recommended)."),
		}, w, h)
		return strings.Join(lines, "\n")
	}

	body := h - 3
	var content widgets.Block
	s := m.view()
	switch {
	case m.help:
		content = m.viewHelp(w, body)
	case !s.Ready:
		content = m.viewLoading(w, body)
	default:
		t := m.activeTab()
		content = t.view(m, w, body)
	}
	content = widgets.FitBlock(th, content, w, body)

	out := make([]string, 0, h)
	out = append(out, m.viewHeader(w), m.viewTabBar(w))
	out = append(out, content...)
	out = append(out, m.viewFooter(w))
	return strings.Join(out, "\n")
}
