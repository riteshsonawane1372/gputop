// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPodInfoFromAPI(t *testing.T) {
	var it apiPod
	raw := `{
	  "metadata": {"name": "trainer-0", "namespace": "ml", "uid": "u1", "creationTimestamp": "2026-09-15T10:00:00Z",
	    "labels": {"app": "trainer"}, "ownerReferences": [{"kind": "StatefulSet", "name": "trainer", "controller": true}]},
	  "spec": {"nodeName": "n1", "containers": [
	    {"name": "main", "image": "pytorch:24.08", "resources": {"requests": {"cpu": "4", "nvidia.com/gpu": "2"}, "limits": {"nvidia.com/gpu": "2"}}},
	    {"name": "sidecar", "image": "fluent-bit"}]},
	  "status": {"phase": "Running", "podIP": "10.0.0.5", "hostIP": "192.168.1.2", "qosClass": "Burstable", "startTime": "2026-09-15T10:00:02Z",
	    "conditions": [{"type": "Ready", "status": "False", "reason": "ContainersNotReady"}],
	    "containerStatuses": [
	      {"name": "main", "containerID": "containerd://abc", "ready": true, "restartCount": 3,
	       "state": {"running": {"startedAt": "2026-09-15T11:00:00Z"}},
	       "lastState": {"terminated": {"reason": "OOMKilled", "exitCode": 137, "finishedAt": "2026-09-15T10:59:58Z"}}},
	      {"name": "sidecar", "containerID": "containerd://def", "ready": false, "restartCount": 7,
	       "state": {"waiting": {"reason": "CrashLoopBackOff", "message": "back-off 5m0s"}}}]}
	}`
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatal(err)
	}
	p := podFromAPI(it)
	info := p.Info
	if p.GPURequests != 2 || info.GPURequests != 2 || p.Containers["abc"] != "main" || p.OwnerKind != "StatefulSet" {
		t.Fatalf("pod: %+v", p)
	}
	if info.Node != "n1" || info.PodIP != "10.0.0.5" || info.QoS != "burstable" || info.Source != "api" || info.Created.IsZero() {
		t.Fatalf("info: %+v", info)
	}
	if ready, total := info.Ready(); ready != 1 || total != 2 || info.Restarts() != 10 {
		t.Fatalf("ready %d/%d restarts %d", ready, total, info.Restarts())
	}
	if got := info.Status(); got != "CrashLoopBackOff" {
		t.Fatalf("status = %s, want CrashLoopBackOff", got)
	}
	main := info.Containers[0]
	if main.State != "running" || main.LastState != "OOMKilled (exit 137)" || main.Image != "pytorch:24.08" || main.Requests["nvidia.com/gpu"] != "2" {
		t.Fatalf("main container: %+v", main)
	}
	if len(info.Conditions) != 1 || info.Conditions[0].Reason != "ContainersNotReady" {
		t.Fatalf("conditions: %+v", info.Conditions)
	}
	if got := (PodInfo{Phase: "Succeeded"}).Status(); got != "Completed" {
		t.Fatalf("succeeded status = %s", got)
	}
	if got := SortedLabels(map[string]string{"b": "2", "a": "1"}); !reflect.DeepEqual(got, []string{"a=1", "b=2"}) {
		t.Fatalf("labels: %v", got)
	}
}

func TestReadContainerLog(t *testing.T) {
	dir := t.TempDir()
	pod := PodRef{UID: "1a2b3c4d-1111-2222-3333-444455556666", Name: "trainer-0", Namespace: "ml"}
	cdir := filepath.Join(dir, "ml_trainer-0_"+pod.UID, "main")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := "2026-09-15T10:00:00.000000000Z stdout F from the previous run\n"
	cur := "2026-09-15T10:01:00.000000000Z stdout F step 1\n" +
		"2026-09-15T10:01:01.000000000Z stderr P partial \n" +
		"2026-09-15T10:01:01.100000000Z stderr F line joined\n" +
		"not cri framed\n" +
		"2026-09-15T10:01:02.000000000Z stdout F step 2\n"
	if err := os.WriteFile(filepath.Join(cdir, "0.log"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cdir, "1.log"), []byte(cur), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadContainerLog(dir, pod, "main", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"step 1", "partial line joined", "not cri framed", "step 2"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	if lines, _ = ReadContainerLog(dir, pod, "main", 2); !reflect.DeepEqual(lines, want[2:]) {
		t.Fatalf("tail = %q", lines)
	}
	if _, err := ReadContainerLog(dir, pod, "missing", 10); err == nil {
		t.Fatal("missing container must fail")
	}

	// The correlator prefers node log files and lists containers from them.
	c := NewCorrelator(Environment{Mode: ModeNode, NodeName: "n1"}, dir, nil, nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	pods := c.Pods(map[string]bool{pod.UID: true})
	if len(pods) != 1 || pods[0].Source != "node" || len(pods[0].Containers) != 1 || pods[0].Containers[0].Name != "main" {
		t.Fatalf("pods: %+v", pods)
	}
	if got := c.Pods(nil); len(got) != 0 {
		t.Fatalf("pods without GPU use or requests must be skipped: %+v", got)
	}
	lines, source, err := c.PodLogs(context.Background(), pod, "main", 10)
	if err != nil || source != "node log files" || len(lines) != 4 {
		t.Fatalf("logs: %v %s %v", lines, source, err)
	}
	if _, err := c.PodEvents(context.Background(), pod); err == nil {
		t.Fatal("events without API must fail")
	}
	if !c.Status().Inspect {
		t.Fatal("inspect must be available with a pod log directory")
	}
	none := NewCorrelator(Environment{Mode: ModeNode}, "", nil, nil)
	if _, _, err := none.PodLogs(context.Background(), pod, "main", 10); !errors.Is(err, ErrNoInspect) {
		t.Fatalf("no sources: %v", err)
	}
}

func TestAPIPodLogsAndEvents(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/namespaces/ml/pods/trainer-0/log", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("container") != "main" || r.URL.Query().Get("tailLines") != "50" {
			t.Errorf("log query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("hello\nworld\n"))
	})
	mux.HandleFunc("/api/v1/namespaces/ml/events", func(w http.ResponseWriter, r *http.Request) {
		if fs := r.URL.Query().Get("fieldSelector"); !strings.Contains(fs, "involvedObject.name=trainer-0") {
			t.Errorf("fieldSelector = %q", fs)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			map[string]any{"type": "Warning", "reason": "BackOff", "message": "restarting", "count": 3,
				"lastTimestamp": "2026-09-15T10:05:00Z", "source": map[string]string{"component": "kubelet"}},
			map[string]any{"type": "Normal", "reason": "Scheduled", "message": "assigned", "eventTime": "2026-09-15T10:00:00Z",
				"reportingComponent": "default-scheduler"},
		}})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	api := NewAPIClient(u, "tok", &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test server
	c := NewCorrelator(Environment{Mode: ModeInCluster}, "", api, nil)
	pod := PodRef{Name: "trainer-0", Namespace: "ml"}

	lines, source, err := c.PodLogs(context.Background(), pod, "main", 50)
	if err != nil || source != "kubernetes API" || !reflect.DeepEqual(lines, []string{"hello", "world"}) {
		t.Fatalf("logs: %q %s %v", lines, source, err)
	}
	evs, err := c.PodEvents(context.Background(), pod)
	if err != nil || len(evs) != 2 {
		t.Fatalf("events: %+v %v", evs, err)
	}
	if evs[0].Reason != "Scheduled" || evs[0].From != "default-scheduler" || evs[0].Last.IsZero() || evs[1].Count != 3 || evs[1].From != "kubelet" {
		t.Fatalf("events not sorted/normalized: %+v", evs)
	}
	if _, err := api.PodLogs(context.Background(), "ml", "nope", "", 1); err == nil {
		t.Fatal("404 must fail")
	}
}
