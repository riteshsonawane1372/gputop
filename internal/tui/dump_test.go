// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/riteshsonawane1372/gputop/internal/collector"
	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/gpu/sim"
	"github.com/riteshsonawane1372/gputop/internal/history"
	"github.com/riteshsonawane1372/gputop/internal/host"
)

// TestDumpFrames writes rendered frames for visual review when
// GPUTOP_DUMP_DIR is set (used to produce README screenshots). The
// simulation runs 60x faster than real time so live charts fill up.
func TestDumpFrames(t *testing.T) {
	dir := os.Getenv("GPUTOP_DUMP_DIR")
	if dir == "" {
		t.Skip("set GPUTOP_DUMP_DIR to dump frames")
	}
	w, _ := strconv.Atoi(os.Getenv("GPUTOP_DUMP_W"))
	h, _ := strconv.Atoi(os.Getenv("GPUTOP_DUMP_H"))
	if w == 0 {
		w, h = 160, 48
	}
	store, _, _ := history.Open(history.Options{Retention: 30 * time.Minute, Resolution: 100 * time.Millisecond})
	e := collector.New(collector.Options{
		Providers: []gpu.Provider{sim.New(sim.Options{GPUs: 8, Speed: 60})},
		Intervals: collector.Intervals{Fast: 25 * time.Millisecond, Normal: 50 * time.Millisecond, Slow: 100 * time.Millisecond, Inventory: time.Minute, Timeout: time.Second},
		Host:      host.NewCollector(), History: store, Demo: true,
	})
	src := &staticSource{store: store}
	sub, cancel := e.Subscribe()
	defer cancel()
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	go func() { _ = e.Run(ctx) }()

	var m *Model
	for {
		select {
		case s := <-sub:
			src.snap = s
			if m == nil && s.Ready {
				m = newTestModel(src, w, h)
			} else if m != nil {
				m.snap = s
				m.live.observe(s)
			}
			continue
		case <-ctx.Done():
		}
		break
	}
	if m == nil {
		t.Fatal("no snapshot")
	}
	m.ensureSelection()
	for _, tab := range m.visibleTabs() {
		m.activeID = tab.id
		if tab.id == "history" {
			if cmd := m.maybeQueryHistory(); cmd != nil {
				m.Update(cmd())
			}
		}
		frame := m.View()
		_ = os.WriteFile(filepath.Join(dir, tab.id+".ansi"), []byte(frame), 0o644)
		_ = os.WriteFile(filepath.Join(dir, tab.id+".txt"), []byte(ansi.Strip(frame)), 0o644)
	}
	m.activeID, m.gpus.detail = "gpus", true
	_ = os.WriteFile(filepath.Join(dir, "gpu-detail.txt"), []byte(ansi.Strip(m.View())), 0o644)
}
