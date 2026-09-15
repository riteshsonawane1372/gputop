// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package remote connects the TUI to a gputop agent running in service mode.
//
//	GPU node: gputop --service  ──HTTPS + bearer token──►  laptop: gputop --remote node
//
// The client verifies the agent certificate (system roots or ca_file),
// supports mutual TLS and reads tokens from a file or environment variable;
// tokens are never stored in the configuration file.
package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gputop/gputop/internal/config"
	"github.com/gputop/gputop/internal/history"
	"github.com/gputop/gputop/internal/model"
	"github.com/gputop/gputop/internal/paths"
	"github.com/gputop/gputop/internal/server"
	"github.com/gputop/gputop/internal/tui"
)

// TokenEnv is the default environment variable holding a remote token.
const TokenEnv = "GPUTOP_TOKEN"

// Client polls one agent. It implements tui.Source.
type Client struct {
	name     string
	base     *url.URL
	token    string
	http     *http.Client
	interval time.Duration

	latest  atomic.Pointer[model.Snapshot]
	lastErr atomic.Pointer[string]
	refresh chan struct{}

	subMu sync.Mutex
	subs  map[chan *model.Snapshot]struct{}
}

var _ tui.Source = (*Client)(nil)

// New creates a client for a configured node.
func New(node config.RemoteNode, interval time.Duration) (*Client, error) {
	u, err := node.URL()
	if err != nil {
		return nil, fmt.Errorf("remote %s: %w", node.Name, err)
	}
	tlsConf := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: node.ServerName}
	if node.CAFile != "" {
		pem, err := os.ReadFile(paths.Expand(node.CAFile))
		if err != nil {
			return nil, fmt.Errorf("remote %s ca_file: %w", node.Name, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("remote %s: ca_file contains no certificates", node.Name)
		}
		tlsConf.RootCAs = pool
	}
	if node.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(paths.Expand(node.CertFile), paths.Expand(node.KeyFile))
		if err != nil {
			return nil, fmt.Errorf("remote %s client certificate: %w", node.Name, err)
		}
		tlsConf.Certificates = []tls.Certificate{cert}
	}
	token := ""
	switch {
	case node.TokenFile != "":
		token, err = server.ReadToken(paths.Expand(node.TokenFile), nil)
		if err != nil {
			return nil, fmt.Errorf("remote %s: %w", node.Name, err)
		}
	case node.TokenEnv != "":
		token = strings.TrimSpace(os.Getenv(node.TokenEnv))
		if token == "" {
			return nil, fmt.Errorf("remote %s: environment variable %s is empty", node.Name, node.TokenEnv)
		}
	default:
		token = strings.TrimSpace(os.Getenv(TokenEnv))
	}
	if interval <= 0 {
		interval = time.Second
	}
	name := node.Name
	if name == "" {
		name = u.Host
	}
	c := &Client{
		name: name, base: u, token: token, interval: interval,
		http: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{
			TLSClientConfig: tlsConf, MaxIdleConns: 4, IdleConnTimeout: 90 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second, ForceAttemptHTTP2: true,
		}},
		refresh: make(chan struct{}, 1), subs: map[chan *model.Snapshot]struct{}{},
	}
	return c, nil
}

// Resolve finds a configured node by name, or accepts a URL.
func Resolve(cfg config.Remote, target string) (config.RemoteNode, error) {
	for _, n := range cfg.Nodes {
		if n.Name == target {
			return n, nil
		}
	}
	if strings.Contains(target, "://") || strings.Contains(target, ":") || strings.Contains(target, ".") {
		n := config.RemoteNode{Name: target, Address: target}
		if _, err := n.URL(); err != nil {
			return n, err
		}
		return n, nil
	}
	var names []string
	for _, n := range cfg.Nodes {
		names = append(names, n.Name)
	}
	return config.RemoteNode{}, fmt.Errorf("unknown remote node %q (configured: %s)", target, strings.Join(names, ", "))
}

// Name returns the node name.
func (c *Client) Name() string { return c.name }

// Address returns the agent URL.
func (c *Client) Address() string { return c.base.String() }

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusUnauthorized {
			return errors.New("unauthorized: check the token (token_file / token_env / $" + TokenEnv + ")")
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 128<<20)).Decode(out)
}

// Run polls the agent until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		c.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.refresh:
		}
	}
}

func (c *Client) poll(ctx context.Context) {
	var s model.Snapshot
	if err := c.get(ctx, "/api/v1/snapshot", nil, &s); err != nil {
		if ctx.Err() != nil {
			return
		}
		msg := err.Error()
		c.lastErr.Store(&msg)
		return
	}
	c.lastErr.Store(nil)
	s.Node.Source, s.Node.Remote = "remote", c.name
	c.latest.Store(&s)
	c.subMu.Lock()
	for ch := range c.subs {
		select {
		case ch <- &s:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- &s:
			default:
			}
		}
	}
	c.subMu.Unlock()
}

// Err returns the last polling error.
func (c *Client) Err() error {
	if p := c.lastErr.Load(); p != nil {
		return errors.New(*p)
	}
	return nil
}

// Subscribe implements tui.Source.
func (c *Client) Subscribe() (<-chan *model.Snapshot, func()) {
	ch := make(chan *model.Snapshot, 1)
	c.subMu.Lock()
	c.subs[ch] = struct{}{}
	c.subMu.Unlock()
	return ch, func() {
		c.subMu.Lock()
		delete(c.subs, ch)
		c.subMu.Unlock()
	}
}

// Latest implements tui.Source. Before the first successful poll it returns
// a not-ready snapshot carrying the connection error, if any.
func (c *Client) Latest() *model.Snapshot {
	if s := c.latest.Load(); s != nil {
		return s
	}
	s := &model.Snapshot{Schema: model.SchemaVersion, Time: time.Now(), Node: model.Node{Source: "remote", Remote: c.name}}
	if err := c.Err(); err != nil {
		s.Providers = []model.ProviderStatus{{Name: "remote " + c.name, Error: err.Error()}}
	}
	return s
}

// RefreshNow implements tui.Source.
func (c *Client) RefreshNow() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// History implements tui.Source.
func (c *Client) History() history.Reader { return historyReader{c} }

type historyReader struct{ c *Client }

func (h historyReader) Query(ctx context.Context, q history.Query) (*history.Result, error) {
	v := url.Values{}
	if !q.Since.IsZero() {
		v.Set("since", time.Since(q.Since).Round(time.Second).String())
	}
	if q.MaxPoints > 0 {
		v.Set("max_points", strconv.Itoa(q.MaxPoints))
	}
	if len(q.Keys) > 0 {
		v.Set("keys", strings.Join(q.Keys, ","))
	}
	if len(q.Metrics) > 0 {
		var keys []string
		for _, m := range q.Metrics {
			keys = append(keys, history.Describe(m).Key)
		}
		v.Set("metrics", strings.Join(keys, ","))
	}
	var res history.Result
	if err := h.c.get(ctx, "/api/v1/history", v, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Fleet polls summaries of several agents for the Nodes tab.
type Fleet struct {
	clients []*Client
	mu      sync.RWMutex
	state   map[string]tui.NodeSummary
}

var _ tui.NodeLister = (*Fleet)(nil)

// NewFleet builds clients for every configured node. Nodes that fail to
// configure are reported as errored summaries.
func NewFleet(cfg config.Remote) *Fleet {
	f := &Fleet{state: map[string]tui.NodeSummary{}}
	for _, n := range cfg.Nodes {
		c, err := New(n, 10*time.Second)
		if err != nil {
			f.state[n.Name] = tui.NodeSummary{Name: n.Name, Address: n.Address, Error: err.Error()}
			continue
		}
		f.clients = append(f.clients, c)
		f.state[n.Name] = tui.NodeSummary{Name: n.Name, Address: c.Address()}
	}
	return f
}

// Empty reports whether no nodes are configured.
func (f *Fleet) Empty() bool { return len(f.state) == 0 }

// Run polls all nodes every 10 seconds.
func (f *Fleet) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		var wg sync.WaitGroup
		for _, c := range f.clients {
			wg.Add(1)
			go func(c *Client) {
				defer wg.Done()
				var s model.Snapshot
				err := c.get(ctx, "/api/v1/summary", nil, &s)
				sum := tui.NodeSummary{Name: c.name, Address: c.Address(), OK: err == nil}
				if err != nil {
					sum.Error = err.Error()
				} else {
					sum.Snapshot = &s
				}
				f.mu.Lock()
				f.state[c.name] = sum
				f.mu.Unlock()
			}(c)
		}
		wg.Wait()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Nodes implements tui.NodeLister.
func (f *Fleet) Nodes() []tui.NodeSummary {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]tui.NodeSummary, 0, len(f.state))
	for _, s := range f.state {
		out = append(out, s)
	}
	sortSummaries(out)
	return out
}

func sortSummaries(s []tui.NodeSummary) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Name < s[j-1].Name; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
