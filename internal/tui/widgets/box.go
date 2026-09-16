// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package widgets

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/theme"
)

// BoxOpts configure a bordered panel.
type BoxOpts struct {
	Title      string
	RightTitle string
	Focus      bool
}

// Box draws a rounded border of exactly w×h around content lines. Content
// is clipped to the inner area ((w-2)×(h-2)).
func Box(th *theme.Theme, o BoxOpts, w, h int, content []string) Block {
	if w < 4 || h < 2 {
		return Blank(th, max(0, w), max(0, h))
	}
	border := th.Border
	if o.Focus {
		border = th.BorderFocus
	}
	inner := w - 2
	out := make(Block, 0, h)

	// Top border with title segments.
	var top strings.Builder
	top.WriteString(border.Render("╭─"))
	used := 2
	if o.Title != "" {
		t := " " + o.Title + " "
		if tw := Width(t); tw > inner-2 {
			t = Fit(th, t, max(0, inner-2))
		}
		top.WriteString(th.Title.Render(t))
		used += Width(t)
	}
	right := ""
	if o.RightTitle != "" {
		right = " " + o.RightTitle + " "
		if used+Width(right)+2 > w {
			right = ""
		}
	}
	fill := w - used - Width(right) - 2
	if fill > 0 {
		top.WriteString(border.Render(strings.Repeat("─", fill)))
	}
	if right != "" {
		top.WriteString(th.Dim.Render(right))
	}
	top.WriteString(border.Render("─╮"))
	out = append(out, Fit(th, top.String(), w))

	side := border.Render("│")
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(content) {
			line = content[i]
		}
		out = append(out, side+Fit(th, line, inner)+side)
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return out
}

// Rule draws a horizontal separator with an optional label.
func Rule(th *theme.Theme, label string, w int) string {
	if label == "" {
		return th.Border.Render(strings.Repeat("─", max(0, w)))
	}
	l := " " + label + " "
	rest := w - Width(l) - 2
	if rest < 0 {
		return Fit(th, th.Dim.Render(label), w)
	}
	return th.Border.Render("──") + th.Dim.Render(l) + th.Border.Render(strings.Repeat("─", rest))
}

// Scrollbar draws a scroll thumb over the last column of b (typically the
// right border of a box) when the content is scrolled by offset of at most
// maxOffset lines. b is returned unchanged when nothing is hidden.
func Scrollbar(th *theme.Theme, b Block, offset, maxOffset int) Block {
	h := len(b)
	if maxOffset <= 0 || h < 3 {
		return b
	}
	thumb := max(1, h*h/(h+maxOffset))
	pos := (h - thumb) * min(max(offset, 0), maxOffset) / maxOffset
	out := make(Block, h)
	for i, l := range b {
		if i < pos || i >= pos+thumb {
			out[i] = l
			continue
		}
		w := Width(l)
		out[i] = ansi.Truncate(l, w-1, "") + th.BorderFocus.Render("┃")
	}
	return out
}
