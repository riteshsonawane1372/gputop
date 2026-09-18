// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package sim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// vLLM's default histogram buckets.
var (
	ttftBuckets  = []float64{0.001, 0.005, 0.01, 0.02, 0.04, 0.06, 0.08, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
	itlBuckets   = []float64{0.01, 0.025, 0.05, 0.075, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5, 0.75, 1, 2.5}
	e2eBuckets   = []float64{0.3, 0.5, 0.8, 1, 1.5, 2, 2.5, 5, 10, 15, 20, 30, 40, 50, 60}
	queueBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
)

// simHist accumulates observations drawn from a log-normal distribution.
// Counts are whole observations, as in a real histogram.
type simHist struct {
	bounds  []float64
	counts  []float64 // per bucket (not cumulative), last one is +Inf
	sum     float64
	pending float64 // fractional observations carried to the next step
}

func newSimHist(bounds []float64) *simHist {
	return &simHist{bounds: bounds, counts: make([]float64, len(bounds)+1)}
}

// observe adds n observations with the given median and log-space spread.
func (h *simHist) observe(n, median, sigma float64) {
	h.pending += n
	k := math.Floor(h.pending)
	if k <= 0 {
		return
	}
	h.pending -= k
	cdf := func(x float64) float64 { return 0.5 * math.Erfc(-(math.Log(x)-math.Log(median))/(sigma*math.Sqrt2)) }
	prev := 0.0
	for i, b := range h.bounds {
		c := math.Round(k * cdf(b))
		h.counts[i] += c - prev
		prev = c
	}
	h.counts[len(h.bounds)] += k - prev
	h.sum += k * median * math.Exp(sigma*sigma/2)
}

func (h *simHist) write(b *bytes.Buffer, name, labels string) {
	cum := 0.0
	for i, le := range h.bounds {
		cum += h.counts[i]
		fmt.Fprintf(b, "%s_bucket{%s,le=\"%s\"} %.0f\n", name, labels, strconv.FormatFloat(le, 'g', -1, 64), cum)
	}
	cum += h.counts[len(h.bounds)]
	fmt.Fprintf(b, "%s_bucket{%s,le=\"+Inf\"} %.0f\n", name, labels, cum)
	fmt.Fprintf(b, "%s_sum{%s} %g\n", name, labels, h.sum)
	fmt.Fprintf(b, "%s_count{%s} %.0f\n", name, labels, cum)
}

// simServer is the simulated vLLM server of the MIG GPU's chat-api pod.
type simServer struct {
	lastT                       float64
	started                     bool
	requests, prompt, gen, pre  float64
	prefixHits, prefixQueries   float64
	ttft, itl, e2e, queue       *simHist
	running, waiting, kvPercent float64
}

// servingLoad is the chat-api load factor at time t: a slow daily-like
// wave plus a 40 s traffic burst every 5 minutes that fills the KV cache,
// queues requests and pushes TTFT up.
func servingLoad(t float64) float64 {
	l := 0.55 + 0.2*math.Sin(t/45) + jitter(7, t, 0.05)
	if math.Mod(t+120, 300) < 40 {
		l += 0.4
	}
	return clamp(l, 0.05, 1.2)
}

func (s *simServer) advance(t float64) {
	if !s.started {
		s.started, s.lastT = true, t-3*3600 // pretend the server has been up for hours
		s.ttft, s.itl, s.e2e, s.queue = newSimHist(ttftBuckets), newSimHist(itlBuckets), newSimHist(e2eBuckets), newSimHist(queueBuckets)
	}
	// Integrate in steps so a long gap (first call) reflects varying load.
	for s.lastT < t {
		dt := math.Min(5, t-s.lastT)
		mid := s.lastT + dt/2
		s.lastT += dt
		l := servingLoad(mid)
		over := math.Max(0, l-0.85)
		n := 14 * math.Min(l, 0.95) * dt
		s.requests += n
		s.prompt += n * 850
		s.gen += n * 240
		s.prefixQueries += n * 850
		s.prefixHits += n * 850 * 0.41
		s.pre += over * 3 * dt
		queueMed := 0.004 + 2.5*over
		itlMed := 0.016 + 0.014*l
		ttftMed := 0.085 + 0.09*l + queueMed
		s.queue.observe(n, queueMed, 0.9)
		s.ttft.observe(n, ttftMed, 0.55)
		s.itl.observe(n*240, itlMed, 0.35)
		s.e2e.observe(n, ttftMed+240*itlMed, 0.45)
	}
	l := servingLoad(t)
	s.running = math.Round(clamp(64*l, 1, 64))
	s.waiting = math.Round(math.Max(0, (l-0.85)*90))
	s.kvPercent = clamp(0.3+0.65*l, 0, 0.99)
}

func (s *simServer) text() []byte {
	var b bytes.Buffer
	const labels = `engine="0",model_name="mistralai/Mistral-7B-Instruct-v0.3"`
	gauge := func(name string, v float64) {
		fmt.Fprintf(&b, "# TYPE %s gauge\n%s{%s} %g\n", name, name, labels, v)
	}
	counter := func(name string, v float64) {
		fmt.Fprintf(&b, "# TYPE %s counter\n%s{%s} %.0f\n", name, name, labels, math.Floor(v))
	}
	gauge("vllm:num_requests_running", s.running)
	gauge("vllm:num_requests_waiting", s.waiting)
	gauge("vllm:kv_cache_usage_perc", s.kvPercent)
	counter("vllm:prompt_tokens_total", s.prompt)
	counter("vllm:generation_tokens_total", s.gen)
	counter("vllm:num_preemptions_total", s.pre)
	counter("vllm:prefix_cache_queries_total", s.prefixQueries)
	counter("vllm:prefix_cache_hits_total", s.prefixHits)
	fmt.Fprintf(&b, "# TYPE vllm:request_success_total counter\n")
	fmt.Fprintf(&b, "vllm:request_success_total{%s,finished_reason=\"stop\"} %.0f\n", labels, math.Floor(s.requests*0.93))
	fmt.Fprintf(&b, "vllm:request_success_total{%s,finished_reason=\"length\"} %.0f\n", labels, math.Floor(s.requests*0.07))
	for _, h := range []struct {
		name string
		h    *simHist
	}{
		{"vllm:time_to_first_token_seconds", s.ttft}, {"vllm:inter_token_latency_seconds", s.itl},
		{"vllm:e2e_request_latency_seconds", s.e2e}, {"vllm:request_queue_time_seconds", s.queue},
	} {
		fmt.Fprintf(&b, "# TYPE %s histogram\n", h.name)
		h.h.write(&b, h.name, labels)
	}
	return b.Bytes()
}

// InferenceMetrics serves the simulated inference servers' Prometheus
// endpoints. It stands in for HTTP in demo mode: the chat-api vLLM server
// answers on port 8000, every other endpoint is unreachable.
func (p *Provider) InferenceMetrics(ctx context.Context, url string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(url, ":8000/") {
		return nil, errors.New("connection refused (simulated)")
	}
	t := p.elapsed()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.serving == nil {
		p.serving = &simServer{}
	}
	p.serving.advance(t)
	return p.serving.text(), nil
}
