// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package nvml is a minimal, dependency-light binding to the NVIDIA
// Management Library (libnvidia-ml).
//
// It loads the library at runtime with dlopen (via purego) so gputop builds
// with CGO_ENABLED=0 for every platform and runs on machines without NVIDIA
// drivers. Only the functions gputop needs are bound. Every constant and
// struct layout in this file is taken from nvml.h (as vendored by
// github.com/NVIDIA/go-nvml v0.13.4) and the cgo-generated Go layouts in that
// project; see layout_test.go.
package nvml

import (
	"fmt"
	"unsafe"
)

// Return is nvmlReturn_t.
type Return int32

// nvmlReturn_t values.
const (
	SUCCESS                         Return = 0
	ERROR_UNINITIALIZED             Return = 1
	ERROR_INVALID_ARGUMENT          Return = 2
	ERROR_NOT_SUPPORTED             Return = 3
	ERROR_NO_PERMISSION             Return = 4
	ERROR_ALREADY_INITIALIZED       Return = 5
	ERROR_NOT_FOUND                 Return = 6
	ERROR_INSUFFICIENT_SIZE         Return = 7
	ERROR_INSUFFICIENT_POWER        Return = 8
	ERROR_DRIVER_NOT_LOADED         Return = 9
	ERROR_TIMEOUT                   Return = 10
	ERROR_IRQ_ISSUE                 Return = 11
	ERROR_LIBRARY_NOT_FOUND         Return = 12
	ERROR_FUNCTION_NOT_FOUND        Return = 13
	ERROR_CORRUPTED_INFOROM         Return = 14
	ERROR_GPU_IS_LOST               Return = 15
	ERROR_RESET_REQUIRED            Return = 16
	ERROR_OPERATING_SYSTEM          Return = 17
	ERROR_LIB_RM_VERSION_MISMATCH   Return = 18
	ERROR_IN_USE                    Return = 19
	ERROR_MEMORY                    Return = 20
	ERROR_NO_DATA                   Return = 21
	ERROR_ARGUMENT_VERSION_MISMATCH Return = 25
	ERROR_DEPRECATED                Return = 26
	ERROR_NOT_READY                 Return = 27
	ERROR_GPU_NOT_FOUND             Return = 28
	ERROR_INVALID_STATE             Return = 29
	ERROR_UNKNOWN                   Return = 999
)

var returnText = map[Return]string{
	SUCCESS:                         "success",
	ERROR_UNINITIALIZED:             "NVML not initialized",
	ERROR_INVALID_ARGUMENT:          "invalid argument",
	ERROR_NOT_SUPPORTED:             "not supported",
	ERROR_NO_PERMISSION:             "insufficient permissions",
	ERROR_ALREADY_INITIALIZED:       "already initialized",
	ERROR_NOT_FOUND:                 "not found",
	ERROR_INSUFFICIENT_SIZE:         "insufficient size",
	ERROR_INSUFFICIENT_POWER:        "insufficient external power",
	ERROR_DRIVER_NOT_LOADED:         "NVIDIA driver is not loaded",
	ERROR_TIMEOUT:                   "timeout",
	ERROR_IRQ_ISSUE:                 "kernel detected an interrupt issue",
	ERROR_LIBRARY_NOT_FOUND:         "NVML shared library not found",
	ERROR_FUNCTION_NOT_FOUND:        "function not implemented by this NVML version",
	ERROR_CORRUPTED_INFOROM:         "infoROM is corrupted",
	ERROR_GPU_IS_LOST:               "GPU has fallen off the bus or is inaccessible",
	ERROR_RESET_REQUIRED:            "GPU requires a reset",
	ERROR_OPERATING_SYSTEM:          "blocked by the operating system / cgroups",
	ERROR_LIB_RM_VERSION_MISMATCH:   "driver/library version mismatch",
	ERROR_IN_USE:                    "GPU is in use",
	ERROR_MEMORY:                    "insufficient memory",
	ERROR_NO_DATA:                   "no data",
	ERROR_ARGUMENT_VERSION_MISMATCH: "struct version mismatch",
	ERROR_DEPRECATED:                "deprecated",
	ERROR_NOT_READY:                 "system not ready",
	ERROR_GPU_NOT_FOUND:             "no GPUs found",
	ERROR_INVALID_STATE:             "invalid state",
	ERROR_UNKNOWN:                   "unknown internal driver error",
}

// Error implements error.
func (r Return) Error() string {
	if s, ok := returnText[r]; ok {
		return "nvml: " + s
	}
	return fmt.Sprintf("nvml: return code %d", int32(r))
}

// OK reports success.
func (r Return) OK() bool { return r == SUCCESS }

// Device is nvmlDevice_t (an opaque pointer).
type Device uintptr

// EventSet is nvmlEventSet_t (an opaque pointer).
type EventSet uintptr

// Buffer sizes.
const (
	DeviceNameBufferSize        = 96 // NVML_DEVICE_NAME_V2_BUFFER_SIZE
	DeviceUUIDBufferSize        = 96 // NVML_DEVICE_UUID_V2_BUFFER_SIZE
	DeviceSerialBufferSize      = 30 // NVML_DEVICE_SERIAL_BUFFER_SIZE
	DeviceVbiosBufferSize       = 32 // NVML_DEVICE_VBIOS_VERSION_BUFFER_SIZE
	DevicePartNumberBufferSize  = 80 // NVML_DEVICE_PART_NUMBER_BUFFER_SIZE
	SystemDriverBufferSize      = 80 // NVML_SYSTEM_DRIVER_VERSION_BUFFER_SIZE
	SystemNVMLVersionBufferSize = 80 // NVML_SYSTEM_NVML_VERSION_BUFFER_SIZE
	NVLinkMaxLinks              = 36 // NVML_NVLINK_MAX_LINKS
)

// ValueNotAvailable is NVML_VALUE_NOT_AVAILABLE for unsigned long long fields.
const ValueNotAvailable = ^uint64(0)

// Enum values.
const (
	FEATURE_DISABLED = 0
	FEATURE_ENABLED  = 1

	TEMPERATURE_GPU = 0

	TEMPERATURE_THRESHOLD_SHUTDOWN = 0
	TEMPERATURE_THRESHOLD_SLOWDOWN = 1
	TEMPERATURE_THRESHOLD_MEM_MAX  = 2
	TEMPERATURE_THRESHOLD_GPU_MAX  = 3

	CLOCK_GRAPHICS = 0
	CLOCK_MEM      = 2

	PCIE_UTIL_TX_BYTES = 0
	PCIE_UTIL_RX_BYTES = 1

	MEMORY_ERROR_TYPE_CORRECTED   = 0
	MEMORY_ERROR_TYPE_UNCORRECTED = 1
	VOLATILE_ECC                  = 0
	AGGREGATE_ECC                 = 1

	PAGE_RETIREMENT_CAUSE_MULTIPLE_SINGLE_BIT_ECC_ERRORS = 0
	PAGE_RETIREMENT_CAUSE_DOUBLE_BIT_ECC_ERROR           = 1

	PERF_POLICY_POWER   = 0
	PERF_POLICY_THERMAL = 1

	NVLINK_ERROR_DL_REPLAY   = 0
	NVLINK_ERROR_DL_RECOVERY = 1
	NVLINK_ERROR_DL_CRC_FLIT = 2
	NVLINK_ERROR_DL_CRC_DATA = 3

	NVLINK_DEVICE_TYPE_GPU     = 0x00
	NVLINK_DEVICE_TYPE_IBMNPU  = 0x01
	NVLINK_DEVICE_TYPE_SWITCH  = 0x02
	NVLINK_DEVICE_TYPE_UNKNOWN = 0xFF

	TOPOLOGY_INTERNAL   = 0
	TOPOLOGY_SINGLE     = 10
	TOPOLOGY_MULTIPLE   = 20
	TOPOLOGY_HOSTBRIDGE = 30
	TOPOLOGY_NODE       = 40
	TOPOLOGY_SYSTEM     = 50

	COMPUTEMODE_DEFAULT           = 0
	COMPUTEMODE_EXCLUSIVE_THREAD  = 1
	COMPUTEMODE_PROHIBITED        = 2
	COMPUTEMODE_EXCLUSIVE_PROCESS = 3

	DEVICE_MIG_DISABLE = 0
	DEVICE_MIG_ENABLE  = 1

	VALUE_TYPE_DOUBLE             = 0
	VALUE_TYPE_UNSIGNED_INT       = 1
	VALUE_TYPE_UNSIGNED_LONG      = 2
	VALUE_TYPE_UNSIGNED_LONG_LONG = 3
	VALUE_TYPE_SIGNED_LONG_LONG   = 4
	VALUE_TYPE_SIGNED_INT         = 5
	VALUE_TYPE_UNSIGNED_SHORT     = 6
)

// Architecture values (nvmlDeviceArchitecture_t).
var Architectures = map[uint32]string{
	2: "Kepler", 3: "Maxwell", 4: "Pascal", 5: "Volta", 6: "Turing",
	7: "Ampere", 8: "Ada Lovelace", 9: "Hopper", 10: "Blackwell",
	11: "DLA", 12: "DLA2", 13: "Rubin", 15: "NPU3",
}

// Brand values (nvmlBrandType_t).
var Brands = map[uint32]string{
	1: "Quadro", 2: "Tesla", 3: "NVS", 4: "GRID", 5: "GeForce", 6: "Titan",
	7: "NVIDIA vApps", 8: "NVIDIA vPC", 9: "NVIDIA vCS", 10: "NVIDIA vWS",
	11: "NVIDIA Cloud Gaming", 12: "Quadro RTX", 13: "NVIDIA RTX", 14: "NVIDIA",
	15: "GeForce RTX", 16: "Titan RTX",
}

// Clock event (throttle) reason bits (nvmlClocksEventReason*).
const (
	ClocksEventReasonGpuIdle                   uint64 = 0x0000000000000001
	ClocksEventReasonApplicationsClocksSetting uint64 = 0x0000000000000002
	ClocksEventReasonSwPowerCap                uint64 = 0x0000000000000004
	ClocksEventReasonHwSlowdown                uint64 = 0x0000000000000008
	ClocksEventReasonSyncBoost                 uint64 = 0x0000000000000010
	ClocksEventReasonSwThermalSlowdown         uint64 = 0x0000000000000020
	ClocksEventReasonHwThermalSlowdown         uint64 = 0x0000000000000040
	ClocksEventReasonHwPowerBrakeSlowdown      uint64 = 0x0000000000000080
	ClocksEventReasonDisplayClockSetting       uint64 = 0x0000000000000100
	ClocksEventReasonBoardLimit                uint64 = 0x0000000000000200
	ClocksEventReasonReliability               uint64 = 0x0000000000000400
)

// Event type bits (nvmlEventType*).
const (
	EventTypeSingleBitEccError   uint64 = 0x0000000000000001
	EventTypeDoubleBitEccError   uint64 = 0x0000000000000002
	EventTypePState              uint64 = 0x0000000000000004
	EventTypeXidCriticalError    uint64 = 0x0000000000000008
	EventTypeClock               uint64 = 0x0000000000000010
	EventTypePowerSourceChange   uint64 = 0x0000000000000080
	EventMigConfigChange         uint64 = 0x0000000000000100
	EventTypeGpuUnavailableError uint64 = 0x0000000000004000
	EventTypeGpuRecoveryAction   uint64 = 0x0000000000008000
)

// Field identifiers used with nvmlDeviceGetFieldValues (NVML_FI_DEV_*).
const (
	FI_DEV_NVLINK_CRC_DATA_ERROR_COUNT_TOTAL = 45
	FI_DEV_NVLINK_REPLAY_ERROR_COUNT_TOTAL   = 52
	FI_DEV_NVLINK_RECOVERY_ERROR_COUNT_TOTAL = 59
	FI_DEV_MEMORY_TEMP                       = 82
	FI_DEV_NVLINK_SPEED_MBPS_COMMON          = 90
	FI_DEV_NVLINK_LINK_COUNT                 = 91
	FI_DEV_PCIE_REPLAY_COUNTER               = 94
	FI_DEV_NVLINK_THROUGHPUT_DATA_TX         = 138
	FI_DEV_NVLINK_THROUGHPUT_DATA_RX         = 139
	FI_DEV_PCIE_COUNT_CORRECTABLE_ERRORS     = 173
	FI_DEV_PCIE_COUNT_NON_FATAL_ERROR        = 179
	FI_DEV_PCIE_COUNT_FATAL_ERROR            = 180
	FI_DEV_PCIE_COUNT_TX_BYTES               = 197
	FI_DEV_PCIE_COUNT_RX_BYTES               = 198
	FI_DEV_GET_GPU_RECOVERY_ACTION           = 230
)

// ScopeAllLinks is the scopeId that aggregates NVLink counters over links.
const ScopeAllLinks = ^uint32(0)

// GPU recovery actions (nvmlDeviceGpuRecoveryAction_t).
var RecoveryActions = map[uint64]string{
	0: "none", 1: "gpu_reset", 2: "node_reboot", 3: "drain_p2p",
	4: "drain_and_reset", 5: "recover_imex_domain", 6: "bus_reset", 7: "system_reboot",
}

// PciInfo is nvmlPciInfo_t (68 bytes).
type PciInfo struct {
	BusIdLegacy    [16]byte
	Domain         uint32
	Bus            uint32
	Device         uint32
	PciDeviceId    uint32
	PciSubSystemId uint32
	BusId          [32]byte
}

// Memory is nvmlMemory_t.
type Memory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// Memory_v2 is nvmlMemory_v2_t.
type Memory_v2 struct {
	Version  uint32
	Total    uint64
	Reserved uint64
	Free     uint64
	Used     uint64
}

// Memory_v2Version is NVML_STRUCT_VERSION(Memory, 2).
var Memory_v2Version = uint32(unsafe.Sizeof(Memory_v2{})) | 2<<24

// Temperature is nvmlTemperature_v1_t.
type Temperature struct {
	Version     uint32
	SensorType  uint32
	Temperature int32
}

// TemperatureVersion is NVML_STRUCT_VERSION(Temperature, 1).
var TemperatureVersion = uint32(unsafe.Sizeof(Temperature{})) | 1<<24

// Utilization is nvmlUtilization_t.
type Utilization struct {
	Gpu    uint32
	Memory uint32
}

// ProcessInfo is nvmlProcessInfo_t (== nvmlProcessInfo_v2_t).
type ProcessInfo struct {
	Pid               uint32
	UsedGpuMemory     uint64
	GpuInstanceId     uint32
	ComputeInstanceId uint32
}

// ProcessUtilizationSample is nvmlProcessUtilizationSample_t.
type ProcessUtilizationSample struct {
	Pid       uint32
	TimeStamp uint64
	SmUtil    uint32
	MemUtil   uint32
	EncUtil   uint32
	DecUtil   uint32
}

// EventData is nvmlEventData_t.
type EventData struct {
	Device            Device
	EventType         uint64
	EventData         uint64
	GpuInstanceId     uint32
	ComputeInstanceId uint32
}

// FieldValue is nvmlFieldValue_t.
type FieldValue struct {
	FieldId     uint32
	ScopeId     uint32
	Timestamp   int64
	LatencyUsec int64
	ValueType   uint32
	NvmlReturn  uint32
	Value       [8]byte
}

// Uint64 decodes the value union as an unsigned integer where possible.
func (f FieldValue) Uint64() (uint64, bool) {
	p := unsafe.Pointer(&f.Value[0])
	switch f.ValueType {
	case VALUE_TYPE_UNSIGNED_INT:
		return uint64(*(*uint32)(p)), true
	case VALUE_TYPE_UNSIGNED_LONG, VALUE_TYPE_UNSIGNED_LONG_LONG:
		return *(*uint64)(p), true
	case VALUE_TYPE_SIGNED_LONG_LONG:
		v := *(*int64)(p)
		return uint64(v), v >= 0
	case VALUE_TYPE_SIGNED_INT:
		v := *(*int32)(p)
		return uint64(v), v >= 0
	case VALUE_TYPE_UNSIGNED_SHORT:
		return uint64(*(*uint16)(p)), true
	case VALUE_TYPE_DOUBLE:
		v := *(*float64)(p)
		return uint64(v), v >= 0
	}
	return 0, false
}

// Float64 decodes the value union as a float.
func (f FieldValue) Float64() (float64, bool) {
	if f.ValueType == VALUE_TYPE_DOUBLE {
		return *(*float64)(unsafe.Pointer(&f.Value[0])), true
	}
	u, ok := f.Uint64()
	if f.ValueType == VALUE_TYPE_SIGNED_LONG_LONG {
		return float64(*(*int64)(unsafe.Pointer(&f.Value[0]))), true
	}
	if f.ValueType == VALUE_TYPE_SIGNED_INT {
		return float64(*(*int32)(unsafe.Pointer(&f.Value[0]))), true
	}
	return float64(u), ok
}

// ViolationTime is nvmlViolationTime_t.
type ViolationTime struct {
	ReferenceTime uint64 // CPU timestamp in microseconds
	ViolationTime uint64 // nanoseconds
}

// CString converts a NUL-terminated C char buffer into a Go string.
func CString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
