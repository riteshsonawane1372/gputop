// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkObserveCommitPersist(b *testing.B) {
	for _, n := range []int{1, 8, 16} {
		b.Run(fmt.Sprintf("%dgpus", n), func(b *testing.B) {
			ids := make([]string, n)
			for i := range ids {
				ids[i] = fmt.Sprintf("GPU-%036d", i)
			}
			c := &clock{base}
			s, _, err := Open(Options{Retention: 24 * time.Hour, Resolution: time.Second, Dir: b.TempDir(), Persist: true, Now: c.now})
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = s.Close() }()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Every observation lands in a new bucket: worst case (commit + disk write).
				c.t = base.Add(time.Duration(i) * time.Second)
				s.Observe(snapAt(c.t, float64(i%100), ids...))
			}
		})
	}
}

func BenchmarkQuery30m(b *testing.B) {
	c := &clock{base}
	s, _, _ := Open(Options{Retention: 30 * time.Minute, Resolution: 5 * time.Second, Now: c.now})
	ids := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	for i := 0; i < 400; i++ {
		s.Observe(snapAt(base.Add(time.Duration(i)*5*time.Second), float64(i%100), ids...))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Query(context.Background(), Query{MaxPoints: 320})
	}
}
