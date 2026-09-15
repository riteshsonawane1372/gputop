// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package kube provides optional Kubernetes awareness: detecting the
// environment, mapping processes to containers and pods through their
// cgroups, and resolving pods to workloads.
//
// Kubernetes is never required. On a plain Linux host every function here
// degrades to "not detected".
package kube

import (
	"regexp"
	"strings"
)

// ContainerRef is what can be learned about a process from its cgroup.
type ContainerRef struct {
	Runtime     string `json:"runtime,omitempty"` // containerd, cri-o, docker, podman
	ContainerID string `json:"container_id,omitempty"`
	PodUID      string `json:"pod_uid,omitempty"`
	QoS         string `json:"qos,omitempty"` // guaranteed, burstable, besteffort
}

// IsZero reports whether nothing was found.
func (c ContainerRef) IsZero() bool { return c.ContainerID == "" && c.PodUID == "" }

var (
	// pod<uid> with dashes (cgroupfs driver) or underscores (systemd driver).
	podUIDRe = regexp.MustCompile(`pod([0-9a-fA-F]{8}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{12})`)
	// 64-hex container id, optionally prefixed by a runtime scope name.
	containerRe = regexp.MustCompile(`(?:(cri-containerd|crio|docker|libpod)-)?([0-9a-f]{64})(?:\.scope)?$`)
)

// ParseCgroup extracts container and pod identity from the contents of
// /proc/<pid>/cgroup (cgroup v1 or v2, cgroupfs or systemd drivers).
func ParseCgroup(content string) ContainerRef {
	var ref ContainerRef
	for _, line := range strings.Split(content, "\n") {
		// hierarchy-ID:controller-list:cgroup-path
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if ref.PodUID == "" {
			if m := podUIDRe.FindStringSubmatch(path); m != nil {
				ref.PodUID = strings.ReplaceAll(strings.ToLower(m[1]), "_", "-")
			}
		}
		if ref.QoS == "" && strings.Contains(path, "kubepods") {
			switch {
			case strings.Contains(path, "burstable"):
				ref.QoS = "burstable"
			case strings.Contains(path, "besteffort"):
				ref.QoS = "besteffort"
			default:
				ref.QoS = "guaranteed"
			}
		}
		if ref.ContainerID == "" {
			segs := strings.Split(strings.TrimRight(path, "/"), "/")
			last := segs[len(segs)-1]
			if m := containerRe.FindStringSubmatch(last); m != nil {
				ref.ContainerID = m[2]
				switch m[1] {
				case "cri-containerd":
					ref.Runtime = "containerd"
				case "crio":
					ref.Runtime = "cri-o"
				case "docker":
					ref.Runtime = "docker"
				case "libpod":
					ref.Runtime = "podman"
				default:
					if strings.Contains(path, "/docker/") {
						ref.Runtime = "docker"
					}
				}
			}
		}
	}
	return ref
}

// ShortID returns the conventional 12-character container ID.
func ShortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// safeAlphabet is the alphabet Kubernetes uses for generated name suffixes
// (k8s.io/apimachinery/pkg/util/rand.SafeEncodeString).
const safeAlphabet = "bcdfghjklmnpqrstvwxz2456789"

var deploymentPodRe = regexp.MustCompile(`^(.+)-([` + safeAlphabet + `]{6,10})-([` + safeAlphabet + `]{5})$`)

// InferWorkloadFromPodName guesses a Deployment from a pod name of the form
// <deployment>-<replicaset-hash>-<suffix>. It returns ok=false when the name
// does not match; callers must mark the result as inferred.
func InferWorkloadFromPodName(pod string) (kind, name string, ok bool) {
	if m := deploymentPodRe.FindStringSubmatch(pod); m != nil {
		return "Deployment", m[1], true
	}
	return "", "", false
}
