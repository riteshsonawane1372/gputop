// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package procinfo

import (
	"os"
	"testing"
)

func TestLookupSelf(t *testing.T) {
	r := NewResolver()
	if r.NestedPIDNamespace() {
		t.Skip("running in a nested PID namespace")
	}
	info := r.Lookup(os.Getpid())
	if !info.Visible || info.Name == "" || info.StartTime.IsZero() {
		t.Fatalf("self lookup: %+v", info)
	}
	if again := r.Lookup(os.Getpid()); again.Name != info.Name {
		t.Fatal("cached lookup mismatch")
	}
	if missing := r.Lookup(1 << 30); missing.Visible {
		t.Fatal("nonexistent pid must not be visible")
	}
}
