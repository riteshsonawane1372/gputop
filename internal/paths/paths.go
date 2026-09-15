// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package paths resolves gputop's configuration and state directories.
//
// gputop follows the XDG base directory convention on every Unix platform
// (including macOS, matching btop and most terminal tools):
//
//	config: $XDG_CONFIG_HOME/gputop  (default ~/.config/gputop)
//	state:  $XDG_STATE_HOME/gputop   (default ~/.local/state/gputop)
package paths

import (
	"os"
	"path/filepath"
)

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.TempDir()
}

// ConfigDir returns the configuration directory.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "gputop")
	}
	return filepath.Join(home(), ".config", "gputop")
}

// StateDir returns the directory for history and logs.
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "gputop")
	}
	return filepath.Join(home(), ".local", "state", "gputop")
}

// ConfigFile returns the default config file path.
func ConfigFile() string { return filepath.Join(ConfigDir(), "config.yaml") }

// ThemesDir returns the directory for user themes.
func ThemesDir() string { return filepath.Join(ConfigDir(), "themes") }

// HistoryDir returns the default history directory.
func HistoryDir() string { return filepath.Join(StateDir(), "history") }

// Expand resolves a leading "~/" to the home directory.
func Expand(p string) string {
	if len(p) >= 2 && p[0] == '~' && (p[1] == '/' || p[1] == filepath.Separator) {
		return filepath.Join(home(), p[2:])
	}
	return p
}
