// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package host collects node-level CPU, memory, disk and network telemetry
// that helps explain GPU behaviour (for example an input pipeline starving
// the GPU: low GPU utilization, CPU pegged, network RX idle).
package host

import (
	"time"

	"github.com/gputop/gputop/internal/metric"
)

// Snapshot is the host state at a point in time.
type Snapshot struct {
	Time   time.Time     `json:"time"`
	Info   Info          `json:"info"`
	CPU    CPU           `json:"cpu"`
	Memory Memory        `json:"memory"`
	Disks  []Filesystem  `json:"filesystems"`
	IO     []BlockDevice `json:"block_devices"`
	Net    []NetIf       `json:"network"`
}

// Info is static host identity.
type Info struct {
	Hostname       string        `json:"hostname"`
	OS             string        `json:"os"`
	Platform       string        `json:"platform,omitempty"`
	Kernel         string        `json:"kernel,omitempty"`
	Arch           string        `json:"arch"`
	Uptime         time.Duration `json:"uptime_ns"`
	CPUModel       string        `json:"cpu_model,omitempty"`
	Virtualization string        `json:"virtualization,omitempty"`
}

// CPU is processor utilization.
type CPU struct {
	Cores       int                 `json:"cores"`
	Threads     int                 `json:"threads"`
	UtilPercent metric.Opt[float64] `json:"util_percent"`
	IOWaitPct   metric.Opt[float64] `json:"iowait_percent"`
	StealPct    metric.Opt[float64] `json:"steal_percent"`
	PerCore     []float64           `json:"per_core_percent,omitempty"`
	Load1       metric.Opt[float64] `json:"load1"`
	Load5       metric.Opt[float64] `json:"load5"`
	Load15      metric.Opt[float64] `json:"load15"`
	FreqMHz     metric.Opt[float64] `json:"freq_mhz"`
}

// Memory is RAM and swap usage in bytes.
type Memory struct {
	Total     metric.Opt[uint64] `json:"total_bytes"`
	Used      metric.Opt[uint64] `json:"used_bytes"`
	Available metric.Opt[uint64] `json:"available_bytes"`
	Cached    metric.Opt[uint64] `json:"cached_bytes"`
	Buffers   metric.Opt[uint64] `json:"buffers_bytes"`
	SwapTotal metric.Opt[uint64] `json:"swap_total_bytes"`
	SwapUsed  metric.Opt[uint64] `json:"swap_used_bytes"`
}

// UsedFraction returns (total-available)/total.
func (m Memory) UsedFraction() metric.Opt[float64] {
	if !m.Total.OK || m.Total.V == 0 {
		return metric.None[float64]()
	}
	if m.Available.OK {
		return metric.Some(float64(m.Total.V-min(m.Available.V, m.Total.V)) / float64(m.Total.V))
	}
	if m.Used.OK {
		return metric.Some(float64(m.Used.V) / float64(m.Total.V))
	}
	return metric.None[float64]()
}

// Filesystem is a mounted filesystem.
type Filesystem struct {
	Mount  string             `json:"mount"`
	Device string             `json:"device"`
	FSType string             `json:"fstype"`
	Total  metric.Opt[uint64] `json:"total_bytes"`
	Used   metric.Opt[uint64] `json:"used_bytes"`
}

// BlockDevice is disk I/O throughput.
type BlockDevice struct {
	Name      string              `json:"name"`
	ReadBps   metric.Opt[float64] `json:"read_bps"`
	WriteBps  metric.Opt[float64] `json:"write_bps"`
	ReadIOPS  metric.Opt[float64] `json:"read_iops"`
	WriteIOPS metric.Opt[float64] `json:"write_iops"`
	BusyPct   metric.Opt[float64] `json:"busy_percent"`
}

// NetIf is a network interface (Ethernet or InfiniBand port).
type NetIf struct {
	Name      string              `json:"name"`
	Kind      string              `json:"kind"` // ethernet, infiniband, loopback, virtual
	Up        bool                `json:"up"`
	SpeedMbps metric.Opt[float64] `json:"speed_mbps"`
	RxBps     metric.Opt[float64] `json:"rx_bps"`
	TxBps     metric.Opt[float64] `json:"tx_bps"`
	RxPps     metric.Opt[float64] `json:"rx_pps"`
	TxPps     metric.Opt[float64] `json:"tx_pps"`
	RxErrors  metric.Opt[uint64]  `json:"rx_errors"`
	TxErrors  metric.Opt[uint64]  `json:"tx_errors"`
	RxDrops   metric.Opt[uint64]  `json:"rx_drops"`
	TxDrops   metric.Opt[uint64]  `json:"tx_drops"`
}
