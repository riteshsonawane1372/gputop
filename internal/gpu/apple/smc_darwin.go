// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package apple

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"unsafe"
)

// The System Management Controller exposes GPU die temperatures as "Tg??"
// keys of type "flt " on Apple silicon. It is read through the
// AppleSMCKeysEndpoint user client; no privileges are required.

// smcParam mirrors the kernel's 80-byte SMCKeyData_t.
type smcParam struct {
	Key      uint32
	Vers     [6]byte
	_        [2]byte
	PLimit   [16]byte
	DataSize uint32
	DataType uint32
	DataAttr uint8
	_        [3]byte
	Result   uint8
	Status   uint8
	Data8    uint8
	_        uint8
	Data32   uint32
	Bytes    [32]byte
}

const (
	smcSelector       = 2 // kSMCHandleYPCEvent
	smcCmdReadKey     = 5
	smcCmdKeyFromIdx  = 8
	smcCmdGetKeyInfo  = 9
	smcTypeFloat      = 0x666c7420 // "flt "
	smcMaxKeysScanned = 8192
)

var _ = [1]struct{}{}[unsafe.Sizeof(smcParam{})-80] // layout guard

type smcKey struct {
	key  uint32
	size uint32
	typ  uint32
}

type smc struct {
	conn uint32
}

func fourCC(s string) uint32 {
	var b [4]byte
	copy(b[:], s)
	return binary.BigEndian.Uint32(b[:])
}

func fourCCString(v uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return string(b[:])
}

func openSMC() (*smc, error) {
	if machTaskSelf == 0 {
		return nil, errors.New("mach_task_self unavailable")
	}
	task := machTaskSelf
	var conn uint32
	eachService("AppleSMC", func(e uint32) bool {
		if entryName(e) != "AppleSMCKeysEndpoint" {
			return true
		}
		var c uint32
		if ioServiceOpen(e, task, 0, &c) == 0 {
			conn = c
		}
		return false
	})
	if conn == 0 {
		return nil, errors.New("AppleSMCKeysEndpoint not found")
	}
	return &smc{conn: conn}, nil
}

func (s *smc) Close() {
	if s != nil && s.conn != 0 {
		ioServiceClose(s.conn)
		s.conn = 0
	}
}

func (s *smc) call(in *smcParam) (*smcParam, error) {
	var out smcParam
	size := unsafe.Sizeof(out)
	if r := ioConnectCallStructMethod(s.conn, smcSelector, unsafe.Pointer(in), unsafe.Sizeof(*in), unsafe.Pointer(&out), &size); r != 0 {
		return nil, errors.New("smc call failed")
	}
	if out.Result != 0 {
		return nil, errors.New("smc key error")
	}
	return &out, nil
}

func (s *smc) keyInfo(key uint32) (smcKey, error) {
	out, err := s.call(&smcParam{Key: key, Data8: smcCmdGetKeyInfo})
	if err != nil {
		return smcKey{}, err
	}
	return smcKey{key: key, size: out.DataSize, typ: out.DataType}, nil
}

func (s *smc) read(k smcKey) ([]byte, error) {
	out, err := s.call(&smcParam{Key: k.key, DataSize: k.size, Data8: smcCmdReadKey})
	if err != nil {
		return nil, err
	}
	return out.Bytes[:min(int(k.size), len(out.Bytes))], nil
}

// keysWithPrefix enumerates float keys whose name starts with prefix.
func (s *smc) keysWithPrefix(prefix string) []smcKey {
	countKey, err := s.keyInfo(fourCC("#KEY"))
	if err != nil {
		return nil
	}
	b, err := s.read(countKey)
	if err != nil || len(b) < 4 {
		return nil
	}
	n := min(int(binary.BigEndian.Uint32(b)), smcMaxKeysScanned)
	var keys []smcKey
	for i := 0; i < n; i++ {
		out, err := s.call(&smcParam{Data8: smcCmdKeyFromIdx, Data32: uint32(i)})
		if err != nil {
			continue
		}
		if !strings.HasPrefix(fourCCString(out.Key), prefix) {
			continue
		}
		info, err := s.keyInfo(out.Key)
		if err == nil && info.typ == smcTypeFloat && info.size == 4 {
			keys = append(keys, info)
		}
	}
	return keys
}

// readFloat reads a little-endian float32 key.
func (s *smc) readFloat(k smcKey) (float64, bool) {
	b, err := s.read(k)
	if err != nil || len(b) < 4 {
		return 0, false
	}
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), true
}
