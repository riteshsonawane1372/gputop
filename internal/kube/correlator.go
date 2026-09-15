// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
	// Pods are the pods using or requesting GPUs on this node.
	Pods []PodInfo `json:"pods,omitempty"`
	// Inspect is true when pod logs or events can be fetched.
	Inspect bool `json:"inspect,omitempty"`
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
		status: Status{Environment: env, APIEnabled: api != nil, Inspect: api != nil || podLogsDir != ""},
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
				byUID[uid] = Pod{PodRef: r, Info: PodInfo{UID: uid, Name: r.Name, Namespace: r.Namespace, Node: c.env.NodeName, Source: "node"}}
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

	// Pods known only from log directories: containers come from their
	// subdirectories so the logs view can offer them.
	if c.podLogsDir != "" {
		for uid, p := range byUID {
			if p.Info.Source != "node" || len(p.Info.Containers) > 0 {
				continue
			}
			if entries, err := os.ReadDir(filepath.Join(c.podLogsDir, p.Namespace+"_"+p.Name+"_"+uid)); err == nil {
				for _, e := range entries {
					if e.IsDir() {
						p.Info.Containers = append(p.Info.Containers, ContainerInfo{Name: e.Name()})
					}
				}
				byUID[uid] = p
			}
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

// Pods returns info for pods that request GPUs or whose UID is in using
// (pods with GPU processes), sorted by namespace and name.
func (c *Correlator) Pods(using map[string]bool) []PodInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []PodInfo
	for uid, p := range c.byUID {
		if p.GPURequests == 0 && !using[uid] {
			continue
		}
		info := p.Info
		if info.UID == "" {
			info = PodInfo{UID: p.UID, Name: p.Name, Namespace: p.Namespace, Source: "node"}
		}
		if w, ok := c.workloads[uid]; ok {
			info.WorkloadKind, info.WorkloadName = w[0], w[1]
		}
		out = append(out, info)
	}
	SortPods(out)
	return out
}

// ErrNoInspect is returned when neither the API nor pod log files are
// available.
var ErrNoInspect = errors.New("pod logs and events need the Kubernetes API (kubernetes.api) or the node's pod log directory")

// PodLogs returns the last tail lines of a container's log, preferring the
// node's log files and falling back to the API. source names where they
// came from.
func (c *Correlator) PodLogs(ctx context.Context, pod PodRef, container string, tail int) (lines []string, source string, err error) {
	if c.podLogsDir != "" && pod.UID != "" {
		lines, err = ReadContainerLog(c.podLogsDir, pod, container, tail)
		if err == nil {
			return lines, "node log files", nil
		}
	}
	if c.api != nil {
		lines, err = c.api.PodLogs(ctx, pod.Namespace, pod.Name, container, tail)
		return lines, "kubernetes API", err
	}
	if err == nil {
		err = ErrNoInspect
	}
	return nil, "", err
}

// PodEvents lists a pod's events (API only).
func (c *Correlator) PodEvents(ctx context.Context, pod PodRef) ([]PodEvent, error) {
	if c.api == nil {
		return nil, errors.New("pod events need Kubernetes API access (kubernetes.api: true)")
	}
	return c.api.PodEvents(ctx, pod.Namespace, pod.Name)
}
