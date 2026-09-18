// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/metric"
)

// spec maps one engine's metric names onto gputop's fields. Each field
// lists alternative names (engine versions renamed some); the first name
// present in a scrape is used.
type spec struct {
	engine string
	prefix string

	running, waiting    []string // gauges, summed over series
	kvUsage, cacheHit   []string // ratio gauges (0..1), max over series
	genTPS, promptTPS   []string // throughput gauges, used when counters are absent
	promptTok, genTok   []string // counters
	requests, preempted []string // counters
	prefixHits          []string // counters (with prefixQueries: hit ratio)
	prefixQueries       []string
	ttft, itl, e2e, que []string // histogram base names
}

var specs = []spec{
	{
		engine: EngineVLLM, prefix: "vllm:",
		running: []string{"vllm:num_requests_running"}, waiting: []string{"vllm:num_requests_waiting"},
		kvUsage:   []string{"vllm:kv_cache_usage_perc", "vllm:gpu_cache_usage_perc"},
		promptTok: []string{"vllm:prompt_tokens_total"}, genTok: []string{"vllm:generation_tokens_total"},
		requests: []string{"vllm:request_success_total"}, preempted: []string{"vllm:num_preemptions_total"},
		prefixHits: []string{"vllm:prefix_cache_hits_total"}, prefixQueries: []string{"vllm:prefix_cache_queries_total"},
		ttft: []string{"vllm:time_to_first_token_seconds"},
		itl:  []string{"vllm:inter_token_latency_seconds", "vllm:time_per_output_token_seconds"},
		e2e:  []string{"vllm:e2e_request_latency_seconds"}, que: []string{"vllm:request_queue_time_seconds"},
	},
	{
		engine: EngineSGLang, prefix: "sglang:",
		running: []string{"sglang:num_running_reqs"}, waiting: []string{"sglang:num_queue_reqs"},
		kvUsage: []string{"sglang:token_usage"}, cacheHit: []string{"sglang:cache_hit_rate"},
		genTPS:    []string{"sglang:gen_throughput"},
		promptTok: []string{"sglang:prompt_tokens_total"}, genTok: []string{"sglang:generation_tokens_total"},
		requests: []string{"sglang:num_requests_total"},
		ttft:     []string{"sglang:time_to_first_token_seconds"},
		itl:      []string{"sglang:inter_token_latency_seconds", "sglang:time_per_output_token_seconds"},
		e2e:      []string{"sglang:e2e_request_latency_seconds"}, que: []string{"sglang:queue_time_seconds"},
	},
	{
		// TGI exposes no time-to-first-token histogram.
		engine: EngineTGI, prefix: "tgi_",
		running: []string{"tgi_batch_current_size"}, waiting: []string{"tgi_queue_size"},
		promptTok: []string{"tgi_request_input_length_sum"}, genTok: []string{"tgi_request_generated_tokens_sum"},
		requests: []string{"tgi_request_success", "tgi_request_success_total"},
		itl:      []string{"tgi_request_mean_time_per_token_duration"},
		e2e:      []string{"tgi_request_duration"}, que: []string{"tgi_request_queue_duration"},
	},
	{
		// llama-server (--metrics) exposes no latency histograms.
		engine: EngineLlamaCpp, prefix: "llamacpp:",
		running: []string{"llamacpp:requests_processing"}, waiting: []string{"llamacpp:requests_deferred"},
		kvUsage: []string{"llamacpp:kv_cache_usage_ratio"},
		genTPS:  []string{"llamacpp:predicted_tokens_seconds"}, promptTPS: []string{"llamacpp:prompt_tokens_seconds"},
		promptTok: []string{"llamacpp:prompt_tokens_total"}, genTok: []string{"llamacpp:tokens_predicted_total"},
	},
}

// hist is a cumulative histogram summed over all of its series.
type hist struct {
	buckets map[float64]float64 // upper bound -> cumulative count
	sum     float64
	count   float64
}

// raw is one scrape reduced to the fields gputop uses.
type raw struct {
	at     time.Time
	engine string
	models []string

	running, waiting, kvUsage, cacheHit, genTPS, promptTPS metric.Opt[float64]
	promptTok, genTok, requests, preempted                 metric.Opt[float64]
	prefixHits, prefixQueries                              metric.Opt[float64]
	ttft, itl, e2e, que                                    *hist
}

// index groups a scrape by metric name.
type index map[string][]series

func newIndex(ss []series) index {
	ix := index{}
	for _, s := range ss {
		ix[s.name] = append(ix[s.name], s)
	}
	return ix
}

func (ix index) first(names []string) []series {
	for _, n := range names {
		if ss, ok := ix[n]; ok {
			return ss
		}
	}
	return nil
}

func (ix index) sum(names []string) metric.Opt[float64] {
	ss := ix.first(names)
	if len(ss) == 0 {
		return metric.None[float64]()
	}
	var v float64
	for _, s := range ss {
		if !math.IsNaN(s.value) {
			v += s.value
		}
	}
	return metric.Some(v)
}

func (ix index) max(names []string) metric.Opt[float64] {
	ss := ix.first(names)
	if len(ss) == 0 {
		return metric.None[float64]()
	}
	v := math.Inf(-1)
	for _, s := range ss {
		if !math.IsNaN(s.value) {
			v = math.Max(v, s.value)
		}
	}
	if math.IsInf(v, -1) {
		return metric.None[float64]()
	}
	return metric.Some(v)
}

func (ix index) hist(names []string) *hist {
	for _, n := range names {
		bs, ok := ix[n+"_bucket"]
		if !ok {
			continue
		}
		h := &hist{buckets: map[float64]float64{}}
		for _, b := range bs {
			le, err := parseFloat(b.labels["le"])
			if err != nil || math.IsNaN(b.value) {
				continue
			}
			h.buckets[le] += b.value
		}
		for _, s := range ix[n+"_sum"] {
			h.sum += s.value
		}
		for _, s := range ix[n+"_count"] {
			h.count += s.value
		}
		if c, ok := h.buckets[math.Inf(1)]; ok && len(ix[n+"_count"]) == 0 {
			h.count = c
		}
		return h
	}
	return nil
}

// detect picks the engine whose prefix most metric names carry.
func detect(ix index) *spec {
	var best *spec
	bestN := 0
	for i := range specs {
		n := 0
		for name := range ix {
			if strings.HasPrefix(name, specs[i].prefix) {
				n++
			}
		}
		if n > bestN {
			best, bestN = &specs[i], n
		}
	}
	return best
}

// extract reduces a parsed scrape. It returns nil when no known engine's
// metrics are present.
func extract(ss []series, at time.Time) *raw {
	ix := newIndex(ss)
	sp := detect(ix)
	if sp == nil {
		return nil
	}
	r := &raw{
		at: at, engine: sp.engine,
		running: ix.sum(sp.running), waiting: ix.sum(sp.waiting),
		kvUsage: ix.max(sp.kvUsage), cacheHit: ix.max(sp.cacheHit),
		genTPS: ix.sum(sp.genTPS), promptTPS: ix.sum(sp.promptTPS),
		promptTok: ix.sum(sp.promptTok), genTok: ix.sum(sp.genTok),
		requests: ix.sum(sp.requests), preempted: ix.sum(sp.preempted),
		prefixHits: ix.sum(sp.prefixHits), prefixQueries: ix.sum(sp.prefixQueries),
		ttft: ix.hist(sp.ttft), itl: ix.hist(sp.itl), e2e: ix.hist(sp.e2e), que: ix.hist(sp.que),
	}
	models := map[string]bool{}
	for name, list := range ix {
		if !strings.HasPrefix(name, sp.prefix) {
			continue
		}
		for _, s := range list {
			if m := s.labels["model_name"]; m != "" {
				models[m] = true
			}
		}
	}
	for m := range models {
		r.models = append(r.models, m)
	}
	sort.Strings(r.models)
	return r
}

// reset reports whether cur looks like a restart relative to base
// (a cumulative counter went backwards).
func reset(base, cur *raw) bool {
	back := func(a, b metric.Opt[float64]) bool { return a.OK && b.OK && b.V < a.V }
	hback := func(a, b *hist) bool { return a != nil && b != nil && b.count < a.count }
	return base.engine != cur.engine ||
		back(base.promptTok, cur.promptTok) || back(base.genTok, cur.genTok) || back(base.requests, cur.requests) ||
		hback(base.ttft, cur.ttft) || hback(base.e2e, cur.e2e)
}

// compute derives window metrics from two scrapes. base may be nil (first
// scrape): only gauges are then available.
func compute(base, cur *raw) Metrics {
	m := Metrics{
		Running: cur.running, Waiting: cur.waiting,
		KVCacheUsage: clampRatio(cur.kvUsage), PrefixCacheHitRate: clampRatio(cur.cacheHit),
	}
	if base == nil {
		m.GenTokensPerSec, m.PromptTokensPerSec = cur.genTPS, cur.promptTPS
		return m
	}
	dt := cur.at.Sub(base.at)
	if dt <= 0 {
		return m
	}
	m.Window = dt
	sec := dt.Seconds()
	rate := func(a, b metric.Opt[float64]) metric.Opt[float64] {
		if !a.OK || !b.OK || b.V < a.V {
			return metric.None[float64]()
		}
		return metric.Some((b.V - a.V) / sec)
	}
	m.RequestsPerSec = rate(base.requests, cur.requests)
	m.PromptTokensPerSec = rate(base.promptTok, cur.promptTok)
	m.GenTokensPerSec = rate(base.genTok, cur.genTok)
	m.PreemptionsPerSec = rate(base.preempted, cur.preempted)
	if !m.GenTokensPerSec.OK {
		m.GenTokensPerSec = cur.genTPS
	}
	if !m.PromptTokensPerSec.OK {
		m.PromptTokensPerSec = cur.promptTPS
	}
	if !m.PrefixCacheHitRate.OK && base.prefixQueries.OK && cur.prefixQueries.OK && cur.prefixHits.OK && base.prefixHits.OK {
		if dq := cur.prefixQueries.V - base.prefixQueries.V; dq > 0 {
			m.PrefixCacheHitRate = clampRatio(metric.Some((cur.prefixHits.V - base.prefixHits.V) / dq))
		}
	}
	m.TTFT = latency(base.ttft, cur.ttft)
	m.ITL = latency(base.itl, cur.itl)
	m.E2E = latency(base.e2e, cur.e2e)
	m.Queue = latency(base.que, cur.que)
	return m
}

func clampRatio(o metric.Opt[float64]) metric.Opt[float64] {
	if !o.OK {
		return o
	}
	return metric.Some(math.Max(0, math.Min(1, o.V)))
}

// latency summarizes the observations between two cumulative histograms.
func latency(a, b *hist) Latency {
	if a == nil || b == nil {
		return Latency{}
	}
	n := b.count - a.count
	if n <= 0 {
		return Latency{}
	}
	bounds := make([]float64, 0, len(b.buckets))
	for le := range b.buckets {
		bounds = append(bounds, le)
	}
	sort.Float64s(bounds)
	counts := make([]float64, len(bounds))
	for i, le := range bounds {
		// Cumulative counts must not decrease with the bound; scrapes that
		// are not atomic can break that, so repair it like PromQL does.
		counts[i] = math.Max(0, b.buckets[le]-a.buckets[le])
		if i > 0 {
			counts[i] = math.Max(counts[i], counts[i-1])
		}
	}
	l := Latency{Count: n, Mean: metric.Some((b.sum - a.sum) / n)}
	l.P50 = quantile(0.50, bounds, counts)
	l.P90 = quantile(0.90, bounds, counts)
	l.P99 = quantile(0.99, bounds, counts)
	return l
}

// quantile estimates the q-quantile from cumulative bucket counts sorted by
// upper bound, interpolating linearly within the bucket (as PromQL does).
func quantile(q float64, bounds, counts []float64) metric.Opt[float64] {
	if len(bounds) == 0 {
		return metric.None[float64]()
	}
	total := counts[len(counts)-1]
	if !math.IsInf(bounds[len(bounds)-1], 1) {
		// Without a +Inf bucket the total is the largest cumulative count.
		for _, c := range counts {
			total = math.Max(total, c)
		}
	}
	if total <= 0 {
		return metric.None[float64]()
	}
	rank := q * total
	i := sort.Search(len(counts), func(i int) bool { return counts[i] >= rank })
	if i >= len(bounds) {
		i = len(bounds) - 1
	}
	if math.IsInf(bounds[i], 1) {
		if i == 0 {
			return metric.None[float64]()
		}
		return metric.Some(bounds[i-1])
	}
	lower, below := 0.0, 0.0
	if i > 0 {
		lower, below = bounds[i-1], counts[i-1]
	} else if bounds[0] <= 0 {
		return metric.Some(bounds[0])
	}
	inBucket := counts[i] - below
	if inBucket <= 0 {
		return metric.Some(bounds[i])
	}
	return metric.Some(lower + (bounds[i]-lower)*(rank-below)/inBucket)
}
