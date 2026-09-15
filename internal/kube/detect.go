// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Mode describes how gputop relates to Kubernetes.
type Mode string

const (
	ModeNone       Mode = "none"
	ModeInCluster  Mode = "in-cluster" // running inside a pod
	ModeNode       Mode = "node"       // running on a Kubernetes node (host)
	ModeKubeconfig Mode = "kubeconfig" // kubeconfig available, not on a node
)

// Environment is the detected Kubernetes context.
type Environment struct {
	Mode       Mode   `json:"mode"`
	NodeName   string `json:"node_name,omitempty"`
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

// Detected reports whether any Kubernetes context was found.
func (e Environment) Detected() bool { return e.Mode != ModeNone && e.Mode != "" }

const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// DetectOptions tune detection (tests override paths).
type DetectOptions struct {
	NodeName   string
	PodLogsDir string
	Root       string // filesystem root prefix for tests
	Getenv     func(string) string
}

// Detect inspects the environment without contacting any server.
func Detect(o DetectOptions) Environment {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	exists := func(p string) bool {
		_, err := os.Stat(filepath.Join(o.Root, p))
		return err == nil
	}
	env := Environment{Mode: ModeNone}
	env.NodeName = o.NodeName
	if env.NodeName == "" {
		env.NodeName = getenv("NODE_NAME")
	}

	switch {
	case getenv("KUBERNETES_SERVICE_HOST") != "" && exists(filepath.Join(saDir, "token")):
		env.Mode = ModeInCluster
		if b, err := os.ReadFile(filepath.Join(o.Root, saDir, "namespace")); err == nil {
			env.Namespace = strings.TrimSpace(string(b))
		}
	case exists("/var/lib/kubelet/pods") || exists("/etc/kubernetes/kubelet.conf") ||
		(o.PodLogsDir != "" && exists(o.PodLogsDir)):
		env.Mode = ModeNode
	}

	kc := getenv("KUBECONFIG")
	if kc == "" {
		if home, err := os.UserHomeDir(); err == nil {
			kc = filepath.Join(home, ".kube", "config")
		}
	}
	if kc != "" {
		first := strings.Split(kc, string(os.PathListSeparator))[0]
		if _, err := os.Stat(first); err == nil {
			env.Kubeconfig = first
			env.Context = currentContext(first)
			if env.Mode == ModeNone {
				env.Mode = ModeKubeconfig
			}
		}
	}
	if env.NodeName == "" && env.Mode == ModeNode {
		env.NodeName, _ = os.Hostname()
	}
	return env
}

// currentContext reads current-context from a kubeconfig without a YAML
// dependency on the full schema.
func currentContext(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "current-context:") {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "current-context:")), `"'`)
		}
	}
	return ""
}
