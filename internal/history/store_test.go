// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var base = time.UnixMilli(1_800_000_000_000)

func snapAt(t time.Time, util float64, ids ...string) *model.Snapshot {
	s := &model.Snapshot{Ready: true, Time: t}
	for i, id := range ids {
		s.GPUs = append(s.GPUs, model.GPU{
			Device:    gpu.Device{ID: gpu.ID(id), Index: i, Name: "Test"},
			Available: true,
			Sample: gpu.Sample{UtilPercent: metric.Some(util), PowerW: metric.Some(100.0),
				Throttle: metric.Some(gpu.ThrottleReasons(1 << uint(int(util)%3)))},
		})
	}
	return s
}

func open(t *testing.T, c *clock, dir string, retention time.Duration) (*Store, []model.Event) {
	t.Helper()
	s, evs, err := Open(Options{Retention: retention, Resolution: 5 * time.Second, Dir: dir, Persist: dir != "", Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	return s, evs
}

func TestBucketAveragingAndMask(t *testing.T) {
	c := &clock{base}
	s, _ := open(t, c, "", 30*time.Minute)
	for i := 0; i < 5; i++ { // one 5s bucket: utils 0..4 (avg 2)
		s.Observe(snapAt(base.Add(time.Duration(i)*time.Second), float64(i), "A"))
	}
	s.Observe(snapAt(base.Add(5*time.Second), 50, "A")) // next bucket commits the first
	r, _ := s.Query(context.Background(), Query{Keys: []string{"A"}, Metrics: []Metric{Util, ThrottleMask}})
	if len(r.Times) != 1 || r.Times[0] != base.UnixMilli()+5000 {
		t.Fatalf("times: %v", r.Times)
	}
	if u := r.Column("A", Util)[0]; u != 2 {
		t.Fatalf("avg util = %v", u)
	}
	if m := r.Column("A", ThrottleMask)[0]; m != 7 { // bits 1,2,4 OR-ed
		t.Fatalf("mask = %v", m)
	}
	if st := s.Status(); st.Points != 1 || st.Persistent {
		t.Fatalf("status: %+v", st)
	}
}

func TestRingRetentionAndSeriesRolloff(t *testing.T) {
	c := &clock{base}
	s, _ := open(t, c, "", time.Minute) // capacity 13
	for i := 0; i < 100; i++ {
		ids := []string{"A"}
		if i < 10 {
			ids = append(ids, "B")
		}
		s.Observe(snapAt(base.Add(time.Duration(i)*5*time.Second), float64(i), ids...))
	}
	r, _ := s.Query(context.Background(), Query{})
	if len(r.Times) != 13 {
		t.Fatalf("points = %d", len(r.Times))
	}
	if len(r.Series) != 1 || r.Series[0].Key != "A" {
		t.Fatalf("B must roll off once all its slots expired: %+v", r.Series)
	}
	col := r.Column("A", Util)
	if col[len(col)-1] != 98 {
		t.Fatalf("latest = %v", col[len(col)-1])
	}
	// Downsampling.
	r2, _ := s.Query(context.Background(), Query{Keys: []string{"A"}, Metrics: []Metric{Util}, MaxPoints: 4})
	if len(r2.Times) != 4 || r2.Times[3] != r.Times[12] {
		t.Fatalf("downsample: %v", r2.Times)
	}
}

func TestMissingSeriesSlotsAreNaN(t *testing.T) {
	c := &clock{base}
	s, _ := open(t, c, "", 10*time.Minute)
	s.Observe(snapAt(base, 10, "A", "B"))
	s.Observe(snapAt(base.Add(5*time.Second), 20, "A"))
	s.Observe(snapAt(base.Add(10*time.Second), 30, "A"))
	r, _ := s.Query(context.Background(), Query{Metrics: []Metric{Util}})
	b := r.Column("B", Util)
	if len(b) != 2 || b[0] != 10 || !math.IsNaN(float64(b[1])) {
		t.Fatalf("B = %v", b)
	}
}

func TestPersistenceRoundTripAndEvents(t *testing.T) {
	dir := t.TempDir()
	c := &clock{base}
	s, evs := open(t, c, dir, 30*time.Minute)
	if len(evs) != 0 {
		t.Fatal("fresh store has no events")
	}
	for i := 0; i < 20; i++ {
		c.t = base.Add(time.Duration(i) * 5 * time.Second)
		s.Observe(snapAt(c.t, float64(i), "GPU-A"))
	}
	s.AppendEvents([]model.Event{{Time: base.Add(time.Minute), Kind: "xid", Message: "Xid 13", Severity: model.SevWarning}})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	c.t = base.Add(3 * time.Minute)
	s2, evs := open(t, c, dir, 30*time.Minute)
	defer s2.Close()
	if len(evs) != 1 || evs[0].Message != "Xid 13" {
		t.Fatalf("events: %+v", evs)
	}
	r, _ := s2.Query(context.Background(), Query{Keys: []string{"GPU-A"}, Metrics: []Metric{Util}})
	if len(r.Times) != 20 { // 19 committed + 1 flushed on Close
		t.Fatalf("restored points = %d", len(r.Times))
	}
	if r.Series[0].Meta.Name != "Test" {
		t.Fatalf("meta not restored: %+v", r.Series[0].Meta)
	}
	if st := s2.Status(); !st.Persistent || st.DiskBytes == 0 || st.Error != "" {
		t.Fatalf("status: %+v", st)
	}
}

func TestCorruptTailIsTolerated(t *testing.T) {
	dir := t.TempDir()
	c := &clock{base}
	s, _ := open(t, c, dir, 30*time.Minute)
	for i := 0; i < 10; i++ {
		c.t = base.Add(time.Duration(i) * 5 * time.Second)
		s.Observe(snapAt(c.t, float64(i), "A"))
	}
	s.Close()
	segs, _ := listSegments(dir)
	f, _ := os.OpenFile(segs[0].path, os.O_APPEND|os.O_WRONLY, 0)
	f.Write([]byte{recFrame, 0xff, 0xff, 0, 0, 1, 2}) // torn record
	f.Close()
	// A garbage file with a valid name must not break loading.
	os.WriteFile(filepath.Join(dir, segmentName(base.Add(-time.Second))), []byte("garbage"), 0o600)

	c.t = base.Add(time.Minute)
	s2, _ := open(t, c, dir, 30*time.Minute)
	defer s2.Close()
	r, _ := s2.Query(context.Background(), Query{Keys: []string{"A"}})
	if len(r.Times) != 10 {
		t.Fatalf("points after corruption = %d", len(r.Times))
	}
}

func TestRetentionCleanupAndLock(t *testing.T) {
	dir := t.TempDir()
	c := &clock{base}
	s, _ := open(t, c, dir, 5*time.Minute) // segment span 1m
	for i := 0; i < 12*20; i++ {           // 20 minutes of data
		c.t = base.Add(time.Duration(i) * 5 * time.Second)
		s.Observe(snapAt(c.t, 1, "A"))
	}
	segs, _ := listSegments(dir)
	if len(segs) > 8 {
		t.Fatalf("expected old segments to be deleted, have %d", len(segs))
	}
	if segs[0].start.Before(c.t.Add(-7 * time.Minute)) {
		t.Fatalf("oldest segment %s too old (now %s)", segs[0].start, c.t)
	}

	// A second store on the same directory falls back to memory-only.
	s2, _ := open(t, c, dir, 5*time.Minute)
	st := s2.Status()
	if st.Persistent || !strings.Contains(st.Error, "in use") {
		t.Fatalf("locked dir status: %+v", st)
	}
	s2.Close()
	s.Close()
}

func TestDiskCap(t *testing.T) {
	dir := t.TempDir()
	c := &clock{base}
	s, _, err := Open(Options{Retention: 24 * time.Hour, Resolution: 5 * time.Second, Dir: dir, Persist: true, Now: c.now, MaxDiskBytes: 20_000})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12*60*6; i++ { // 6h, segment span 1h
		c.t = base.Add(time.Duration(i) * 5 * time.Second)
		s.Observe(snapAt(c.t, 1, "GPU-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "GPU-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"))
	}
	defer s.Close()
	segs, _ := listSegments(dir)
	var total int64
	for _, sg := range segs[:len(segs)-1] {
		total += sg.size
	}
	if total > 20_000 {
		t.Fatalf("closed segments use %d bytes, cap 20000", total)
	}
}

func TestColumnJSON(t *testing.T) {
	c := Column{1.5, nan, 3}
	b, err := json.Marshal(c)
	if err != nil || string(b) != "[1.5,null,3]" {
		t.Fatalf("%s %v", b, err)
	}
	var back Column
	if err := json.Unmarshal(b, &back); err != nil || back[0] != 1.5 || !math.IsNaN(float64(back[1])) {
		t.Fatalf("%v %v", back, err)
	}
}

func TestInvalidOptions(t *testing.T) {
	if _, _, err := Open(Options{Retention: time.Second, Resolution: 5 * time.Second}); err == nil {
		t.Fatal("retention < resolution must fail")
	}
}
