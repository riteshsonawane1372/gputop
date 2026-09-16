// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package events turns snapshot transitions into a timeline of events and
// maintains the set of currently active alerts.
//
// gputop distinguishes three things:
//
//   - Metric: a sampled value (utilization, temperature).
//   - Event:  a transition worth remembering ("thermal throttling started").
//   - Alert:  a condition that is active right now and needs attention.
package events

import (
	"sync"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/model"
)

// Log is a bounded, thread-safe ring buffer of events.
type Log struct {
	mu   sync.RWMutex
	buf  []model.Event
	next int
	full bool
}

// NewLog creates a log holding up to capacity events.
func NewLog(capacity int) *Log {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Log{buf: make([]model.Event, capacity)}
}

// Append adds events in order.
func (l *Log) Append(evs ...model.Event) {
	if len(evs) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range evs {
		l.buf[l.next] = e
		l.next = (l.next + 1) % len(l.buf)
		if l.next == 0 {
			l.full = true
		}
	}
}

// Len returns the number of stored events.
func (l *Log) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.full {
		return len(l.buf)
	}
	return l.next
}

// Recent returns up to n most recent events, oldest first.
func (l *Log) Recent(n int) []model.Event {
	l.mu.RLock()
	defer l.mu.RUnlock()
	size := l.next
	if l.full {
		size = len(l.buf)
	}
	if n <= 0 || n > size {
		n = size
	}
	out := make([]model.Event, n)
	start := (l.next - n + len(l.buf)) % len(l.buf)
	for i := 0; i < n; i++ {
		out[i] = l.buf[(start+i)%len(l.buf)]
	}
	return out
}

// Since returns events at or after t, oldest first.
func (l *Log) Since(t time.Time) []model.Event {
	all := l.Recent(0)
	i := len(all)
	for i > 0 && !all[i-1].Time.Before(t) {
		i--
	}
	return all[i:]
}
