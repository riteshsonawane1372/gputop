---
title: Themes
---

# Themes

gputop never uses literal colors in UI code. Components ask the theme for
semantic roles, so a theme fully controls the look.

## Built-in themes

| Name | Style |
|---|---|
| `green` | default: black background, phosphor green, amber warnings, red criticals |
| `amber` | monochrome amber terminal |
| `ice` | dark blue with cyan accents |
| `mono` | grayscale, for limited terminals and screenshots |
| `dusk` | dark purple |

List themes, including your own, with `gputop --list-themes`. Select one with
`theme.name` or `--theme`.

## Roles

| Role | Used for |
|---|---|
| `background` | painted background (unless `theme.transparent: true`) |
| `surface` | header, tab bar and footer background |
| `foreground` | normal text |
| `primary` | active tab, highlights, key hints |
| `secondary` | labels, dimmed text |
| `muted` | idle states, empty bar segments, axes |
| `accent` | informational values (active state, rates, event markers) |
| `ok` | healthy / busy-and-fine |
| `warning` | warnings, throttling, degraded |
| `critical` | errors, lost GPUs, critical alerts |
| `unavailable` | `N/A` values |
| `border` / `border_focus` | panel borders / focused panel |
| `title` | panel titles |
| `selection` / `selection_fg` | selected table row |
| `graph_low` / `graph_mid` / `graph_high` | gradient for bars, charts and sparklines |

Colors are `#rrggbb`, `#rgb`, or an ANSI 256-color number (`"214"`). On
terminals with fewer colors, lipgloss downsamples automatically.

## Custom theme files

Create `~/.config/gputop/themes/<name>.yaml`:

```yaml
name: corp            # informational
extends: ice          # optional; defaults to green
colors:
  primary: "#ff00ff"
  border: "#333366"
  warning: "214"
```

- Omitted roles inherit from `extends`, which may itself be a user theme (up to
  8 levels deep).
- Unknown keys or roles and invalid colors are reported; gputop then starts with
  the built-in `green` theme and shows the error in the footer.
- A complete example is in `examples/themes/solarized-dark.yaml`.

## Inline overrides

Without a theme file:

```yaml
theme:
  name: green
  colors:
    background: "#000000"
    primary: "#00ff66"
```

## Transparent background

```yaml
theme:
  transparent: true
```

gputop then uses your terminal's background color, which is useful with
translucent terminals or light terminal color schemes (pair it with `mono` or a
custom theme).
