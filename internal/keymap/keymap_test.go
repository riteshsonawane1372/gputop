// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package keymap

import (
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	m := Default()
	cases := map[string]Action{
		"q": Quit, "ctrl+c": Quit, "?": Help, "tab": NextTab, "shift+tab": PrevTab,
		"h": History, "r": Refresh, "f": Filter, "/": Search, "enter": Select, "esc": Back,
		"up": Up, "down": Down, "left": PrevTab, "right": NextTab, "l": Right, "+": ZoomIn, "-": ZoomOut,
		"1": Tab1, "9": Tab9, "G": End, "g": Home, " ": Pause,
	}
	for k, want := range cases {
		if got := m.Lookup(k); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", k, got, want)
		}
	}
	if i, ok := TabIndex(Tab3); !ok || i != 2 {
		t.Fatalf("TabIndex(Tab3) = %d %v", i, ok)
	}
	if _, ok := TabIndex(Quit); ok {
		t.Fatal("Quit is not a tab action")
	}
}

func TestOverrides(t *testing.T) {
	m, err := New(map[string][]string{"quit": {"x"}, "history": {"Escape"}, "back": {"backspace"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Lookup("x") != Quit || m.Lookup("q") != None {
		t.Fatal("override must replace default keys")
	}
	if m.Lookup("esc") != History || m.Lookup("backspace") != Back {
		t.Fatal("normalized override keys")
	}
	if m.Label(Quit) != "x" {
		t.Fatalf("label = %q", m.Label(Quit))
	}
}

func TestConflictsAndUnknown(t *testing.T) {
	_, err := New(map[string][]string{"refresh": {"q"}})
	if err == nil || !strings.Contains(err.Error(), "bound to both") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	m, err := New(map[string][]string{"launch_missiles": {"z"}})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected unknown action error, got %v", err)
	}
	if m == nil || m.Lookup("q") != Quit {
		t.Fatal("invalid overrides must fall back to defaults")
	}
}
