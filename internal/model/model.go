// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package model defines the normalized, immutable node snapshot that flows
// from collectors to the UI, history store, JSON output and HTTP API.
//
// A published *Snapshot is never mutated; producers build a new one per
// refresh. Its JSON encoding is the stable machine-readable schema
// identified by SchemaVersion.
package model

import (
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/health"
	"github.com/riteshsonawane1372/gputop/internal/host"
	"github.com/riteshsonawane1372/gputop/internal/inference"
	"github.com/riteshsonawane1372/gputop/internal/kube"
	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// SchemaVersion identifies the JSON schema of Snapshot.
const SchemaVersion = "gputop.snapshot/v1"

// Snapshot is the complete observable state of one node.
type Snapshot struct {
	Schema string    `json:"schema"`
	Seq    uint64    `json:"seq"`
	Time   time.Time `json:"time"`
	// Ready is false until the first device inventory completed.
	Ready bool `json:"ready"`

	Node       Node               `json:"node"`
	Providers  []ProviderStatus   `json:"providers"`
	GPUs       []GPU              `json:"gpus"`
	Processes  []Process          `json:"processes"`
	Topology   []TopologyEdge     `json:"topology,omitempty"`
	Host       *host.Snapshot     `json:"host,omitempty"`
	Kubernetes kube.Status        `json:"kubernetes"`
	Inference  []inference.Server `json:"inference,omitempty"`
	Fleet      Fleet              `json:"fleet"`
	Events     []Event            `json:"events"`
	Alerts     []Alert            `json:"alerts"`
	Collectors []CollectorStatus  `json:"collectors"`
	History    HistoryStatus      `json:"history"`
	Self       SelfStats          `json:"self"`
}

// Node identifies the monitored machine.
type Node struct {
	Hostname string `json:"hostname"`
	// Source is "local" or "remote".
	Source string `json:"source"`
	// Remote is the configured remote node name when Source is "remote".
	Remote  string `json:"remote,omitempty"`
	Demo    bool   `json:"demo"`
	Version string `json:"gputop_version"`
}

// ProviderStatus describes an accelerator provider.
type ProviderStatus struct {
	Name        string          `json:"name"`
	Vendor      gpu.Vendor      `json:"vendor"`
	Available   bool            `json:"available"`
	Error       string          `json:"error,omitempty"`
	System      gpu.SystemInfo  `json:"system"`
	Diagnostics gpu.Diagnostics `json:"diagnostics"`
}

// GPU aggregates everything known about one device.
type GPU struct {
	Device     gpu.Device         `json:"device"`
	Provider   string             `json:"provider"`
	Available  bool               `json:"available"`
	Error      string             `json:"error,omitempty"`
	Sample     gpu.Sample         `json:"sample"`
	Counters   gpu.HealthCounters `json:"health_counters"`
	Links      []gpu.Link         `json:"links,omitempty"`
	Partitions []gpu.Partition    `json:"partitions,omitempty"`
	Health     health.Result      `json:"health"`
	Derived    Derived            `json:"derived"`
	Processes  int                `json:"process_count"`
}

// State is a coarse activity classification.
type State string

const (
	StateBusy        State = "busy"        // utilization >= 60%
	StateActive      State = "active"      // utilization >= idle threshold
	StateIdle        State = "idle"        // utilization < idle threshold
	StateUnavailable State = "unavailable" // not readable
	StateUnknown     State = "unknown"     // utilization not supported
)

// Derived holds gputop-derived per-GPU values (source: derived).
type Derived struct {
	State         State               `json:"state"`
	Throttled     bool                `json:"throttled"`
	VRAMFraction  metric.Opt[float64] `json:"vram_used_fraction"`
	VRAMHeadroom  metric.Opt[uint64]  `json:"vram_headroom_bytes"`
	PowerFraction metric.Opt[float64] `json:"power_fraction_of_limit"`
	Allocated     bool                `json:"allocated"`
	IdleFor       time.Duration       `json:"idle_for_ns"`
	// IdleAllocated: has processes but stayed below the idle threshold for
	// at least gpu.idle_after.
	IdleAllocated bool                `json:"idle_allocated"`
	UtilAvg       metric.Opt[float64] `json:"util_avg_window"`
	Outlier       bool                `json:"outlier"`
	OutlierDelta  metric.Opt[float64] `json:"outlier_delta_pp"`
	Efficiency    Efficiency          `json:"efficiency"`
	LinksActive   int                 `json:"links_active"`
	NVLinkTxBps   metric.Opt[float64] `json:"nvlink_tx_bps"`
	NVLinkRxBps   metric.Opt[float64] `json:"nvlink_rx_bps"`
}

// Efficiency is the documented composite utilization-efficiency indicator
// (see docs/derived-metrics.md). It is not a throughput measurement.
type Efficiency struct {
	Score  metric.Opt[int] `json:"score"`
	Grade  string          `json:"grade,omitempty"`
	Window time.Duration   `json:"window_ns"`
	Note   string          `json:"note,omitempty"`

	ComputePct      float64 `json:"input_compute_pct"`
	MemBandwidthPct float64 `json:"input_mem_bandwidth_pct"`
	VRAMPct         float64 `json:"input_vram_pct"`
	ThrottleFrac    float64 `json:"input_throttle_fraction"`
}

// Process is a GPU process enriched with OS and Kubernetes metadata.
type Process struct {
	gpu.Process
	DeviceIndex    int              `json:"device_index"`
	PartitionIndex int              `json:"partition_index"`
	Name           string           `json:"name"`
	User           string           `json:"user,omitempty"`
	Command        string           `json:"command,omitempty"`
	StartTime      time.Time        `json:"start_time,omitzero"`
	Visible        bool             `json:"visible"`
	Reason         string           `json:"reason,omitempty"`
	Kube           kube.Attribution `json:"kubernetes"`
}

// WorkloadKey groups processes that belong together.
func (p Process) WorkloadKey() (kind, name string) {
	switch {
	case p.Kube.WorkloadName != "":
		return p.Kube.WorkloadKind, p.Kube.Namespace + "/" + p.Kube.WorkloadName
	case p.Kube.PodName != "":
		return "Pod", p.Kube.Namespace + "/" + p.Kube.PodName
	case p.Kube.ContainerID != "":
		return "Container", kube.ShortID(p.Kube.ContainerID)
	case p.Name != "":
		return "Process", p.Name + " (" + p.User + ")"
	}
	return "Process", "pid " + itoa(p.PID)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}

// TopologyEdge is the relationship between two devices.
type TopologyEdge struct {
	A       gpu.ID            `json:"a"`
	B       gpu.ID            `json:"b"`
	Level   gpu.TopologyLevel `json:"pcie"`
	NVLinks int               `json:"nvlinks"`
}

// Fleet summarizes all GPUs on the node (source: derived).
type Fleet struct {
	GPUs          int                 `json:"gpus"`
	Available     int                 `json:"available"`
	Unavailable   int                 `json:"unavailable"`
	Allocated     int                 `json:"allocated"`
	Active        int                 `json:"active"`
	Busy          int                 `json:"busy"`
	Idle          int                 `json:"idle"`
	IdleAllocated int                 `json:"idle_allocated"`
	Throttled     int                 `json:"throttled"`
	Processes     int                 `json:"processes"`
	UtilAvg       metric.Opt[float64] `json:"util_avg_percent"`
	PowerW        metric.Opt[float64] `json:"power_w"`
	PowerLimitW   metric.Opt[float64] `json:"power_limit_w"`
	TempAvgC      metric.Opt[float64] `json:"temp_avg_c"`
	TempMaxC      metric.Opt[float64] `json:"temp_max_c"`
	VRAMUsed      metric.Opt[uint64]  `json:"vram_used_bytes"`
	VRAMTotal     metric.Opt[uint64]  `json:"vram_total_bytes"`
	VRAMFraction  metric.Opt[float64] `json:"vram_used_fraction"`
	HealthAvg     metric.Opt[float64] `json:"health_avg"`
	HealthMin     metric.Opt[int]     `json:"health_min"`
	// UnusedAllocated is the allocated capacity left unused over the
	// derivation window, in GPU-equivalents.
	UnusedAllocated metric.Opt[float64] `json:"unused_allocated_gpu_equivalents"`
	Imbalance       Imbalance           `json:"imbalance"`
}

// Imbalance describes utilization spread across a cohort of GPUs that
// share a workload (or all active GPUs when no workload grouping exists).
type Imbalance struct {
	Valid    bool     `json:"valid"`
	Cohort   string   `json:"cohort,omitempty"`
	Members  int      `json:"members"`
	Median   float64  `json:"median_pct"`
	Min      float64  `json:"min_pct"`
	Max      float64  `json:"max_pct"`
	Spread   float64  `json:"spread_pp"`
	StdDev   float64  `json:"stddev_pp"`
	Slowest  gpu.ID   `json:"slowest,omitempty"`
	Fastest  gpu.ID   `json:"fastest,omitempty"`
	Outliers []gpu.ID `json:"outliers,omitempty"`
}

// Severity grades events and alerts.
type Severity string

const (
	SevInfo     Severity = "info"
	SevWarning  Severity = "warning"
	SevCritical Severity = "critical"
)

// Rank orders severities.
func (s Severity) Rank() int {
	switch s {
	case SevCritical:
		return 2
	case SevWarning:
		return 1
	}
	return 0
}

// Event is a state transition worth recording (not a metric, not an alert).
type Event struct {
	Time        time.Time         `json:"time"`
	Kind        string            `json:"kind"`
	Severity    Severity          `json:"severity"`
	DeviceID    gpu.ID            `json:"device_id,omitempty"`
	DeviceIndex int               `json:"device_index"`
	Message     string            `json:"message"`
	Source      metric.Source     `json:"source"`
	Attrs       map[string]string `json:"attrs,omitempty"`
}

// Alert is a currently active condition that needs attention.
type Alert struct {
	Key         string    `json:"key"`
	Severity    Severity  `json:"severity"`
	DeviceID    gpu.ID    `json:"device_id,omitempty"`
	DeviceIndex int       `json:"device_index"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail,omitempty"`
	Since       time.Time `json:"since"`
}

// CollectorStatus is self-observability for one collector.
type CollectorStatus struct {
	Name         string        `json:"name"`
	Tier         string        `json:"tier"`
	Interval     time.Duration `json:"interval_ns"`
	Runs         uint64        `json:"runs"`
	Errors       uint64        `json:"errors"`
	Overruns     uint64        `json:"overruns"`
	LastRun      time.Time     `json:"last_run,omitzero"`
	LastDuration time.Duration `json:"last_duration_ns"`
	AvgDuration  time.Duration `json:"avg_duration_ns"`
	LastError    string        `json:"last_error,omitempty"`
	Healthy      bool          `json:"healthy"`
}

// HistoryStatus describes the history store.
type HistoryStatus struct {
	Enabled    bool          `json:"enabled"`
	Persistent bool          `json:"persistent"`
	Retention  time.Duration `json:"retention_ns"`
	Resolution time.Duration `json:"resolution_ns"`
	Points     int           `json:"points"`
	Oldest     time.Time     `json:"oldest,omitzero"`
	DiskBytes  int64         `json:"disk_bytes"`
	WriteAvg   time.Duration `json:"write_avg_ns"`
	Error      string        `json:"error,omitempty"`
}

// SelfStats is gputop's own resource usage.
type SelfStats struct {
	StartTime   time.Time           `json:"start_time"`
	CPUPercent  metric.Opt[float64] `json:"cpu_percent"`
	HeapBytes   uint64              `json:"heap_bytes"`
	SysBytes    uint64              `json:"sys_bytes"`
	Goroutines  int                 `json:"goroutines"`
	CollectTime time.Duration       `json:"last_collect_ns"`
}

// GPUByID finds a GPU in the snapshot.
func (s *Snapshot) GPUByID(id gpu.ID) (*GPU, bool) {
	for i := range s.GPUs {
		if s.GPUs[i].Device.ID == id {
			return &s.GPUs[i], true
		}
	}
	return nil, false
}

// HasLinks reports whether any GPU has interconnect links.
func (s *Snapshot) HasLinks() bool {
	for _, g := range s.GPUs {
		if g.Device.LinkCount > 0 || len(g.Links) > 0 {
			return true
		}
	}
	return false
}

// HasPartitioning reports whether any GPU supports MIG.
func (s *Snapshot) HasPartitioning() bool {
	for _, g := range s.GPUs {
		if g.Device.MIG.Supported {
			return true
		}
	}
	return false
}
