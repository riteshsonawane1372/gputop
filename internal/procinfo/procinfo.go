// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package procinfo resolves OS metadata (name, user, command line, start
// time, cgroup) for PIDs reported by accelerator libraries.
//
// PID namespaces matter: NVML reports PIDs in the host PID namespace. When
// gputop itself runs in a nested PID namespace (a container without
// hostPID), those PIDs do not correspond to entries in our /proc and must
// not be looked up, or unrelated processes would be mislabelled.
package procinfo

import (
	"os/user"
	"sync"
	"time"
)

// Info is OS-level process metadata.
type Info struct {
	PID       int       `json:"pid"`
	Name      string    `json:"name"`
	User      string    `json:"user,omitempty"`
	Command   string    `json:"command,omitempty"`
	StartTime time.Time `json:"start_time,omitzero"`
	Cgroup    string    `json:"-"`
	// Visible is false when the PID could not be resolved.
	Visible bool `json:"visible"`
	// Reason explains why a process is not visible.
	Reason string `json:"reason,omitempty"`
}

// Resolver caches lookups. Safe for concurrent use.
type Resolver struct {
	mu      sync.Mutex
	cache   map[int]entry
	users   map[string]string
	nested  bool
	checked bool
}

type entry struct {
	startTicks uint64
	info       Info
	seen       time.Time
}

// NewResolver creates a resolver.
func NewResolver() *Resolver {
	return &Resolver{cache: map[int]entry{}, users: map[string]string{}}
}

// NestedPIDNamespace reports whether gputop runs in a nested PID namespace.
func (r *Resolver) NestedPIDNamespace() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.checked {
		r.nested = nestedPIDNamespace()
		r.checked = true
	}
	return r.nested
}

// Lookup resolves a PID from the host PID namespace.
func (r *Resolver) Lookup(pid int) Info {
	if r.NestedPIDNamespace() {
		return Info{PID: pid, Reason: "gputop runs in a nested PID namespace; host PIDs are not visible (run with hostPID)"}
	}
	start, ok := startTicks(pid)
	if !ok {
		return Info{PID: pid, Reason: "process not found in /proc"}
	}
	r.mu.Lock()
	if e, hit := r.cache[pid]; hit && e.startTicks == start {
		e.seen = time.Now()
		r.cache[pid] = e
		r.mu.Unlock()
		return e.info
	}
	r.mu.Unlock()

	info := readInfo(pid, start)
	if info.Visible && info.User != "" {
		info.User = r.username(info.User)
	}
	r.mu.Lock()
	r.cache[pid] = entry{startTicks: start, info: info, seen: time.Now()}
	r.mu.Unlock()
	return info
}

// Prune drops cache entries not looked up within maxAge.
func (r *Resolver) Prune(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	r.mu.Lock()
	defer r.mu.Unlock()
	for pid, e := range r.cache {
		if e.seen.Before(cutoff) {
			delete(r.cache, pid)
		}
	}
}

func (r *Resolver) username(uid string) string {
	r.mu.Lock()
	name, ok := r.users[uid]
	r.mu.Unlock()
	if ok {
		return name
	}
	name = uid
	if u, err := user.LookupId(uid); err == nil {
		name = u.Username
	}
	r.mu.Lock()
	r.users[uid] = name
	r.mu.Unlock()
	return name
}
