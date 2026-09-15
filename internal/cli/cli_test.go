// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gputop/gputop/internal/model"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out, errb bytes.Buffer
	code := Main(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestVersionAndHelp(t *testing.T) {
	code, out, _ := run(t, "--version")
	if code != 0 || !strings.HasPrefix(out, "gputop ") {
		t.Fatalf("version: %d %q", code, out)
	}
	code, _, errOut := run(t, "--help")
	if code != 0 || !strings.Contains(errOut, "--once") {
		t.Fatalf("help: %d", code)
	}
	if code, _, _ := run(t, "--bogus"); code != 2 {
		t.Fatalf("unknown flag exit %d", code)
	}
	if code, _, _ := run(t, "extra-arg"); code != 2 {
		t.Fatal("positional args are rejected")
	}
}

func TestPrintConfigAppliesFlags(t *testing.T) {
	code, out, errOut := run(t, "--print-config", "--theme", "amber", "--retention", "2h", "--no-history")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	for _, want := range []string{"name: amber", "retention: 2h", "enabled: false"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestInvalidConfigFails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(p, []byte("refresh:\n  interval: 1ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, "--config", p, "--once")
	if code != 1 || !strings.Contains(errOut, "refresh.interval") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if code, _, _ := run(t, "--config", "/nonexistent/gputop.yaml", "--once"); code != 1 {
		t.Fatal("missing explicit config must fail")
	}
}

func TestOnceJSONDemo(t *testing.T) {
	code, out, errOut := run(t, "--demo", "--demo-gpus", "4", "--once", "--json")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	var s model.Snapshot
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if s.Schema != model.SchemaVersion || !s.Ready || len(s.GPUs) != 4 || !s.Node.Demo {
		t.Fatalf("snapshot: schema=%s ready=%v gpus=%d demo=%v", s.Schema, s.Ready, len(s.GPUs), s.Node.Demo)
	}
	// Stable schema: key fields must be present by name.
	var raw map[string]any
	_ = json.Unmarshal([]byte(out), &raw)
	for _, k := range []string{"schema", "time", "node", "providers", "gpus", "processes", "fleet", "events", "alerts", "collectors", "history", "self", "kubernetes"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("JSON schema missing top-level key %q", k)
		}
	}
	g := raw["gpus"].([]any)[0].(map[string]any)
	for _, k := range []string{"device", "sample", "health_counters", "health", "derived", "available"} {
		if _, ok := g[k]; !ok {
			t.Errorf("gpu object missing %q", k)
		}
	}
	if smp := g["sample"].(map[string]any); smp["fan_percent"] != nil {
		t.Error("unsupported metrics must encode as null, not 0")
	}
}

func TestOnceTextDemoAndNoGPU(t *testing.T) {
	code, out, _ := run(t, "--demo", "--demo-gpus", "2", "--once")
	if code != 0 || !strings.Contains(out, "DEMO") || !strings.Contains(out, "GPU  NAME") {
		t.Fatalf("text summary: %d\n%s", code, out)
	}
}

func TestGenToken(t *testing.T) {
	code, out, _ := run(t, "--gen-token")
	if code != 0 || !strings.Contains(out, "token_sha256: ") {
		t.Fatalf("%d %s", code, out)
	}
	fields := strings.Fields(out)
	if len(fields[1]) != 64 || len(fields[3]) != 64 {
		t.Fatalf("token lengths: %q %q", fields[1], fields[3])
	}
}

func TestServiceRefusesInsecureListen(t *testing.T) {
	code, _, errOut := run(t, "--demo", "--service", "--listen", "0.0.0.0:0")
	if code != 1 || !strings.Contains(errOut, "refusing") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}
