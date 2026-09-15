// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsComplete(t *testing.T) {
	for _, name := range Builtin() {
		p, err := Resolve(name, "", nil)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, role := range Roles {
			if err := ValidateColor(*p.field(role)); err != nil {
				t.Errorf("%s.%s: %v", name, role, err)
			}
		}
	}
	if _, err := Resolve("nope", "", nil); err == nil {
		t.Fatal("unknown theme must error")
	}
}

func TestUserThemeExtendsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("corp.yaml", "name: corp\nextends: ice\ncolors:\n  primary: \"#ff00ff\"\n  warning: \"214\"\n")
	write("corp-dark.yml", "extends: corp\ncolors:\n  background: \"#000\"\n")
	write("broken.yaml", "colors:\n  primary: \"magenta\"\n")
	write("typo.yaml", "colours:\n  primary: \"#fff\"\n")

	p, err := Resolve("corp-dark", dir, map[string]string{"critical": "#123456"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Primary != "#ff00ff" || p.Warning != "214" || p.Background != "#000" || p.Critical != "#123456" {
		t.Fatalf("unexpected palette: %+v", p)
	}
	if p.Accent != builtins["ice"].Accent {
		t.Fatal("extends must inherit from base theme")
	}
	if _, err := Resolve("broken", dir, nil); err == nil || !strings.Contains(err.Error(), "invalid color") {
		t.Fatalf("broken theme err = %v", err)
	}
	if _, err := Resolve("typo", dir, nil); err == nil {
		t.Fatal("unknown keys in theme files must error")
	}
	if _, err := Resolve("green", "", map[string]string{"sparkle": "#fff"}); err == nil {
		t.Fatal("unknown role override must error")
	}
}

func TestGradient(t *testing.T) {
	p := builtins["green"]
	if got := gradientColor(p, 0); got != strings.ToLower(p.GraphLow) {
		t.Fatalf("gradient(0) = %s", got)
	}
	if got := gradientColor(p, 1); got != strings.ToLower(p.GraphHigh) {
		t.Fatalf("gradient(1) = %s", got)
	}
	if lerpHex("#000000", "#ffffff", 0.5) != "#808080" {
		t.Fatal("lerp midpoint")
	}
	th := New("green", p, false)
	_ = th.Gradient(0.73).Render("x")
	_ = th.Level(90, 80, 95).Render("x")
}
