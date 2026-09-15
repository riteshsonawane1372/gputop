// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package host

import "github.com/gputop/gputop/internal/metric"

func cpuFrequency() metric.Opt[float64] { return metric.None[float64]() }

func linkSpeed(string) metric.Opt[float64] { return metric.None[float64]() }

type ibPort struct {
	name                             string
	up                               bool
	speed                            metric.Opt[float64]
	rxBytes, txBytes, rxPkts, txPkts uint64
	rxErrors                         metric.Opt[uint64]
}

type ibReader struct{}

func (ibReader) read() []ibPort { return nil }
