// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package nvml

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

type (
	fnVoid       func() Return
	fnStr        func(buf *byte, length uint32) Return
	fnInt        func(v *int32) Return
	fnUint       func(v *uint32) Return
	fnDevStr     func(d Device, buf *byte, length uint32) Return
	fnDevU32     func(d Device, v *uint32) Return
	fnDevI32     func(d Device, v *int32) Return
	fnDevU64     func(d Device, v *uint64) Return
	fnDevEnumU32 func(d Device, e uint32, v *uint32) Return
	fnDevU32U32  func(d Device, a, b *uint32) Return
	fnDevUtilS   func(d Device, util, sampling *uint32) Return
	fnDevProcs   func(d Device, count *uint32, infos *ProcessInfo) Return
)

// lib is the purego-backed NVML implementation. Optional functions that are
// absent from the loaded library remain nil and report
// ERROR_FUNCTION_NOT_FOUND.
type lib struct {
	path   string
	handle uintptr

	init     fnVoid
	shutdown fnVoid

	systemGetDriverVersion         fnStr
	systemGetNVMLVersion           fnStr
	systemGetCudaDriverVersion     fnInt
	deviceGetCount                 fnUint
	deviceGetHandleByIndex         func(index uint32, d *Device) Return
	deviceGetName                  fnDevStr
	deviceGetUUID                  fnDevStr
	deviceGetSerial                fnDevStr
	deviceGetBoardPartNumber       fnDevStr
	deviceGetVbiosVersion          fnDevStr
	deviceGetBrand                 fnDevU32
	deviceGetArchitecture          fnDevU32
	deviceGetCudaComputeCapability func(d Device, major, minor *int32) Return
	deviceGetPersistenceMode       fnDevU32
	deviceGetIndex                 fnDevU32
	deviceGetPciInfo               func(d Device, p *PciInfo) Return
	deviceGetNumaNodeId            fnDevU32
	deviceGetComputeMode           fnDevU32

	deviceGetMemoryInfoV2       func(d Device, m *Memory_v2) Return
	deviceGetMemoryInfo         func(d Device, m *Memory) Return
	deviceGetUtilizationRates   func(d Device, u *Utilization) Return
	deviceGetEncoderUtilization fnDevUtilS
	deviceGetDecoderUtilization fnDevUtilS
	deviceGetJpgUtilization     fnDevUtilS
	deviceGetOfaUtilization     fnDevUtilS

	deviceGetTemperatureV         func(d Device, t *Temperature) Return
	deviceGetTemperature          fnDevEnumU32
	deviceGetTemperatureThreshold fnDevEnumU32
	deviceGetFanSpeed             fnDevU32

	deviceGetPowerUsage                      fnDevU32
	deviceGetEnforcedPowerLimit              fnDevU32
	deviceGetPowerManagementLimitConstraints fnDevU32U32
	deviceGetPowerManagementDefaultLimit     fnDevU32
	deviceGetTotalEnergyConsumption          fnDevU64

	deviceGetClockInfo                    fnDevEnumU32
	deviceGetMaxClockInfo                 fnDevEnumU32
	deviceGetCurrentClocksEventReasons    fnDevU64
	deviceGetCurrentClocksThrottleReasons fnDevU64
	deviceGetPerformanceState             fnDevU32
	deviceGetViolationStatus              func(d Device, policy uint32, v *ViolationTime) Return

	deviceGetCurrPcieLinkGeneration   fnDevU32
	deviceGetCurrPcieLinkWidth        fnDevU32
	deviceGetMaxPcieLinkGeneration    fnDevU32
	deviceGetGpuMaxPcieLinkGeneration fnDevU32
	deviceGetMaxPcieLinkWidth         fnDevU32
	deviceGetPcieThroughput           fnDevEnumU32
	deviceGetPcieReplayCounter        fnDevU32

	deviceGetComputeRunningProcesses  fnDevProcs
	deviceGetGraphicsRunningProcesses fnDevProcs
	deviceGetProcessUtilization       func(d Device, s *ProcessUtilizationSample, count *uint32, lastSeen uint64) Return

	deviceGetEccMode                   fnDevU32U32
	deviceGetTotalEccErrors            func(d Device, errType, counterType uint32, v *uint64) Return
	deviceGetRetiredPages              func(d Device, cause uint32, count *uint32, addrs *uint64) Return
	deviceGetRetiredPagesPendingStatus fnDevU32
	deviceGetRemappedRows              func(d Device, corr, unc, pending, failure *uint32) Return

	deviceGetMigMode                fnDevU32U32
	deviceGetMaxMigDeviceCount      fnDevU32
	deviceGetMigDeviceHandleByIndex func(d Device, index uint32, mig *Device) Return
	deviceGetGpuInstanceId          fnDevU32
	deviceGetComputeInstanceId      fnDevU32

	deviceGetNvLinkState            fnDevEnumU32
	deviceGetNvLinkVersion          fnDevEnumU32
	deviceGetNvLinkRemotePciInfo    func(d Device, link uint32, p *PciInfo) Return
	deviceGetNvLinkRemoteDeviceType fnDevEnumU32
	deviceGetNvLinkErrorCounter     func(d Device, link, counter uint32, v *uint64) Return

	deviceGetFieldValues            func(d Device, count int32, values *FieldValue) Return
	deviceGetTopologyCommonAncestor func(a, b Device, level *uint32) Return

	eventSetCreate               func(set *EventSet) Return
	deviceGetSupportedEventTypes fnDevU64
	deviceRegisterEvents         func(d Device, types uint64, set EventSet) Return
	eventSetWait                 func(set EventSet, data *EventData, timeoutMS uint32) Return
	eventSetFree                 func(set EventSet) Return
}

var (
	loadOnce sync.Once
	loaded   *lib
	loadErr  error
)

// Load opens libnvidia-ml from the first path that succeeds. The library is
// loaded once per process; subsequent calls return the same instance.
func Load(paths []string) (API, error) {
	loadOnce.Do(func() {
		if len(paths) == 0 {
			paths = DefaultLibraryPaths
		}
		var errs []string
		missing := 0
		for _, p := range paths {
			h, err := purego.Dlopen(p, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				if strings.Contains(err.Error(), "No such file") || strings.Contains(err.Error(), "cannot open shared object") {
					missing++
				} else {
					errs = append(errs, fmt.Sprintf("%s: %v", p, err))
				}
				continue
			}
			l := &lib{path: p, handle: h}
			if err := l.bindAll(); err != nil {
				_ = purego.Dlclose(h)
				errs = append(errs, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			loaded = l
			return
		}
		if len(errs) == 0 {
			loadErr = fmt.Errorf("libnvidia-ml.so.1 not found (searched the dynamic linker path and %d standard locations)", missing-1)
			return
		}
		loadErr = errors.New("unable to load libnvidia-ml: " + strings.Join(errs, "; "))
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return loaded, nil
}

func (l *lib) bind(name string, fptr any) bool {
	sym, err := purego.Dlsym(l.handle, name)
	if err != nil || sym == 0 {
		return false
	}
	purego.RegisterFunc(fptr, sym)
	return true
}

func (l *lib) bindAll() error {
	if !l.bind("nvmlInit_v2", &l.init) || !l.bind("nvmlShutdown", &l.shutdown) ||
		!l.bind("nvmlDeviceGetCount_v2", &l.deviceGetCount) ||
		!l.bind("nvmlDeviceGetHandleByIndex_v2", &l.deviceGetHandleByIndex) {
		return errors.New("library is missing required NVML symbols")
	}
	l.bind("nvmlSystemGetDriverVersion", &l.systemGetDriverVersion)
	l.bind("nvmlSystemGetNVMLVersion", &l.systemGetNVMLVersion)
	l.bind("nvmlSystemGetCudaDriverVersion_v2", &l.systemGetCudaDriverVersion)
	l.bind("nvmlDeviceGetName", &l.deviceGetName)
	l.bind("nvmlDeviceGetUUID", &l.deviceGetUUID)
	l.bind("nvmlDeviceGetSerial", &l.deviceGetSerial)
	l.bind("nvmlDeviceGetBoardPartNumber", &l.deviceGetBoardPartNumber)
	l.bind("nvmlDeviceGetVbiosVersion", &l.deviceGetVbiosVersion)
	l.bind("nvmlDeviceGetBrand", &l.deviceGetBrand)
	l.bind("nvmlDeviceGetArchitecture", &l.deviceGetArchitecture)
	l.bind("nvmlDeviceGetCudaComputeCapability", &l.deviceGetCudaComputeCapability)
	l.bind("nvmlDeviceGetPersistenceMode", &l.deviceGetPersistenceMode)
	l.bind("nvmlDeviceGetIndex", &l.deviceGetIndex)
	l.bind("nvmlDeviceGetPciInfo_v3", &l.deviceGetPciInfo)
	l.bind("nvmlDeviceGetNumaNodeId", &l.deviceGetNumaNodeId)
	l.bind("nvmlDeviceGetComputeMode", &l.deviceGetComputeMode)
	l.bind("nvmlDeviceGetMemoryInfo_v2", &l.deviceGetMemoryInfoV2)
	l.bind("nvmlDeviceGetMemoryInfo", &l.deviceGetMemoryInfo)
	l.bind("nvmlDeviceGetUtilizationRates", &l.deviceGetUtilizationRates)
	l.bind("nvmlDeviceGetEncoderUtilization", &l.deviceGetEncoderUtilization)
	l.bind("nvmlDeviceGetDecoderUtilization", &l.deviceGetDecoderUtilization)
	l.bind("nvmlDeviceGetJpgUtilization", &l.deviceGetJpgUtilization)
	l.bind("nvmlDeviceGetOfaUtilization", &l.deviceGetOfaUtilization)
	l.bind("nvmlDeviceGetTemperatureV", &l.deviceGetTemperatureV)
	l.bind("nvmlDeviceGetTemperature", &l.deviceGetTemperature)
	l.bind("nvmlDeviceGetTemperatureThreshold", &l.deviceGetTemperatureThreshold)
	l.bind("nvmlDeviceGetFanSpeed", &l.deviceGetFanSpeed)
	l.bind("nvmlDeviceGetPowerUsage", &l.deviceGetPowerUsage)
	l.bind("nvmlDeviceGetEnforcedPowerLimit", &l.deviceGetEnforcedPowerLimit)
	l.bind("nvmlDeviceGetPowerManagementLimitConstraints", &l.deviceGetPowerManagementLimitConstraints)
	l.bind("nvmlDeviceGetPowerManagementDefaultLimit", &l.deviceGetPowerManagementDefaultLimit)
	l.bind("nvmlDeviceGetTotalEnergyConsumption", &l.deviceGetTotalEnergyConsumption)
	l.bind("nvmlDeviceGetClockInfo", &l.deviceGetClockInfo)
	l.bind("nvmlDeviceGetMaxClockInfo", &l.deviceGetMaxClockInfo)
	l.bind("nvmlDeviceGetCurrentClocksEventReasons", &l.deviceGetCurrentClocksEventReasons)
	l.bind("nvmlDeviceGetCurrentClocksThrottleReasons", &l.deviceGetCurrentClocksThrottleReasons)
	l.bind("nvmlDeviceGetPerformanceState", &l.deviceGetPerformanceState)
	l.bind("nvmlDeviceGetViolationStatus", &l.deviceGetViolationStatus)
	l.bind("nvmlDeviceGetCurrPcieLinkGeneration", &l.deviceGetCurrPcieLinkGeneration)
	l.bind("nvmlDeviceGetCurrPcieLinkWidth", &l.deviceGetCurrPcieLinkWidth)
	l.bind("nvmlDeviceGetMaxPcieLinkGeneration", &l.deviceGetMaxPcieLinkGeneration)
	l.bind("nvmlDeviceGetGpuMaxPcieLinkGeneration", &l.deviceGetGpuMaxPcieLinkGeneration)
	l.bind("nvmlDeviceGetMaxPcieLinkWidth", &l.deviceGetMaxPcieLinkWidth)
	l.bind("nvmlDeviceGetPcieThroughput", &l.deviceGetPcieThroughput)
	l.bind("nvmlDeviceGetPcieReplayCounter", &l.deviceGetPcieReplayCounter)
	if !l.bind("nvmlDeviceGetComputeRunningProcesses_v3", &l.deviceGetComputeRunningProcesses) {
		// _v2 uses the same struct layout as _v3.
		l.bind("nvmlDeviceGetComputeRunningProcesses_v2", &l.deviceGetComputeRunningProcesses)
	}
	if !l.bind("nvmlDeviceGetGraphicsRunningProcesses_v3", &l.deviceGetGraphicsRunningProcesses) {
		l.bind("nvmlDeviceGetGraphicsRunningProcesses_v2", &l.deviceGetGraphicsRunningProcesses)
	}
	l.bind("nvmlDeviceGetProcessUtilization", &l.deviceGetProcessUtilization)
	l.bind("nvmlDeviceGetEccMode", &l.deviceGetEccMode)
	l.bind("nvmlDeviceGetTotalEccErrors", &l.deviceGetTotalEccErrors)
	l.bind("nvmlDeviceGetRetiredPages", &l.deviceGetRetiredPages)
	l.bind("nvmlDeviceGetRetiredPagesPendingStatus", &l.deviceGetRetiredPagesPendingStatus)
	l.bind("nvmlDeviceGetRemappedRows", &l.deviceGetRemappedRows)
	l.bind("nvmlDeviceGetMigMode", &l.deviceGetMigMode)
	l.bind("nvmlDeviceGetMaxMigDeviceCount", &l.deviceGetMaxMigDeviceCount)
	l.bind("nvmlDeviceGetMigDeviceHandleByIndex", &l.deviceGetMigDeviceHandleByIndex)
	l.bind("nvmlDeviceGetGpuInstanceId", &l.deviceGetGpuInstanceId)
	l.bind("nvmlDeviceGetComputeInstanceId", &l.deviceGetComputeInstanceId)
	l.bind("nvmlDeviceGetNvLinkState", &l.deviceGetNvLinkState)
	l.bind("nvmlDeviceGetNvLinkVersion", &l.deviceGetNvLinkVersion)
	l.bind("nvmlDeviceGetNvLinkRemotePciInfo_v2", &l.deviceGetNvLinkRemotePciInfo)
	l.bind("nvmlDeviceGetNvLinkRemoteDeviceType", &l.deviceGetNvLinkRemoteDeviceType)
	l.bind("nvmlDeviceGetNvLinkErrorCounter", &l.deviceGetNvLinkErrorCounter)
	l.bind("nvmlDeviceGetFieldValues", &l.deviceGetFieldValues)
	l.bind("nvmlDeviceGetTopologyCommonAncestor", &l.deviceGetTopologyCommonAncestor)
	l.bind("nvmlEventSetCreate", &l.eventSetCreate)
	l.bind("nvmlDeviceGetSupportedEventTypes", &l.deviceGetSupportedEventTypes)
	l.bind("nvmlDeviceRegisterEvents", &l.deviceRegisterEvents)
	l.bind("nvmlEventSetWait_v2", &l.eventSetWait)
	l.bind("nvmlEventSetFree", &l.eventSetFree)
	return nil
}

const notFound = ERROR_FUNCTION_NOT_FOUND

func (l *lib) LibraryPath() string { return l.path }
func (l *lib) Init() Return        { return l.init() }
func (l *lib) Shutdown() Return    { return l.shutdown() }

func callStr(f fnStr, size int) (string, Return) {
	if f == nil {
		return "", notFound
	}
	buf := make([]byte, size)
	r := f(&buf[0], uint32(size))
	return CString(buf), r
}

func devStr(f fnDevStr, d Device, size int) (string, Return) {
	if f == nil {
		return "", notFound
	}
	buf := make([]byte, size)
	r := f(d, &buf[0], uint32(size))
	return CString(buf), r
}

func devU32(f fnDevU32, d Device) (uint32, Return) {
	if f == nil {
		return 0, notFound
	}
	var v uint32
	r := f(d, &v)
	return v, r
}

func devU64(f fnDevU64, d Device) (uint64, Return) {
	if f == nil {
		return 0, notFound
	}
	var v uint64
	r := f(d, &v)
	return v, r
}

func devEnum(f fnDevEnumU32, d Device, e int) (uint32, Return) {
	if f == nil {
		return 0, notFound
	}
	var v uint32
	r := f(d, uint32(e), &v)
	return v, r
}

func devUtil(f fnDevUtilS, d Device) (uint32, uint32, Return) {
	if f == nil {
		return 0, 0, notFound
	}
	var u, s uint32
	r := f(d, &u, &s)
	return u, s, r
}

func (l *lib) SystemGetDriverVersion() (string, Return) {
	return callStr(l.systemGetDriverVersion, SystemDriverBufferSize)
}

func (l *lib) SystemGetNVMLVersion() (string, Return) {
	return callStr(l.systemGetNVMLVersion, SystemNVMLVersionBufferSize)
}

func (l *lib) SystemGetCudaDriverVersion() (int, Return) {
	if l.systemGetCudaDriverVersion == nil {
		return 0, notFound
	}
	var v int32
	r := l.systemGetCudaDriverVersion(&v)
	return int(v), r
}

func (l *lib) DeviceGetCount() (int, Return) {
	var v uint32
	r := l.deviceGetCount(&v)
	return int(v), r
}

func (l *lib) DeviceGetHandleByIndex(index int) (Device, Return) {
	var d Device
	r := l.deviceGetHandleByIndex(uint32(index), &d)
	return d, r
}

func (l *lib) DeviceGetName(d Device) (string, Return) {
	return devStr(l.deviceGetName, d, DeviceNameBufferSize)
}

func (l *lib) DeviceGetUUID(d Device) (string, Return) {
	return devStr(l.deviceGetUUID, d, DeviceUUIDBufferSize)
}

func (l *lib) DeviceGetSerial(d Device) (string, Return) {
	return devStr(l.deviceGetSerial, d, DeviceSerialBufferSize)
}

func (l *lib) DeviceGetBoardPartNumber(d Device) (string, Return) {
	return devStr(l.deviceGetBoardPartNumber, d, DevicePartNumberBufferSize)
}

func (l *lib) DeviceGetVbiosVersion(d Device) (string, Return) {
	return devStr(l.deviceGetVbiosVersion, d, DeviceVbiosBufferSize)
}

func (l *lib) DeviceGetBrand(d Device) (uint32, Return) { return devU32(l.deviceGetBrand, d) }
func (l *lib) DeviceGetArchitecture(d Device) (uint32, Return) {
	return devU32(l.deviceGetArchitecture, d)
}

func (l *lib) DeviceGetCudaComputeCapability(d Device) (int, int, Return) {
	if l.deviceGetCudaComputeCapability == nil {
		return 0, 0, notFound
	}
	var major, minor int32
	r := l.deviceGetCudaComputeCapability(d, &major, &minor)
	return int(major), int(minor), r
}

func (l *lib) DeviceGetPersistenceMode(d Device) (uint32, Return) {
	return devU32(l.deviceGetPersistenceMode, d)
}

func (l *lib) DeviceGetIndex(d Device) (int, Return) {
	v, r := devU32(l.deviceGetIndex, d)
	return int(v), r
}

func (l *lib) DeviceGetPciInfo(d Device) (PciInfo, Return) {
	var p PciInfo
	if l.deviceGetPciInfo == nil {
		return p, notFound
	}
	r := l.deviceGetPciInfo(d, &p)
	return p, r
}

func (l *lib) DeviceGetNumaNodeId(d Device) (int, Return) {
	v, r := devU32(l.deviceGetNumaNodeId, d)
	return int(v), r
}

func (l *lib) DeviceGetComputeMode(d Device) (uint32, Return) {
	return devU32(l.deviceGetComputeMode, d)
}

func (l *lib) DeviceGetMemoryInfoV2(d Device) (Memory_v2, Return) {
	m := Memory_v2{Version: Memory_v2Version}
	if l.deviceGetMemoryInfoV2 == nil {
		return m, notFound
	}
	r := l.deviceGetMemoryInfoV2(d, &m)
	return m, r
}

func (l *lib) DeviceGetMemoryInfo(d Device) (Memory, Return) {
	var m Memory
	if l.deviceGetMemoryInfo == nil {
		return m, notFound
	}
	r := l.deviceGetMemoryInfo(d, &m)
	return m, r
}

func (l *lib) DeviceGetUtilizationRates(d Device) (Utilization, Return) {
	var u Utilization
	if l.deviceGetUtilizationRates == nil {
		return u, notFound
	}
	r := l.deviceGetUtilizationRates(d, &u)
	return u, r
}

func (l *lib) DeviceGetEncoderUtilization(d Device) (uint32, uint32, Return) {
	return devUtil(l.deviceGetEncoderUtilization, d)
}

func (l *lib) DeviceGetDecoderUtilization(d Device) (uint32, uint32, Return) {
	return devUtil(l.deviceGetDecoderUtilization, d)
}

func (l *lib) DeviceGetJpgUtilization(d Device) (uint32, uint32, Return) {
	return devUtil(l.deviceGetJpgUtilization, d)
}

func (l *lib) DeviceGetOfaUtilization(d Device) (uint32, uint32, Return) {
	return devUtil(l.deviceGetOfaUtilization, d)
}

func (l *lib) DeviceGetTemperature(d Device) (int, Return) {
	// Prefer the versioned API; nvmlDeviceGetTemperature is deprecated in
	// NVML 13 but remains the only option on older drivers.
	if l.deviceGetTemperatureV != nil {
		t := Temperature{Version: TemperatureVersion, SensorType: TEMPERATURE_GPU}
		if r := l.deviceGetTemperatureV(d, &t); r == SUCCESS {
			return int(t.Temperature), r
		} else if l.deviceGetTemperature == nil {
			return 0, r
		}
	}
	v, r := devEnum(l.deviceGetTemperature, d, TEMPERATURE_GPU)
	return int(v), r
}

func (l *lib) DeviceGetTemperatureThreshold(d Device, threshold int) (int, Return) {
	v, r := devEnum(l.deviceGetTemperatureThreshold, d, threshold)
	return int(v), r
}

func (l *lib) DeviceGetFanSpeed(d Device) (uint32, Return) { return devU32(l.deviceGetFanSpeed, d) }

func (l *lib) DeviceGetPowerUsage(d Device) (uint32, Return) {
	return devU32(l.deviceGetPowerUsage, d)
}

func (l *lib) DeviceGetEnforcedPowerLimit(d Device) (uint32, Return) {
	return devU32(l.deviceGetEnforcedPowerLimit, d)
}

func (l *lib) DeviceGetPowerManagementLimitConstraints(d Device) (uint32, uint32, Return) {
	if l.deviceGetPowerManagementLimitConstraints == nil {
		return 0, 0, notFound
	}
	var a, b uint32
	r := l.deviceGetPowerManagementLimitConstraints(d, &a, &b)
	return a, b, r
}

func (l *lib) DeviceGetPowerManagementDefaultLimit(d Device) (uint32, Return) {
	return devU32(l.deviceGetPowerManagementDefaultLimit, d)
}

func (l *lib) DeviceGetTotalEnergyConsumption(d Device) (uint64, Return) {
	return devU64(l.deviceGetTotalEnergyConsumption, d)
}

func (l *lib) DeviceGetClockInfo(d Device, clock int) (uint32, Return) {
	return devEnum(l.deviceGetClockInfo, d, clock)
}

func (l *lib) DeviceGetMaxClockInfo(d Device, clock int) (uint32, Return) {
	return devEnum(l.deviceGetMaxClockInfo, d, clock)
}

func (l *lib) DeviceGetCurrentClocksEventReasons(d Device) (uint64, Return) {
	if l.deviceGetCurrentClocksEventReasons != nil {
		return devU64(l.deviceGetCurrentClocksEventReasons, d)
	}
	return devU64(l.deviceGetCurrentClocksThrottleReasons, d)
}

func (l *lib) DeviceGetPerformanceState(d Device) (int, Return) {
	v, r := devU32(l.deviceGetPerformanceState, d)
	return int(v), r
}

func (l *lib) DeviceGetViolationStatus(d Device, policy int) (ViolationTime, Return) {
	var v ViolationTime
	if l.deviceGetViolationStatus == nil {
		return v, notFound
	}
	r := l.deviceGetViolationStatus(d, uint32(policy), &v)
	return v, r
}

func (l *lib) DeviceGetCurrPcieLinkGeneration(d Device) (int, Return) {
	v, r := devU32(l.deviceGetCurrPcieLinkGeneration, d)
	return int(v), r
}

func (l *lib) DeviceGetCurrPcieLinkWidth(d Device) (int, Return) {
	v, r := devU32(l.deviceGetCurrPcieLinkWidth, d)
	return int(v), r
}

func (l *lib) DeviceGetMaxPcieLinkGeneration(d Device) (int, Return) {
	v, r := devU32(l.deviceGetMaxPcieLinkGeneration, d)
	return int(v), r
}

func (l *lib) DeviceGetGpuMaxPcieLinkGeneration(d Device) (int, Return) {
	v, r := devU32(l.deviceGetGpuMaxPcieLinkGeneration, d)
	return int(v), r
}

func (l *lib) DeviceGetMaxPcieLinkWidth(d Device) (int, Return) {
	v, r := devU32(l.deviceGetMaxPcieLinkWidth, d)
	return int(v), r
}

func (l *lib) DeviceGetPcieThroughput(d Device, counter int) (uint32, Return) {
	return devEnum(l.deviceGetPcieThroughput, d, counter)
}

func (l *lib) DeviceGetPcieReplayCounter(d Device) (uint32, Return) {
	return devU32(l.deviceGetPcieReplayCounter, d)
}

func listProcs(f fnDevProcs, d Device) ([]ProcessInfo, Return) {
	if f == nil {
		return nil, notFound
	}
	// Size the buffer with headroom; processes can start between calls.
	count := uint32(32)
	for range 4 {
		buf := make([]ProcessInfo, count)
		n := count
		r := f(d, &n, &buf[0])
		switch r {
		case SUCCESS:
			return buf[:min(n, count)], r
		case ERROR_INSUFFICIENT_SIZE:
			count = n + 16
		default:
			return nil, r
		}
	}
	return nil, ERROR_INSUFFICIENT_SIZE
}

func (l *lib) DeviceGetComputeRunningProcesses(d Device) ([]ProcessInfo, Return) {
	return listProcs(l.deviceGetComputeRunningProcesses, d)
}

func (l *lib) DeviceGetGraphicsRunningProcesses(d Device) ([]ProcessInfo, Return) {
	return listProcs(l.deviceGetGraphicsRunningProcesses, d)
}

func (l *lib) DeviceGetProcessUtilization(d Device, lastSeen uint64) ([]ProcessUtilizationSample, Return) {
	if l.deviceGetProcessUtilization == nil {
		return nil, notFound
	}
	var n uint32
	r := l.deviceGetProcessUtilization(d, nil, &n, lastSeen)
	if r != ERROR_INSUFFICIENT_SIZE && r != SUCCESS {
		return nil, r
	}
	if n == 0 {
		return nil, SUCCESS
	}
	buf := make([]ProcessUtilizationSample, n)
	r = l.deviceGetProcessUtilization(d, &buf[0], &n, lastSeen)
	if r != SUCCESS {
		return nil, r
	}
	return buf[:min(int(n), len(buf))], r
}

func (l *lib) DeviceGetEccMode(d Device) (uint32, uint32, Return) {
	if l.deviceGetEccMode == nil {
		return 0, 0, notFound
	}
	var a, b uint32
	r := l.deviceGetEccMode(d, &a, &b)
	return a, b, r
}

func (l *lib) DeviceGetTotalEccErrors(d Device, errorType, counterType int) (uint64, Return) {
	if l.deviceGetTotalEccErrors == nil {
		return 0, notFound
	}
	var v uint64
	r := l.deviceGetTotalEccErrors(d, uint32(errorType), uint32(counterType), &v)
	return v, r
}

func (l *lib) DeviceGetRetiredPagesCount(d Device, cause int) (int, Return) {
	if l.deviceGetRetiredPages == nil {
		return 0, notFound
	}
	// Query the count with a zero-sized buffer; NVML returns SUCCESS when
	// there are none and INSUFFICIENT_SIZE (with the count) otherwise.
	var n uint32
	r := l.deviceGetRetiredPages(d, uint32(cause), &n, nil)
	if r == ERROR_INSUFFICIENT_SIZE {
		r = SUCCESS
	}
	return int(n), r
}

func (l *lib) DeviceGetRetiredPagesPendingStatus(d Device) (uint32, Return) {
	return devU32(l.deviceGetRetiredPagesPendingStatus, d)
}

func (l *lib) DeviceGetRemappedRows(d Device) (uint32, uint32, bool, bool, Return) {
	if l.deviceGetRemappedRows == nil {
		return 0, 0, false, false, notFound
	}
	var corr, unc, pending, failure uint32
	r := l.deviceGetRemappedRows(d, &corr, &unc, &pending, &failure)
	return corr, unc, pending != 0, failure != 0, r
}

func (l *lib) DeviceGetMigMode(d Device) (uint32, uint32, Return) {
	if l.deviceGetMigMode == nil {
		return 0, 0, notFound
	}
	var a, b uint32
	r := l.deviceGetMigMode(d, &a, &b)
	return a, b, r
}

func (l *lib) DeviceGetMaxMigDeviceCount(d Device) (int, Return) {
	v, r := devU32(l.deviceGetMaxMigDeviceCount, d)
	return int(v), r
}

func (l *lib) DeviceGetMigDeviceHandleByIndex(d Device, index int) (Device, Return) {
	var m Device
	if l.deviceGetMigDeviceHandleByIndex == nil {
		return 0, notFound
	}
	r := l.deviceGetMigDeviceHandleByIndex(d, uint32(index), &m)
	return m, r
}

func (l *lib) DeviceGetGpuInstanceId(d Device) (int, Return) {
	v, r := devU32(l.deviceGetGpuInstanceId, d)
	return int(v), r
}

func (l *lib) DeviceGetComputeInstanceId(d Device) (int, Return) {
	v, r := devU32(l.deviceGetComputeInstanceId, d)
	return int(v), r
}

func (l *lib) DeviceGetNvLinkState(d Device, link int) (uint32, Return) {
	return devEnum(l.deviceGetNvLinkState, d, link)
}

func (l *lib) DeviceGetNvLinkVersion(d Device, link int) (uint32, Return) {
	return devEnum(l.deviceGetNvLinkVersion, d, link)
}

func (l *lib) DeviceGetNvLinkRemotePciInfo(d Device, link int) (PciInfo, Return) {
	var p PciInfo
	if l.deviceGetNvLinkRemotePciInfo == nil {
		return p, notFound
	}
	r := l.deviceGetNvLinkRemotePciInfo(d, uint32(link), &p)
	return p, r
}

func (l *lib) DeviceGetNvLinkRemoteDeviceType(d Device, link int) (uint32, Return) {
	return devEnum(l.deviceGetNvLinkRemoteDeviceType, d, link)
}

func (l *lib) DeviceGetNvLinkErrorCounter(d Device, link, counter int) (uint64, Return) {
	if l.deviceGetNvLinkErrorCounter == nil {
		return 0, notFound
	}
	var v uint64
	r := l.deviceGetNvLinkErrorCounter(d, uint32(link), uint32(counter), &v)
	return v, r
}

func (l *lib) DeviceGetFieldValues(d Device, values []FieldValue) Return {
	if l.deviceGetFieldValues == nil {
		return notFound
	}
	if len(values) == 0 {
		return SUCCESS
	}
	return l.deviceGetFieldValues(d, int32(len(values)), &values[0])
}

func (l *lib) DeviceGetTopologyCommonAncestor(a, b Device) (int, Return) {
	if l.deviceGetTopologyCommonAncestor == nil {
		return 0, notFound
	}
	var v uint32
	r := l.deviceGetTopologyCommonAncestor(a, b, &v)
	return int(v), r
}

func (l *lib) EventSetCreate() (EventSet, Return) {
	if l.eventSetCreate == nil {
		return 0, notFound
	}
	var s EventSet
	r := l.eventSetCreate(&s)
	return s, r
}

func (l *lib) DeviceGetSupportedEventTypes(d Device) (uint64, Return) {
	return devU64(l.deviceGetSupportedEventTypes, d)
}

func (l *lib) DeviceRegisterEvents(d Device, types uint64, set EventSet) Return {
	if l.deviceRegisterEvents == nil {
		return notFound
	}
	return l.deviceRegisterEvents(d, types, set)
}

func (l *lib) EventSetWait(set EventSet, timeoutMS uint32) (EventData, Return) {
	var e EventData
	if l.eventSetWait == nil {
		return e, notFound
	}
	r := l.eventSetWait(set, &e, timeoutMS)
	return e, r
}

func (l *lib) EventSetFree(set EventSet) Return {
	if l.eventSetFree == nil {
		return notFound
	}
	return l.eventSetFree(set)
}
