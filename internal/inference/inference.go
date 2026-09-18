// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package inference scrapes the Prometheus endpoints of LLM inference
// servers (vLLM, SGLang, Text Generation Inference, llama.cpp) and turns
// their cumulative counters and histograms into serving metrics: time to
// first token, inter-token latency, end-to-end latency, queue time,
// throughput, queue depth and KV-cache usage.
//
// Servers are either configured explicitly or discovered from the command
// lines of GPU processes. Rates and latency percentiles are computed over a
// sliding window from the difference between two scrapes, the same way
// PromQL's rate() and histogram_quantile() would, so they describe recent
// traffic rather than the server's lifetime.
package inference

import (
	"time"

	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// Engines recognized from their metric names.
const (
	EngineVLLM     = "vllm"
	EngineSGLang   = "sglang"
	EngineTGI      = "tgi"
	EngineLlamaCpp = "llama.cpp"
)

// Origins of a target.
const (
	OriginConfig     = "config"
	OriginDiscovered = "discovered"
)

// Server is the observed state of one inference server.
type Server struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Engine is detected from the metric names ("" until the first
	// successful scrape).
	Engine string   `json:"engine,omitempty"`
	Models []string `json:"models,omitempty"`
	Origin string   `json:"origin"`
	// PID and GPUs are known for discovered servers.
	PID  int    `json:"pid,omitempty"`
	GPUs []int  `json:"gpus,omitempty"`
	Pod  string `json:"pod,omitempty"`

	Up             bool          `json:"up"`
	Error          string        `json:"error,omitempty"`
	LastScrape     time.Time     `json:"last_scrape,omitzero"`
	ScrapeDuration time.Duration `json:"scrape_duration_ns"`

	Metrics Metrics `json:"metrics"`
}

// Metrics are serving metrics over the most recent window. Rates and
// latencies are unavailable until two scrapes have been made, and
// latencies are unavailable while no request completed in the window.
type Metrics struct {
	// Window is the time span the rates and latencies cover.
	Window time.Duration `json:"window_ns"`

	Running metric.Opt[float64] `json:"requests_running"`
	Waiting metric.Opt[float64] `json:"requests_waiting"`
	// KVCacheUsage is the fraction (0..1) of KV-cache blocks in use.
	KVCacheUsage metric.Opt[float64] `json:"kv_cache_usage_ratio"`
	// PrefixCacheHitRate is the fraction (0..1) of prompt tokens served
	// from the prefix cache.
	PrefixCacheHitRate metric.Opt[float64] `json:"prefix_cache_hit_ratio"`

	RequestsPerSec     metric.Opt[float64] `json:"requests_per_second"`
	PromptTokensPerSec metric.Opt[float64] `json:"prompt_tokens_per_second"`
	GenTokensPerSec    metric.Opt[float64] `json:"generation_tokens_per_second"`
	PreemptionsPerSec  metric.Opt[float64] `json:"preemptions_per_second"`

	// TTFT is the time to first token.
	TTFT Latency `json:"ttft_seconds"`
	// ITL is the inter-token latency (time per output token).
	ITL Latency `json:"itl_seconds"`
	// E2E is the end-to-end request latency.
	E2E Latency `json:"e2e_seconds"`
	// Queue is the time requests waited before being scheduled.
	Queue Latency `json:"queue_seconds"`
}

// Latency summarizes a histogram over the window, in seconds. Percentiles
// are estimated by linear interpolation within buckets, like PromQL's
// histogram_quantile, so their precision is bounded by the bucket layout.
type Latency struct {
	Mean metric.Opt[float64] `json:"mean"`
	P50  metric.Opt[float64] `json:"p50"`
	P90  metric.Opt[float64] `json:"p90"`
	P99  metric.Opt[float64] `json:"p99"`
	// Count is the number of observations in the window.
	Count float64 `json:"count"`
}

// Target is an endpoint to scrape.
type Target struct {
	Name   string
	URL    string
	Origin string
	PID    int
	GPUs   []int
	Pod    string
}
