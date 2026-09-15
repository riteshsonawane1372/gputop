// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultsAreValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.History.Retention.D() != 30*time.Minute || c.Refresh.Interval.D() != time.Second || c.Theme.Name != "green" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if !strings.HasPrefix(c.Server.Listen, "127.0.0.1:") {
		t.Fatal("service must default to loopback")
	}
}

func TestParseOverridesAndDurations(t *testing.T) {
	c := Default()
	err := Parse([]byte(`
history:
  enabled: true
  retention: 24h
  resolution: 5s
refresh:
  interval: 500ms
  normal: 2
theme:
  name: my-theme
  colors:
    primary: "#ff00ff"
keys:
  quit: [q, x]
  help: "?"
kubernetes:
  enabled: false
remote:
  nodes:
    - name: gpu-node-01
      address: gpu-node-01.example.com
      port: 9500
      token_file: ~/.config/gputop/tokens/node01
`), &c)
	if err != nil {
		t.Fatal(err)
	}
	if c.History.Retention.D() != 24*time.Hour || c.Refresh.Interval.D() != 500*time.Millisecond || c.Refresh.Normal.D() != 2*time.Second {
		t.Fatalf("durations: %+v %+v", c.History, c.Refresh)
	}
	if len(c.Keys["quit"]) != 2 || c.Keys["help"][0] != "?" || c.Kubernetes.Enabled != False {
		t.Fatalf("keys/kube: %+v %v", c.Keys, c.Kubernetes.Enabled)
	}
	u, err := c.Remote.Nodes[0].URL()
	if err != nil || u.String() != "https://gpu-node-01.example.com:9500" {
		t.Fatalf("remote url %v %v", u, err)
	}
	// Unset fields keep their defaults.
	if c.GPU.IdleThreshold != 5 || !c.Prometheus.Enabled {
		t.Fatal("defaults must survive partial config")
	}
	if d, _ := parseDuration("7d"); d != 7*24*time.Hour {
		t.Fatal("day unit")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"unknown key":        "histroy:\n  enabled: true\n",
		"bad duration":       "history:\n  retention: soon\n",
		"too many points":    "history:\n  retention: 7d\n  resolution: 1s\n",
		"retention too low":  "history:\n  retention: 10s\n",
		"interval too fast":  "refresh:\n  interval: 10ms\n",
		"tier order":         "refresh:\n  interval: 5s\n  normal: 1s\n",
		"bad theme name":     "theme:\n  name: ../../etc\n",
		"bad provider":       "gpu:\n  providers: [amd]\n",
		"plaintext remote":   "remote:\n  nodes:\n    - name: a\n      address: http://gpu.example.com\n",
		"duplicate remote":   "remote:\n  nodes:\n    - {name: a, address: x.example.com}\n    - {name: a, address: y.example.com}\n",
		"half tls":           "server:\n  tls_cert_file: cert.pem\n",
		"bad token digest":   "server:\n  token_sha256: abc\n",
		"bad autobool":       "kubernetes:\n  enabled: maybe\n",
		"bad temp unit":      "ui:\n  temperature_unit: k\n",
		"token file and env": "remote:\n  nodes:\n    - {name: a, address: a.example.com, token_file: f, token_env: E}\n",
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			c := Default()
			if err := Parse([]byte(doc), &c); err == nil {
				t.Fatalf("expected error for %q", doc)
			}
		})
	}
	c := Default()
	err := Parse([]byte("refresh:\n  interval: 10ms\ngpu:\n  idle_threshold: 200\n"), &c)
	var ve *ValidationError
	if err == nil || !strings.Contains(err.Error(), "refresh.interval") || !strings.Contains(err.Error(), "idle_threshold") {
		t.Fatalf("all problems must be reported: %v", err)
	}
	_ = ve
}

func TestLoadFileBehaviour(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yaml"), false); err != nil {
		t.Fatalf("missing default config is fine: %v", err)
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml"), true); err == nil {
		t.Fatal("missing explicit config must error")
	}
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, []byte("theme:\n  name: amber\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, true)
	if err != nil || c.Theme.Name != "amber" {
		t.Fatalf("load: %v %+v", err, c.Theme)
	}
	out, err := c.Marshal()
	if err != nil || !strings.Contains(string(out), "retention: 30m") {
		t.Fatalf("marshal: %v\n%s", err, out)
	}
	back := Default()
	if err := Parse(out, &back); err != nil || back.Theme.Name != "amber" {
		t.Fatalf("print-config output must round-trip: %v", err)
	}
}
