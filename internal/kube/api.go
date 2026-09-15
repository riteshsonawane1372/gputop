// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Pod is the subset of a Kubernetes pod gputop uses.
type Pod struct {
	PodRef
	Node         string
	Containers   map[string]string // container ID (no runtime prefix) -> container name
	OwnerKind    string
	OwnerName    string
	GPURequests  int
	templateHash string
	// Info is the detail shown in the UI.
	Info PodInfo
}

// APIClient talks to the Kubernetes API server with the in-cluster service
// account. It performs read-only GETs on pods (and jobs, to resolve CronJob
// owners). Required RBAC: get/list pods; get jobs (optional); get pods/log
// and list events for the pod logs and events views (optional).
type APIClient struct {
	base   *url.URL
	token  string
	client *http.Client

	mu       sync.Mutex
	jobOwner map[string]string // ns/job -> cronjob ("" when none)
}

// NewInClusterClient builds a client from the pod's service account.
func NewInClusterClient(root string) (*APIClient, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("KUBERNETES_SERVICE_HOST/PORT not set")
	}
	token, err := os.ReadFile(filepath.Join(root, saDir, "token"))
	if err != nil {
		return nil, fmt.Errorf("reading service account token: %w", err)
	}
	ca, err := os.ReadFile(filepath.Join(root, saDir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("reading service account CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("service account CA contains no certificates")
	}
	u := &url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)}
	return NewAPIClient(u, strings.TrimSpace(string(token)), &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}), nil
}

// NewAPIClient builds a client for base with a bearer token.
func NewAPIClient(base *url.URL, token string, tlsConf *tls.Config) *APIClient {
	tr := &http.Transport{TLSClientConfig: tlsConf, ResponseHeaderTimeout: 5 * time.Second, MaxIdleConns: 2}
	return &APIClient{base: base, token: token, client: &http.Client{Transport: tr, Timeout: 10 * time.Second}, jobOwner: map[string]string{}}
}

type objectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	UID               string            `json:"uid"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels"`
	OwnerReferences   []struct {
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Controller *bool  `json:"controller"`
	} `json:"ownerReferences"`
}

type containerState struct {
	Running *struct {
		StartedAt time.Time `json:"startedAt"`
	} `json:"running"`
	Waiting *struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"waiting"`
	Terminated *struct {
		Reason     string    `json:"reason"`
		Message    string    `json:"message"`
		ExitCode   int       `json:"exitCode"`
		StartedAt  time.Time `json:"startedAt"`
		FinishedAt time.Time `json:"finishedAt"`
	} `json:"terminated"`
}

type apiPod struct {
	Metadata objectMeta `json:"metadata"`
	Spec     struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name      string `json:"name"`
			Image     string `json:"image"`
			Resources struct {
				Requests map[string]string `json:"requests"`
				Limits   map[string]string `json:"limits"`
			} `json:"resources"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase      string    `json:"phase"`
		Reason     string    `json:"reason"`
		Message    string    `json:"message"`
		PodIP      string    `json:"podIP"`
		HostIP     string    `json:"hostIP"`
		QOSClass   string    `json:"qosClass"`
		StartTime  time.Time `json:"startTime"`
		Conditions []struct {
			Type               string    `json:"type"`
			Status             string    `json:"status"`
			Reason             string    `json:"reason"`
			Message            string    `json:"message"`
			LastTransitionTime time.Time `json:"lastTransitionTime"`
		} `json:"conditions"`
		ContainerStatuses []struct {
			Name         string         `json:"name"`
			Image        string         `json:"image"`
			ContainerID  string         `json:"containerID"`
			Ready        bool           `json:"ready"`
			RestartCount int            `json:"restartCount"`
			State        containerState `json:"state"`
			LastState    containerState `json:"lastState"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

type podList struct {
	Items []apiPod `json:"items"`
}

func (c *APIClient) get(ctx context.Context, path string, query url.Values, out any) error {
	u := *c.base
	u.Path = path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return &StatusError{Code: resp.StatusCode, Path: path}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

// StatusError is a non-200 API response.
type StatusError struct {
	Code int
	Path string
}

func (e *StatusError) Error() string {
	if e.Code == http.StatusForbidden {
		return fmt.Sprintf("kubernetes API %s: forbidden (grant get/list on pods to gputop's service account)", e.Path)
	}
	return fmt.Sprintf("kubernetes API %s: HTTP %d", e.Path, e.Code)
}

// ListNodePods lists pods scheduled on node.
func (c *APIClient) ListNodePods(ctx context.Context, node string) ([]Pod, error) {
	q := url.Values{}
	if node != "" {
		q.Set("fieldSelector", "spec.nodeName="+node)
	}
	var list podList
	if err := c.get(ctx, "/api/v1/pods", q, &list); err != nil {
		return nil, err
	}
	pods := make([]Pod, 0, len(list.Items))
	for _, it := range list.Items {
		pods = append(pods, podFromAPI(it))
	}
	return pods, nil
}

func podFromAPI(it apiPod) Pod {
	p := Pod{
		PodRef:       PodRef{UID: it.Metadata.UID, Name: it.Metadata.Name, Namespace: it.Metadata.Namespace},
		Node:         it.Spec.NodeName,
		Containers:   map[string]string{},
		templateHash: it.Metadata.Labels["pod-template-hash"],
	}
	st := it.Status
	info := PodInfo{
		UID: p.UID, Name: p.Name, Namespace: p.Namespace, Node: p.Node,
		PodIP: st.PodIP, HostIP: st.HostIP, Phase: st.Phase, Reason: st.Reason, Message: st.Message,
		QoS: strings.ToLower(st.QOSClass), Created: it.Metadata.CreationTimestamp, StartTime: st.StartTime,
		Labels: it.Metadata.Labels, Source: "api",
	}
	for _, c := range st.Conditions {
		info.Conditions = append(info.Conditions, PodCondition{Type: c.Type, Status: c.Status, Reason: c.Reason, Message: c.Message, LastTransition: c.LastTransitionTime})
	}
	for _, ct := range it.Spec.Containers {
		ci := ContainerInfo{Name: ct.Name, Image: ct.Image, Requests: ct.Resources.Requests, Limits: ct.Resources.Limits}
		for k, v := range ct.Resources.Limits {
			if isGPUResource(k) {
				var n int
				_, _ = fmt.Sscanf(v, "%d", &n)
				p.GPURequests += n
			}
		}
		for _, cs := range st.ContainerStatuses {
			if cs.Name != ct.Name {
				continue
			}
			ci.ID = stripRuntime(cs.ContainerID)
			if cs.Image != "" {
				ci.Image = cs.Image
			}
			ci.Ready, ci.Restarts = cs.Ready, cs.RestartCount
			switch s := cs.State; {
			case s.Running != nil:
				ci.State, ci.StartedAt = "running", s.Running.StartedAt
			case s.Waiting != nil:
				ci.State, ci.StateReason, ci.StateMessage = "waiting", s.Waiting.Reason, s.Waiting.Message
			case s.Terminated != nil:
				ci.State, ci.StateReason, ci.StateMessage = "terminated", s.Terminated.Reason, s.Terminated.Message
				ci.StartedAt = s.Terminated.StartedAt
			}
			if lt := cs.LastState.Terminated; lt != nil {
				ci.LastState = fmt.Sprintf("%s (exit %d)", lt.Reason, lt.ExitCode)
				ci.LastFinished = lt.FinishedAt
			}
		}
		if ci.ID != "" {
			p.Containers[ci.ID] = ci.Name
		}
		info.Containers = append(info.Containers, ci)
	}
	for _, o := range it.Metadata.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			p.OwnerKind, p.OwnerName = o.Kind, o.Name
		}
	}
	info.OwnerKind, info.OwnerName, info.GPURequests = p.OwnerKind, p.OwnerName, p.GPURequests
	p.Info = info
	return p
}

// PodLogs returns the last tail lines of a container's log
// (RBAC: get pods/log).
func (c *APIClient) PodLogs(ctx context.Context, namespace, pod, container string, tail int) ([]string, error) {
	q := url.Values{}
	q.Set("tailLines", fmt.Sprint(tail))
	if container != "" {
		q.Set("container", container)
	}
	u := *c.base
	u.Path = "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(pod) + "/log"
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &StatusError{Code: resp.StatusCode, Path: u.Path}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return splitLines(string(b)), nil
}

type eventList struct {
	Items []struct {
		Type           string    `json:"type"`
		Reason         string    `json:"reason"`
		Message        string    `json:"message"`
		Count          int       `json:"count"`
		FirstTimestamp time.Time `json:"firstTimestamp"`
		LastTimestamp  time.Time `json:"lastTimestamp"`
		EventTime      time.Time `json:"eventTime"`
		Source         struct {
			Component string `json:"component"`
		} `json:"source"`
		ReportingComponent string `json:"reportingComponent"`
	} `json:"items"`
}

// PodEvents lists events about a pod, newest last (RBAC: list events).
func (c *APIClient) PodEvents(ctx context.Context, namespace, pod string) ([]PodEvent, error) {
	q := url.Values{}
	q.Set("fieldSelector", "involvedObject.kind=Pod,involvedObject.name="+pod)
	var list eventList
	if err := c.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/events", q, &list); err != nil {
		return nil, err
	}
	out := make([]PodEvent, 0, len(list.Items))
	for _, e := range list.Items {
		ev := PodEvent{Type: e.Type, Reason: e.Reason, Message: e.Message, Count: max(1, e.Count),
			First: e.FirstTimestamp, Last: e.LastTimestamp, From: e.Source.Component}
		if ev.Last.IsZero() {
			ev.Last = e.EventTime
		}
		if ev.First.IsZero() {
			ev.First = ev.Last
		}
		if ev.From == "" {
			ev.From = e.ReportingComponent
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.Before(out[j].Last) })
	return out, nil
}

func stripRuntime(id string) string {
	if i := strings.Index(id, "://"); i >= 0 {
		return id[i+3:]
	}
	return id
}

// Workload resolves the top-level controller of a pod. ReplicaSets owned
// by Deployments are resolved with the pod-template-hash naming contract;
// Jobs are resolved to CronJobs with a (cached) Job lookup.
func (c *APIClient) Workload(ctx context.Context, p Pod) (kind, name string) {
	switch p.OwnerKind {
	case "":
		return "Pod", p.Name
	case "ReplicaSet":
		if p.templateHash != "" && strings.HasSuffix(p.OwnerName, "-"+p.templateHash) {
			return "Deployment", strings.TrimSuffix(p.OwnerName, "-"+p.templateHash)
		}
	case "Job":
		key := p.Namespace + "/" + p.OwnerName
		c.mu.Lock()
		owner, cached := c.jobOwner[key]
		c.mu.Unlock()
		if !cached {
			var job struct {
				Metadata objectMeta `json:"metadata"`
			}
			if err := c.get(ctx, "/apis/batch/v1/namespaces/"+url.PathEscape(p.Namespace)+"/jobs/"+url.PathEscape(p.OwnerName), nil, &job); err == nil {
				for _, o := range job.Metadata.OwnerReferences {
					if o.Kind == "CronJob" {
						owner = o.Name
					}
				}
			}
			c.mu.Lock()
			c.jobOwner[key] = owner
			c.mu.Unlock()
		}
		if owner != "" {
			return "CronJob", owner
		}
	}
	return p.OwnerKind, p.OwnerName
}
