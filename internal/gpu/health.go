// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package gpu

import (
	"time"

	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// HealthCounters are reliability counters collected on the normal tier.
type HealthCounters struct {
	Time time.Time `json:"time"`

	ECCCorrectedVolatile    metric.Opt[uint64] `json:"ecc_corrected_volatile"`
	ECCUncorrectedVolatile  metric.Opt[uint64] `json:"ecc_uncorrected_volatile"`
	ECCCorrectedAggregate   metric.Opt[uint64] `json:"ecc_corrected_aggregate"`
	ECCUncorrectedAggregate metric.Opt[uint64] `json:"ecc_uncorrected_aggregate"`

	RetiredPagesSBE metric.Opt[uint64] `json:"retired_pages_sbe"`
	RetiredPagesDBE metric.Opt[uint64] `json:"retired_pages_dbe"`
	RetiredPending  metric.Opt[bool]   `json:"retired_pages_pending"`

	RemappedCorrectable   metric.Opt[uint64] `json:"remapped_rows_correctable"`
	RemappedUncorrectable metric.Opt[uint64] `json:"remapped_rows_uncorrectable"`
	RemapPending          metric.Opt[bool]   `json:"remap_pending"`
	RemapFailure          metric.Opt[bool]   `json:"remap_failure"`

	PCIeReplays           metric.Opt[uint64] `json:"pcie_replay_counter"`
	PCIeCorrectableErrors metric.Opt[uint64] `json:"pcie_correctable_errors"`
	PCIeNonFatalErrors    metric.Opt[uint64] `json:"pcie_nonfatal_errors"`
	PCIeFatalErrors       metric.Opt[uint64] `json:"pcie_fatal_errors"`

	// Cumulative time spent below application clocks due to a policy.
	ViolationPower   metric.Opt[time.Duration] `json:"violation_power_ns"`
	ViolationThermal metric.Opt[time.Duration] `json:"violation_thermal_ns"`

	// RecoveryAction is the vendor-reported recommended recovery action
	// ("none", "gpu_reset", "node_reboot", "drain_p2p", ...).
	RecoveryAction metric.Opt[string] `json:"recovery_action"`
}

// LinkState is the state of an interconnect link.
type LinkState string

const (
	LinkActive   LinkState = "active"
	LinkInactive LinkState = "inactive"
	LinkSleep    LinkState = "sleep"
	LinkDisabled LinkState = "disabled" // active but not usable for traffic
	LinkUnknown  LinkState = "unknown"
)

// EndpointType is what is on the far side of a link.
type EndpointType string

const (
	EndpointGPU     EndpointType = "gpu"
	EndpointSwitch  EndpointType = "switch"
	EndpointCPU     EndpointType = "cpu"
	EndpointUnknown EndpointType = "unknown"
)

// Link is one high-speed device interconnect link (NVLink for NVIDIA).
type Link struct {
	Index   int             `json:"index"`
	Kind    string          `json:"kind"` // "nvlink"
	Version metric.Opt[int] `json:"version"`
	State   LinkState       `json:"state"`

	RemoteType  EndpointType `json:"remote_type"`
	RemoteBusID string       `json:"remote_pci_bus_id,omitempty"`
	// RemoteID is filled by the collector when the remote bus ID matches a
	// local device.
	RemoteID ID `json:"remote_id,omitempty"`

	// Cumulative counters in bytes.
	TxBytes metric.Opt[uint64] `json:"tx_bytes"`
	RxBytes metric.Opt[uint64] `json:"rx_bytes"`
	// Rates are derived by the collector from consecutive counters.
	TxBps metric.Opt[float64] `json:"tx_bps"`
	RxBps metric.Opt[float64] `json:"rx_bps"`

	ErrReplay   metric.Opt[uint64] `json:"err_replay"`
	ErrRecovery metric.Opt[uint64] `json:"err_recovery"`
	ErrCRCFlit  metric.Opt[uint64] `json:"err_crc_flit"`
	ErrCRCData  metric.Opt[uint64] `json:"err_crc_data"`
}

// ErrorTotal sums the known error counters.
func (l Link) ErrorTotal() uint64 {
	return l.ErrReplay.Or(0) + l.ErrRecovery.Or(0) + l.ErrCRCFlit.Or(0) + l.ErrCRCData.Or(0)
}

// Partition is a hardware partition of a device (NVIDIA MIG instance).
type Partition struct {
	ID       ID     `json:"id"` // partition UUID
	ParentID ID     `json:"parent_id"`
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Profile  string `json:"profile,omitempty"` // e.g. "1g.10gb"

	InstanceID        metric.Opt[int] `json:"gpu_instance_id"`
	ComputeInstanceID metric.Opt[int] `json:"compute_instance_id"`

	MemTotal metric.Opt[uint64] `json:"memory_total_bytes"`
	MemUsed  metric.Opt[uint64] `json:"memory_used_bytes"`
}

// ProcessType classifies how a process uses the device.
type ProcessType string

const (
	ProcessCompute  ProcessType = "compute"
	ProcessGraphics ProcessType = "graphics"
)

// Process is a process with a context on a device, as reported by the
// vendor library. OS/container enrichment is added by the collector.
type Process struct {
	PID         int         `json:"pid"`
	DeviceID    ID          `json:"device_id"`
	PartitionID ID          `json:"partition_id,omitempty"`
	Type        ProcessType `json:"type"`

	MemUsed metric.Opt[uint64]  `json:"memory_used_bytes"`
	SMUtil  metric.Opt[float64] `json:"sm_util_percent"`
	MemUtil metric.Opt[float64] `json:"mem_util_percent"`
	EncUtil metric.Opt[float64] `json:"enc_util_percent"`
	DecUtil metric.Opt[float64] `json:"dec_util_percent"`

	// Optional metadata a provider may know better than the OS (used by the
	// simulated provider). Real providers leave this empty.
	Meta *ProcessMeta `json:"-"`
}

// ProcessMeta is provider-supplied process metadata.
type ProcessMeta struct {
	Name, User, Command        string
	ContainerID, PodUID        string
	PodName, Namespace         string
	WorkloadKind, WorkloadName string
	StartTime                  time.Time
}
