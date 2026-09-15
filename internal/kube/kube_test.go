// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

const cid = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseCgroup(t *testing.T) {
	cases := []struct {
		name, in string
		want     ContainerRef
	}{
		{"v2 systemd containerd",
			"0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod1a2b3c4d_1111_2222_3333_444455556666.slice/cri-containerd-" + cid + ".scope\n",
			ContainerRef{Runtime: "containerd", ContainerID: cid, PodUID: "1a2b3c4d-1111-2222-3333-444455556666", QoS: "burstable"}},
		{"v1 cgroupfs guaranteed",
			"12:memory:/kubepods/pod1a2b3c4d-1111-2222-3333-444455556666/" + cid + "\n11:cpu:/kubepods/pod1a2b3c4d-1111-2222-3333-444455556666/" + cid,
			ContainerRef{ContainerID: cid, PodUID: "1a2b3c4d-1111-2222-3333-444455556666", QoS: "guaranteed"}},
		{"cri-o besteffort",
			"0::/kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-podaaaaaaaa_bbbb_cccc_dddd_eeeeeeeeeeee.slice/crio-" + cid + ".scope",
			ContainerRef{Runtime: "cri-o", ContainerID: cid, PodUID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", QoS: "besteffort"}},
		{"docker", "0::/system.slice/docker-" + cid + ".scope", ContainerRef{Runtime: "docker", ContainerID: cid}},
		{"docker v1", "4:pids:/docker/" + cid, ContainerRef{Runtime: "docker", ContainerID: cid}},
		{"plain host", "0::/user.slice/user-1000.slice/session-3.scope", ContainerRef{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseCgroup(c.in); got != c.want {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestInferWorkload(t *testing.T) {
	if k, n, ok := InferWorkloadFromPodName("chat-api-7d9f8b6c5-x2kqp"); !ok || k != "Deployment" || n != "chat-api" {
		t.Fatalf("got %s %s %v", k, n, ok)
	}
	for _, name := range []string{"notebook-alice-0", "etcd", "job-abcde"} {
		if _, _, ok := InferWorkloadFromPodName(name); ok {
			t.Errorf("%s must not be inferred", name)
		}
	}
}

func TestScanPodLogsAndDetect(t *testing.T) {
	root := t.TempDir()
	logs := filepath.Join(root, "pods")
	uid := "1a2b3c4d-1111-2222-3333-444455556666"
	for _, d := range []string{"ml_trainer-0_" + uid, "junk", "a_b_c"} {
		if err := os.MkdirAll(filepath.Join(logs, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := ScanPodLogs(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[uid].Name != "trainer-0" || refs[uid].Namespace != "ml" {
		t.Fatalf("refs: %+v", refs)
	}

	env := Detect(DetectOptions{PodLogsDir: logs, Getenv: func(string) string { return "" }, NodeName: "n1"})
	if env.Mode != ModeNode && env.Mode != ModeKubeconfig {
		t.Fatalf("mode: %+v", env)
	}
	c := NewCorrelator(env, logs, nil, nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := c.Resolve(ContainerRef{PodUID: uid, ContainerID: cid})
	if a.PodName != "trainer-0" || a.Namespace != "ml" || a.WorkloadName != "" {
		t.Fatalf("attribution: %+v", a)
	}
}

func TestAPIClientWorkloads(t *testing.T) {
	yes := true
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("fieldSelector") != "spec.nodeName=n1" {
			t.Errorf("fieldSelector = %q", r.URL.Query().Get("fieldSelector"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
			map[string]any{
				"metadata": map[string]any{"name": "web-5f7d8c9b6-abcde", "namespace": "prod", "uid": "u1",
					"labels":          map[string]string{"pod-template-hash": "5f7d8c9b6"},
					"ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": "web-5f7d8c9b6", "controller": yes}}},
				"spec":   map[string]any{"nodeName": "n1", "containers": []any{map[string]any{"name": "app", "resources": map[string]any{"limits": map[string]string{"nvidia.com/gpu": "2"}}}}},
				"status": map[string]any{"containerStatuses": []any{map[string]any{"name": "app", "containerID": "containerd://" + cid}}},
			},
			map[string]any{
				"metadata": map[string]any{"name": "nightly-28100-xyz", "namespace": "batch", "uid": "u2",
					"ownerReferences": []any{map[string]any{"kind": "Job", "name": "nightly-28100", "controller": yes}}},
				"spec": map[string]any{"nodeName": "n1"},
			},
		}})
	})
	mux.HandleFunc("/apis/batch/v1/namespaces/batch/jobs/nightly-28100", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"ownerReferences": []any{map[string]any{"kind": "CronJob", "name": "nightly"}}}})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	api := NewAPIClient(u, "tok", &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // test server

	c := NewCorrelator(Environment{Mode: ModeInCluster, NodeName: "n1"}, "", api, nil)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := c.Resolve(ContainerRef{ContainerID: cid})
	if a.PodName != "web-5f7d8c9b6-abcde" || a.Container != "app" || a.WorkloadKind != "Deployment" || a.WorkloadName != "web" || a.Inferred {
		t.Fatalf("deployment attribution: %+v", a)
	}
	c.mu.RLock()
	w := c.workloads["u2"]
	gpus := c.byUID["u1"].GPURequests
	c.mu.RUnlock()
	if w != [2]string{"CronJob", "nightly"} || gpus != 2 {
		t.Fatalf("cronjob=%v gpus=%d", w, gpus)
	}

	bad := NewCorrelator(Environment{Mode: ModeInCluster}, "", NewAPIClient(u, "wrong", &tls.Config{InsecureSkipVerify: true}), nil) //nolint:gosec // test server
	if err := bad.Refresh(context.Background()); err == nil || bad.Status().APIError == "" {
		t.Fatal("forbidden must surface as an API error")
	}
}
