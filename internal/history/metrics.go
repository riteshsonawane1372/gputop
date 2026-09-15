// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package history

// Metric identifies a stored time series column. Values are persisted by
// numeric ID: append new metrics at the end, never renumber.
type Metric uint8

// Per-GPU metrics.
const (
	Util Metric = iota
	MemBandwidth
	VRAMPercent
	VRAMUsedGiB
	PowerW
	TempC
	MemTempC
	ClockCoreMHz
	ClockMemMHz
	PCIeTxMBps
	PCIeRxMBps
	NVLinkTxMBps
	NVLinkRxMBps
	EncoderPct
	DecoderPct
	FanPct
	ProcVRAMGiB
	ProcCount
	HealthScore
	ThrottleMask
	numGPUMetrics
)

// Host metrics (stored under the "host" series key).
const (
	HostCPU Metric = 100 + iota
	HostMemPercent
	HostNetRxMBps
	HostNetTxMBps
	HostDiskReadMBps
	HostDiskWriteMBps
	HostLoad1
	hostEnd
)

const numHostMetrics = int(hostEnd - HostCPU)

// HostKey is the series key for host metrics.
const HostKey = "host"

// Info describes a metric for display.
type Info struct {
	Metric Metric `json:"id"`
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	// Max is a natural upper bound for fixed-scale charts (0 = autoscale).
	Max float64 `json:"max"`
	// Chartable is false for bitmask series.
	Chartable bool `json:"chartable"`
}

var infos = map[Metric]Info{
	Util:              {Util, "util", "GPU utilization", "%", 100, true},
	MemBandwidth:      {MemBandwidth, "mem_bandwidth", "Memory bandwidth util", "%", 100, true},
	VRAMPercent:       {VRAMPercent, "vram_pct", "VRAM used", "%", 100, true},
	VRAMUsedGiB:       {VRAMUsedGiB, "vram_gib", "VRAM used", "GiB", 0, true},
	PowerW:            {PowerW, "power", "Power draw", "W", 0, true},
	TempC:             {TempC, "temp", "GPU temperature", "°C", 0, true},
	MemTempC:          {MemTempC, "mem_temp", "Memory temperature", "°C", 0, true},
	ClockCoreMHz:      {ClockCoreMHz, "clock_core", "Core clock", "MHz", 0, true},
	ClockMemMHz:       {ClockMemMHz, "clock_mem", "Memory clock", "MHz", 0, true},
	PCIeTxMBps:        {PCIeTxMBps, "pcie_tx", "PCIe TX", "MB/s", 0, true},
	PCIeRxMBps:        {PCIeRxMBps, "pcie_rx", "PCIe RX", "MB/s", 0, true},
	NVLinkTxMBps:      {NVLinkTxMBps, "nvlink_tx", "NVLink TX", "MB/s", 0, true},
	NVLinkRxMBps:      {NVLinkRxMBps, "nvlink_rx", "NVLink RX", "MB/s", 0, true},
	EncoderPct:        {EncoderPct, "encoder", "Encoder util", "%", 100, true},
	DecoderPct:        {DecoderPct, "decoder", "Decoder util", "%", 100, true},
	FanPct:            {FanPct, "fan", "Fan speed", "%", 100, true},
	ProcVRAMGiB:       {ProcVRAMGiB, "proc_vram", "Process VRAM", "GiB", 0, true},
	ProcCount:         {ProcCount, "proc_count", "GPU processes", "", 0, true},
	HealthScore:       {HealthScore, "health", "Health score", "", 100, true},
	ThrottleMask:      {ThrottleMask, "throttle", "Throttle reasons", "mask", 0, false},
	HostCPU:           {HostCPU, "host_cpu", "Host CPU", "%", 100, true},
	HostMemPercent:    {HostMemPercent, "host_mem", "Host memory", "%", 100, true},
	HostNetRxMBps:     {HostNetRxMBps, "host_net_rx", "Network RX", "MB/s", 0, true},
	HostNetTxMBps:     {HostNetTxMBps, "host_net_tx", "Network TX", "MB/s", 0, true},
	HostDiskReadMBps:  {HostDiskReadMBps, "host_disk_read", "Disk read", "MB/s", 0, true},
	HostDiskWriteMBps: {HostDiskWriteMBps, "host_disk_write", "Disk write", "MB/s", 0, true},
	HostLoad1:         {HostLoad1, "host_load1", "Load average (1m)", "", 0, true},
}

// Describe returns display information for a metric.
func Describe(m Metric) Info { return infos[m] }

// GPUMetrics lists per-GPU metrics in display order.
func GPUMetrics() []Metric {
	out := make([]Metric, 0, numGPUMetrics)
	for m := Metric(0); m < numGPUMetrics; m++ {
		out = append(out, m)
	}
	return out
}

// HostMetrics lists host metrics in display order.
func HostMetrics() []Metric {
	out := make([]Metric, 0, numHostMetrics)
	for m := HostCPU; m < hostEnd; m++ {
		out = append(out, m)
	}
	return out
}

// ByKey resolves a metric by its key.
func ByKey(key string) (Metric, bool) {
	for m, i := range infos {
		if i.Key == key {
			return m, true
		}
	}
	return 0, false
}

func slot(m Metric) int {
	if m >= HostCPU {
		return int(m - HostCPU)
	}
	return int(m)
}

func width(key string) int {
	if key == HostKey {
		return numHostMetrics
	}
	return int(numGPUMetrics)
}

func metricAt(key string, i int) Metric {
	if key == HostKey {
		return HostCPU + Metric(i)
	}
	return Metric(i)
}
