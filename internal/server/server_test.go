// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/config"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/history"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

type src struct {
	s     *model.Snapshot
	store *history.Store
}

func (s src) Latest() *model.Snapshot { return s.s }
func (s src) History() history.Reader {
	if s.store == nil {
		return nil
	}
	return s.store
}

func snapshot() *model.Snapshot {
	return &model.Snapshot{
		Schema: model.SchemaVersion, Ready: true, Time: time.Unix(1_800_000_000, 0),
		Providers: []model.ProviderStatus{{Name: "nvml", Available: true, System: gpu.SystemInfo{DriverVersion: "560.35"}}},
		GPUs: []model.GPU{{
			Device:   gpu.Device{ID: "GPU-abc", Index: 0, Name: `Test "GPU"`, LinkCount: 2},
			Provider: "nvml", Available: true,
			Sample:    gpu.Sample{UtilPercent: metric.Some(94.0), PowerW: metric.Some(300.5), Throttle: metric.Some(gpu.ThrottleHWThermal)},
			Counters:  gpu.HealthCounters{ECCUncorrectedVolatile: metric.Some(uint64(0))},
			Processes: 1,
		}},
		Processes:  []model.Process{{Process: gpu.Process{PID: 42, DeviceID: "GPU-abc"}, Name: "python", User: "alice", Command: "python train.py --api-key=SECRET"}},
		Collectors: []model.CollectorStatus{{Name: "gpu", Runs: 3}},
	}
}

func token() string { return "0123456789abcdef0123456789abcdef" }

func TestRefusesInsecureRemoteListen(t *testing.T) {
	_, err := New(Options{Server: config.Server{Listen: "0.0.0.0:9469"}, Source: src{s: snapshot()}})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("expected refusal, got %v", err)
	}
	sum := sha256.Sum256([]byte(token()))
	_, err = New(Options{Server: config.Server{Listen: "0.0.0.0:9469", TokenSHA256: hex.EncodeToString(sum[:])}, Source: src{s: snapshot()}})
	if err == nil {
		t.Fatal("token without TLS on a public address must be refused")
	}
	if _, err := New(Options{Server: config.Server{Listen: "127.0.0.1:0"}, Source: src{s: snapshot()}}); err != nil {
		t.Fatalf("loopback without auth is allowed: %v", err)
	}
	if _, err := New(Options{Server: config.Server{Listen: ":9469"}, Source: src{s: snapshot()}}); err == nil {
		t.Fatal("empty host means all interfaces and must be refused")
	}
}

func TestAuthAndRedaction(t *testing.T) {
	dir := t.TempDir()
	tf := filepath.Join(dir, "token")
	os.WriteFile(tf, []byte(token()+"\n"), 0o600)
	s, err := New(Options{Server: config.Server{Listen: "127.0.0.1:0", TokenFile: tf, ExposeProcesses: false},
		Prometheus: config.Prometheus{Enabled: true, Path: "/metrics"}, Source: src{s: snapshot()}, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	do := func(method, path, tok string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("GET", "/healthz", ""); rec.Code != 200 {
		t.Fatalf("healthz %d", rec.Code)
	}
	for _, p := range []string{"/api/v1/snapshot", "/api/v1/summary", "/api/v1/history", "/api/v1/events", "/metrics"} {
		if rec := do("GET", p, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without token: %d", p, rec.Code)
		}
		if rec := do("GET", p, "wrong-token-wrong-token"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s with bad token: %d", p, rec.Code)
		}
	}
	if rec := do("POST", "/api/v1/snapshot", token()); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
	rec := do("GET", "/api/v1/snapshot", token())
	if rec.Code != 200 {
		t.Fatalf("snapshot %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "SECRET") || strings.Contains(body, "alice") || strings.Contains(body, "python") {
		t.Fatalf("process details leaked: %s", body)
	}
	var back model.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &back); err != nil || back.GPUs[0].Sample.UtilPercent.V != 94 || back.Processes[0].PID != 42 {
		t.Fatalf("round trip: %v %+v", err, back.GPUs)
	}
	if rec := do("GET", "/api/v1/history", token()); rec.Code != http.StatusNotFound {
		t.Fatalf("history disabled: %d", rec.Code)
	}
	if rec := do("GET", "/api/v1/events?since=bogus", token()); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad since: %d", rec.Code)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	store, _, _ := history.Open(history.Options{Retention: time.Hour, Resolution: time.Second})
	snap := snapshot()
	for i := 0; i < 5; i++ {
		c := *snap
		c.Time = time.Now().Add(time.Duration(i-5) * time.Second)
		store.Observe(&c)
	}
	s, _ := New(Options{Server: config.Server{Listen: "127.0.0.1:0"}, Source: src{s: snap, store: store}})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/history?since=10m&metrics=util,power&max_points=100", nil))
	if rec.Code != 200 {
		t.Fatalf("history: %d %s", rec.Code, rec.Body)
	}
	var res history.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || len(res.Times) != 4 || len(res.Series[0].Columns) != 2 {
		t.Fatalf("history result: %v %+v", err, res)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/history?metrics=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatal("unknown metric must be rejected")
	}
}

func TestPrometheusExposition(t *testing.T) {
	var buf bytes.Buffer
	if err := WritePrometheus(&buf, snapshot(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`gputop_gpu_utilization_ratio{gpu="0",uuid="GPU-abc"} 0.94`,
		`name="Test \"GPU\""`,
		`gputop_gpu_throttle_reason_active{gpu="0",uuid="GPU-abc",reason="hw_thermal"} 1`,
		`gputop_gpu_ecc_errors_total{gpu="0",uuid="GPU-abc",type="uncorrected",counter="volatile"} 0`,
		"# TYPE gputop_gpu_power_watts gauge",
		`gputop_collector_runs_total{collector="gpu"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, forbidden := range []string{"pid=", "container", "pod_uid", "alice"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("high-cardinality or sensitive label %q present", forbidden)
		}
	}
	if strings.Count(out, "# TYPE gputop_gpu_throttle_reason_active") != 1 {
		t.Error("metric families must be declared once")
	}
	// Unavailable values are omitted, never exported as 0.
	if strings.Contains(out, "gputop_gpu_temperature_celsius{") {
		t.Error("unavailable temperature must not be exported")
	}
}
