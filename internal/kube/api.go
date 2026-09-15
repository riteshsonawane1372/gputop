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
}

// APIClient talks to the Kubernetes API server with the in-cluster service
// account. It performs read-only GETs on pods (and jobs, to resolve CronJob
// owners). Required RBAC: get/list pods; get jobs (optional).
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
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	UID             string            `json:"uid"`
	Labels          map[string]string `json:"labels"`
	OwnerReferences []struct {
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Controller *bool  `json:"controller"`
	} `json:"ownerReferences"`
}

type podList struct {
	Items []struct {
		Metadata objectMeta `json:"metadata"`
		Spec     struct {
			NodeName   string `json:"nodeName"`
			Containers []struct {
				Name      string `json:"name"`
				Resources struct {
					Limits map[string]string `json:"limits"`
				} `json:"resources"`
			} `json:"containers"`
		} `json:"spec"`
		Status struct {
			ContainerStatuses []struct {
				Name        string `json:"name"`
				ContainerID string `json:"containerID"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
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
		p := Pod{
			PodRef:       PodRef{UID: it.Metadata.UID, Name: it.Metadata.Name, Namespace: it.Metadata.Namespace},
			Node:         it.Spec.NodeName,
			Containers:   map[string]string{},
			templateHash: it.Metadata.Labels["pod-template-hash"],
		}
		for _, cs := range it.Status.ContainerStatuses {
			if id := stripRuntime(cs.ContainerID); id != "" {
				p.Containers[id] = cs.Name
			}
		}
		for _, ct := range it.Spec.Containers {
			for k, v := range ct.Resources.Limits {
				if strings.HasSuffix(k, "/gpu") {
					var n int
					_, _ = fmt.Sscanf(v, "%d", &n)
					p.GPURequests += n
				}
			}
		}
		for _, o := range it.Metadata.OwnerReferences {
			if o.Controller != nil && *o.Controller {
				p.OwnerKind, p.OwnerName = o.Kind, o.Name
			}
		}
		pods = append(pods, p)
	}
	return pods, nil
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
