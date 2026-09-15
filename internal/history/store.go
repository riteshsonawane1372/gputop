// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package history is gputop's lightweight local time-series store: the
// "GPU time machine".
//
// Samples are averaged into fixed resolution buckets (default 5s) and kept
// in a bounded ring in memory (retention/resolution slots per series). When
// persistence is enabled, every committed bucket is also appended to
// CRC-protected segment files so history survives restarts. Old segments
// are deleted by retention and a disk-usage cap. The store never collects
// anything itself; the collector feeds it snapshots.
package history

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/gputop/gputop/internal/model"
)

// SeriesMeta labels a series (GPU index and name at last observation).
type SeriesMeta struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

// Options configure a store.
type Options struct {
	Retention  time.Duration
	Resolution time.Duration
	// Dir enables persistence when Persist is set.
	Dir          string
	Persist      bool
	MaxDiskBytes int64
	Now          func() time.Time
	Log          *slog.Logger
}

// Store is safe for concurrent use.
type Store struct {
	opts     Options
	resMs    int64
	capacity int

	mu     sync.RWMutex
	times  []int64 // unix ms, ring
	head   int
	size   int
	seq    uint64
	series map[string]*column
	meta   map[string]SeriesMeta

	bucket int64
	acc    map[string]*accum

	lock       *dirLock
	seg        *segmentWriter
	persistErr string
	diskBytes  int64
	writeAvg   time.Duration
	lastEvent  time.Time
	pending    *pendingFrame // replay state, used only during Open
}

type column struct {
	width   int
	values  []float32 // capacity*width
	lastSeq uint64
}

type accum struct {
	sum    []float64
	count  []uint32
	mask   uint32
	maskOK bool
}

// Open creates a store, replaying persisted history within retention. It
// returns events recovered from disk (oldest first). Persistence problems
// (for example a corrupt or locked directory) never fail Open: the store
// falls back to memory-only mode and reports the problem in Status.
func Open(o Options) (*Store, []model.Event, error) {
	if o.Resolution <= 0 || o.Retention < o.Resolution {
		return nil, nil, fmt.Errorf("history: invalid retention %s / resolution %s", o.Retention, o.Resolution)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.MaxDiskBytes <= 0 {
		o.MaxDiskBytes = 256 << 20
	}
	capacity := int(o.Retention/o.Resolution) + 1
	s := &Store{
		opts: o, resMs: o.Resolution.Milliseconds(), capacity: capacity,
		times: make([]int64, capacity), series: map[string]*column{}, meta: map[string]SeriesMeta{},
		acc: map[string]*accum{},
	}
	var events []model.Event
	if o.Persist && o.Dir != "" {
		var err error
		events, err = s.openDisk()
		if err != nil {
			s.persistErr = err.Error()
			o.Log.Warn("history persistence disabled", "dir", o.Dir, "err", err)
			s.closeDisk()
		}
	}
	return s, events, nil
}

func (s *Store) segmentSpan() time.Duration {
	span := s.opts.Retention / 10
	return max(time.Minute, min(time.Hour, span))
}

func (s *Store) openDisk() ([]model.Event, error) {
	if err := os.MkdirAll(s.opts.Dir, 0o700); err != nil {
		return nil, err
	}
	lock, err := lockDir(s.opts.Dir)
	if err != nil {
		return nil, err
	}
	s.lock = lock
	now := s.opts.Now()
	cutoff := now.Add(-s.opts.Retention).UnixMilli()

	segs, err := listSegments(s.opts.Dir)
	if err != nil {
		return nil, err
	}
	var events []model.Event
	for i, seg := range segs {
		end := now
		if i+1 < len(segs) {
			end = segs[i+1].start
		}
		if end.UnixMilli() < cutoff {
			continue // removed by cleanup below
		}
		_, rerr := readSegment(seg.path, segmentVisitor{
			meta: func(key string, m SeriesMeta) { s.meta[key] = m },
			frame: func(ts int64, key string, vals []float32) {
				if ts < cutoff || ts > now.UnixMilli()+s.resMs {
					return
				}
				s.loadFrameValue(ts, key, vals)
			},
			event: func(e model.Event) {
				if e.Time.UnixMilli() >= cutoff {
					events = append(events, e)
				}
			},
		})
		if rerr != nil {
			s.opts.Log.Warn("history segment damaged; kept readable prefix", "file", seg.path, "err", rerr)
		}
	}
	s.flushLoaded()

	seg, err := createSegment(s.opts.Dir, now, s.opts.Resolution)
	if err != nil {
		return nil, err
	}
	s.seg = seg
	s.cleanup(now)
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	if len(events) > 0 {
		s.lastEvent = events[len(events)-1].Time
	}
	return events, nil
}

// loading state: frames arrive series by series; group by timestamp.
type pendingFrame struct {
	ts   int64
	vals map[string][]float32
}

func (s *Store) loadFrameValue(ts int64, key string, vals []float32) {
	if s.pending != nil && s.pending.ts != ts {
		s.flushLoaded()
	}
	if s.pending == nil {
		s.pending = &pendingFrame{ts: ts, vals: map[string][]float32{}}
	}
	s.pending.vals[key] = vals
}

func (s *Store) flushLoaded() {
	if s.pending == nil {
		return
	}
	p := s.pending
	s.pending = nil
	if s.size > 0 && p.ts <= s.times[(s.head-1+s.capacity)%s.capacity] {
		return // non-monotonic (clock change); skip
	}
	s.commitLocked(p.ts, p.vals, false)
}

func (s *Store) closeDisk() {
	if s.seg != nil {
		_ = s.seg.close()
		s.seg = nil
	}
	s.lock.unlock()
	s.lock = nil
}

// Observe feeds a snapshot. Values are averaged into resolution buckets; a
// bucket is committed when the first snapshot of the next bucket arrives.
func (s *Store) Observe(snap *model.Snapshot) {
	if snap == nil || !snap.Ready {
		return
	}
	values := Extract(snap)
	ms := snap.Time.UnixMilli()
	b := ms / s.resMs

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, g := range snap.GPUs {
		m := SeriesMeta{Index: g.Device.Index, Name: g.Device.Name}
		if s.meta[string(g.Device.ID)] != m {
			s.meta[string(g.Device.ID)] = m
			if s.seg != nil {
				delete(s.seg.metaFor, string(g.Device.ID))
			}
		}
	}
	if len(s.acc) > 0 && b != s.bucket {
		s.commitBucketLocked()
	}
	s.bucket = b
	for key, vals := range values {
		a := s.acc[key]
		if a == nil {
			a = &accum{sum: make([]float64, len(vals)), count: make([]uint32, len(vals))}
			s.acc[key] = a
		}
		for i, v := range vals {
			if math.IsNaN(float64(v)) {
				continue
			}
			if key != HostKey && Metric(i) == ThrottleMask {
				a.mask |= uint32(v)
				a.maskOK = true
				continue
			}
			a.sum[i] += float64(v)
			a.count[i]++
		}
	}
}

func (s *Store) commitBucketLocked() {
	ts := (s.bucket + 1) * s.resMs
	frame := make(map[string][]float32, len(s.acc))
	for key, a := range s.acc {
		vals := make([]float32, len(a.sum))
		for i := range vals {
			if a.count[i] > 0 {
				vals[i] = float32(a.sum[i] / float64(a.count[i]))
			} else {
				vals[i] = nan
			}
		}
		if key != HostKey && int(ThrottleMask) < len(vals) {
			if a.maskOK {
				vals[ThrottleMask] = float32(a.mask)
			} else {
				vals[ThrottleMask] = nan
			}
		}
		frame[key] = vals
	}
	s.acc = map[string]*accum{}
	if s.size > 0 && ts <= s.times[(s.head-1+s.capacity)%s.capacity] {
		return
	}
	s.commitLocked(ts, frame, true)
}

func (s *Store) commitLocked(ts int64, frame map[string][]float32, persist bool) {
	idx := s.head
	s.times[idx] = ts
	s.seq++
	for key, vals := range frame {
		c := s.series[key]
		if c == nil {
			w := width(key)
			c = &column{width: w, values: make([]float32, s.capacity*w)}
			for i := range c.values {
				c.values[i] = nan
			}
			s.series[key] = c
		}
		copy(c.values[idx*c.width:(idx+1)*c.width], vals)
		for i := len(vals); i < c.width; i++ {
			c.values[idx*c.width+i] = nan
		}
		c.lastSeq = s.seq
	}
	for key, c := range s.series {
		if c.lastSeq == s.seq {
			continue
		}
		if s.seq-c.lastSeq >= uint64(s.capacity) {
			delete(s.series, key) // every slot has rolled off
			continue
		}
		for i := idx * c.width; i < (idx+1)*c.width; i++ {
			c.values[i] = nan
		}
	}
	s.head = (s.head + 1) % s.capacity
	if s.size < s.capacity {
		s.size++
	}
	if persist && s.seg != nil {
		s.persistFrameLocked(ts, frame)
	}
}

func (s *Store) persistFrameLocked(ts int64, frame map[string][]float32) {
	start := time.Now()
	now := s.opts.Now()
	if now.Sub(s.seg.start) >= s.segmentSpan() {
		if err := s.rotateLocked(now); err != nil {
			s.failPersistLocked(err)
			return
		}
	}
	keys := make([]string, 0, len(frame))
	for k := range frame {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := make([][]float32, len(keys))
	for i, k := range keys {
		vals[i] = frame[k]
		if m, ok := s.meta[k]; ok && !s.seg.metaFor[k] {
			if err := s.seg.writeMeta(k, m); err != nil {
				s.failPersistLocked(err)
				return
			}
		}
	}
	if err := s.seg.writeFrame(ts, keys, vals); err != nil {
		s.failPersistLocked(err)
		return
	}
	// Flush every frame (bounded loss on crash); fsync at most every 30s.
	if err := s.seg.flush(time.Since(s.seg.lastSync) >= 30*time.Second); err != nil {
		s.failPersistLocked(err)
		return
	}
	d := time.Since(start)
	if s.writeAvg == 0 {
		s.writeAvg = d
	} else {
		s.writeAvg = (s.writeAvg*7 + d) / 8
	}
}

func (s *Store) failPersistLocked(err error) {
	s.persistErr = "persistence stopped: " + err.Error()
	s.opts.Log.Warn("history persistence stopped", "err", err)
	s.closeDisk()
}

func (s *Store) rotateLocked(now time.Time) error {
	if err := s.seg.close(); err != nil {
		return err
	}
	seg, err := createSegment(s.opts.Dir, now, s.opts.Resolution)
	if err != nil {
		s.seg = nil
		return err
	}
	s.seg = seg
	s.cleanup(now)
	return nil
}

// cleanup deletes segments past retention and enforces the disk cap.
func (s *Store) cleanup(now time.Time) {
	segs, err := listSegments(s.opts.Dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-s.opts.Retention)
	var kept []segmentFile
	for i, seg := range segs {
		current := s.seg != nil && seg.path == s.seg.path
		end := now
		if i+1 < len(segs) {
			end = segs[i+1].start
		}
		if !current && end.Before(cutoff) {
			_ = os.Remove(seg.path)
			continue
		}
		kept = append(kept, seg)
	}
	var total int64
	for _, k := range kept {
		total += k.size
	}
	for len(kept) > 1 && total > s.opts.MaxDiskBytes {
		if s.seg != nil && kept[0].path == s.seg.path {
			break
		}
		_ = os.Remove(kept[0].path)
		total -= kept[0].size
		kept = kept[1:]
	}
	s.diskBytes = total
}

// AppendEvents persists events (they are also kept by the in-memory event
// log owned by the collector).
func (s *Store) AppendEvents(evs []model.Event) {
	if len(evs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seg == nil {
		return
	}
	for _, e := range evs {
		if err := s.seg.writeEvent(e); err != nil {
			s.failPersistLocked(err)
			return
		}
	}
	if err := s.seg.flush(false); err != nil {
		s.failPersistLocked(err)
	}
}

// Status reports store state.
func (s *Store) Status() model.HistoryStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := model.HistoryStatus{
		Enabled: true, Persistent: s.seg != nil, Retention: s.opts.Retention, Resolution: s.opts.Resolution,
		Points: s.size, WriteAvg: s.writeAvg, Error: s.persistErr, DiskBytes: s.diskBytes,
	}
	if s.seg != nil {
		st.DiskBytes += s.seg.size
	}
	if s.size > 0 {
		st.Oldest = time.UnixMilli(s.times[(s.head-s.size+s.capacity)%s.capacity])
	}
	return st
}

// Close flushes and releases the store.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.acc) > 0 {
		s.commitBucketLocked()
	}
	var err error
	if s.seg != nil {
		err = s.seg.close()
		s.seg = nil
	}
	s.lock.unlock()
	s.lock = nil
	return err
}

// Query selects a time range.
type Query struct {
	// Keys limits the series (GPU IDs or HostKey); empty means all.
	Keys []string
	// Metrics limits the columns; empty means all columns of each series.
	Metrics []Metric
	Since   time.Time
	Until   time.Time // zero means now
	// MaxPoints downsamples by averaging when there are more points.
	MaxPoints int
}

// Column is a series of values; NaN (unavailable) encodes as JSON null.
type Column []float32

// MarshalJSON encodes NaN as null.
func (c Column) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.Grow(len(c) * 6)
	b.WriteByte('[')
	for i, v := range c {
		if i > 0 {
			b.WriteByte(',')
		}
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			b.WriteString("null")
		} else {
			b.WriteString(strconv.FormatFloat(float64(v), 'g', 6, 32))
		}
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

// UnmarshalJSON decodes null as NaN.
func (c *Column) UnmarshalJSON(data []byte) error {
	var raw []*float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make(Column, len(raw))
	for i, v := range raw {
		if v == nil {
			out[i] = nan
		} else {
			out[i] = float32(*v)
		}
	}
	*c = out
	return nil
}

// Series is one key's columns aligned with Result.Times.
type Series struct {
	Key     string            `json:"key"`
	Meta    SeriesMeta        `json:"meta"`
	Columns map[string]Column `json:"columns"` // metric key -> values
}

// Result is a query result.
type Result struct {
	Resolution time.Duration `json:"resolution_ns"`
	Retention  time.Duration `json:"retention_ns"`
	Times      []int64       `json:"times_ms"`
	Series     []Series      `json:"series"`
}

// Column returns values for a series and metric (nil if absent).
func (r *Result) Column(key string, m Metric) Column {
	for _, s := range r.Series {
		if s.Key == key {
			return s.Columns[Describe(m).Key]
		}
	}
	return nil
}

// Reader is implemented by the local store and by remote clients.
type Reader interface {
	Query(ctx context.Context, q Query) (*Result, error)
}

var _ Reader = (*Store)(nil)

// ErrClosed is returned after Close.
var ErrClosed = errors.New("history store closed")

// Query implements Reader.
func (s *Store) Query(_ context.Context, q Query) (*Result, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := &Result{Resolution: s.opts.Resolution, Retention: s.opts.Retention}
	sinceMs, untilMs := q.Since.UnixMilli(), int64(math.MaxInt64)
	if q.Since.IsZero() {
		sinceMs = math.MinInt64
	}
	if !q.Until.IsZero() {
		untilMs = q.Until.UnixMilli()
	}
	var slots []int
	for i := 0; i < s.size; i++ {
		idx := (s.head - s.size + i + s.capacity) % s.capacity
		if t := s.times[idx]; t >= sinceMs && t <= untilMs {
			slots = append(slots, idx)
		}
	}
	groups := groupSlots(len(slots), q.MaxPoints)
	res.Times = make([]int64, len(groups))
	for gi, g := range groups {
		res.Times[gi] = s.times[slots[g[1]-1]]
	}

	keys := q.Keys
	if len(keys) == 0 {
		for k := range s.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	for _, key := range keys {
		c := s.series[key]
		if c == nil {
			continue
		}
		metrics := q.Metrics
		if len(metrics) == 0 {
			for i := 0; i < c.width; i++ {
				metrics = append(metrics, metricAt(key, i))
			}
		}
		ser := Series{Key: key, Meta: s.meta[key], Columns: map[string]Column{}}
		for _, m := range metrics {
			if (key == HostKey) != (m >= HostCPU) {
				continue
			}
			j := slot(m)
			if j >= c.width {
				continue
			}
			col := make(Column, len(groups))
			for gi, g := range groups {
				col[gi] = aggregate(c, slots[g[0]:g[1]], j, m == ThrottleMask && key != HostKey)
			}
			ser.Columns[Describe(m).Key] = col
		}
		res.Series = append(res.Series, ser)
	}
	return res, nil
}

// groupSlots splits n points into at most max contiguous groups.
func groupSlots(n, maxPoints int) [][2]int {
	if maxPoints <= 0 || n <= maxPoints {
		out := make([][2]int, n)
		for i := range out {
			out[i] = [2]int{i, i + 1}
		}
		return out
	}
	out := make([][2]int, maxPoints)
	for i := range out {
		out[i] = [2]int{i * n / maxPoints, (i + 1) * n / maxPoints}
	}
	return out
}

func aggregate(c *column, slots []int, j int, orMask bool) float32 {
	var sum float64
	var n int
	var mask uint32
	for _, idx := range slots {
		v := c.values[idx*c.width+j]
		if math.IsNaN(float64(v)) {
			continue
		}
		if orMask {
			mask |= uint32(v)
		} else {
			sum += float64(v)
		}
		n++
	}
	if n == 0 {
		return nan
	}
	if orMask {
		return float32(mask)
	}
	return float32(sum / float64(n))
}
