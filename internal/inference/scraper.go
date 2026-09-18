// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

// maxBody bounds a scrape response.
const maxBody = 16 << 20

// FetchFunc retrieves a metrics page.
type FetchFunc func(ctx context.Context, url string) ([]byte, error)

// Options configure a Scraper.
type Options struct {
	// Window is the span rates and latency percentiles are computed over
	// (default 1m).
	Window time.Duration
	// Fetch retrieves metrics; defaults to an HTTP GET.
	Fetch FetchFunc
	Now   func() time.Time
}

// Scraper polls inference servers and keeps a short window of scrapes per
// server. It is safe for concurrent use.
type Scraper struct {
	o Options

	mu      sync.Mutex
	targets map[string]*targetState
}

type targetState struct {
	t        Target
	samples  []*raw // oldest first, within the window
	up       bool
	err      string
	last     time.Time
	duration time.Duration
}

// NewScraper creates a scraper.
func NewScraper(o Options) *Scraper {
	if o.Window <= 0 {
		o.Window = time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Fetch == nil {
		o.Fetch = httpFetch(&http.Client{})
	}
	return &Scraper{o: o, targets: map[string]*targetState{}}
}

func httpFetch(c *http.Client) FetchFunc {
	return func(ctx context.Context, url string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "text/plain;version=0.0.4;q=1,*/*;q=0.1")
		req.Header.Set("User-Agent", "gputop")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			return nil, fmt.Errorf("HTTP %s", resp.Status)
		}
		return io.ReadAll(io.LimitReader(resp.Body, maxBody))
	}
}

// Scrape polls every target concurrently. Targets no longer listed are
// forgotten. The error covers configured targets only: a discovered
// server without a metrics endpoint is shown as down, not as a failure.
func (s *Scraper) Scrape(ctx context.Context, targets []Target) error {
	s.mu.Lock()
	keep := map[string]bool{}
	states := make([]*targetState, 0, len(targets))
	for _, t := range targets {
		if keep[t.URL] {
			continue
		}
		keep[t.URL] = true
		st := s.targets[t.URL]
		if st == nil {
			st = &targetState{}
			s.targets[t.URL] = st
		}
		st.t = t
		states = append(states, st)
	}
	for u := range s.targets {
		if !keep[u] {
			delete(s.targets, u)
		}
	}
	s.mu.Unlock()

	var (
		wg   sync.WaitGroup
		emu  sync.Mutex
		errs []error
	)
	for _, st := range states {
		wg.Add(1)
		go func(st *targetState) {
			defer wg.Done()
			if err := s.scrapeOne(ctx, st); err != nil && st.t.Origin == OriginConfig {
				emu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", st.t.Name, err))
				emu.Unlock()
			}
		}(st)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (s *Scraper) scrapeOne(ctx context.Context, st *targetState) error {
	s.mu.Lock()
	target := st.t.URL
	s.mu.Unlock()
	begin := time.Now()
	body, err := s.o.Fetch(ctx, target)
	now := s.o.Now()
	var r *raw
	if err == nil {
		if r = extract(parseText(body), now); r == nil {
			err = errors.New("no vLLM, SGLang, TGI or llama.cpp metrics found")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st.last, st.duration = now, time.Since(begin)
	if err != nil {
		st.up, st.err, st.samples = false, err.Error(), nil
		return err
	}
	st.up, st.err = true, ""
	if n := len(st.samples); n > 0 && reset(st.samples[n-1], r) {
		st.samples = nil
	}
	st.samples = append(st.samples, r)
	// Keep the newest sample at or before the window start as the base, so
	// the window stays full once enough history exists.
	cutoff := now.Add(-s.o.Window)
	drop := 0
	for drop+1 < len(st.samples) && !st.samples[drop+1].at.After(cutoff) {
		drop++
	}
	st.samples = append(st.samples[:0], st.samples[drop:]...)
	return nil
}

// redactURL hides a password in the URL's user info.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	return u.Redacted()
}

// Servers returns the current state of every target, configured ones first.
func (s *Scraper) Servers() []Server {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Server, 0, len(s.targets))
	for _, st := range s.targets {
		sv := Server{
			Name: st.t.Name, URL: redactURL(st.t.URL), Origin: st.t.Origin, PID: st.t.PID,
			GPUs: append([]int(nil), st.t.GPUs...), Pod: st.t.Pod,
			Up: st.up, Error: st.err, LastScrape: st.last, ScrapeDuration: st.duration,
		}
		if n := len(st.samples); n > 0 {
			cur := st.samples[n-1]
			var base *raw
			if n > 1 {
				base = st.samples[0]
			}
			sv.Engine = cur.engine
			sv.Models = append([]string(nil), cur.models...)
			sv.Metrics = compute(base, cur)
		}
		out = append(out, sv)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Origin != out[j].Origin {
			return out[i].Origin == OriginConfig
		}
		return out[i].Name < out[j].Name
	})
	return out
}
