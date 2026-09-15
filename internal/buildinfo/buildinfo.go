// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package buildinfo carries version information injected at link time:
//
//	go build -ldflags "-X github.com/gputop/gputop/internal/buildinfo.Version=v0.1.0"
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set via -ldflags by the release pipeline.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info returns the effective version, falling back to module build info
// for `go install` builds.
func Info() (version, commit, date string) {
	version, commit, date = Version, Commit, Date
	if bi, ok := debug.ReadBuildInfo(); ok {
		if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if commit == "" {
					commit = s.Value
				}
			case "vcs.time":
				if date == "" {
					date = s.Value
				}
			}
		}
	}
	return version, commit, date
}

// String is the one-line version string printed by --version.
func String() string {
	v, c, d := Info()
	s := "gputop " + v
	if c != "" {
		if len(c) > 12 {
			c = c[:12]
		}
		s += " (" + c
		if d != "" {
			s += ", " + d
		}
		s += ")"
	}
	return fmt.Sprintf("%s %s/%s", s, runtime.GOOS, runtime.GOARCH)
}
