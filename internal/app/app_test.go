// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"runtime"
	"testing"

	"github.com/gputop/gputop/internal/config"
	"github.com/gputop/gputop/internal/gpu"
)

func TestAutoProvider(t *testing.T) {
	if got := autoProvider("darwin"); got != "apple" {
		t.Errorf("darwin -> %s", got)
	}
	if got := autoProvider("linux"); got != "nvidia" {
		t.Errorf("linux -> %s", got)
	}
}

func TestProviders(t *testing.T) {
	vendors := func(names ...string) []gpu.Vendor {
		cfg := config.Default()
		cfg.GPU.Providers = names
		var out []gpu.Vendor
		for _, p := range Providers(cfg, false, 0) {
			out = append(out, p.Vendor())
		}
		return out
	}
	want := gpu.VendorNVIDIA
	if runtime.GOOS == "darwin" {
		want = gpu.VendorApple
	}
	if got := vendors("auto"); len(got) != 1 || got[0] != want {
		t.Errorf("auto on %s = %v, want %s", runtime.GOOS, got, want)
	}
	if got := vendors("apple"); len(got) != 1 || got[0] != gpu.VendorApple {
		t.Errorf("apple = %v", got)
	}
	if got := vendors("nvidia", "auto", "nvidia"); len(got) > 2 || got[0] != gpu.VendorNVIDIA {
		t.Errorf("duplicates must collapse: %v", got)
	}
	if got := Providers(config.Default(), true, 2); len(got) != 1 || got[0].Vendor() != gpu.VendorSimulated {
		t.Errorf("demo = %v", got)
	}
}
