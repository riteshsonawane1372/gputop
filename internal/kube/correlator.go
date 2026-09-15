// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Attribution links a process to Kubernetes objects.
type Attribution struct {
	ContainerRef
	PodName      string `json:"pod,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Container    string `json:"container,omitempty"`
	WorkloadKind string `json:"workload_kind,omitempty"`
	WorkloadName string `json:"workload_name,omitempty"`
	// Inferred is true when the workload was guessed from naming patterns
	// rather than read from the API server.
	Inferred bool `json:"workload_inferred,omitempty"`
}

// Status summarizes the correlator for the UI.
type Status struct {
	Environment Environment `json:"environment"`
	APIEnabled  bool        `json:"api_enabled"`
	APIError    string      `json:"api_error,omitempty"`
	PodsKnown   int         `json:"pods_known"`
	LastRefresh time.Time   `json:"last_refresh,omitzero"`
}

// Correlator resolves cgroup references to pods and workloads. Safe for
// concurrent use.
type Correlator struct {
	env        Environment
	podLogsDir string
	api        *APIClient
	log        *slog.Logger

	mu        sync.RWMutex
	byUID     map[string]Pod
	byCont    map[string]string // container ID -> pod UID
	workloads map[string][2]string
	status    Status
}

// NewCorrelator creates a correlator; api may be nil.
func NewCorrelator(env Environment, podLogsDir string, api *APIClient, log *slog.Logger) *Correlator {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Correlator{
		env: env, podLogsDir: podLogsDir, api: api, log: log,
		byUID: map[string]Pod{}, byCont: map[string]string{}, workloads: map[string][2]string{},
		status: Status{Environment: env, APIEnabled: api != nil},
	}
}

// Refresh reloads pod metadata (slow tier).
func (c *Correlator) Refresh(ctx context.Context) error {
	byUID := map[string]Pod{}
	byCont := map[string]string{}
	workloads := map[string][2]string{}
	var apiErr error

	if c.podLogsDir != "" {
		if refs, err := ScanPodLogs(c.podLogsDir); err == nil {
			for uid, r := range refs {
				byUID[uid] = Pod{PodRef: r}
			}
		}
	}
	if c.api != nil {
		pods, err := c.api.ListNodePods(ctx, c.env.NodeName)
		if err != nil {
			apiErr = err
			c.log.Debug("kubernetes pod list failed", "err", err)
		}
		for _, p := range pods {
			byUID[p.UID] = p
			for id := range p.Containers {
				byCont[id] = p.UID
			}
			k, n := c.api.Workload(ctx, p)
			workloads[p.UID] = [2]string{k, n}
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.byUID, c.byCont, c.workloads = byUID, byCont, workloads
	c.status.PodsKnown = len(byUID)
	c.status.LastRefresh = time.Now()
	c.status.APIError = ""
	if apiErr != nil {
		c.status.APIError = apiErr.Error()
	}
	return apiErr
}

// Resolve attributes a process given its cgroup reference.
func (c *Correlator) Resolve(ref ContainerRef) Attribution {
	a := Attribution{ContainerRef: ref}
	if ref.IsZero() {
		return a
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	uid := ref.PodUID
	if uid == "" && ref.ContainerID != "" {
		uid = c.byCont[ref.ContainerID]
		a.PodUID = uid
	}
	pod, ok := c.byUID[uid]
	if !ok {
		return a
	}
	a.PodName, a.Namespace = pod.Name, pod.Namespace
	if name, ok := pod.Containers[ref.ContainerID]; ok {
		a.Container = name
	}
	if w, ok := c.workloads[uid]; ok {
		a.WorkloadKind, a.WorkloadName = w[0], w[1]
	} else if k, n, ok := InferWorkloadFromPodName(pod.Name); ok {
		a.WorkloadKind, a.WorkloadName, a.Inferred = k, n, true
	}
	return a
}

// Status returns the correlator status.
func (c *Correlator) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}
