// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"math"

	"github.com/riteshsonawane1372/gputop/internal/gpu"
	"github.com/riteshsonawane1372/gputop/internal/metric"
	"github.com/riteshsonawane1372/gputop/internal/model"
)

var nan = float32(math.NaN())

func of(o metric.Opt[float64], scale float64) float32 {
	if !o.OK {
		return nan
	}
	return float32(o.V * scale)
}

const mib = 1 << 20
const gib = 1 << 30

// Extract flattens a snapshot into per-series metric vectors.
func Extract(s *model.Snapshot) map[string][]float32 {
	out := make(map[string][]float32, len(s.GPUs)+1)
	procVRAM := map[gpu.ID]float64{}
	procVRAMKnown := map[gpu.ID]bool{}
	procCount := map[gpu.ID]float64{}
	for _, p := range s.Processes {
		procCount[p.DeviceID]++
		if p.MemUsed.OK {
			procVRAM[p.DeviceID] += float64(p.MemUsed.V)
			procVRAMKnown[p.DeviceID] = true
		}
	}
	for i := range s.GPUs {
		g := &s.GPUs[i]
		v := make([]float32, numGPUMetrics)
		for j := range v {
			v[j] = nan
		}
		if g.Available {
			sm := g.Sample
			v[Util] = of(sm.UtilPercent, 1)
			v[MemBandwidth] = of(sm.MemBandwidthPercent, 1)
			v[VRAMPercent] = of(g.Derived.VRAMFraction, 100)
			v[VRAMUsedGiB] = of(metric.Float(sm.MemUsed), 1.0/gib)
			v[PowerW] = of(sm.PowerW, 1)
			v[TempC] = of(sm.TempC, 1)
			v[MemTempC] = of(sm.MemTempC, 1)
			v[ClockCoreMHz] = of(sm.ClockCoreMHz, 1)
			v[ClockMemMHz] = of(sm.ClockMemMHz, 1)
			v[PCIeTxMBps] = of(sm.PCIeTxBps, 1.0/mib)
			v[PCIeRxMBps] = of(sm.PCIeRxBps, 1.0/mib)
			v[NVLinkTxMBps] = of(g.Derived.NVLinkTxBps, 1.0/mib)
			v[NVLinkRxMBps] = of(g.Derived.NVLinkRxBps, 1.0/mib)
			v[EncoderPct] = of(sm.EncoderPercent, 1)
			v[DecoderPct] = of(sm.DecoderPercent, 1)
			v[FanPct] = of(sm.FanPct, 1)
			if procVRAMKnown[g.Device.ID] {
				v[ProcVRAMGiB] = float32(procVRAM[g.Device.ID] / gib)
			}
			v[ProcCount] = float32(procCount[g.Device.ID])
			if sm.Throttle.OK {
				v[ThrottleMask] = float32(sm.Throttle.V)
			}
		}
		v[HealthScore] = float32(g.Health.Score)
		out[string(g.Device.ID)] = v
	}
	if h := s.Host; h != nil {
		v := make([]float32, numHostMetrics)
		for j := range v {
			v[j] = nan
		}
		v[slot(HostCPU)] = of(h.CPU.UtilPercent, 1)
		v[slot(HostMemPercent)] = of(h.Memory.UsedFraction(), 100)
		v[slot(HostLoad1)] = of(h.CPU.Load1, 1)
		var rx, tx float64
		netOK := false
		for _, n := range h.Net {
			if n.Kind == "loopback" || n.Kind == "virtual" {
				continue
			}
			if n.RxBps.OK {
				rx += n.RxBps.V
				netOK = true
			}
			tx += n.TxBps.Or(0)
		}
		if netOK {
			v[slot(HostNetRxMBps)], v[slot(HostNetTxMBps)] = float32(rx/mib), float32(tx/mib)
		}
		var rd, wr float64
		diskOK := false
		for _, d := range h.IO {
			if d.ReadBps.OK {
				rd += d.ReadBps.V
				wr += d.WriteBps.Or(0)
				diskOK = true
			}
		}
		if diskOK {
			v[slot(HostDiskReadMBps)], v[slot(HostDiskWriteMBps)] = float32(rd/mib), float32(wr/mib)
		}
		out[HostKey] = v
	}
	return out
}
