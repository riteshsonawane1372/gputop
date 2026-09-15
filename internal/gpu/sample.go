// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package gpu

import (
	"strings"
	"time"

	"github.com/gputop/gputop/internal/metric"
)

// Sample is a fast-tier reading of dynamic device state.
type Sample struct {
	Time time.Time `json:"time"`

	UtilPercent         metric.Opt[float64] `json:"util_percent"`
	MemBandwidthPercent metric.Opt[float64] `json:"memory_bandwidth_util_percent"`
	EncoderPercent      metric.Opt[float64] `json:"encoder_util_percent"`
	DecoderPercent      metric.Opt[float64] `json:"decoder_util_percent"`
	JPEGPercent         metric.Opt[float64] `json:"jpeg_util_percent"`
	OFAPercent          metric.Opt[float64] `json:"ofa_util_percent"`

	MemTotal    metric.Opt[uint64] `json:"memory_total_bytes"`
	MemUsed     metric.Opt[uint64] `json:"memory_used_bytes"`
	MemFree     metric.Opt[uint64] `json:"memory_free_bytes"`
	MemReserved metric.Opt[uint64] `json:"memory_reserved_bytes"`

	TempC    metric.Opt[float64] `json:"temp_c"`
	MemTempC metric.Opt[float64] `json:"memory_temp_c"`
	FanPct   metric.Opt[float64] `json:"fan_percent"`

	PowerW      metric.Opt[float64] `json:"power_w"`
	PowerLimitW metric.Opt[float64] `json:"power_limit_w"` // enforced limit
	EnergyJ     metric.Opt[float64] `json:"energy_j"`      // cumulative since driver load

	ClockCoreMHz metric.Opt[float64] `json:"clock_core_mhz"`
	ClockMemMHz  metric.Opt[float64] `json:"clock_mem_mhz"`
	PState       metric.Opt[int]     `json:"pstate"`

	// Throttle is the set of active clock event (throttle) reasons.
	Throttle metric.Opt[ThrottleReasons] `json:"throttle_reasons"`

	PCIeGen   metric.Opt[int]     `json:"pcie_gen"`
	PCIeWidth metric.Opt[int]     `json:"pcie_width"`
	PCIeTxBps metric.Opt[float64] `json:"pcie_tx_bps"`
	PCIeRxBps metric.Opt[float64] `json:"pcie_rx_bps"`
}

// ThrottleReasons is a vendor-neutral bitmask of reasons a device is
// running below its maximum performance.
type ThrottleReasons uint32

const (
	ThrottleIdle ThrottleReasons = 1 << iota
	ThrottleAppClockSetting
	ThrottleSWPowerCap
	ThrottleHWSlowdown
	ThrottleSyncBoost
	ThrottleSWThermal
	ThrottleHWThermal
	ThrottleHWPowerBrake
	ThrottleDisplayClockSetting
	ThrottleBoardLimit
	ThrottleReliability
)

var throttleNames = []struct {
	bit  ThrottleReasons
	name string
}{
	{ThrottleIdle, "idle"},
	{ThrottleAppClockSetting, "app_clock_setting"},
	{ThrottleSWPowerCap, "sw_power_cap"},
	{ThrottleHWSlowdown, "hw_slowdown"},
	{ThrottleSyncBoost, "sync_boost"},
	{ThrottleSWThermal, "sw_thermal"},
	{ThrottleHWThermal, "hw_thermal"},
	{ThrottleHWPowerBrake, "hw_power_brake"},
	{ThrottleDisplayClockSetting, "display_clock_setting"},
	{ThrottleBoardLimit, "board_limit"},
	{ThrottleReliability, "reliability"},
}

// ThrottlePerformance is the subset of reasons that indicate the device is
// being held back (as opposed to informational reasons such as idle).
const ThrottlePerformance = ThrottleSWPowerCap | ThrottleHWSlowdown | ThrottleSWThermal |
	ThrottleHWThermal | ThrottleHWPowerBrake | ThrottleBoardLimit | ThrottleReliability | ThrottleSyncBoost

// ThrottleThermal is the subset of reasons caused by temperature.
const ThrottleThermal = ThrottleSWThermal | ThrottleHWThermal

// ThrottlePower is the subset of reasons caused by power limits.
const ThrottlePower = ThrottleSWPowerCap | ThrottleHWPowerBrake | ThrottleBoardLimit

// ThrottleHardware is the subset of reasons asserted by hardware protection.
const ThrottleHardware = ThrottleHWSlowdown | ThrottleHWThermal | ThrottleHWPowerBrake

// Names returns the stable names of the set bits.
func (t ThrottleReasons) Names() []string {
	var out []string
	for _, n := range throttleNames {
		if t&n.bit != 0 {
			out = append(out, n.name)
		}
	}
	return out
}

// String joins the names with commas ("none" if empty).
func (t ThrottleReasons) String() string {
	if t == 0 {
		return "none"
	}
	return strings.Join(t.Names(), ",")
}

// MarshalJSON encodes the reasons as a list of names.
func (t ThrottleReasons) MarshalJSON() ([]byte, error) {
	names := t.Names()
	if len(names) == 0 {
		return []byte("[]"), nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(n)
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return []byte(b.String()), nil
}

// UnmarshalJSON decodes a list of reason names.
func (t *ThrottleReasons) UnmarshalJSON(b []byte) error {
	var v ThrottleReasons
	s := strings.Trim(string(b), "[] \n\t")
	if s != "" {
		for _, part := range strings.Split(s, ",") {
			name := strings.Trim(strings.TrimSpace(part), `"`)
			for _, n := range throttleNames {
				if n.name == name {
					v |= n.bit
				}
			}
		}
	}
	*t = v
	return nil
}

// VRAMUsedFraction returns used/total if both are known.
func (s Sample) VRAMUsedFraction() metric.Opt[float64] {
	if !s.MemUsed.OK || !s.MemTotal.OK || s.MemTotal.V == 0 {
		return metric.None[float64]()
	}
	return metric.Some(float64(s.MemUsed.V) / float64(s.MemTotal.V))
}
