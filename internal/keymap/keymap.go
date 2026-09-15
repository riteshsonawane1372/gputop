// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package keymap maps key presses to UI actions. All key bindings are
// defined here; UI code matches on actions, never on literal keys.
package keymap

import (
	"fmt"
	"sort"
	"strings"
)

// Action is a user intent.
type Action string

const (
	None        Action = ""
	Quit        Action = "quit"
	Help        Action = "help"
	NextTab     Action = "next_tab"
	PrevTab     Action = "prev_tab"
	History     Action = "history"
	Refresh     Action = "refresh"
	Filter      Action = "filter"
	Search      Action = "search"
	Select      Action = "select"
	Back        Action = "back"
	Up          Action = "up"
	Down        Action = "down"
	Left        Action = "left"
	Right       Action = "right"
	PageUp      Action = "page_up"
	PageDown    Action = "page_down"
	Home        Action = "home"
	End         Action = "end"
	ZoomIn      Action = "zoom_in"
	ZoomOut     Action = "zoom_out"
	SortNext    Action = "sort_next"
	SortReverse Action = "sort_reverse"
	Pause       Action = "pause"
	NextMetric  Action = "next_metric"
	PrevMetric  Action = "prev_metric"
	NextGPU     Action = "next_gpu"
	PrevGPU     Action = "prev_gpu"
	ScrubBack   Action = "scrub_back"
	ScrubFwd    Action = "scrub_forward"
	ScrubNow    Action = "scrub_now"
	Tab1        Action = "tab_1"
	Tab2        Action = "tab_2"
	Tab3        Action = "tab_3"
	Tab4        Action = "tab_4"
	Tab5        Action = "tab_5"
	Tab6        Action = "tab_6"
	Tab7        Action = "tab_7"
	Tab8        Action = "tab_8"
	Tab9        Action = "tab_9"
)

// Binding describes an action for help screens.
type Binding struct {
	Action Action
	Keys   []string
	Help   string
}

// defaults is the canonical binding table (order = help order).
var defaults = []Binding{
	{Quit, []string{"q", "ctrl+c"}, "quit"},
	{Help, []string{"?"}, "toggle help"},
	{NextTab, []string{"tab", "right"}, "next tab"},
	{PrevTab, []string{"shift+tab", "left"}, "previous tab"},
	{Tab1, []string{"1"}, "tab 1"}, {Tab2, []string{"2"}, "tab 2"}, {Tab3, []string{"3"}, "tab 3"},
	{Tab4, []string{"4"}, "tab 4"}, {Tab5, []string{"5"}, "tab 5"}, {Tab6, []string{"6"}, "tab 6"},
	{Tab7, []string{"7"}, "tab 7"}, {Tab8, []string{"8"}, "tab 8"}, {Tab9, []string{"9"}, "tab 9"},
	{History, []string{"h"}, "history (time machine)"},
	{Refresh, []string{"r"}, "refresh now"},
	{Filter, []string{"f"}, "filter (key:value)"},
	{Search, []string{"/"}, "search"},
	{Select, []string{"enter"}, "open detail"},
	{Back, []string{"esc"}, "back / clear"},
	{Up, []string{"up", "k"}, "move up"},
	{Down, []string{"down", "j"}, "move down"},
	// The arrow keys switch tabs; left/right stay bindable for users who
	// prefer them inside tables.
	{Left, nil, "move left"},
	{Right, []string{"l"}, "move right"},
	{PageUp, []string{"pgup", "ctrl+u"}, "page up"},
	{PageDown, []string{"pgdown", "ctrl+d"}, "page down"},
	{Home, []string{"home", "g"}, "first row"},
	{End, []string{"end", "G"}, "last row"},
	{ZoomIn, []string{"+", "="}, "zoom in (shorter window)"},
	{ZoomOut, []string{"-", "_"}, "zoom out (longer window)"},
	{SortNext, []string{"s"}, "next sort column"},
	{SortReverse, []string{"S"}, "reverse sort"},
	{Pause, []string{"p", " "}, "pause/resume display"},
	{NextMetric, []string{"m"}, "next metric"},
	{PrevMetric, []string{"M"}, "previous metric"},
	{NextGPU, []string{"]"}, "next GPU"},
	{PrevGPU, []string{"["}, "previous GPU"},
	{ScrubBack, []string{","}, "scrub back in time"},
	{ScrubFwd, []string{"."}, "scrub forward in time"},
	{ScrubNow, []string{"n"}, "jump to now"},
}

// Map resolves keys to actions.
type Map struct {
	byKey    map[string]Action
	bindings []Binding
}

// Default returns the built-in key map.
func Default() *Map {
	m, _ := New(nil)
	return m
}

// Actions returns every known action name (for config validation/docs).
func Actions() []string {
	out := make([]string, 0, len(defaults))
	for _, b := range defaults {
		out = append(out, string(b.Action))
	}
	sort.Strings(out)
	return out
}

// New builds a key map from defaults plus overrides (action -> keys). An
// override replaces all default keys for that action. Conflicts (one key
// bound to two actions) are reported as errors.
func New(overrides map[string][]string) (*Map, error) {
	known := map[Action]int{}
	bindings := make([]Binding, len(defaults))
	for i, b := range defaults {
		bindings[i] = Binding{Action: b.Action, Keys: append([]string(nil), b.Keys...), Help: b.Help}
		known[b.Action] = i
	}
	var errs []string
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		idx, ok := known[Action(name)]
		if !ok {
			errs = append(errs, fmt.Sprintf("keys.%s: unknown action (known: %s)", name, strings.Join(Actions(), ", ")))
			continue
		}
		var keys []string
		for _, k := range overrides[name] {
			nk := Normalize(k)
			if nk == "" {
				errs = append(errs, fmt.Sprintf("keys.%s: empty key", name))
				continue
			}
			keys = append(keys, nk)
		}
		bindings[idx].Keys = keys
	}

	m := &Map{byKey: map[string]Action{}, bindings: bindings}
	for _, b := range bindings {
		for _, k := range b.Keys {
			if prev, dup := m.byKey[k]; dup && prev != b.Action {
				errs = append(errs, fmt.Sprintf("key %q is bound to both %s and %s", k, prev, b.Action))
				continue
			}
			m.byKey[k] = b.Action
		}
	}
	if len(errs) > 0 {
		return Default(), fmt.Errorf("invalid key bindings:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return m, nil
}

// Normalize canonicalizes a key name as reported by the terminal library.
func Normalize(k string) string {
	if k == " " || strings.EqualFold(k, "space") {
		return " "
	}
	k = strings.TrimSpace(k)
	switch strings.ToLower(k) {
	case "return":
		return "enter"
	case "escape":
		return "esc"
	case "pageup":
		return "pgup"
	case "pagedown":
		return "pgdown"
	case "backtab":
		return "shift+tab"
	}
	if len(k) == 1 {
		return k // keep case: "G" differs from "g"
	}
	return strings.ToLower(k)
}

// Lookup returns the action for a key string.
func (m *Map) Lookup(key string) Action { return m.byKey[Normalize(key)] }

// Keys returns the keys bound to an action.
func (m *Map) Keys(a Action) []string {
	for _, b := range m.bindings {
		if b.Action == a {
			return b.Keys
		}
	}
	return nil
}

// Label returns a short display label for an action's primary key.
func (m *Map) Label(a Action) string {
	keys := m.Keys(a)
	if len(keys) == 0 {
		return ""
	}
	return DisplayKey(keys[0])
}

// DisplayKey renders a key for help text.
func DisplayKey(k string) string {
	switch k {
	case " ":
		return "space"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "enter":
		return "⏎"
	case "shift+tab":
		return "⇧tab"
	}
	return k
}

// Bindings returns all bindings for help display.
func (m *Map) Bindings() []Binding { return m.bindings }

// TabIndex returns the 0-based tab index for Tab1..Tab9 actions.
func TabIndex(a Action) (int, bool) {
	if strings.HasPrefix(string(a), "tab_") && len(a) == 5 {
		return int(a[4] - '1'), true
	}
	return 0, false
}
