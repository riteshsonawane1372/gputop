// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"math"

	"github.com/riteshsonawane1372/gputop/internal/model"
)

// liveCap is the number of recent samples kept for live charts (at the
// default 1s refresh, 10 minutes).
const liveCap = 600

// ring is a fixed-capacity float series.
type ring struct {
	buf  [liveCap]float64
	head int
	n    int
}

func (r *ring) push(v float64) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % liveCap
	if r.n < liveCap {
		r.n++
	}
}

// last returns up to n most recent values, oldest first.
func (r *ring) last(n int) []float64 {
	if n <= 0 || n > r.n {
		n = r.n
	}
	out := make([]float64, n)
	start := (r.head - n + liveCap) % liveCap
	for i := 0; i < n; i++ {
		out[i] = r.buf[(start+i)%liveCap]
	}
	return out
}

// live keeps short in-memory series for charts independent of the history
// store (so charts work with history disabled and for remote sources).
type live struct {
	series  map[string]*ring
	lastSeq uint64
}

func newLive() *live { return &live{series: map[string]*ring{}} }

func (l *live) get(key string) *ring {
	r := l.series[key]
	if r == nil {
		r = &ring{}
		l.series[key] = r
	}
	return r
}

func (l *live) values(key string, n int) []float64 {
	if r := l.series[key]; r != nil {
		return r.last(n)
	}
	return nil
}

func val(ok bool, v float64) float64 {
	if !ok {
		return math.NaN()
	}
	return v
}

func (l *live) observe(s *model.Snapshot) {
	if s == nil || !s.Ready || (s.Seq != 0 && s.Seq == l.lastSeq) {
		return
	}
	l.lastSeq = s.Seq
	for _, g := range s.GPUs {
		id := string(g.Device.ID)
		sm := g.Sample
		avail := g.Available
		l.get("util/" + id).push(val(avail && sm.UtilPercent.OK, sm.UtilPercent.V))
		l.get("vram/" + id).push(val(avail && g.Derived.VRAMFraction.OK, g.Derived.VRAMFraction.V*100))
		l.get("power/" + id).push(val(avail && sm.PowerW.OK, sm.PowerW.V))
		l.get("temp/" + id).push(val(avail && sm.TempC.OK, sm.TempC.V))
		l.get("membw/" + id).push(val(avail && sm.MemBandwidthPercent.OK, sm.MemBandwidthPercent.V))
		l.get("pcietx/" + id).push(val(avail && sm.PCIeTxBps.OK, sm.PCIeTxBps.V))
		l.get("pcierx/" + id).push(val(avail && sm.PCIeRxBps.OK, sm.PCIeRxBps.V))
		l.get("nvltx/" + id).push(val(avail && g.Derived.NVLinkTxBps.OK, g.Derived.NVLinkTxBps.V))
		l.get("nvlrx/" + id).push(val(avail && g.Derived.NVLinkRxBps.OK, g.Derived.NVLinkRxBps.V))
		l.get("clock/" + id).push(val(avail && sm.ClockCoreMHz.OK, sm.ClockCoreMHz.V))
		l.get("fbused/" + id).push(val(avail && sm.MemUsed.OK, float64(sm.MemUsed.V)/(1<<30)))
		l.get("memtemp/" + id).push(val(avail && sm.MemTempC.OK, sm.MemTempC.V))
		l.get("enc/" + id).push(val(avail && sm.EncoderPercent.OK, sm.EncoderPercent.V))
		l.get("dec/" + id).push(val(avail && sm.DecoderPercent.OK, sm.DecoderPercent.V))
		l.get("fan/" + id).push(val(avail && sm.FanPct.OK, sm.FanPct.V))
	}
	for _, sv := range s.Inference {
		key := "inf/" + sv.URL + "/"
		mt := sv.Metrics
		l.get(key + "ttft50").push(val(sv.Up && mt.TTFT.P50.OK, mt.TTFT.P50.V))
		l.get(key + "ttft99").push(val(sv.Up && mt.TTFT.P99.OK, mt.TTFT.P99.V))
		l.get(key + "gen").push(val(sv.Up && mt.GenTokensPerSec.OK, mt.GenTokensPerSec.V))
		l.get(key + "kv").push(val(sv.Up && mt.KVCacheUsage.OK, mt.KVCacheUsage.V*100))
		l.get(key + "run").push(val(sv.Up && mt.Running.OK, mt.Running.V))
		l.get(key + "wait").push(val(sv.Up && mt.Waiting.OK, mt.Waiting.V))
	}
	f := s.Fleet
	l.get("fleet/util").push(val(f.UtilAvg.OK, f.UtilAvg.V))
	l.get("fleet/power").push(val(f.PowerW.OK, f.PowerW.V))
	l.get("fleet/tempmax").push(val(f.TempMaxC.OK, f.TempMaxC.V))
	l.get("fleet/vram").push(val(f.VRAMFraction.OK, f.VRAMFraction.V*100))
	if h := s.Host; h != nil {
		l.get("host/cpu").push(val(h.CPU.UtilPercent.OK, h.CPU.UtilPercent.V))
		l.get("host/mem").push(val(h.Memory.UsedFraction().OK, h.Memory.UsedFraction().V*100))
		var rx, tx float64
		ok := false
		for _, n := range h.Net {
			if n.Kind == "loopback" || n.Kind == "virtual" {
				continue
			}
			if n.RxBps.OK {
				rx += n.RxBps.V
				ok = true
			}
			tx += n.TxBps.Or(0)
		}
		l.get("host/netrx").push(val(ok, rx))
		l.get("host/nettx").push(val(ok, tx))
	}
}
