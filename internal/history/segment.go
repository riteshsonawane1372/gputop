// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/model"
)

// On-disk format
//
//	file   := header record*
//	header := "GPUTOPH1" (8 bytes) | version u16 | reserved u16 | resolution_ms u32
//	record := type u8 | length u32 | crc32(type||payload) u32 | payload
//
// Record types:
//
//	1 frame: ts_ms i64 | nseries u16 | { keylen u8 | key | n u8 | float32*n }*
//	2 meta:  keylen u8 | key | index i32 | namelen u16 | name
//	3 event: JSON-encoded model.Event
//
// All integers are little endian. A torn or corrupted record stops reading
// of that segment; everything before it is kept.
const (
	magic        = "GPUTOPH1"
	fileVersion  = 1
	headerSize   = 16
	recFrame     = 1
	recMeta      = 2
	recEvent     = 3
	maxRecordLen = 16 << 20
	segPrefix    = "seg-"
	segSuffix    = ".gth"
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

type segmentWriter struct {
	f        *os.File
	w        *bufio.Writer
	path     string
	start    time.Time
	size     int64
	metaFor  map[string]bool
	lastSync time.Time
	scratch  []byte
}

func segmentName(start time.Time) string {
	return fmt.Sprintf("%s%d%s", segPrefix, start.UnixMilli(), segSuffix)
}

func createSegment(dir string, start time.Time, resolution time.Duration) (*segmentWriter, error) {
	path := filepath.Join(dir, segmentName(start))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, headerSize)
	copy(hdr, magic)
	binary.LittleEndian.PutUint16(hdr[8:], fileVersion)
	binary.LittleEndian.PutUint32(hdr[12:], uint32(resolution.Milliseconds()))
	if _, err := f.Write(hdr); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &segmentWriter{f: f, w: bufio.NewWriterSize(f, 64<<10), path: path, start: start, size: headerSize,
		metaFor: map[string]bool{}, lastSync: time.Now()}, nil
}

func (s *segmentWriter) record(typ byte, payload []byte) error {
	var hdr [9]byte
	hdr[0] = typ
	binary.LittleEndian.PutUint32(hdr[1:], uint32(len(payload)))
	crc := crc32.Update(0, crcTable, hdr[:1])
	crc = crc32.Update(crc, crcTable, payload)
	binary.LittleEndian.PutUint32(hdr[5:], crc)
	if _, err := s.w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := s.w.Write(payload); err != nil {
		return err
	}
	s.size += int64(len(hdr) + len(payload))
	return nil
}

func (s *segmentWriter) writeMeta(key string, m SeriesMeta) error {
	b := s.scratch[:0]
	b = append(b, byte(len(key)))
	b = append(b, key...)
	b = binary.LittleEndian.AppendUint32(b, uint32(int32(m.Index)))
	name := m.Name
	if len(name) > math.MaxUint16 {
		name = name[:math.MaxUint16]
	}
	b = binary.LittleEndian.AppendUint16(b, uint16(len(name)))
	b = append(b, name...)
	s.scratch = b
	s.metaFor[key] = true
	return s.record(recMeta, b)
}

func (s *segmentWriter) writeFrame(ts int64, keys []string, values [][]float32) error {
	b := s.scratch[:0]
	b = binary.LittleEndian.AppendUint64(b, uint64(ts))
	b = binary.LittleEndian.AppendUint16(b, uint16(len(keys)))
	for i, k := range keys {
		b = append(b, byte(len(k)))
		b = append(b, k...)
		b = append(b, byte(len(values[i])))
		for _, v := range values[i] {
			b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
		}
	}
	s.scratch = b
	return s.record(recFrame, b)
}

func (s *segmentWriter) writeEvent(e model.Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.record(recEvent, b)
}

func (s *segmentWriter) flush(sync bool) error {
	if err := s.w.Flush(); err != nil {
		return err
	}
	if sync {
		s.lastSync = time.Now()
		return s.f.Sync()
	}
	return nil
}

func (s *segmentWriter) close() error {
	err := s.flush(true)
	if cerr := s.f.Close(); err == nil {
		err = cerr
	}
	return err
}

type segmentFile struct {
	path  string
	start time.Time
	size  int64
}

func listSegments(dir string) ([]segmentFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []segmentFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, segPrefix) || !strings.HasSuffix(name, segSuffix) {
			continue
		}
		ms, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, segPrefix), segSuffix), 10, 64)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, segmentFile{path: filepath.Join(dir, name), start: time.UnixMilli(ms), size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start.Before(out[j].start) })
	return out, nil
}

type segmentVisitor struct {
	frame func(ts int64, key string, values []float32)
	meta  func(key string, m SeriesMeta)
	event func(e model.Event)
}

var errCorrupt = errors.New("corrupt record")

// readSegment replays a segment. It returns the number of valid bytes and
// errCorrupt if reading stopped at a damaged record.
func readSegment(path string, v segmentVisitor) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReaderSize(f, 64<<10)
	hdr := make([]byte, headerSize)
	if _, err := io.ReadFull(r, hdr); err != nil || string(hdr[:8]) != magic {
		return 0, errCorrupt
	}
	if binary.LittleEndian.Uint16(hdr[8:]) != fileVersion {
		return headerSize, fmt.Errorf("unsupported history file version %d", binary.LittleEndian.Uint16(hdr[8:]))
	}
	offset := int64(headerSize)
	var rh [9]byte
	var payload []byte
	for {
		if _, err := io.ReadFull(r, rh[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return offset, nil
			}
			return offset, errCorrupt // torn header
		}
		n := binary.LittleEndian.Uint32(rh[1:])
		if n > maxRecordLen {
			return offset, errCorrupt
		}
		if cap(payload) < int(n) {
			payload = make([]byte, n)
		}
		payload = payload[:n]
		if _, err := io.ReadFull(r, payload); err != nil {
			return offset, errCorrupt
		}
		crc := crc32.Update(0, crcTable, rh[:1])
		crc = crc32.Update(crc, crcTable, payload)
		if crc != binary.LittleEndian.Uint32(rh[5:]) {
			return offset, errCorrupt
		}
		if err := decodeRecord(rh[0], payload, v); err != nil {
			return offset, errCorrupt
		}
		offset += int64(len(rh)) + int64(n)
	}
}

func decodeRecord(typ byte, p []byte, v segmentVisitor) error {
	switch typ {
	case recFrame:
		if len(p) < 10 {
			return errCorrupt
		}
		ts := int64(binary.LittleEndian.Uint64(p))
		n := int(binary.LittleEndian.Uint16(p[8:]))
		p = p[10:]
		for i := 0; i < n; i++ {
			if len(p) < 1 {
				return errCorrupt
			}
			kl := int(p[0])
			if len(p) < 2+kl {
				return errCorrupt
			}
			key := string(p[1 : 1+kl])
			cnt := int(p[1+kl])
			p = p[2+kl:]
			if len(p) < 4*cnt {
				return errCorrupt
			}
			vals := make([]float32, cnt)
			for j := range vals {
				vals[j] = math.Float32frombits(binary.LittleEndian.Uint32(p[4*j:]))
			}
			p = p[4*cnt:]
			if v.frame != nil {
				v.frame(ts, key, vals)
			}
		}
	case recMeta:
		if len(p) < 1 {
			return errCorrupt
		}
		kl := int(p[0])
		if len(p) < 1+kl+6 {
			return errCorrupt
		}
		key := string(p[1 : 1+kl])
		idx := int(int32(binary.LittleEndian.Uint32(p[1+kl:])))
		nl := int(binary.LittleEndian.Uint16(p[5+kl:]))
		if len(p) < 7+kl+nl {
			return errCorrupt
		}
		if v.meta != nil {
			v.meta(key, SeriesMeta{Index: idx, Name: string(p[7+kl : 7+kl+nl])})
		}
	case recEvent:
		var e model.Event
		if err := json.Unmarshal(p, &e); err != nil {
			return err
		}
		if v.event != nil {
			v.event(e)
		}
	default:
		// Unknown record types from newer versions are skipped.
	}
	return nil
}
