// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package gpu defines the vendor-neutral accelerator domain model and the
// Provider interface implemented by vendor backends (gpu/nvidia, gpu/apple, and
// in the future gpu/amd, gpu/intel, ...).
//
// Nothing in this package may assume a particular vendor. Vendor-specific
// concepts are mapped onto these types by the provider.
package gpu

import (
	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// Vendor identifies an accelerator vendor.
type Vendor string

const (
	VendorNVIDIA    Vendor = "nvidia"
	VendorApple     Vendor = "apple"
	VendorSimulated Vendor = "simulated"
)

// ID is the stable identity of a device: the vendor UUID.
//
// Enumeration indices are NOT stable (they can change across reboots,
// driver reloads or CUDA_VISIBLE_DEVICES) and must never be used as a key.
type ID string

// Device is the (near) static inventory of an accelerator. It is collected
// on the inventory tier and refreshed rarely.
type Device struct {
	ID     ID     `json:"id"`
	Vendor Vendor `json:"vendor"`
	// Index is the enumeration index reported by the provider. Display only.
	Index int `json:"index"`

	Name              string `json:"name"`
	Brand             string `json:"brand,omitempty"`
	Architecture      string `json:"architecture,omitempty"`
	ComputeCapability string `json:"compute_capability,omitempty"` // vendor-defined (e.g. CUDA "8.9")
	Serial            string `json:"serial,omitempty"`
	PartNumber        string `json:"part_number,omitempty"`
	FirmwareVersion   string `json:"firmware_version,omitempty"` // VBIOS for NVIDIA

	PCI      PCIAddress         `json:"pci"`
	NUMANode metric.Opt[int]    `json:"numa_node"`
	Memory   metric.Opt[uint64] `json:"memory_total_bytes"`

	PersistenceMode metric.Opt[bool] `json:"persistence_mode"`
	ComputeMode     string           `json:"compute_mode,omitempty"`
	ECCEnabled      metric.Opt[bool] `json:"ecc_enabled"`

	PowerLimitDefaultW metric.Opt[float64] `json:"power_limit_default_w"`
	PowerLimitMinW     metric.Opt[float64] `json:"power_limit_min_w"`
	PowerLimitMaxW     metric.Opt[float64] `json:"power_limit_max_w"`

	TempSlowdownC metric.Opt[float64] `json:"temp_slowdown_c"`
	TempShutdownC metric.Opt[float64] `json:"temp_shutdown_c"`
	TempMaxOpC    metric.Opt[float64] `json:"temp_max_operating_c"`
	MemTempMaxC   metric.Opt[float64] `json:"memory_temp_max_c"`

	ClockCoreMaxMHz metric.Opt[float64] `json:"clock_core_max_mhz"`
	ClockMemMaxMHz  metric.Opt[float64] `json:"clock_mem_max_mhz"`

	PCIeMaxGen       metric.Opt[int] `json:"pcie_max_gen"`
	PCIeMaxWidth     metric.Opt[int] `json:"pcie_max_width"`
	PCIeDeviceMaxGen metric.Opt[int] `json:"pcie_device_max_gen"`

	MIG MIGMode `json:"mig"`

	// LinkCount is the number of high-speed interconnect links (NVLink).
	LinkCount int `json:"link_count"`

	Capabilities Capabilities `json:"capabilities"`
}

// PCIAddress identifies a device on the PCI bus.
type PCIAddress struct {
	BusID       string `json:"bus_id,omitempty"` // domain:bus:device.function
	DeviceID    uint32 `json:"device_id,omitempty"`
	SubsystemID uint32 `json:"subsystem_id,omitempty"`
}

// MIGMode describes Multi-Instance GPU (or equivalent partitioning) state.
type MIGMode struct {
	Supported    bool `json:"supported"`
	Enabled      bool `json:"enabled"`
	Pending      bool `json:"pending_enabled"`
	MaxInstances int  `json:"max_instances"`
}

// Capability names an optional feature or metric family.
type Capability string

const (
	CapMIG              Capability = "mig"
	CapNVLink           Capability = "nvlink"
	CapECC              Capability = "ecc"
	CapMemoryTemp       Capability = "memory_temperature"
	CapFan              Capability = "fan"
	CapEnergy           Capability = "energy"
	CapEncoder          Capability = "encoder"
	CapDecoder          Capability = "decoder"
	CapJPEG             Capability = "jpeg"
	CapOFA              Capability = "ofa"
	CapProcessUtil      Capability = "process_utilization"
	CapPCIeThroughput   Capability = "pcie_throughput"
	CapClockReasons     Capability = "clock_event_reasons"
	CapRemappedRows     Capability = "remapped_rows"
	CapRetiredPages     Capability = "retired_pages"
	CapXIDEvents        Capability = "xid_events"
	CapPowerLimit       Capability = "power_limit"
	CapViolationCounter Capability = "violation_counters"
	CapHotspotTemp      Capability = "hotspot_temperature"
)

// CapState is the detected support state of a capability.
type CapState string

const (
	CapUnknown      CapState = "unknown"
	CapSupported    CapState = "supported"
	CapUnsupported  CapState = "unsupported"
	CapNoPermission CapState = "no_permission"
)

// Capabilities maps capability to detected state.
type Capabilities map[Capability]CapState

// Has reports whether c is known to be supported.
func (c Capabilities) Has(k Capability) bool { return c[k] == CapSupported }

// State returns the detected state (unknown if never probed).
func (c Capabilities) State(k Capability) CapState {
	if s, ok := c[k]; ok {
		return s
	}
	return CapUnknown
}

// Clone returns a copy safe for publication.
func (c Capabilities) Clone() Capabilities {
	out := make(Capabilities, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}

// SystemInfo is provider-wide (driver/runtime) information.
type SystemInfo struct {
	DriverVersion  string `json:"driver_version,omitempty"`
	LibraryVersion string `json:"library_version,omitempty"` // e.g. NVML version
	RuntimeName    string `json:"runtime_name,omitempty"`    // e.g. "CUDA"
	RuntimeVersion string `json:"runtime_version,omitempty"` // max runtime supported by the driver
}
