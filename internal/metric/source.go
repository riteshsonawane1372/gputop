// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package metric

// Source records the provenance of a value.
//
// Official vendor metrics (for example NVML readings) and gputop-derived
// values must never be mixed without this distinction being visible.
type Source string

const (
	SourceNVML        Source = "nvml"        // NVIDIA Management Library
	SourceDCGM        Source = "dcgm"        // NVIDIA Data Center GPU Manager (planned)
	SourceHost        Source = "host"        // operating system counters
	SourceProcfs      Source = "procfs"      // per-process information from the OS
	SourceKubernetes  Source = "kubernetes"  // Kubernetes API / kubelet artifacts
	SourceApplication Source = "application" // application-reported telemetry (planned)
	SourceDerived     Source = "derived"     // computed by gputop from other values
	SourceSimulated   Source = "simulated"   // --demo mode; never real hardware
)

// Class is the collection frequency class of a metric.
type Class string

const (
	ClassFast      Class = "fast"      // every refresh (default 1s)
	ClassNormal    Class = "normal"    // every few seconds
	ClassSlow      Class = "slow"      // tens of seconds
	ClassInventory Class = "inventory" // static or near-static
	ClassEvent     Class = "event"     // event driven
)

// Descriptor documents a single metric exposed by gputop.
type Descriptor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Unit        string `json:"unit"`
	Class       Class  `json:"class"`
	Source      Source `json:"source"`
	Description string `json:"description"`
}
