// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package nvml

// Unsupported implements API with every call returning ERROR_NOT_SUPPORTED.
// It is intended to be embedded by test fakes and partial implementations.
type Unsupported struct{}

var _ API = Unsupported{}

func (Unsupported) Init() Return                                      { return ERROR_NOT_SUPPORTED }
func (Unsupported) Shutdown() Return                                  { return ERROR_NOT_SUPPORTED }
func (Unsupported) LibraryPath() string                               { return "" }
func (Unsupported) SystemGetDriverVersion() (string, Return)          { return "", ERROR_NOT_SUPPORTED }
func (Unsupported) SystemGetNVMLVersion() (string, Return)            { return "", ERROR_NOT_SUPPORTED }
func (Unsupported) SystemGetCudaDriverVersion() (int, Return)         { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetCount() (int, Return)                     { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetHandleByIndex(index int) (Device, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetName(d Device) (string, Return)           { return "", ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetUUID(d Device) (string, Return)           { return "", ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetSerial(d Device) (string, Return)         { return "", ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetBoardPartNumber(d Device) (string, Return) {
	return "", ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetBrand(d Device) (uint32, Return)        { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetArchitecture(d Device) (uint32, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetCudaComputeCapability(d Device) (major, minor int, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetVbiosVersion(d Device) (string, Return)    { return "", ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetPersistenceMode(d Device) (uint32, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetIndex(d Device) (int, Return)              { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetPciInfo(d Device) (PciInfo, Return) {
	return PciInfo{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetNumaNodeId(d Device) (int, Return)     { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetComputeMode(d Device) (uint32, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetMemoryInfoV2(d Device) (Memory_v2, Return) {
	return Memory_v2{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetMemoryInfo(d Device) (Memory, Return) {
	return Memory{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetUtilizationRates(d Device) (Utilization, Return) {
	return Utilization{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetEncoderUtilization(d Device) (util uint32, samplingUs uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetDecoderUtilization(d Device) (util uint32, samplingUs uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetJpgUtilization(d Device) (util uint32, samplingUs uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetOfaUtilization(d Device) (util uint32, samplingUs uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetTemperature(d Device) (int, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetTemperatureThreshold(d Device, threshold int) (int, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetFanSpeed(d Device) (uint32, Return)   { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetPowerUsage(d Device) (uint32, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetEnforcedPowerLimit(d Device) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetPowerManagementLimitConstraints(d Device) (minMW, maxMW uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetPowerManagementDefaultLimit(d Device) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetTotalEnergyConsumption(d Device) (uint64, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetClockInfo(d Device, clock int) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetMaxClockInfo(d Device, clock int) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetCurrentClocksEventReasons(d Device) (uint64, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetPerformanceState(d Device) (int, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetViolationStatus(d Device, policy int) (ViolationTime, Return) {
	return ViolationTime{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetCurrPcieLinkGeneration(d Device) (int, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetCurrPcieLinkWidth(d Device) (int, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetMaxPcieLinkGeneration(d Device) (int, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetGpuMaxPcieLinkGeneration(d Device) (int, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetMaxPcieLinkWidth(d Device) (int, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetPcieThroughput(d Device, counter int) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetPcieReplayCounter(d Device) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetComputeRunningProcesses(d Device) ([]ProcessInfo, Return) {
	return nil, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetGraphicsRunningProcesses(d Device) ([]ProcessInfo, Return) {
	return nil, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetProcessUtilization(d Device, lastSeen uint64) ([]ProcessUtilizationSample, Return) {
	return nil, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetEccMode(d Device) (current, pending uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetTotalEccErrors(d Device, errorType, counterType int) (uint64, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetRetiredPagesCount(d Device, cause int) (int, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetRetiredPagesPendingStatus(d Device) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetRemappedRows(d Device) (corr, unc uint32, pending, failure bool, ret Return) {
	return 0, 0, false, false, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetMigMode(d Device) (current, pending uint32, ret Return) {
	return 0, 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetMaxMigDeviceCount(d Device) (int, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetMigDeviceHandleByIndex(d Device, index int) (Device, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetGpuInstanceId(d Device) (int, Return)     { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetComputeInstanceId(d Device) (int, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetNvLinkState(d Device, link int) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetNvLinkVersion(d Device, link int) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetNvLinkRemotePciInfo(d Device, link int) (PciInfo, Return) {
	return PciInfo{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetNvLinkRemoteDeviceType(d Device, link int) (uint32, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetNvLinkErrorCounter(d Device, link, counter int) (uint64, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetFieldValues(d Device, values []FieldValue) Return {
	return ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceGetTopologyCommonAncestor(a, b Device) (int, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) EventSetCreate() (EventSet, Return) { return 0, ERROR_NOT_SUPPORTED }
func (Unsupported) DeviceGetSupportedEventTypes(d Device) (uint64, Return) {
	return 0, ERROR_NOT_SUPPORTED
}
func (Unsupported) DeviceRegisterEvents(d Device, types uint64, set EventSet) Return {
	return ERROR_NOT_SUPPORTED
}
func (Unsupported) EventSetWait(set EventSet, timeoutMS uint32) (EventData, Return) {
	return EventData{}, ERROR_NOT_SUPPORTED
}
func (Unsupported) EventSetFree(set EventSet) Return { return ERROR_NOT_SUPPORTED }
