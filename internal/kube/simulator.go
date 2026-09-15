// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

// Simulator is implemented by the demo provider (--demo) to fabricate GPU
// pods, their logs and events, so the Kubernetes views can be explored
// without a cluster.
type Simulator interface {
	SimulatedPods() []PodInfo
	SimulatedLogs(pod PodRef, container string, tail int) []string
	SimulatedEvents(pod PodRef) []PodEvent
}
