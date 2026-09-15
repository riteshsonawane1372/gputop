// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package procinfo

import (
	"os"
	"testing"
)

func TestLookupSelfDarwin(t *testing.T) {
	info := NewResolver().Lookup(os.Getpid())
	if !info.Visible || info.Name == "" || info.Command == "" || info.User == "" || info.StartTime.IsZero() {
		t.Fatalf("self lookup incomplete: %+v", info)
	}
	if got := NewResolver().Lookup(-1); got.Visible {
		t.Fatalf("invalid pid resolved: %+v", got)
	}
}
