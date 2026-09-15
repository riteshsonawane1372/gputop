// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"sort"
	"strings"
	"time"
)

// PodInfo is what gputop shows about a GPU pod: the API server's view when
// API access is enabled, otherwise the identity recovered from the node.
type PodInfo struct {
	UID       string    `json:"uid"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Node      string    `json:"node,omitempty"`
	PodIP     string    `json:"pod_ip,omitempty"`
	HostIP    string    `json:"host_ip,omitempty"`
	Phase     string    `json:"phase,omitempty"` // Pending, Running, Succeeded, Failed, Unknown
	Reason    string    `json:"reason,omitempty"`
	Message   string    `json:"message,omitempty"`
	QoS       string    `json:"qos,omitempty"`
	Created   time.Time `json:"created,omitzero"`
	StartTime time.Time `json:"start_time,omitzero"`

	Labels       map[string]string `json:"labels,omitempty"`
	OwnerKind    string            `json:"owner_kind,omitempty"`
	OwnerName    string            `json:"owner_name,omitempty"`
	WorkloadKind string            `json:"workload_kind,omitempty"`
	WorkloadName string            `json:"workload_name,omitempty"`
	// GPURequests is the sum of */gpu limits over the pod's containers.
	GPURequests int `json:"gpu_requests"`

	Containers []ContainerInfo `json:"containers,omitempty"`
	Conditions []PodCondition  `json:"conditions,omitempty"`
	// Source is "api" or "node" (pod log directories only).
	Source string `json:"source"`
}

// ContainerInfo is one container of a pod.
type ContainerInfo struct {
	Name         string            `json:"name"`
	Image        string            `json:"image,omitempty"`
	ID           string            `json:"id,omitempty"` // without the runtime prefix
	Ready        bool              `json:"ready"`
	Restarts     int               `json:"restarts"`
	State        string            `json:"state,omitempty"` // running, waiting, terminated
	StateReason  string            `json:"state_reason,omitempty"`
	StateMessage string            `json:"state_message,omitempty"`
	StartedAt    time.Time         `json:"started_at,omitzero"`
	LastState    string            `json:"last_state,omitempty"` // e.g. "OOMKilled (exit 137)"
	LastFinished time.Time         `json:"last_finished,omitzero"`
	Requests     map[string]string `json:"requests,omitempty"`
	Limits       map[string]string `json:"limits,omitempty"`
}

// PodCondition is a pod status condition.
type PodCondition struct {
	Type           string    `json:"type"`
	Status         string    `json:"status"`
	Reason         string    `json:"reason,omitempty"`
	Message        string    `json:"message,omitempty"`
	LastTransition time.Time `json:"last_transition,omitzero"`
}

// PodEvent is a Kubernetes event about a pod.
type PodEvent struct {
	Type    string    `json:"type"` // Normal, Warning
	Reason  string    `json:"reason"`
	Message string    `json:"message"`
	Count   int       `json:"count"`
	First   time.Time `json:"first,omitzero"`
	Last    time.Time `json:"last,omitzero"`
	From    string    `json:"from,omitempty"`
}

// Ready returns the number of ready containers and the total.
func (p PodInfo) Ready() (ready, total int) {
	for _, c := range p.Containers {
		if c.Ready {
			ready++
		}
	}
	return ready, len(p.Containers)
}

// Restarts sums container restarts.
func (p PodInfo) Restarts() int {
	n := 0
	for _, c := range p.Containers {
		n += c.Restarts
	}
	return n
}

// Status is the kubectl-style status column: a waiting or terminated
// container reason (CrashLoopBackOff, OOMKilled, ...) wins over the phase.
func (p PodInfo) Status() string {
	for _, c := range p.Containers {
		if c.State == "waiting" && c.StateReason != "" {
			return c.StateReason
		}
	}
	for _, c := range p.Containers {
		if c.State == "terminated" && c.StateReason != "" && p.Phase != "Succeeded" {
			return c.StateReason
		}
	}
	switch {
	case p.Reason != "":
		return p.Reason
	case p.Phase == "Succeeded":
		return "Completed"
	}
	return p.Phase
}

// Age is the time since the pod was created (or started).
func (p PodInfo) Age(now time.Time) time.Duration {
	t := p.Created
	if t.IsZero() {
		t = p.StartTime
	}
	if t.IsZero() {
		return -1
	}
	return now.Sub(t)
}

// ContainerNames lists container names in order.
func (p PodInfo) ContainerNames() []string {
	out := make([]string, 0, len(p.Containers))
	for _, c := range p.Containers {
		out = append(out, c.Name)
	}
	return out
}

// SortPods orders pods by namespace and name.
func SortPods(pods []PodInfo) {
	sort.SliceStable(pods, func(i, j int) bool {
		if pods[i].Namespace != pods[j].Namespace {
			return pods[i].Namespace < pods[j].Namespace
		}
		return pods[i].Name < pods[j].Name
	})
}

// SortedLabels renders labels as sorted key=value pairs.
func SortedLabels(labels map[string]string) []string {
	out := make([]string, 0, len(labels))
	for k, v := range labels {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// isGPUResource reports whether a resource name is an accelerator
// (nvidia.com/gpu, amd.com/gpu, nvidia.com/mig-1g.10gb, ...).
func isGPUResource(name string) bool {
	return strings.HasSuffix(name, "/gpu") || strings.Contains(name, "/mig-")
}
