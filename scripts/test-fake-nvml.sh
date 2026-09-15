#!/usr/bin/env bash
# Verifies the NVML binding ABI on Linux against a fake libnvidia-ml.so.1.
# Usage: scripts/test-fake-nvml.sh [amd64|arm64]
# Runs natively on Linux with gcc, otherwise in Docker (gcc:13 image).
set -euo pipefail
arch="${1:-$(go env GOARCH)}"
root="$(cd "$(dirname "$0")/.." && pwd)"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$out/gputop" "$root/cmd/gputop"
cp "$root/internal/gpu/nvidia/nvml/testdata/fakenvml.c" "$out/"

run_checks='
  gcc -shared -fPIC -O1 -o "$W/libnvidia-ml.so.1" "$W/fakenvml.c"
  export LD_LIBRARY_PATH="$W" HOME=/tmp
  "$W/gputop" --once --json > "$W/snap.json"
  "$W/gputop" --once
'
if [[ "$(uname -s)" == Linux && "$arch" == "$(go env GOHOSTARCH)" ]] && command -v gcc >/dev/null; then
  W="$out" bash -euo pipefail -c "$run_checks"
else
  docker run --rm --platform "linux/$arch" -e W=/w -v "$out:/w" gcc:13 bash -euo pipefail -c "$run_checks"
fi
python3 - "$out/snap.json" <<'PY'
import json, sys
s = json.load(open(sys.argv[1]))
g = {x["device"]["id"]: x for x in s["gpus"]}
def check(cond, msg):
    if not cond:
        sys.exit("FAIL: " + msg)
check(s["providers"][0]["available"], "provider available")
check(s["providers"][0]["system"]["driver_version"] == "999.88.77", "driver version string buffer")
check(s["providers"][0]["system"]["runtime_version"] == "12.8", "cuda version int pointer")
a, b = g["GPU-fake-0000-aaaa"], g["GPU-fake-0001-bbbb"]
d, sm = a["device"], a["sample"]
check(d["architecture"] == "Hopper" and d["compute_capability"] == "9.0", "arch/compute capability")
check(d["pci"]["bus_id"] == "0000:17:00.0", "PciInfo_t busId offset: %r" % d["pci"]["bus_id"])
check(d["pci"]["device_id"] == 0x233010de, "PciInfo_t device id offset")
check(sm["util_percent"] == 91 and b["sample"]["util_percent"] == 92, "nvmlUtilization_t")
check(sm["memory_bandwidth_util_percent"] == 37, "utilization.memory")
check(sm["memory_total_bytes"] == 85899345920 and sm["memory_reserved_bytes"] == 536870912, "nvmlMemory_v2_t layout/version")
check(sm["memory_used_bytes"] == 21474836480, "memory used")
check(sm["temp_c"] == 66 and sm["memory_temp_c"] == 71, "TemperatureV struct / field value uint")
check(abs(sm["power_w"] - 312.345) < 1e-6 and sm["power_limit_w"] == 700, "power mW conversion")
check(abs(sm["energy_j"] - 123456789.012) < 1e-3, "unsigned long long energy")
check(sm["clock_core_mhz"] == 2600 and sm["clock_mem_mhz"] == 3100, "clock enum arguments (MEM=2)")
check(sorted(sm["throttle_reasons"]) == ["hw_thermal", "sw_power_cap"], "clock event reason bits: %r" % sm["throttle_reasons"])
check(sm["pcie_gen"] == 4 and b["sample"]["pcie_width"] == 8, "pcie link")
hc = a["health_counters"]
check(hc["ecc_uncorrected_aggregate"] == 1013 and hc["ecc_corrected_volatile"] == 3, "ECC enum args: %r" % hc)
check(hc["remapped_rows_correctable"] == 2 and hc["remapped_rows_uncorrectable"] == 1, "remapped rows pointers")
check(hc["recovery_action"] == "none", "recovery action field")
links = a["links"]
check(len(links) == 2 and links[0]["state"] == "active" and links[1]["state"] == "inactive", "nvlink state")
check(links[0]["tx_bytes"] == 1000 * 1024 and links[1]["rx_bytes"] == 2001 * 1024, "field value scopeId + ull union")
check(links[0]["err_crc_data"] == 3 and links[1]["err_replay"] == 100, "nvlink error counter args")
check(links[0]["remote_id"] == "GPU-fake-0001-bbbb", "nvlink remote PCI mapping")
procs = [p for p in s["processes"] if p["device_id"] == "GPU-fake-0000-aaaa"]
check(len(procs) == 2, "process list with INSUFFICIENT_SIZE retry")
p1 = [p for p in procs if p["pid"] == 1][0]
p2 = [p for p in procs if p["pid"] == 424242][0]
check(p1["memory_used_bytes"] == 4294967296, "nvmlProcessInfo_t usedGpuMemory offset")
check(p2["memory_used_bytes"] is None, "NVML_VALUE_NOT_AVAILABLE -> null")
topo = s["topology"]
check(len(topo) == 1 and topo[0]["pcie"] == "phb" and topo[0]["nvlinks"] == 1, "topology: %r" % topo)
print("fake NVML ABI checks passed (%s GPUs)" % len(s["gpus"]))
PY
