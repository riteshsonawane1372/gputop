// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package collector

import (
	"runtime"
	"time"
)

type cpuSample struct {
	at  time.Time
	cpu time.Duration
}

func readCPU() (cpuSample, bool) { return cpuSample{}, false }

func memStats() (heap, sys uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc, m.Sys
}
