// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0
//
// A fake libnvidia-ml.so.1 used to verify gputop's dlopen/purego binding
// ABI (struct layouts, string buffers, 64-bit arguments, field value unions)
// on Linux without NVIDIA hardware. Values are fixed and checked by
// scripts/test-fake-nvml.sh. Signatures follow nvml.h.

#include <string.h>
#include <stdint.h>

typedef int nvmlReturn_t;
typedef void *nvmlDevice_t;
#define OK 0
#define NOT_SUPPORTED 3
#define INVALID_ARGUMENT 2
#define INSUFFICIENT_SIZE 7

typedef struct { unsigned int version; unsigned long long total, reserved, free, used; } nvmlMemory_v2_t;
typedef struct { unsigned long long total, free, used; } nvmlMemory_t;
typedef struct { unsigned int gpu, memory; } nvmlUtilization_t;
typedef struct { unsigned int version; int sensorType; int temperature; } nvmlTemperature_t;
typedef struct { unsigned int pid; unsigned long long usedGpuMemory; unsigned int gpuInstanceId, computeInstanceId; } nvmlProcessInfo_t;
typedef struct { unsigned int pid; unsigned long long timeStamp; unsigned int smUtil, memUtil, encUtil, decUtil; } nvmlProcessUtilizationSample_t;
typedef struct { char busIdLegacy[16]; unsigned int domain, bus, device, pciDeviceId, pciSubSystemId; char busId[32]; } nvmlPciInfo_t;
typedef union { double dVal; unsigned int uiVal; unsigned long ulVal; unsigned long long ullVal; signed long long sllVal; int siVal; unsigned short usVal; } nvmlValue_t;
typedef struct { unsigned int fieldId, scopeId; long long timestamp, latencyUsec; int valueType; nvmlReturn_t nvmlReturn; nvmlValue_t value; } nvmlFieldValue_t;

static nvmlDevice_t dev(unsigned int i) { return (nvmlDevice_t)(uintptr_t)(0x1000 + i); }
static int idx(nvmlDevice_t d) { return (int)((uintptr_t)d - 0x1000); }
static nvmlReturn_t copystr(char *buf, unsigned int len, const char *s) {
	if (strlen(s) + 1 > len) return INSUFFICIENT_SIZE;
	strcpy(buf, s);
	return OK;
}

nvmlReturn_t nvmlInit_v2(void) { return OK; }
nvmlReturn_t nvmlShutdown(void) { return OK; }
nvmlReturn_t nvmlSystemGetDriverVersion(char *v, unsigned int l) { return copystr(v, l, "999.88.77"); }
nvmlReturn_t nvmlSystemGetNVMLVersion(char *v, unsigned int l) { return copystr(v, l, "13.999.88"); }
nvmlReturn_t nvmlSystemGetCudaDriverVersion_v2(int *v) { *v = 12080; return OK; }
nvmlReturn_t nvmlDeviceGetCount_v2(unsigned int *n) { *n = 2; return OK; }
nvmlReturn_t nvmlDeviceGetHandleByIndex_v2(unsigned int i, nvmlDevice_t *d) {
	if (i >= 2) return INVALID_ARGUMENT;
	*d = dev(i);
	return OK;
}
nvmlReturn_t nvmlDeviceGetIndex(nvmlDevice_t d, unsigned int *i) { *i = idx(d); return OK; }
nvmlReturn_t nvmlDeviceGetUUID(nvmlDevice_t d, char *b, unsigned int l) {
	return copystr(b, l, idx(d) == 0 ? "GPU-fake-0000-aaaa" : "GPU-fake-0001-bbbb");
}
nvmlReturn_t nvmlDeviceGetName(nvmlDevice_t d, char *b, unsigned int l) { return copystr(b, l, "Fake NVIDIA Test GPU"); }
nvmlReturn_t nvmlDeviceGetArchitecture(nvmlDevice_t d, unsigned int *a) { *a = 9; return OK; }
nvmlReturn_t nvmlDeviceGetCudaComputeCapability(nvmlDevice_t d, int *maj, int *min) { *maj = 9; *min = 0; return OK; }
nvmlReturn_t nvmlDeviceGetPciInfo_v3(nvmlDevice_t d, nvmlPciInfo_t *p) {
	memset(p, 0, sizeof *p);
	strcpy(p->busId, idx(d) == 0 ? "00000000:17:00.0" : "00000000:65:00.0");
	p->pciDeviceId = 0x233010de;
	return OK;
}
nvmlReturn_t nvmlDeviceGetMemoryInfo_v2(nvmlDevice_t d, nvmlMemory_v2_t *m) {
	if (m->version != (sizeof(nvmlMemory_v2_t) | (2 << 24))) return 25; // ARGUMENT_VERSION_MISMATCH
	m->total = 85899345920ULL; m->reserved = 536870912ULL; m->used = 21474836480ULL + idx(d); m->free = m->total - m->used;
	return OK;
}
nvmlReturn_t nvmlDeviceGetUtilizationRates(nvmlDevice_t d, nvmlUtilization_t *u) { u->gpu = 91 + idx(d); u->memory = 37; return OK; }
nvmlReturn_t nvmlDeviceGetTemperatureV(nvmlDevice_t d, nvmlTemperature_t *t) {
	if (t->version != (sizeof(nvmlTemperature_t) | (1 << 24)) || t->sensorType != 0) return 25;
	t->temperature = 66;
	return OK;
}
nvmlReturn_t nvmlDeviceGetTemperatureThreshold(nvmlDevice_t d, int type, unsigned int *t) {
	switch (type) { case 0: *t = 95; return OK; case 1: *t = 90; return OK; default: return NOT_SUPPORTED; }
}
nvmlReturn_t nvmlDeviceGetPowerUsage(nvmlDevice_t d, unsigned int *p) { *p = 312345; return OK; }
nvmlReturn_t nvmlDeviceGetEnforcedPowerLimit(nvmlDevice_t d, unsigned int *p) { *p = 700000; return OK; }
nvmlReturn_t nvmlDeviceGetTotalEnergyConsumption(nvmlDevice_t d, unsigned long long *e) { *e = 123456789012ULL; return OK; }
nvmlReturn_t nvmlDeviceGetClockInfo(nvmlDevice_t d, int type, unsigned int *c) {
	if (type == 0) { *c = 2600; return OK; }
	if (type == 2) { *c = 3100; return OK; }
	return INVALID_ARGUMENT;
}
nvmlReturn_t nvmlDeviceGetCurrentClocksEventReasons(nvmlDevice_t d, unsigned long long *r) { *r = 0x4ULL | 0x40ULL; return OK; }
nvmlReturn_t nvmlDeviceGetPerformanceState(nvmlDevice_t d, int *p) { *p = 0; return OK; }
nvmlReturn_t nvmlDeviceGetCurrPcieLinkGeneration(nvmlDevice_t d, unsigned int *g) { *g = 4; return OK; }
nvmlReturn_t nvmlDeviceGetCurrPcieLinkWidth(nvmlDevice_t d, unsigned int *w) { *w = idx(d) == 1 ? 8 : 16; return OK; }
nvmlReturn_t nvmlDeviceGetMaxPcieLinkGeneration(nvmlDevice_t d, unsigned int *g) { *g = 4; return OK; }
nvmlReturn_t nvmlDeviceGetMaxPcieLinkWidth(nvmlDevice_t d, unsigned int *w) { *w = 16; return OK; }
nvmlReturn_t nvmlDeviceGetComputeRunningProcesses_v3(nvmlDevice_t d, unsigned int *n, nvmlProcessInfo_t *infos) {
	if (idx(d) != 0) { *n = 0; return OK; }
	if (*n < 2) { *n = 2; return INSUFFICIENT_SIZE; }
	*n = 2;
	infos[0].pid = 1; infos[0].usedGpuMemory = 4294967296ULL; infos[0].gpuInstanceId = 0xFFFFFFFF; infos[0].computeInstanceId = 0xFFFFFFFF;
	infos[1].pid = 424242; infos[1].usedGpuMemory = 0xFFFFFFFFFFFFFFFFULL; // NVML_VALUE_NOT_AVAILABLE
	return OK;
}
nvmlReturn_t nvmlDeviceGetProcessUtilization(nvmlDevice_t d, nvmlProcessUtilizationSample_t *s, unsigned int *n, unsigned long long last) {
	if (idx(d) != 0 || last == 0) return 6; // NOT_FOUND
	if (s == NULL) { *n = 1; return INSUFFICIENT_SIZE; }
	*n = 1;
	s[0].pid = 1; s[0].timeStamp = last + 1; s[0].smUtil = 77; s[0].memUtil = 33; s[0].encUtil = 0; s[0].decUtil = 0;
	return OK;
}
nvmlReturn_t nvmlDeviceGetTotalEccErrors(nvmlDevice_t d, int type, int counter, unsigned long long *c) {
	*c = (unsigned long long)(type * 1000 + counter * 10 + 3);
	return OK;
}
nvmlReturn_t nvmlDeviceGetEccMode(nvmlDevice_t d, int *cur, int *pend) { *cur = 1; *pend = 1; return OK; }
nvmlReturn_t nvmlDeviceGetRemappedRows(nvmlDevice_t d, unsigned int *c, unsigned int *u, unsigned int *p, unsigned int *f) {
	*c = 2; *u = 1; *p = 0; *f = 0;
	return OK;
}
nvmlReturn_t nvmlDeviceGetFieldValues(nvmlDevice_t d, int count, nvmlFieldValue_t *v) {
	for (int i = 0; i < count; i++) {
		v[i].nvmlReturn = NOT_SUPPORTED;
		switch (v[i].fieldId) {
		case 82: v[i].valueType = 1; v[i].value.uiVal = 71; v[i].nvmlReturn = OK; break;          // memory temp
		case 91: v[i].valueType = 1; v[i].value.uiVal = idx(d) == 0 ? 2 : 0; v[i].nvmlReturn = OK; break; // link count
		case 138: v[i].valueType = 3; v[i].value.ullVal = 1000ULL + v[i].scopeId; v[i].nvmlReturn = OK; break;
		case 139: v[i].valueType = 3; v[i].value.ullVal = 2000ULL + v[i].scopeId; v[i].nvmlReturn = OK; break;
		case 197: v[i].valueType = 3; v[i].value.ullVal = 5000000000ULL; v[i].nvmlReturn = OK; break;
		case 198: v[i].valueType = 3; v[i].value.ullVal = 6000000000ULL; v[i].nvmlReturn = OK; break;
		case 230: v[i].valueType = 1; v[i].value.uiVal = 0; v[i].nvmlReturn = OK; break;
		}
	}
	return OK;
}
nvmlReturn_t nvmlDeviceGetNvLinkState(nvmlDevice_t d, unsigned int link, int *active) {
	if (idx(d) != 0 || link >= 2) return NOT_SUPPORTED;
	*active = link == 0 ? 1 : 0;
	return OK;
}
nvmlReturn_t nvmlDeviceGetNvLinkVersion(nvmlDevice_t d, unsigned int link, unsigned int *v) { *v = 4; return OK; }
nvmlReturn_t nvmlDeviceGetNvLinkRemotePciInfo_v2(nvmlDevice_t d, unsigned int link, nvmlPciInfo_t *p) {
	memset(p, 0, sizeof *p);
	strcpy(p->busId, "00000000:65:00.0");
	return OK;
}
nvmlReturn_t nvmlDeviceGetNvLinkRemoteDeviceType(nvmlDevice_t d, unsigned int link, int *t) { *t = 0; return OK; }
nvmlReturn_t nvmlDeviceGetNvLinkErrorCounter(nvmlDevice_t d, unsigned int link, int counter, unsigned long long *v) {
	*v = (unsigned long long)(link * 100 + counter);
	return OK;
}
nvmlReturn_t nvmlDeviceGetMigMode(nvmlDevice_t d, unsigned int *cur, unsigned int *pend) { return NOT_SUPPORTED; }
nvmlReturn_t nvmlDeviceGetTopologyCommonAncestor(nvmlDevice_t a, nvmlDevice_t b, int *level) { *level = 30; return OK; }
