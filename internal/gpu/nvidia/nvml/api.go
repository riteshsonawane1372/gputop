// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package nvml

import "errors"

// ErrPlatformUnsupported is returned by Load on platforms where NVML is not
// available (for example macOS).
var ErrPlatformUnsupported = errors.New("NVML is not available on this platform")

// API is the subset of NVML used by gputop, expressed with Go types.
//
// The production implementation (Load) calls into libnvidia-ml. Tests use
// fakes so the NVIDIA provider can be exercised without hardware. Functions
// missing from an older driver return ERROR_FUNCTION_NOT_FOUND.
type API interface {
	Init() Return
	Shutdown() Return
	// LibraryPath returns the path of the loaded shared library.
	LibraryPath() string

	SystemGetDriverVersion() (string, Return)
	SystemGetNVMLVersion() (string, Return)
	SystemGetCudaDriverVersion() (int, Return)

	DeviceGetCount() (int, Return)
	DeviceGetHandleByIndex(index int) (Device, Return)

	DeviceGetName(d Device) (string, Return)
	DeviceGetUUID(d Device) (string, Return)
	DeviceGetSerial(d Device) (string, Return)
	DeviceGetBoardPartNumber(d Device) (string, Return)
	DeviceGetBrand(d Device) (uint32, Return)
	DeviceGetArchitecture(d Device) (uint32, Return)
	DeviceGetCudaComputeCapability(d Device) (major, minor int, ret Return)
	DeviceGetVbiosVersion(d Device) (string, Return)
	DeviceGetPersistenceMode(d Device) (uint32, Return)
	DeviceGetIndex(d Device) (int, Return)
	DeviceGetPciInfo(d Device) (PciInfo, Return)
	DeviceGetNumaNodeId(d Device) (int, Return)
	DeviceGetComputeMode(d Device) (uint32, Return)

	DeviceGetMemoryInfoV2(d Device) (Memory_v2, Return)
	DeviceGetMemoryInfo(d Device) (Memory, Return)
	DeviceGetUtilizationRates(d Device) (Utilization, Return)
	DeviceGetEncoderUtilization(d Device) (util uint32, samplingUs uint32, ret Return)
	DeviceGetDecoderUtilization(d Device) (util uint32, samplingUs uint32, ret Return)
	DeviceGetJpgUtilization(d Device) (util uint32, samplingUs uint32, ret Return)
	DeviceGetOfaUtilization(d Device) (util uint32, samplingUs uint32, ret Return)

	DeviceGetTemperature(d Device) (int, Return)
	DeviceGetTemperatureThreshold(d Device, threshold int) (int, Return)
	DeviceGetFanSpeed(d Device) (uint32, Return)

	DeviceGetPowerUsage(d Device) (uint32, Return)
	DeviceGetEnforcedPowerLimit(d Device) (uint32, Return)
	DeviceGetPowerManagementLimitConstraints(d Device) (minMW, maxMW uint32, ret Return)
	DeviceGetPowerManagementDefaultLimit(d Device) (uint32, Return)
	DeviceGetTotalEnergyConsumption(d Device) (uint64, Return)

	DeviceGetClockInfo(d Device, clock int) (uint32, Return)
	DeviceGetMaxClockInfo(d Device, clock int) (uint32, Return)
	DeviceGetCurrentClocksEventReasons(d Device) (uint64, Return)
	DeviceGetPerformanceState(d Device) (int, Return)
	DeviceGetViolationStatus(d Device, policy int) (ViolationTime, Return)

	DeviceGetCurrPcieLinkGeneration(d Device) (int, Return)
	DeviceGetCurrPcieLinkWidth(d Device) (int, Return)
	DeviceGetMaxPcieLinkGeneration(d Device) (int, Return)
	DeviceGetGpuMaxPcieLinkGeneration(d Device) (int, Return)
	DeviceGetMaxPcieLinkWidth(d Device) (int, Return)
	DeviceGetPcieThroughput(d Device, counter int) (uint32, Return)
	DeviceGetPcieReplayCounter(d Device) (uint32, Return)

	DeviceGetComputeRunningProcesses(d Device) ([]ProcessInfo, Return)
	DeviceGetGraphicsRunningProcesses(d Device) ([]ProcessInfo, Return)
	DeviceGetProcessUtilization(d Device, lastSeen uint64) ([]ProcessUtilizationSample, Return)

	DeviceGetEccMode(d Device) (current, pending uint32, ret Return)
	DeviceGetTotalEccErrors(d Device, errorType, counterType int) (uint64, Return)
	DeviceGetRetiredPagesCount(d Device, cause int) (int, Return)
	DeviceGetRetiredPagesPendingStatus(d Device) (uint32, Return)
	DeviceGetRemappedRows(d Device) (corr, unc uint32, pending, failure bool, ret Return)

	DeviceGetMigMode(d Device) (current, pending uint32, ret Return)
	DeviceGetMaxMigDeviceCount(d Device) (int, Return)
	DeviceGetMigDeviceHandleByIndex(d Device, index int) (Device, Return)
	DeviceGetGpuInstanceId(d Device) (int, Return)
	DeviceGetComputeInstanceId(d Device) (int, Return)

	DeviceGetNvLinkState(d Device, link int) (uint32, Return)
	DeviceGetNvLinkVersion(d Device, link int) (uint32, Return)
	DeviceGetNvLinkRemotePciInfo(d Device, link int) (PciInfo, Return)
	DeviceGetNvLinkRemoteDeviceType(d Device, link int) (uint32, Return)
	DeviceGetNvLinkErrorCounter(d Device, link, counter int) (uint64, Return)

	// DeviceGetFieldValues fills values in place; per-value status is in
	// FieldValue.NvmlReturn.
	DeviceGetFieldValues(d Device, values []FieldValue) Return
	DeviceGetTopologyCommonAncestor(a, b Device) (int, Return)

	EventSetCreate() (EventSet, Return)
	DeviceGetSupportedEventTypes(d Device) (uint64, Return)
	DeviceRegisterEvents(d Device, types uint64, set EventSet) Return
	EventSetWait(set EventSet, timeoutMS uint32) (EventData, Return)
	EventSetFree(set EventSet) Return
}

// DefaultLibraryPaths lists where libnvidia-ml is searched, in order.
var DefaultLibraryPaths = []string{
	"libnvidia-ml.so.1",
	"libnvidia-ml.so",
	"/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1",
	"/usr/lib/aarch64-linux-gnu/libnvidia-ml.so.1",
	"/usr/lib64/libnvidia-ml.so.1",
	"/usr/lib/libnvidia-ml.so.1",
	"/usr/local/nvidia/lib64/libnvidia-ml.so.1",      // Kubernetes device plugin mount
	"/run/nvidia/driver/usr/lib64/libnvidia-ml.so.1", // NVIDIA GPU Operator driver container
	"/run/nvidia/driver/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1",
	"/usr/lib/wsl/lib/libnvidia-ml.so.1", // WSL2
}
