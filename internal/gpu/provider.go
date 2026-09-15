// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package gpu

import (
	"context"
	"errors"
	"time"
)

// Common provider errors. Providers wrap these so callers can classify
// failures with errors.Is without knowing vendor error codes.
var (
	ErrNotSupported = errors.New("not supported")
	ErrNoPermission = errors.New("insufficient permissions")
	ErrDeviceLost   = errors.New("device lost")
	ErrNotFound     = errors.New("device not found")
	ErrUnavailable  = errors.New("provider unavailable")
)

// Check is one diagnostic step performed while probing a provider.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Diagnostics explains what a provider looked for, for the "no GPU" screen.
type Diagnostics struct {
	Provider string   `json:"provider"`
	Checks   []Check  `json:"checks"`
	Hints    []string `json:"hints,omitempty"`
}

// Provider is implemented by each accelerator backend.
//
// Implementations must be safe for concurrent use: the collector calls
// different methods from different tiers concurrently. Methods must not
// panic when hardware, drivers or features are missing; they return an
// error wrapping one of the sentinel errors above instead.
type Provider interface {
	// Name is a short identifier ("nvml", "simulated").
	Name() string
	// Vendor is the vendor this provider serves.
	Vendor() Vendor

	// Open initializes the underlying library. It returns diagnostics in
	// all cases; the error is non-nil if the provider is unusable.
	Open(ctx context.Context) (Diagnostics, error)
	// Close releases resources.
	Close() error

	// System returns driver/runtime information.
	System(ctx context.Context) (SystemInfo, error)
	// Devices enumerates devices with their static inventory.
	Devices(ctx context.Context) ([]Device, error)
	// Sample reads fast-changing state of a device.
	Sample(ctx context.Context, id ID) (Sample, error)
	// Processes lists processes with a context on the device.
	Processes(ctx context.Context, id ID) ([]Process, error)
	// Health reads reliability counters.
	Health(ctx context.Context, id ID) (HealthCounters, error)
	// Links reads interconnect links. Returns nil, nil when there are none.
	Links(ctx context.Context, id ID) ([]Link, error)
	// Partitions lists hardware partitions (MIG). nil, nil when not enabled.
	Partitions(ctx context.Context, id ID) ([]Partition, error)
}

// DeviceEvent is an asynchronous event reported by the device library.
type DeviceEvent struct {
	Time     time.Time
	DeviceID ID
	// Kind is a vendor-neutral kind: "xid", "ecc_single_bit", "ecc_double_bit",
	// "partition_config_change", "power_source_change", "gpu_unavailable",
	// "recovery_action".
	Kind string
	// Code is the vendor event code (for example the NVIDIA Xid number).
	Code        uint64
	PartitionID ID
	Detail      string
	// Severity is the provider's classification: info, warning or critical.
	Severity string
}

// EventSource is optionally implemented by providers that deliver events
// without polling.
type EventSource interface {
	// WatchEvents blocks, calling emit for every event, until ctx is done.
	WatchEvents(ctx context.Context, emit func(DeviceEvent)) error
}

// TopologyLevel is the closest common ancestor of two devices.
type TopologyLevel string

const (
	TopoSame       TopologyLevel = "internal" // same board
	TopoSingle     TopologyLevel = "pix"      // single PCIe switch
	TopoMultiple   TopologyLevel = "pxb"      // multiple PCIe switches
	TopoHostBridge TopologyLevel = "phb"      // same host bridge
	TopoNode       TopologyLevel = "node"     // same NUMA node
	TopoSystem     TopologyLevel = "sys"      // across NUMA nodes (SMP interconnect)
	TopoUnknown    TopologyLevel = "unknown"
)

// TopologyProvider is optionally implemented by providers that can report
// the PCIe relationship between devices.
type TopologyProvider interface {
	Topology(ctx context.Context, a, b ID) (TopologyLevel, error)
}
