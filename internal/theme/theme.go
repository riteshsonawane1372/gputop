// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package theme defines semantic color palettes and the lipgloss styles
// derived from them. UI components ask for roles ("warning", "border"),
// never for literal colors.
package theme

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"go.yaml.in/yaml/v3"
)

// Palette holds every semantic color role. Values are "#rrggbb", "#rgb"
// or an ANSI 256 color number.
type Palette struct {
	Background  string `yaml:"background"`
	Surface     string `yaml:"surface"` // header/tab bar background
	Foreground  string `yaml:"foreground"`
	Primary     string `yaml:"primary"`
	Secondary   string `yaml:"secondary"`
	Muted       string `yaml:"muted"`
	Accent      string `yaml:"accent"` // informational (cyan by default)
	OK          string `yaml:"ok"`
	Warning     string `yaml:"warning"`
	Critical    string `yaml:"critical"`
	Unavailable string `yaml:"unavailable"`
	Border      string `yaml:"border"`
	BorderFocus string `yaml:"border_focus"`
	Title       string `yaml:"title"`
	Selection   string `yaml:"selection"`
	SelectionFg string `yaml:"selection_fg"`
	GraphLow    string `yaml:"graph_low"`
	GraphMid    string `yaml:"graph_mid"`
	GraphHigh   string `yaml:"graph_high"`
}

// Roles lists palette keys in documentation order.
var Roles = []string{
	"background", "surface", "foreground", "primary", "secondary", "muted", "accent",
	"ok", "warning", "critical", "unavailable", "border", "border_focus", "title",
	"selection", "selection_fg", "graph_low", "graph_mid", "graph_high",
}

func (p *Palette) field(role string) *string {
	switch role {
	case "background":
		return &p.Background
	case "surface":
		return &p.Surface
	case "foreground":
		return &p.Foreground
	case "primary":
		return &p.Primary
	case "secondary":
		return &p.Secondary
	case "muted":
		return &p.Muted
	case "accent":
		return &p.Accent
	case "ok":
		return &p.OK
	case "warning":
		return &p.Warning
	case "critical":
		return &p.Critical
	case "unavailable":
		return &p.Unavailable
	case "border":
		return &p.Border
	case "border_focus":
		return &p.BorderFocus
	case "title":
		return &p.Title
	case "selection":
		return &p.Selection
	case "selection_fg":
		return &p.SelectionFg
	case "graph_low":
		return &p.GraphLow
	case "graph_mid":
		return &p.GraphMid
	case "graph_high":
		return &p.GraphHigh
	}
	return nil
}

// Set assigns a role, validating the color.
func (p *Palette) Set(role, color string) error {
	f := p.field(role)
	if f == nil {
		return fmt.Errorf("unknown color role %q (known: %s)", role, strings.Join(Roles, ", "))
	}
	if err := ValidateColor(color); err != nil {
		return fmt.Errorf("%s: %w", role, err)
	}
	*f = color
	return nil
}

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ValidateColor accepts #rgb, #rrggbb or an ANSI color number 0-255.
func ValidateColor(c string) error {
	if hexColor.MatchString(c) {
		return nil
	}
	if n, err := strconv.Atoi(c); err == nil && n >= 0 && n <= 255 {
		return nil
	}
	return fmt.Errorf("invalid color %q (use #rrggbb, #rgb or 0-255)", c)
}

var builtins = map[string]Palette{
	"green": {
		Background: "#050805", Surface: "#0b140d", Foreground: "#b8f5c8", Primary: "#39ff7a",
		Secondary: "#6fbf86", Muted: "#4a6b53", Accent: "#4fd6e0", OK: "#39ff7a",
		Warning: "#ffb020", Critical: "#ff4545", Unavailable: "#3d4a40", Border: "#1f4a2a",
		BorderFocus: "#39ff7a", Title: "#7dffa6", Selection: "#123d1e", SelectionFg: "#e6ffec",
		GraphLow: "#1f9e4a", GraphMid: "#b5e61d", GraphHigh: "#ff5a36",
	},
	"amber": {
		Background: "#0a0700", Surface: "#151005", Foreground: "#ffd79a", Primary: "#ffb000",
		Secondary: "#c98f2e", Muted: "#6e5630", Accent: "#ffe07a", OK: "#ffc13b",
		Warning: "#ff7f11", Critical: "#ff3b30", Unavailable: "#4a4030", Border: "#4d3a12",
		BorderFocus: "#ffb000", Title: "#ffcc55", Selection: "#3a2a08", SelectionFg: "#fff2d6",
		GraphLow: "#a86f00", GraphMid: "#ffb000", GraphHigh: "#ff3b30",
	},
	"ice": {
		Background: "#05080d", Surface: "#0b111a", Foreground: "#cfe6ff", Primary: "#5cc8ff",
		Secondary: "#7aa2c7", Muted: "#4a5d73", Accent: "#9d8cff", OK: "#4de3a1",
		Warning: "#ffc857", Critical: "#ff5c7a", Unavailable: "#3a4655", Border: "#1d3148",
		BorderFocus: "#5cc8ff", Title: "#8fdcff", Selection: "#13304a", SelectionFg: "#ffffff",
		GraphLow: "#2f7fd1", GraphMid: "#5cc8ff", GraphHigh: "#ff5c7a",
	},
	"mono": {
		Background: "#000000", Surface: "#101010", Foreground: "#d0d0d0", Primary: "#ffffff",
		Secondary: "#a0a0a0", Muted: "#6a6a6a", Accent: "#e0e0e0", OK: "#d0d0d0",
		Warning: "#ffffff", Critical: "#ffffff", Unavailable: "#4a4a4a", Border: "#3a3a3a",
		BorderFocus: "#ffffff", Title: "#ffffff", Selection: "#303030", SelectionFg: "#ffffff",
		GraphLow: "#707070", GraphMid: "#b0b0b0", GraphHigh: "#ffffff",
	},
	"dusk": {
		Background: "#0d0a12", Surface: "#17121f", Foreground: "#e3d9f5", Primary: "#c792ea",
		Secondary: "#9a86b8", Muted: "#5e5270", Accent: "#82aaff", OK: "#c3e88d",
		Warning: "#ffcb6b", Critical: "#ff5370", Unavailable: "#433a50", Border: "#342a44",
		BorderFocus: "#c792ea", Title: "#dcb8ff", Selection: "#2e2240", SelectionFg: "#ffffff",
		GraphLow: "#7e57c2", GraphMid: "#c792ea", GraphHigh: "#ff5370",
	},
}

// Builtin returns the names of the built-in themes.
func Builtin() []string {
	out := make([]string, 0, len(builtins))
	for k := range builtins {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// File is the on-disk theme format (~/.config/gputop/themes/<name>.yaml).
type File struct {
	Name    string            `yaml:"name"`
	Extends string            `yaml:"extends"`
	Colors  map[string]string `yaml:"colors"`
}

// Resolve builds a palette: a built-in or user theme named name (user
// files may extend another theme), with overrides applied last.
func Resolve(name, themesDir string, overrides map[string]string) (Palette, error) {
	pal, err := resolve(name, themesDir, 0)
	if err != nil {
		return builtins["green"], err
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var errs []string
	for _, k := range keys {
		if err := pal.Set(k, overrides[k]); err != nil {
			errs = append(errs, "theme.colors."+err.Error())
		}
	}
	if len(errs) > 0 {
		return pal, errors.New(strings.Join(errs, "; "))
	}
	return pal, nil
}

func resolve(name, dir string, depth int) (Palette, error) {
	if depth > 8 {
		return Palette{}, fmt.Errorf("theme %q: extends chain too deep", name)
	}
	if dir != "" {
		for _, ext := range []string{".yaml", ".yml"} {
			b, err := os.ReadFile(filepath.Join(dir, name+ext))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return Palette{}, fmt.Errorf("theme %q: %w", name, err)
			}
			return parseFile(name, b, dir, depth)
		}
	}
	if p, ok := builtins[name]; ok {
		return p, nil
	}
	return Palette{}, fmt.Errorf("theme %q not found (built-in: %s; user themes in %s)", name, strings.Join(Builtin(), ", "), dir)
}

func parseFile(name string, b []byte, dir string, depth int) (Palette, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return Palette{}, fmt.Errorf("theme %q: %w", name, err)
	}
	base := "green"
	if f.Extends != "" {
		base = f.Extends
	}
	var pal Palette
	var err error
	if base == name {
		p, ok := builtins[base]
		if !ok {
			return Palette{}, fmt.Errorf("theme %q extends itself", name)
		}
		pal = p
	} else if pal, err = resolve(base, dir, depth+1); err != nil {
		return Palette{}, err
	}
	keys := make([]string, 0, len(f.Colors))
	for k := range f.Colors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := pal.Set(k, f.Colors[k]); err != nil {
			return Palette{}, fmt.Errorf("theme %q: %w", name, err)
		}
	}
	return pal, nil
}

// Theme bundles a palette with ready-to-use styles.
type Theme struct {
	Name        string
	P           Palette
	Transparent bool

	Base, Text, Dim, Muted, Bold, Title, Accent lipgloss.Style
	OK, Warn, Crit, NA, Primary                 lipgloss.Style
	Border, BorderFocus, Selected, Surface      lipgloss.Style
	TabActive, TabInactive, Key                 lipgloss.Style

	gradient [101]lipgloss.Style
}

// New builds styles for a palette.
func New(name string, p Palette, transparent bool) *Theme {
	t := &Theme{Name: name, P: p, Transparent: transparent}
	base := lipgloss.NewStyle()
	if !transparent {
		base = base.Background(lipgloss.Color(p.Background))
	}
	fg := func(c string) lipgloss.Style { return base.Foreground(lipgloss.Color(c)) }
	t.Base = base.Foreground(lipgloss.Color(p.Foreground))
	t.Text = t.Base
	t.Dim = fg(p.Secondary)
	t.Muted = fg(p.Muted)
	t.Bold = t.Base.Bold(true)
	t.Title = fg(p.Title).Bold(true)
	t.Accent = fg(p.Accent)
	t.Primary = fg(p.Primary)
	t.OK = fg(p.OK)
	t.Warn = fg(p.Warning)
	t.Crit = fg(p.Critical).Bold(true)
	t.NA = fg(p.Unavailable)
	t.Border = fg(p.Border)
	t.BorderFocus = fg(p.BorderFocus)
	t.Selected = lipgloss.NewStyle().Background(lipgloss.Color(p.Selection)).Foreground(lipgloss.Color(p.SelectionFg)).Bold(true)
	surf := lipgloss.NewStyle()
	if !transparent {
		surf = surf.Background(lipgloss.Color(p.Surface))
	}
	t.Surface = surf.Foreground(lipgloss.Color(p.Secondary))
	t.TabActive = lipgloss.NewStyle().Background(lipgloss.Color(p.Primary)).Foreground(lipgloss.Color(p.Background)).Bold(true)
	t.TabInactive = surf.Foreground(lipgloss.Color(p.Secondary))
	t.Key = surf.Foreground(lipgloss.Color(p.Primary)).Bold(true)
	for i := range t.gradient {
		t.gradient[i] = fg(gradientColor(p, float64(i)/100))
	}
	return t
}

// Gradient returns a style colored along graph_low → graph_mid → graph_high
// for a fraction in [0,1].
func (t *Theme) Gradient(frac float64) lipgloss.Style {
	if math.IsNaN(frac) {
		return t.NA
	}
	i := int(math.Round(math.Max(0, math.Min(1, frac)) * 100))
	return t.gradient[i]
}

// Level picks ok/warn/crit by thresholds (value >= crit is critical).
func (t *Theme) Level(v, warn, crit float64) lipgloss.Style {
	switch {
	case v >= crit:
		return t.Crit
	case v >= warn:
		return t.Warn
	}
	return t.OK
}

// Bg applies the theme background to an arbitrary style.
func (t *Theme) Bg(s lipgloss.Style) lipgloss.Style {
	if t.Transparent {
		return s
	}
	return s.Background(lipgloss.Color(t.P.Background))
}

func gradientColor(p Palette, f float64) string {
	if f <= 0.5 {
		return lerpHex(p.GraphLow, p.GraphMid, f*2)
	}
	return lerpHex(p.GraphMid, p.GraphHigh, (f-0.5)*2)
}

func parseHex(c string) (r, g, b float64, ok bool) {
	if !hexColor.MatchString(c) {
		return 0, 0, 0, false
	}
	h := c[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff), true
}

func lerpHex(a, b string, f float64) string {
	ar, ag, ab, ok1 := parseHex(a)
	br, bg, bb, ok2 := parseHex(b)
	if !ok1 || !ok2 {
		if f < 0.5 {
			return a
		}
		return b
	}
	mix := func(x, y float64) int { return int(math.Round(x + (y-x)*f)) }
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
}
