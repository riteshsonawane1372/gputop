// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package apple

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Bindings to CoreFoundation, IOKit and libIOReport, loaded at runtime with
// purego so the binary stays cgo-free. Only the handful of calls gputop needs
// are declared. CF objects are passed around as uintptr handles.

const (
	cfStringEncodingUTF8 = 0x08000100
	cfNumberSInt32Type   = 3
	cfNumberSInt64Type   = 4

	ioHIDEventTypeTemperature = 15
)

var (
	loadOnce sync.Once
	loadErr  error

	// IOReport and the HID event system are optional: power, frequency and
	// temperature degrade to N/A when they are missing.
	haveIOReport bool
	haveHID      bool

	cfRelease                 func(uintptr)
	cfGetTypeID               func(uintptr) uintptr
	cfStringGetTypeID         func() uintptr
	cfNumberGetTypeID         func() uintptr
	cfArrayGetTypeID          func() uintptr
	cfDataGetTypeID           func() uintptr
	cfStringCreateWithCString func(alloc uintptr, s string, enc uint32) uintptr
	cfStringGetCString        func(s uintptr, buf *byte, size int, enc uint32) bool
	cfStringGetLength         func(s uintptr) int
	cfNumberGetValue          func(n uintptr, typ int, out unsafe.Pointer) bool
	cfNumberCreate            func(alloc uintptr, typ int, val unsafe.Pointer) uintptr
	cfDictionaryGetValue      func(d, key uintptr) uintptr
	cfDictionaryCreateMutable func(alloc uintptr, capacity int, keyCB, valueCB uintptr) uintptr
	cfDictionarySetValue      func(d, key, value uintptr)
	cfArrayGetCount           func(a uintptr) int
	cfArrayGetValueAtIndex    func(a uintptr, i int) uintptr
	cfDataGetLength           func(d uintptr) int
	cfDataGetBytePtr          func(d uintptr) unsafe.Pointer

	cfTypeDictionaryKeyCallBacks   uintptr
	cfTypeDictionaryValueCallBacks uintptr

	ioServiceMatching                 func(name string) uintptr
	ioServiceGetMatchingServices      func(port uint32, matching uintptr, iter *uint32) int32
	ioIteratorNext                    func(iter uint32) uint32
	ioObjectRelease                   func(obj uint32) int32
	ioRegistryEntryCreateCFProperties func(entry uint32, props *uintptr, alloc uintptr, opts uint32) int32
	ioRegistryEntryGetName            func(entry uint32, name *byte) int32
	ioRegistryEntryGetChildIterator   func(entry uint32, plane string, iter *uint32) int32
	ioRegistryEntryGetRegistryEntryID func(entry uint32, id *uint64) int32
	ioRegistryEntryCreateCFProperty   func(entry uint32, key uintptr, alloc uintptr, opts uint32) uintptr
	ioServiceOpen                     func(service uint32, task uint32, typ uint32, conn *uint32) int32
	ioServiceClose                    func(conn uint32) int32
	ioConnectCallStructMethod         func(conn, selector uint32, in unsafe.Pointer, inSize uintptr, out unsafe.Pointer, outSize *uintptr) int32

	ioObjectRetain func(obj uint32) int32

	// machTaskSelf is the task port (libSystem's mach_task_self_), 0 if unknown.
	machTaskSelf uint32

	ioReportCopyChannelsInGroup   func(group, subgroup uintptr, a, b, c uint64) uintptr
	ioReportMergeChannels         func(a, b, unused uintptr)
	ioReportCreateSubscription    func(unused, desired uintptr, subbed *uintptr, id uint64, unused2 uintptr) uintptr
	ioReportCreateSamples         func(sub, subbed, unused uintptr) uintptr
	ioReportCreateSamplesDelta    func(prev, cur, unused uintptr) uintptr
	ioReportChannelGetGroup       func(ch uintptr) uintptr
	ioReportChannelGetSubGroup    func(ch uintptr) uintptr
	ioReportChannelGetChannelName func(ch uintptr) uintptr
	ioReportChannelGetUnitLabel   func(ch uintptr) uintptr
	ioReportSimpleGetIntegerValue func(ch uintptr, unused int32) int64
	ioReportStateGetCount         func(ch uintptr) int32
	ioReportStateGetNameForIndex  func(ch uintptr, i int32) uintptr
	ioReportStateGetResidency     func(ch uintptr, i int32) int64

	ioHIDEventSystemClientCreate       func(alloc uintptr) uintptr
	ioHIDEventSystemClientSetMatching  func(client, matching uintptr) int32
	ioHIDEventSystemClientCopyServices func(client uintptr) uintptr
	ioHIDServiceClientCopyProperty     func(svc, key uintptr) uintptr
	ioHIDServiceClientCopyEvent        func(svc uintptr, typ int64, a int32, b int64) uintptr
	ioHIDEventGetFloatValue            func(event uintptr, field int32) float64
)

const (
	cfPath        = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	ioKitPath     = "/System/Library/Frameworks/IOKit.framework/IOKit"
	ioReportPath  = "/usr/lib/libIOReport.dylib"
	libSystemPath = "/usr/lib/libSystem.B.dylib"
)

// register binds fn to a symbol, converting purego's panic on a missing
// symbol into an error.
func register(fn any, lib uintptr, name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: %v", name, r)
		}
	}()
	if _, err := purego.Dlsym(lib, name); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	purego.RegisterLibFunc(fn, lib, name)
	return nil
}

type binding struct {
	fn   any
	name string
}

func registerAll(lib uintptr, bs []binding) error {
	for _, b := range bs {
		if err := register(b.fn, lib, b.name); err != nil {
			return err
		}
	}
	return nil
}

func loadFrameworks() error {
	loadOnce.Do(func() { loadErr = doLoad() })
	return loadErr
}

func doLoad() error {
	cf, err := purego.Dlopen(cfPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("CoreFoundation: %w", err)
	}
	iokit, err := purego.Dlopen(ioKitPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("IOKit: %w", err)
	}
	if err := registerAll(cf, []binding{
		{&cfRelease, "CFRelease"},
		{&cfGetTypeID, "CFGetTypeID"},
		{&cfStringGetTypeID, "CFStringGetTypeID"},
		{&cfNumberGetTypeID, "CFNumberGetTypeID"},
		{&cfArrayGetTypeID, "CFArrayGetTypeID"},
		{&cfDataGetTypeID, "CFDataGetTypeID"},
		{&cfStringCreateWithCString, "CFStringCreateWithCString"},
		{&cfStringGetCString, "CFStringGetCString"},
		{&cfStringGetLength, "CFStringGetLength"},
		{&cfNumberGetValue, "CFNumberGetValue"},
		{&cfNumberCreate, "CFNumberCreate"},
		{&cfDictionaryGetValue, "CFDictionaryGetValue"},
		{&cfDictionaryCreateMutable, "CFDictionaryCreateMutable"},
		{&cfDictionarySetValue, "CFDictionarySetValue"},
		{&cfArrayGetCount, "CFArrayGetCount"},
		{&cfArrayGetValueAtIndex, "CFArrayGetValueAtIndex"},
		{&cfDataGetLength, "CFDataGetLength"},
		{&cfDataGetBytePtr, "CFDataGetBytePtr"},
	}); err != nil {
		return err
	}
	if cfTypeDictionaryKeyCallBacks, err = purego.Dlsym(cf, "kCFTypeDictionaryKeyCallBacks"); err != nil {
		return err
	}
	if cfTypeDictionaryValueCallBacks, err = purego.Dlsym(cf, "kCFTypeDictionaryValueCallBacks"); err != nil {
		return err
	}
	if err := registerAll(iokit, []binding{
		{&ioServiceMatching, "IOServiceMatching"},
		{&ioServiceGetMatchingServices, "IOServiceGetMatchingServices"},
		{&ioIteratorNext, "IOIteratorNext"},
		{&ioObjectRelease, "IOObjectRelease"},
		{&ioObjectRetain, "IOObjectRetain"},
		{&ioRegistryEntryCreateCFProperties, "IORegistryEntryCreateCFProperties"},
		{&ioRegistryEntryGetName, "IORegistryEntryGetName"},
		{&ioRegistryEntryGetChildIterator, "IORegistryEntryGetChildIterator"},
		{&ioRegistryEntryGetRegistryEntryID, "IORegistryEntryGetRegistryEntryID"},
		{&ioRegistryEntryCreateCFProperty, "IORegistryEntryCreateCFProperty"},
		{&ioServiceOpen, "IOServiceOpen"},
		{&ioServiceClose, "IOServiceClose"},
		{&ioConnectCallStructMethod, "IOConnectCallStructMethod"},
	}); err != nil {
		return err
	}
	if sys, err := purego.Dlopen(libSystemPath, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		if addr, err := purego.Dlsym(sys, "mach_task_self_"); err == nil && addr != 0 {
			machTaskSelf = **(**uint32)(unsafe.Pointer(&addr))
		}
	}
	haveHID = registerAll(iokit, []binding{
		{&ioHIDEventSystemClientCreate, "IOHIDEventSystemClientCreate"},
		{&ioHIDEventSystemClientSetMatching, "IOHIDEventSystemClientSetMatching"},
		{&ioHIDEventSystemClientCopyServices, "IOHIDEventSystemClientCopyServices"},
		{&ioHIDServiceClientCopyProperty, "IOHIDServiceClientCopyProperty"},
		{&ioHIDServiceClientCopyEvent, "IOHIDServiceClientCopyEvent"},
		{&ioHIDEventGetFloatValue, "IOHIDEventGetFloatValue"},
	}) == nil

	if rep, err := purego.Dlopen(ioReportPath, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
		haveIOReport = registerAll(rep, []binding{
			{&ioReportCopyChannelsInGroup, "IOReportCopyChannelsInGroup"},
			{&ioReportMergeChannels, "IOReportMergeChannels"},
			{&ioReportCreateSubscription, "IOReportCreateSubscription"},
			{&ioReportCreateSamples, "IOReportCreateSamples"},
			{&ioReportCreateSamplesDelta, "IOReportCreateSamplesDelta"},
			{&ioReportChannelGetGroup, "IOReportChannelGetGroup"},
			{&ioReportChannelGetSubGroup, "IOReportChannelGetSubGroup"},
			{&ioReportChannelGetChannelName, "IOReportChannelGetChannelName"},
			{&ioReportChannelGetUnitLabel, "IOReportChannelGetUnitLabel"},
			{&ioReportSimpleGetIntegerValue, "IOReportSimpleGetIntegerValue"},
			{&ioReportStateGetCount, "IOReportStateGetCount"},
			{&ioReportStateGetNameForIndex, "IOReportStateGetNameForIndex"},
			{&ioReportStateGetResidency, "IOReportStateGetResidency"},
		}) == nil
	}
	return nil
}

// cfstr creates a CFString the caller must release.
func cfstr(s string) uintptr {
	return cfStringCreateWithCString(0, s, cfStringEncodingUTF8)
}

// goString converts a CFString (borrowed) to a Go string.
func goString(ref uintptr) string {
	if ref == 0 || cfGetTypeID(ref) != cfStringGetTypeID() {
		return ""
	}
	// UTF-16 length; UTF-8 needs at most 4 bytes per unit, plus NUL.
	n := cfStringGetLength(ref)*4 + 1
	buf := make([]byte, n)
	if !cfStringGetCString(ref, &buf[0], n, cfStringEncodingUTF8) {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

// dictGet looks up a string key in a CFDictionary (borrowed result).
func dictGet(dict uintptr, key string) uintptr {
	if dict == 0 {
		return 0
	}
	k := cfstr(key)
	defer cfRelease(k)
	return cfDictionaryGetValue(dict, k)
}

func isType(ref uintptr, typeID func() uintptr) bool {
	return ref != 0 && cfGetTypeID(ref) == typeID()
}

// dictInt reads an integer value from a CFDictionary.
func dictInt(dict uintptr, key string) (int64, bool) {
	v := dictGet(dict, key)
	if !isType(v, cfNumberGetTypeID) {
		return 0, false
	}
	var out int64
	if !cfNumberGetValue(v, cfNumberSInt64Type, unsafe.Pointer(&out)) {
		return 0, false
	}
	return out, true
}

func dictString(dict uintptr, key string) string {
	return goString(dictGet(dict, key))
}

// dictBytes copies a CFData value out of a CFDictionary.
func dictBytes(dict uintptr, key string) []byte {
	v := dictGet(dict, key)
	if !isType(v, cfDataGetTypeID) {
		return nil
	}
	n := cfDataGetLength(v)
	p := cfDataGetBytePtr(v)
	if n <= 0 || p == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice((*byte)(p), n)...)
}

// arrayEach calls fn for every element of a CFArray (borrowed elements).
func arrayEach(arr uintptr, fn func(uintptr)) {
	if !isType(arr, cfArrayGetTypeID) {
		return
	}
	n := cfArrayGetCount(arr)
	for i := 0; i < n; i++ {
		fn(cfArrayGetValueAtIndex(arr, i))
	}
}

// eachService calls fn for every registry entry matching an IOKit class. The
// entry is released after fn returns.
func eachService(class string, fn func(entry uint32) bool) {
	var iter uint32
	// IOServiceGetMatchingServices consumes the matching dictionary.
	if ioServiceGetMatchingServices(0, ioServiceMatching(class), &iter) != 0 {
		return
	}
	defer ioObjectRelease(iter)
	for {
		e := ioIteratorNext(iter)
		if e == 0 {
			return
		}
		more := fn(e)
		ioObjectRelease(e)
		if !more {
			return
		}
	}
}

// eachChild calls fn for every child of entry in the IOService plane.
func eachChild(entry uint32, fn func(child uint32)) {
	var iter uint32
	if ioRegistryEntryGetChildIterator(entry, "IOService", &iter) != 0 {
		return
	}
	defer ioObjectRelease(iter)
	for {
		c := ioIteratorNext(iter)
		if c == 0 {
			return
		}
		fn(c)
		ioObjectRelease(c)
	}
}

// property copies one registry property (caller releases).
func property(entry uint32, key string) uintptr {
	k := cfstr(key)
	defer cfRelease(k)
	return ioRegistryEntryCreateCFProperty(entry, k, 0, 0)
}

// properties returns the registry entry's properties (caller releases).
func properties(entry uint32) uintptr {
	var props uintptr
	if ioRegistryEntryCreateCFProperties(entry, &props, 0, 0) != 0 {
		return 0
	}
	return props
}

func entryName(entry uint32) string {
	var buf [128]byte
	if ioRegistryEntryGetName(entry, &buf[0]) != 0 {
		return ""
	}
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf[:])
}

// cfNumber32 creates a CFNumber the caller must release.
func cfNumber32(v int32) uintptr {
	return cfNumberCreate(0, cfNumberSInt32Type, unsafe.Pointer(&v))
}
