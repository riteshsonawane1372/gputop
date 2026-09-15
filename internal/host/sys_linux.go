// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package host

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gputop/gputop/internal/metric"
)

func readUint(path string) (uint64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v, err == nil
}

// cpuFrequency averages scaling_cur_freq across CPUs (kHz in sysfs).
func cpuFrequency() metric.Opt[float64] {
	files, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq")
	var sum float64
	var n int
	for _, f := range files {
		if v, ok := readUint(f); ok {
			sum += float64(v)
			n++
		}
	}
	if n == 0 {
		return metric.None[float64]()
	}
	return metric.Some(sum / float64(n) / 1000)
}

func linkSpeed(name string) metric.Opt[float64] {
	b, err := os.ReadFile(filepath.Join("/sys/class/net", name, "speed"))
	if err != nil {
		return metric.None[float64]()
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || v <= 0 {
		return metric.None[float64]()
	}
	return metric.Some(v)
}

type ibPort struct {
	name                             string
	up                               bool
	speed                            metric.Opt[float64]
	rxBytes, txBytes, rxPkts, txPkts uint64
	rxErrors                         metric.Opt[uint64]
}

type ibReader struct{}

// read returns InfiniBand/RoCE port counters from sysfs. Per the kernel
// ABI, port_rcv_data/port_xmit_data count octets divided by 4.
func (ibReader) read() []ibPort {
	ports, _ := filepath.Glob("/sys/class/infiniband/*/ports/*")
	var out []ibPort
	for _, p := range ports {
		dev := filepath.Base(filepath.Dir(filepath.Dir(p)))
		port := ibPort{name: dev + "/" + filepath.Base(p)}
		if b, err := os.ReadFile(filepath.Join(p, "state")); err == nil {
			port.up = strings.Contains(string(b), "ACTIVE")
		}
		if b, err := os.ReadFile(filepath.Join(p, "rate")); err == nil {
			// e.g. "400 Gb/sec (4X NDR)"
			if f := strings.Fields(string(b)); len(f) > 0 {
				if v, err := strconv.ParseFloat(f[0], 64); err == nil {
					port.speed = metric.Some(v * 1000)
				}
			}
		}
		c := filepath.Join(p, "counters")
		rx, ok1 := readUint(filepath.Join(c, "port_rcv_data"))
		tx, ok2 := readUint(filepath.Join(c, "port_xmit_data"))
		if !ok1 || !ok2 {
			continue
		}
		port.rxBytes, port.txBytes = rx*4, tx*4
		port.rxPkts, _ = readUint(filepath.Join(c, "port_rcv_packets"))
		port.txPkts, _ = readUint(filepath.Join(c, "port_xmit_packets"))
		if v, ok := readUint(filepath.Join(c, "port_rcv_errors")); ok {
			port.rxErrors = metric.Some(v)
		}
		out = append(out, port)
	}
	return out
}
