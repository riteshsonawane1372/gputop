// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestParseText(t *testing.T) {
	ss := parseText([]byte(`# HELP x help
# TYPE x counter
plain 1
with_ts{a="b"} 2.5 1712345678000
esc{a="q\"uote\\slash\nnl",b="x,y}"} 3
inf_bucket{le="+Inf"} 7
nan NaN
bad{a=unquoted} 1
novalue
`))
	got := map[string]series{}
	for _, s := range ss {
		got[s.name] = s
	}
	if len(ss) != 5 {
		t.Fatalf("parsed %d series, want 5: %+v", len(ss), ss)
	}
	near(t, "plain", got["plain"].value, 1)
	near(t, "with_ts", got["with_ts"].value, 2.5)
	if l := got["esc"].labels; l["a"] != "q\"uote\\slash\nnl" || l["b"] != "x,y}" {
		t.Errorf("escaped labels = %q", l)
	}
	if got["inf_bucket"].labels["le"] != "+Inf" {
		t.Errorf("le = %q", got["inf_bucket"].labels["le"])
	}
	if !math.IsNaN(got["nan"].value) {
		t.Errorf("nan = %v", got["nan"].value)
	}
}

func TestQuantileMatchesPromQL(t *testing.T) {
	// 100 observations: 50 in (0,0.1], 40 in (0.1,0.5], 10 in (0.5,1].
	bounds := []float64{0.1, 0.5, 1, math.Inf(1)}
	counts := []float64{50, 90, 100, 100}
	near(t, "p50", quantile(0.5, bounds, counts).V, 0.1)
	near(t, "p90", quantile(0.9, bounds, counts).V, 0.5)
	near(t, "p99", quantile(0.99, bounds, counts).V, 0.95)
	near(t, "p70", quantile(0.7, bounds, counts).V, 0.3)
	// Observations beyond the last finite bound report that bound.
	near(t, "overflow", quantile(0.99, bounds, []float64{0, 0, 0, 10}).V, 1)
	if quantile(0.5, bounds, []float64{0, 0, 0, 0}).OK {
		t.Error("quantile of an empty histogram should be unavailable")
	}
}

// vllmText renders a minimal vLLM exposition with n requests so far, each
// with a 0.2s TTFT, 20ms inter-token latency and 100 generated tokens.
func vllmText(n float64, waiting float64) string {
	var b strings.Builder
	l := `model_name="meta-llama/Llama-3.1-8B-Instruct"`
	fmt.Fprintf(&b, "vllm:num_requests_running{%s} 12\n", l)
	fmt.Fprintf(&b, "vllm:num_requests_waiting{%s} %g\n", l, waiting)
	fmt.Fprintf(&b, "vllm:gpu_cache_usage_perc{%s} 0.42\n", l)
	fmt.Fprintf(&b, "vllm:prompt_tokens_total{%s} %g\n", l, n*500)
	fmt.Fprintf(&b, "vllm:generation_tokens_total{%s} %g\n", l, n*100)
	fmt.Fprintf(&b, "vllm:request_success_total{%s,finished_reason=\"stop\"} %g\n", l, n*0.9)
	fmt.Fprintf(&b, "vllm:request_success_total{%s,finished_reason=\"length\"} %g\n", l, n*0.1)
	for _, le := range []string{"0.1", "0.25", "+Inf"} {
		c := n
		if le == "0.1" {
			c = 0
		}
		fmt.Fprintf(&b, "vllm:time_to_first_token_seconds_bucket{%s,le=\"%s\"} %g\n", l, le, c)
	}
	fmt.Fprintf(&b, "vllm:time_to_first_token_seconds_sum{%s} %g\n", l, n*0.2)
	fmt.Fprintf(&b, "vllm:time_to_first_token_seconds_count{%s} %g\n", l, n)
	for _, le := range []string{"0.01", "0.025", "+Inf"} {
		c := n * 100
		if le == "0.01" {
			c = 0
		}
		fmt.Fprintf(&b, "vllm:time_per_output_token_seconds_bucket{%s,le=\"%s\"} %g\n", l, le, c)
	}
	fmt.Fprintf(&b, "vllm:time_per_output_token_seconds_sum{%s} %g\n", l, n*100*0.02)
	fmt.Fprintf(&b, "vllm:time_per_output_token_seconds_count{%s} %g\n", l, n*100)
	return b.String()
}

func TestComputeVLLM(t *testing.T) {
	t0 := time.Unix(1000, 0)
	a := extract(parseText([]byte(vllmText(1000, 0))), t0)
	b := extract(parseText([]byte(vllmText(1600, 3))), t0.Add(time.Minute))
	if a == nil || b == nil || b.engine != EngineVLLM {
		t.Fatalf("extract failed: %+v", b)
	}
	if len(b.models) != 1 || b.models[0] != "meta-llama/Llama-3.1-8B-Instruct" {
		t.Errorf("models = %v", b.models)
	}
	m := compute(a, b)
	if m.Window != time.Minute {
		t.Errorf("window = %v", m.Window)
	}
	near(t, "running", m.Running.V, 12)
	near(t, "waiting", m.Waiting.V, 3)
	near(t, "kv", m.KVCacheUsage.V, 0.42)
	near(t, "req/s", m.RequestsPerSec.V, 10)
	near(t, "gen tok/s", m.GenTokensPerSec.V, 1000)
	near(t, "prompt tok/s", m.PromptTokensPerSec.V, 5000)
	near(t, "ttft mean", m.TTFT.Mean.V, 0.2)
	near(t, "ttft count", m.TTFT.Count, 600)
	near(t, "ttft p50", m.TTFT.P50.V, 0.175) // midpoint of (0.1, 0.25]
	near(t, "itl mean", m.ITL.Mean.V, 0.02)
	if m.E2E.P50.OK || m.Queue.Mean.OK {
		t.Error("absent histograms should be unavailable")
	}
	if m.PreemptionsPerSec.OK {
		t.Error("absent counter should be unavailable")
	}

	// The first scrape only has gauges.
	first := compute(nil, a)
	if first.RequestsPerSec.OK || first.TTFT.P50.OK || !first.Running.OK {
		t.Errorf("first scrape: %+v", first)
	}
	// No request completed in the window: latencies unavailable, rates zero.
	idle := compute(a, extract(parseText([]byte(vllmText(1000, 0))), t0.Add(time.Minute)))
	if idle.TTFT.P50.OK || idle.RequestsPerSec.V != 0 {
		t.Errorf("idle window: %+v", idle)
	}
}

func TestDetectOtherEngines(t *testing.T) {
	cases := []struct {
		text   string
		engine string
		check  func(t *testing.T, r *raw)
	}{
		{`sglang:num_running_reqs{model_name="qwen"} 4
sglang:num_queue_reqs{model_name="qwen"} 2
sglang:token_usage{model_name="qwen"} 0.7
sglang:cache_hit_rate{model_name="qwen"} 0.3
sglang:gen_throughput{model_name="qwen"} 812
sglang:time_to_first_token_seconds_bucket{le="0.5"} 3
sglang:time_to_first_token_seconds_bucket{le="+Inf"} 4
sglang:time_to_first_token_seconds_sum 1.2
sglang:time_to_first_token_seconds_count 4
`, EngineSGLang, func(t *testing.T, r *raw) {
			near(t, "running", r.running.V, 4)
			near(t, "cache hit", r.cacheHit.V, 0.3)
			near(t, "gen tps", r.genTPS.V, 812)
			if r.ttft == nil || r.ttft.count != 4 {
				t.Errorf("ttft = %+v", r.ttft)
			}
		}},
		{`tgi_queue_size 5
tgi_batch_current_size 16
tgi_request_success 100
tgi_request_generated_tokens_sum 25000
tgi_request_duration_bucket{le="1"} 10
tgi_request_duration_bucket{le="+Inf"} 100
tgi_request_duration_sum 300
tgi_request_duration_count 100
`, EngineTGI, func(t *testing.T, r *raw) {
			near(t, "waiting", r.waiting.V, 5)
			near(t, "running", r.running.V, 16)
			near(t, "gen", r.genTok.V, 25000)
			if r.ttft != nil {
				t.Error("TGI has no TTFT histogram")
			}
		}},
		{`llamacpp:requests_processing 1
llamacpp:requests_deferred 0
llamacpp:tokens_predicted_total 9000
llamacpp:predicted_tokens_seconds 41.5
`, EngineLlamaCpp, func(t *testing.T, r *raw) {
			near(t, "gen", r.genTok.V, 9000)
			near(t, "gen tps", r.genTPS.V, 41.5)
		}},
	}
	for _, c := range cases {
		r := extract(parseText([]byte(c.text)), time.Now())
		if r == nil || r.engine != c.engine {
			t.Fatalf("%s: got %+v", c.engine, r)
		}
		c.check(t, r)
	}
	if extract(parseText([]byte("go_goroutines 12\nprocess_cpu_seconds_total 3\n")), time.Now()) != nil {
		t.Error("a non-inference endpoint should not be recognized")
	}
}

func TestDiscover(t *testing.T) {
	procs := []Proc{
		{PID: 10, GPU: 0, Command: "/usr/bin/python3 -m vllm.entrypoints.openai.api_server --model m --port 8001"},
		{PID: 11, GPU: 1, Command: "/usr/bin/python3 -m vllm.entrypoints.openai.api_server --model m --port 8001"},
		{PID: 20, GPU: 2, Command: "vllm serve Qwen/Qwen2.5-7B --host 10.1.2.3"},
		{PID: 30, GPU: 3, Command: "python -m sglang.launch_server --model-path x --port=30010 --enable-metrics"},
		{PID: 40, GPU: 4, Command: "text-generation-launcher --model-id x", Containerized: true, PodIP: "10.244.0.9", Pod: "tgi-0"},
		{PID: 50, GPU: 5, Command: "/opt/llama.cpp/llama-server -m model.gguf --metrics"},
		{PID: 60, GPU: 6, Command: "vllm serve m", Containerized: true}, // no pod IP: unreachable
		{PID: 70, GPU: 7, Command: "python train.py --port 8000"},
	}
	got := Discover(procs)
	want := map[string]string{
		"http://127.0.0.1:8001/metrics":  "vllm:8001",
		"http://10.1.2.3:8000/metrics":   "vllm:8000",
		"http://127.0.0.1:30010/metrics": "sglang:30010",
		"http://10.244.0.9:9000/metrics": "tgi-0",
		"http://127.0.0.1:8080/metrics":  "llama.cpp:8080",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d targets, want %d: %+v", len(got), len(want), got)
	}
	for _, tg := range got {
		if want[tg.URL] != tg.Name {
			t.Errorf("target %s named %q, want %q", tg.URL, tg.Name, want[tg.URL])
		}
		if tg.URL == "http://127.0.0.1:8001/metrics" && (len(tg.GPUs) != 2 || tg.PID != 10) {
			t.Errorf("tensor-parallel workers not merged: %+v", tg)
		}
	}
}

// fakeFetch serves scripted pages per URL.
type fakeFetch struct {
	mu    sync.Mutex
	pages map[string]string
}

func (f *fakeFetch) fetch(ctx context.Context, url string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pages[url]
	if !ok {
		return nil, errors.New("connection refused")
	}
	return []byte(p), nil
}

func TestScraperWindow(t *testing.T) {
	now := time.Unix(5000, 0)
	ff := &fakeFetch{pages: map[string]string{}}
	s := NewScraper(Options{Window: 30 * time.Second, Fetch: ff.fetch, Now: func() time.Time { return now }})
	cfg := Target{Name: "cfg", URL: "http://a/metrics", Origin: OriginConfig}
	disc := Target{Name: "disc", URL: "http://b/metrics", Origin: OriginDiscovered}

	// Requests grow by 10/s; scrape every 10s.
	for i := 0; i <= 6; i++ {
		ff.pages[cfg.URL] = vllmText(float64(1000+100*i), 0)
		if err := s.Scrape(context.Background(), []Target{cfg, disc}); err != nil {
			t.Fatalf("scrape %d: %v (a down discovered server is not an error)", i, err)
		}
		now = now.Add(10 * time.Second)
	}
	servers := s.Servers()
	if len(servers) != 2 || servers[0].Name != "cfg" {
		t.Fatalf("servers = %+v", servers)
	}
	sv := servers[0]
	if !sv.Up || sv.Engine != EngineVLLM {
		t.Fatalf("cfg server: %+v", sv)
	}
	if sv.Metrics.Window != 30*time.Second {
		t.Errorf("window = %v, want 30s", sv.Metrics.Window)
	}
	near(t, "req/s", sv.Metrics.RequestsPerSec.V, 10)
	if d := servers[1]; d.Up || !strings.Contains(d.Error, "refused") {
		t.Errorf("discovered server: %+v", d)
	}

	// A restart (counters reset) starts a new window instead of producing
	// negative rates.
	ff.pages[cfg.URL] = vllmText(5, 0)
	_ = s.Scrape(context.Background(), []Target{cfg})
	sv = s.Servers()[0]
	if sv.Metrics.RequestsPerSec.OK {
		t.Errorf("rate after restart = %v, want unavailable", sv.Metrics.RequestsPerSec)
	}
	if len(s.Servers()) != 1 {
		t.Error("targets no longer listed should be forgotten")
	}

	// Credentials in a configured URL are never shown.
	secret := Target{Name: "s", URL: "http://user:hunter2@c/metrics", Origin: OriginConfig}
	ff.pages[secret.URL] = vllmText(1, 0)
	_ = s.Scrape(context.Background(), []Target{secret})
	if u := s.Servers()[0].URL; strings.Contains(u, "hunter2") {
		t.Errorf("URL not redacted: %s", u)
	}

	// A configured server going down is a collector error.
	delete(ff.pages, cfg.URL)
	if err := s.Scrape(context.Background(), []Target{cfg}); err == nil {
		t.Error("expected an error for a configured server that is down")
	}
}
