// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package nvml

import (
	"encoding/binary"
	"math"
	"testing"
	"unsafe"
)

// Struct sizes and offsets must match the C ABI on 64-bit platforms. The
// expected values come from nvml.h / cgo -godefs output in go-nvml.
func TestStructLayouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout expectations are for 64-bit platforms")
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"PciInfo", unsafe.Sizeof(PciInfo{}), 68},
		{"PciInfo.BusId offset", unsafe.Offsetof(PciInfo{}.BusId), 36},
		{"Memory", unsafe.Sizeof(Memory{}), 24},
		{"Memory_v2", unsafe.Sizeof(Memory_v2{}), 40},
		{"Memory_v2.Total offset", unsafe.Offsetof(Memory_v2{}.Total), 8},
		{"Temperature", unsafe.Sizeof(Temperature{}), 12},
		{"Utilization", unsafe.Sizeof(Utilization{}), 8},
		{"ProcessInfo", unsafe.Sizeof(ProcessInfo{}), 24},
		{"ProcessInfo.UsedGpuMemory offset", unsafe.Offsetof(ProcessInfo{}.UsedGpuMemory), 8},
		{"ProcessInfo.GpuInstanceId offset", unsafe.Offsetof(ProcessInfo{}.GpuInstanceId), 16},
		{"ProcessUtilizationSample", unsafe.Sizeof(ProcessUtilizationSample{}), 32},
		{"ProcessUtilizationSample.SmUtil offset", unsafe.Offsetof(ProcessUtilizationSample{}.SmUtil), 16},
		{"EventData", unsafe.Sizeof(EventData{}), 32},
		{"FieldValue", unsafe.Sizeof(FieldValue{}), 40},
		{"FieldValue.Value offset", unsafe.Offsetof(FieldValue{}.Value), 32},
		{"ViolationTime", unsafe.Sizeof(ViolationTime{}), 16},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if Memory_v2Version != 0x02000028 {
		t.Errorf("Memory_v2Version = %#x, want 0x02000028", Memory_v2Version)
	}
	if TemperatureVersion != 0x0100000C {
		t.Errorf("TemperatureVersion = %#x, want 0x0100000C", TemperatureVersion)
	}
}

func TestFieldValueDecode(t *testing.T) {
	var f FieldValue
	f.ValueType = VALUE_TYPE_UNSIGNED_LONG_LONG
	binary.LittleEndian.PutUint64(f.Value[:], 123456789)
	if v, ok := f.Uint64(); !ok || v != 123456789 {
		t.Fatalf("Uint64 = %d %v", v, ok)
	}
	f.ValueType = VALUE_TYPE_UNSIGNED_INT
	binary.LittleEndian.PutUint32(f.Value[:], 77)
	if v, ok := f.Uint64(); !ok || v != 77 {
		t.Fatalf("uint = %d %v", v, ok)
	}
	f.ValueType = VALUE_TYPE_DOUBLE
	binary.LittleEndian.PutUint64(f.Value[:], math.Float64bits(42.5))
	if v, ok := f.Float64(); !ok || v != 42.5 {
		t.Fatalf("double = %v %v", v, ok)
	}
	f.ValueType = VALUE_TYPE_SIGNED_INT
	binary.LittleEndian.PutUint32(f.Value[:], uint32(0xFFFFFFFF))
	if _, ok := f.Uint64(); ok {
		t.Fatal("negative signed int must not decode as unsigned")
	}
}

func TestCStringAndErrors(t *testing.T) {
	if got := CString([]byte{'a', 'b', 0, 'c'}); got != "ab" {
		t.Fatalf("CString = %q", got)
	}
	if got := CString([]byte("xyz")); got != "xyz" {
		t.Fatalf("CString unterminated = %q", got)
	}
	if ERROR_GPU_IS_LOST.Error() == "" || Return(12345).Error() == "" {
		t.Fatal("errors must have text")
	}
}
