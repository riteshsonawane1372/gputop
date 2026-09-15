// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"os"
	"strings"
)

// PodRef identifies a pod.
type PodRef struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// ScanPodLogs maps pod UIDs to names using the kubelet's pod log directory
// layout: <dir>/<namespace>_<pod-name>_<pod-uid>/. Namespaces and pod names
// cannot contain '_', so the split is unambiguous.
func ScanPodLogs(dir string) (map[string]PodRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]PodRef, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		parts := strings.Split(e.Name(), "_")
		if len(parts) != 3 || len(parts[2]) != 36 {
			continue
		}
		out[parts[2]] = PodRef{Namespace: parts[0], Name: parts[1], UID: parts[2]}
	}
	return out, nil
}
