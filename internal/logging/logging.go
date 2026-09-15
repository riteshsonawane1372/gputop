// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package logging configures structured logging.
//
// In TUI mode logs must never be written to the terminal (they would
// corrupt the display), so they go to a file or are discarded. Service and
// one-shot modes log to stderr.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Options control log destination and verbosity.
type Options struct {
	Level string // debug, info, warn, error
	// File, when set, receives logs (created with 0600).
	File string
	// Stderr logs to standard error when File is empty.
	Stderr bool
	JSON   bool
}

// New returns a logger and a function releasing its resources.
func New(o Options) (*slog.Logger, func(), error) {
	var lvl slog.Level
	switch strings.ToLower(o.Level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelWarn
	}
	var w io.Writer
	closeFn := func() {}
	switch {
	case o.File != "":
		if err := os.MkdirAll(filepath.Dir(o.File), 0o700); err != nil {
			return nil, closeFn, err
		}
		f, err := os.OpenFile(o.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, closeFn, err
		}
		w, closeFn = f, func() { _ = f.Close() }
	case o.Stderr:
		w = os.Stderr
	default:
		return slog.New(slog.DiscardHandler), closeFn, nil
	}
	hopts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler = slog.NewTextHandler(w, hopts)
	if o.JSON {
		h = slog.NewJSONHandler(w, hopts)
	}
	return slog.New(h), closeFn, nil
}
