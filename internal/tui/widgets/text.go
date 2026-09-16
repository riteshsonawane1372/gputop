// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package widgets contains terminal rendering primitives: boxes, bars,
// braille charts, sparklines and tables. Every function returns lines of an
// exact display width so layouts never overflow horizontally.
package widgets

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/theme"
)

// Width returns the display width of s (ANSI aware).
func Width(s string) int { return ansi.StringWidth(s) }

// Fit truncates or pads s to exactly w cells using the theme background.
func Fit(th *theme.Theme, s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := Width(s)
	if sw > w {
		s = ansi.Truncate(s, w, "…")
		sw = Width(s)
	}
	if sw < w {
		s += Space(th, w-sw)
	}
	return s
}

// FitRight right-aligns s in w cells.
func FitRight(th *theme.Theme, s string, w int) string {
	sw := Width(s)
	if sw > w {
		return ansi.Truncate(s, w, "…")
	}
	return Space(th, w-sw) + s
}

// Center centers s in w cells.
func Center(th *theme.Theme, s string, w int) string {
	sw := Width(s)
	if sw >= w {
		return Fit(th, s, w)
	}
	left := (w - sw) / 2
	return Space(th, left) + s + Space(th, w-sw-left)
}

// Space returns n background-colored spaces.
func Space(th *theme.Theme, n int) string {
	if n <= 0 {
		return ""
	}
	return th.Base.Render(strings.Repeat(" ", n))
}

// Block is a rectangle of fixed-width lines.
type Block []string

// Blank returns an empty block.
func Blank(th *theme.Theme, w, h int) Block {
	b := make(Block, max(0, h))
	sp := Space(th, w)
	for i := range b {
		b[i] = sp
	}
	return b
}

// FitBlock forces a block to w×h.
func FitBlock(th *theme.Theme, lines []string, w, h int) Block {
	out := make(Block, max(0, h))
	for i := range out {
		if i < len(lines) {
			out[i] = Fit(th, lines[i], w)
		} else {
			out[i] = Space(th, w)
		}
	}
	return out
}

// HJoin places blocks side by side. Blocks must already have uniform
// widths; shorter blocks are padded with widthOf(block) spaces.
func HJoin(th *theme.Theme, blocks ...Block) Block {
	h := 0
	widths := make([]int, len(blocks))
	for i, b := range blocks {
		h = max(h, len(b))
		if len(b) > 0 {
			widths[i] = Width(b[0])
		}
	}
	out := make(Block, h)
	var sb strings.Builder
	for row := 0; row < h; row++ {
		sb.Reset()
		for i, b := range blocks {
			if row < len(b) {
				sb.WriteString(b[row])
			} else {
				sb.WriteString(Space(th, widths[i]))
			}
		}
		out[row] = sb.String()
	}
	return out
}

// VJoin stacks blocks.
func VJoin(blocks ...Block) Block {
	var out Block
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}

// Style renders text with a style (convenience for readability).
func Style(s lipgloss.Style, text string) string { return s.Render(text) }

// Split divides total into n parts with optional weights, distributing the
// remainder left to right.
func Split(total int, weights ...int) []int {
	sum := 0
	for _, w := range weights {
		sum += w
	}
	out := make([]int, len(weights))
	if sum == 0 {
		return out
	}
	used := 0
	for i, w := range weights {
		out[i] = total * w / sum
		used += out[i]
	}
	for i := 0; used < total; i = (i + 1) % len(out) {
		out[i]++
		used++
	}
	return out
}
