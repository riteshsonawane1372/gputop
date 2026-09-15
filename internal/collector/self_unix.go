// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package collector

import (
	"runtime/metrics"
	"syscall"
	"time"
)

type cpuSample struct {
	at  time.Time
	cpu time.Duration
}

func readCPU() (cpuSample, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return cpuSample{}, false
	}
	cpu := time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
	return cpuSample{at: time.Now(), cpu: cpu}, true
}

var memSamples = []metrics.Sample{
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/memory/classes/total:bytes"},
}

func memStats() (heap, sys uint64) {
	s := make([]metrics.Sample, len(memSamples))
	copy(s, memSamples)
	metrics.Read(s)
	if s[0].Value.Kind() == metrics.KindUint64 {
		heap = s[0].Value.Uint64()
	}
	if s[1].Value.Kind() == metrics.KindUint64 {
		sys = s[1].Value.Uint64()
	}
	return heap, sys
}
