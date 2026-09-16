// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/config"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/history"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
	"github.com/riteshsonawane1372/gputop/internal/server"
)

type src struct {
	s     *model.Snapshot
	store *history.Store
}

func (s src) Latest() *model.Snapshot { return s.s }
func (s src) History() history.Reader { return s.store }

const tok = "remote-test-token-0123456789"

func agent(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	store, _, _ := history.Open(history.Options{Retention: time.Hour, Resolution: time.Second})
	snap := &model.Snapshot{Schema: model.SchemaVersion, Ready: true, Time: time.Now(), Node: model.Node{Hostname: "gpu-node-01", Source: "local"},
		GPUs: []model.GPU{{Device: gpu.Device{ID: "GPU-1", Name: "Remote GPU"}, Available: true, Sample: gpu.Sample{UtilPercent: metric.Some(77.0)}}}}
	for i := 0; i < 3; i++ {
		c := *snap
		c.Time = time.Now().Add(time.Duration(i-3) * time.Second)
		store.Observe(&c)
	}
	sum := sha256.Sum256([]byte(tok))
	s, err := server.New(server.Options{Server: config.Server{Listen: "127.0.0.1:0", TokenSHA256: hex.EncodeToString(sum[:]), ExposeProcesses: true},
		Source: src{s: snap, store: store}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(s.Handler())
	ca := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o600)
	return ts, ca
}

func TestClientPollsAndQueriesHistory(t *testing.T) {
	ts, ca := agent(t)
	defer ts.Close()
	tf := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tf, []byte(tok), 0o600)

	c, err := New(config.RemoteNode{Name: "node1", Address: ts.URL, TokenFile: tf, CAFile: ca}, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if c.Latest().Ready {
		t.Fatal("not ready before first poll")
	}
	sub, cancel := c.Subscribe()
	defer cancel()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go c.Run(ctx)
	select {
	case s := <-sub:
		if !s.Ready || s.Node.Source != "remote" || s.Node.Remote != "node1" || s.GPUs[0].Sample.UtilPercent.V != 77 {
			t.Fatalf("snapshot: %+v", s.Node)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no snapshot: %v", c.Err())
	}
	res, err := c.History().Query(context.Background(), history.Query{Since: time.Now().Add(-10 * time.Minute), Metrics: []history.Metric{history.Util}})
	if err != nil || len(res.Times) != 2 || res.Column("GPU-1", history.Util)[0] != 77 {
		t.Fatalf("history: %v %+v", err, res)
	}
}

func TestClientRejectsBadTokenAndUntrustedCert(t *testing.T) {
	ts, ca := agent(t)
	defer ts.Close()
	t.Setenv("BAD_TOKEN", "definitely-not-the-token")
	c, err := New(config.RemoteNode{Name: "n", Address: ts.URL, TokenEnv: "BAD_TOKEN", CAFile: ca}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.poll(context.Background())
	if c.Err() == nil || !strings.Contains(c.Err().Error(), "unauthorized") {
		t.Fatalf("bad token err = %v", c.Err())
	}
	if s := c.Latest(); s.Ready || len(s.Providers) != 1 || s.Providers[0].Error == "" {
		t.Fatal("connection errors must surface in the not-ready snapshot")
	}

	t.Setenv(TokenEnv, tok)
	untrusted, _ := New(config.RemoteNode{Name: "n", Address: ts.URL}, time.Second)
	untrusted.poll(context.Background())
	if untrusted.Err() == nil || !strings.Contains(untrusted.Err().Error(), "certificate") {
		t.Fatalf("self-signed agent without ca_file must be rejected: %v", untrusted.Err())
	}
}

func TestResolveAndPlaintextPolicy(t *testing.T) {
	cfg := config.Remote{Nodes: []config.RemoteNode{{Name: "a", Address: "gpu-a.example.com"}}}
	if n, err := Resolve(cfg, "a"); err != nil || n.Address != "gpu-a.example.com" {
		t.Fatalf("resolve by name: %v", err)
	}
	if _, err := Resolve(cfg, "unknown"); err == nil {
		t.Fatal("unknown name must error")
	}
	if _, err := Resolve(cfg, "http://gpu-b.example.com:9469"); err == nil {
		t.Fatal("plaintext to non-loopback must be refused")
	}
	if n, err := Resolve(cfg, "http://127.0.0.1:9469"); err != nil || n.Address == "" {
		t.Fatalf("loopback plaintext (SSH tunnel) allowed: %v", err)
	}
	u, _ := config.RemoteNode{Address: "gpu-c"}.URL()
	if u.String() != "https://gpu-c:9469" {
		t.Fatalf("default scheme/port: %s", u)
	}
}

func TestFleet(t *testing.T) {
	ts, ca := agent(t)
	defer ts.Close()
	t.Setenv("TOK", tok)
	f := NewFleet(config.Remote{Nodes: []config.RemoteNode{
		{Name: "good", Address: ts.URL, TokenEnv: "TOK", CAFile: ca},
		{Name: "broken", Address: "gpu.example.com", CAFile: "/nonexistent"},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go f.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		nodes := f.Nodes()
		if len(nodes) == 2 && nodes[1].OK {
			if nodes[0].Name != "broken" || nodes[0].Error == "" || nodes[1].Snapshot.Node.Hostname != "gpu-node-01" {
				t.Fatalf("nodes: %+v", nodes)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("fleet did not poll: %+v", f.Nodes())
}
