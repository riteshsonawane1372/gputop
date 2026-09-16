// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/gpu/sim"
	"github.com/riteshsonawane1372/gputop/internal/history"
)

func benchEngine(b *testing.B, n int, withHistory bool) *Engine {
	b.Helper()
	var store *history.Store
	if withHistory {
		var err error
		store, _, err = history.Open(history.Options{Retention: 30 * time.Minute, Resolution: 5 * time.Second})
		if err != nil {
			b.Fatal(err)
		}
	}
	e := New(Options{Providers: []gpu.Provider{sim.New(sim.Options{GPUs: n})}, Intervals: fastIntervals(), History: store})
	ctx := context.Background()
	<-e.start(ctx, e.collectors["inventory"])
	e.runNow(ctx, "health", "links", "processes", "partitions")
	return e
}

// BenchmarkCollectSamples measures one fast-tier device pass (provider
// calls are simulated, so this isolates gputop's own overhead).
func BenchmarkCollectSamples(b *testing.B) {
	for _, n := range []int{1, 4, 8, 16} {
		b.Run(fmt.Sprintf("%dgpus", n), func(b *testing.B) {
			e := benchEngine(b, n, false)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := e.collectSamples(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPublish measures snapshot assembly, derived metrics, health
// scoring, event detection, alerts and history ingestion.
func BenchmarkPublish(b *testing.B) {
	for _, n := range []int{1, 4, 8, 16} {
		b.Run(fmt.Sprintf("%dgpus", n), func(b *testing.B) {
			e := benchEngine(b, n, true)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				e.publish()
			}
		})
	}
}
