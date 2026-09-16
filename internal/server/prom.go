// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

// promWriter emits the Prometheus text exposition format (version 0.0.4).
//
// Label cardinality is deliberately bounded: per-GPU series carry only the
// GPU index and UUID. PIDs, container IDs and pod UIDs are never labels;
// that high-cardinality data is available from the JSON API instead.
type promWriter struct {
	w        *bufio.Writer
	declared map[string]bool
}

func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

func (p *promWriter) family(name, typ, help string) {
	if p.declared[name] {
		return
	}
	p.declared[name] = true
	fmt.Fprintf(p.w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func (p *promWriter) sample(name string, labels []string, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	p.w.WriteString(name)
	if len(labels) > 0 {
		p.w.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				p.w.WriteByte(',')
			}
			p.w.WriteString(labels[i])
			p.w.WriteString(`="`)
			p.w.WriteString(escapeLabel(labels[i+1]))
			p.w.WriteByte('"')
		}
		p.w.WriteByte('}')
	}
	p.w.WriteByte(' ')
	p.w.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
	p.w.WriteByte('\n')
}

func (p *promWriter) gauge(name, help string, labels []string, v float64) {
	p.family(name, "gauge", help)
	p.sample(name, labels, v)
}

func (p *promWriter) counter(name, help string, labels []string, v float64) {
	p.family(name, "counter", help)
	p.sample(name, labels, v)
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func optGauge[T ~int | ~uint64 | ~float64](p *promWriter, name, help string, labels []string, o metric.Opt[T], scale float64) {
	if o.OK {
		p.gauge(name, help, labels, float64(o.V)*scale)
	}
}

var throttleReasons = []struct {
	bit  gpu.ThrottleReasons
	name string
}{
	{gpu.ThrottleSWPowerCap, "sw_power_cap"}, {gpu.ThrottleHWSlowdown, "hw_slowdown"},
	{gpu.ThrottleSWThermal, "sw_thermal"}, {gpu.ThrottleHWThermal, "hw_thermal"},
	{gpu.ThrottleHWPowerBrake, "hw_power_brake"}, {gpu.ThrottleSyncBoost, "sync_boost"},
	{gpu.ThrottleBoardLimit, "board_limit"}, {gpu.ThrottleReliability, "reliability"},
	{gpu.ThrottleIdle, "idle"},
}

// WritePrometheus renders a snapshot as Prometheus metrics.
func WritePrometheus(out io.Writer, s *model.Snapshot, version string) error {
	p := &promWriter{w: bufio.NewWriter(out), declared: map[string]bool{}}
	p.gauge("gputop_build_info", "gputop build information.", []string{"version", version, "goversion", runtime.Version()}, 1)
	p.gauge("gputop_ready", "1 once the first device inventory completed.", nil, b2f(s.Ready))
	p.gauge("gputop_snapshot_timestamp_seconds", "Time of the snapshot these metrics describe.", nil, float64(s.Time.UnixNano())/1e9)

	drivers := map[string]string{}
	for _, pr := range s.Providers {
		p.gauge("gputop_provider_available", "1 if the accelerator provider initialized.", []string{"provider", pr.Name}, b2f(pr.Available))
		drivers[pr.Name] = pr.System.DriverVersion
	}

	for i := range s.GPUs {
		g := &s.GPUs[i]
		d, smp := g.Device, g.Sample
		l := []string{"gpu", strconv.Itoa(d.Index), "uuid", string(d.ID)}
		p.gauge("gputop_gpu_info", "Static GPU identity (value is always 1).",
			append(append([]string(nil), l...), "name", d.Name, "vendor", string(d.Vendor), "pci_bus_id", d.PCI.BusID, "driver", drivers[g.Provider]), 1)
		p.gauge("gputop_gpu_available", "1 if the GPU is readable.", l, b2f(g.Available))
		if !g.Available {
			continue
		}
		optGauge(p, "gputop_gpu_utilization_ratio", "Fraction of the sample period a kernel was executing (NVML utilization.gpu).", l, smp.UtilPercent, 0.01)
		optGauge(p, "gputop_gpu_memory_bandwidth_utilization_ratio", "Fraction of the sample period device memory was read or written (NVML utilization.memory).", l, smp.MemBandwidthPercent, 0.01)
		optGauge(p, "gputop_gpu_encoder_utilization_ratio", "Encoder utilization.", l, smp.EncoderPercent, 0.01)
		optGauge(p, "gputop_gpu_decoder_utilization_ratio", "Decoder utilization.", l, smp.DecoderPercent, 0.01)
		optGauge(p, "gputop_gpu_memory_used_bytes", "Allocated device memory.", l, smp.MemUsed, 1)
		optGauge(p, "gputop_gpu_memory_total_bytes", "Total device memory.", l, smp.MemTotal, 1)
		optGauge(p, "gputop_gpu_memory_reserved_bytes", "Device memory reserved by driver/firmware.", l, smp.MemReserved, 1)
		optGauge(p, "gputop_gpu_temperature_celsius", "GPU die temperature.", l, smp.TempC, 1)
		optGauge(p, "gputop_gpu_memory_temperature_celsius", "GPU memory temperature.", l, smp.MemTempC, 1)
		optGauge(p, "gputop_gpu_temperature_slowdown_celsius", "Temperature at which hardware slowdown begins.", l, d.TempSlowdownC, 1)
		optGauge(p, "gputop_gpu_fan_ratio", "Fan speed as a fraction of maximum.", l, smp.FanPct, 0.01)
		optGauge(p, "gputop_gpu_power_watts", "Power draw.", l, smp.PowerW, 1)
		optGauge(p, "gputop_gpu_power_limit_watts", "Enforced power limit.", l, smp.PowerLimitW, 1)
		if smp.EnergyJ.OK {
			p.counter("gputop_gpu_energy_joules_total", "Energy consumed since the driver was loaded.", l, smp.EnergyJ.V)
		}
		optGauge(p, "gputop_gpu_clock_core_hertz", "Current core clock frequency.", l, smp.ClockCoreMHz, 1e6)
		optGauge(p, "gputop_gpu_clock_memory_hertz", "Current memory clock frequency.", l, smp.ClockMemMHz, 1e6)
		optGauge(p, "gputop_gpu_performance_state", "Performance state (P0 = maximum).", l, smp.PState, 1)
		if smp.Throttle.OK {
			for _, r := range throttleReasons {
				p.gauge("gputop_gpu_throttle_reason_active", "1 while a clock event (throttle) reason is active.",
					append(append([]string(nil), l...), "reason", r.name), b2f(smp.Throttle.V&r.bit != 0))
			}
		}
		optGauge(p, "gputop_gpu_pcie_link_generation", "Current PCIe link generation.", l, smp.PCIeGen, 1)
		optGauge(p, "gputop_gpu_pcie_link_width", "Current PCIe link width.", l, smp.PCIeWidth, 1)
		optGauge(p, "gputop_gpu_pcie_max_link_width", "Maximum PCIe link width.", l, d.PCIeMaxWidth, 1)
		optGauge(p, "gputop_gpu_pcie_tx_bytes_per_second", "PCIe transmit throughput.", l, smp.PCIeTxBps, 1)
		optGauge(p, "gputop_gpu_pcie_rx_bytes_per_second", "PCIe receive throughput.", l, smp.PCIeRxBps, 1)

		c := g.Counters
		for _, e := range []struct {
			typ, counter string
			v            metric.Opt[uint64]
		}{
			{"corrected", "volatile", c.ECCCorrectedVolatile}, {"uncorrected", "volatile", c.ECCUncorrectedVolatile},
			{"corrected", "aggregate", c.ECCCorrectedAggregate}, {"uncorrected", "aggregate", c.ECCUncorrectedAggregate},
		} {
			if e.v.OK {
				p.counter("gputop_gpu_ecc_errors_total", "ECC errors (volatile: since driver load, aggregate: lifetime).",
					append(append([]string(nil), l...), "type", e.typ, "counter", e.counter), float64(e.v.V))
			}
		}
		if c.RemappedCorrectable.OK {
			p.gauge("gputop_gpu_remapped_rows", "Remapped memory rows.", append(append([]string(nil), l...), "type", "correctable"), float64(c.RemappedCorrectable.V))
			p.gauge("gputop_gpu_remapped_rows", "Remapped memory rows.", append(append([]string(nil), l...), "type", "uncorrectable"), float64(c.RemappedUncorrectable.V))
		}
		if c.RemapPending.OK {
			p.gauge("gputop_gpu_remap_pending", "1 when a memory remap requires a GPU reset.", l, b2f(c.RemapPending.V))
			p.gauge("gputop_gpu_remap_failure", "1 when row remapping failed.", l, b2f(c.RemapFailure.V))
		}
		if c.RetiredPagesSBE.OK {
			p.gauge("gputop_gpu_retired_pages", "Retired memory pages.", append(append([]string(nil), l...), "cause", "single_bit"), float64(c.RetiredPagesSBE.V))
			p.gauge("gputop_gpu_retired_pages", "Retired memory pages.", append(append([]string(nil), l...), "cause", "double_bit"), float64(c.RetiredPagesDBE.V))
		}
		if c.PCIeReplays.OK {
			p.counter("gputop_gpu_pcie_replays_total", "PCIe replay counter.", l, float64(c.PCIeReplays.V))
		}
		if d.LinkCount > 0 {
			p.gauge("gputop_gpu_nvlink_links", "NVLink links present.", l, float64(d.LinkCount))
			p.gauge("gputop_gpu_nvlink_active_links", "NVLink links in the active state.", l, float64(g.Derived.LinksActive))
			optGauge(p, "gputop_gpu_nvlink_tx_bytes_per_second", "NVLink transmit throughput summed over links.", l, g.Derived.NVLinkTxBps, 1)
			optGauge(p, "gputop_gpu_nvlink_rx_bytes_per_second", "NVLink receive throughput summed over links.", l, g.Derived.NVLinkRxBps, 1)
			var errs uint64
			for _, lk := range g.Links {
				errs += lk.ErrorTotal()
			}
			p.counter("gputop_gpu_nvlink_errors_total", "NVLink replay, recovery and CRC errors summed over links.", l, float64(errs))
		}
		p.gauge("gputop_gpu_processes", "Processes with a context on the GPU.", l, float64(g.Processes))
		p.gauge("gputop_gpu_mig_enabled", "1 when MIG mode is enabled.", l, b2f(d.MIG.Enabled))

		p.gauge("gputop_gpu_health_score", "gputop-derived health score 0-100 (heuristic, not an NVIDIA metric).", l, float64(g.Health.Score))
		if g.Derived.Efficiency.Score.OK {
			p.gauge("gputop_gpu_efficiency_score", "gputop-derived utilization efficiency 0-100 (see docs/derived-metrics.md).", l, float64(g.Derived.Efficiency.Score.V))
		}
		p.gauge("gputop_gpu_idle_allocated", "1 when the GPU has processes but stayed idle for gpu.idle_after (derived).", l, b2f(g.Derived.IdleAllocated))
		p.gauge("gputop_gpu_utilization_outlier", "1 when utilization is a low outlier within its workload cohort (derived).", l, b2f(g.Derived.Outlier))
	}

	f := s.Fleet
	p.gauge("gputop_fleet_gpus", "GPUs on this node.", nil, float64(f.GPUs))
	p.gauge("gputop_fleet_gpus_allocated", "GPUs with at least one process.", nil, float64(f.Allocated))
	p.gauge("gputop_fleet_gpus_idle_allocated", "Allocated GPUs idle for gpu.idle_after (derived).", nil, float64(f.IdleAllocated))
	optGauge(p, "gputop_fleet_unused_allocated_gpu_equivalents", "Allocated GPU capacity left unused over the window (derived).", nil, f.UnusedAllocated, 1)
	p.gauge("gputop_alerts_active", "Active alerts by severity.", []string{"severity", "critical"}, float64(countAlerts(s, model.SevCritical)))
	p.gauge("gputop_alerts_active", "Active alerts by severity.", []string{"severity", "warning"}, float64(countAlerts(s, model.SevWarning)))

	sort.SliceStable(s.Collectors, func(i, j int) bool { return s.Collectors[i].Name < s.Collectors[j].Name })
	for _, c := range s.Collectors {
		l := []string{"collector", c.Name}
		p.gauge("gputop_collector_last_duration_seconds", "Duration of the collector's last run.", l, c.LastDuration.Seconds())
		p.counter("gputop_collector_runs_total", "Collector runs.", l, float64(c.Runs))
		p.counter("gputop_collector_errors_total", "Collector runs that returned an error.", l, float64(c.Errors))
		p.counter("gputop_collector_overruns_total", "Runs skipped because the previous run was still in progress.", l, float64(c.Overruns))
	}
	if s.History.Enabled {
		p.gauge("gputop_history_points", "Points held in the in-memory history ring.", nil, float64(s.History.Points))
		p.gauge("gputop_history_disk_bytes", "Bytes used by persisted history.", nil, float64(s.History.DiskBytes))
		p.gauge("gputop_history_write_seconds", "Average history write latency.", nil, s.History.WriteAvg.Seconds())
	}
	optGauge(p, "gputop_self_cpu_ratio", "CPU used by gputop (1.0 = one core).", nil, s.Self.CPUPercent, 0.01)
	p.gauge("gputop_self_memory_bytes", "Memory obtained from the OS by the Go runtime.", nil, float64(s.Self.SysBytes))
	p.gauge("gputop_self_goroutines", "Goroutines.", nil, float64(s.Self.Goroutines))
	p.gauge("gputop_self_collect_seconds", "Duration of the last fast collection pass.", nil, s.Self.CollectTime.Seconds())
	if !s.Self.StartTime.IsZero() {
		p.gauge("gputop_self_start_time_seconds", "Start time of the gputop process.", nil, float64(s.Self.StartTime.Unix()))
	}
	return p.w.Flush()
}

func countAlerts(s *model.Snapshot, sev model.Severity) int {
	n := 0
	for _, a := range s.Alerts {
		if a.Severity == sev {
			n++
		}
	}
	return n
}
