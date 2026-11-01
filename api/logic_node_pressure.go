package center

import (
	"math"
	"sort"
)

const (
	pressureCPUCeiling   = 80.0
	pressureEWMAAlpha    = 0.3
	pressureStaleSeconds = 600
	pressureBucket       = 0.05

	capacityMinBusyMbps  = 20.0
	capacityMinSamples   = 30
	capacityMaxStealPct  = 5.0
	capacityPeakMultiple = 5.0
)

// capacitySample is one metrics point used to fit a node's throughput capacity.
type capacitySample struct{ Mbps, BusyPct, StealPct float64 }

// fitNodeCapacity fits BusyPct = b + k*Mbps by least squares over samples whose
// steal is within bounds, and returns M = (80-b)/k clamped to [peak, 5*peak],
// where peak is the max Mbps among the kept samples. used is the number of kept
// samples. ok=false when fewer than capacityMinSamples kept samples carry
// Mbps >= capacityMinBusyMbps, or the slope is not positive.
func fitNodeCapacity(samples []capacitySample) (capacityMbps float64, used int, ok bool) {
	var n, sx, sy, sxx, sxy, peak float64
	busy := 0
	for _, s := range samples {
		if s.StealPct > capacityMaxStealPct {
			continue
		}
		used++
		n++
		sx += s.Mbps
		sy += s.BusyPct
		sxx += s.Mbps * s.Mbps
		sxy += s.Mbps * s.BusyPct
		if s.Mbps > peak {
			peak = s.Mbps
		}
		if s.Mbps >= capacityMinBusyMbps {
			busy++
		}
	}
	if busy < capacityMinSamples {
		return 0, used, false
	}
	den := n*sxx - sx*sx
	if den <= 0 {
		return 0, used, false
	}
	k := (n*sxy - sx*sy) / den
	if k <= 0 {
		return 0, used, false
	}
	b := (sy - k*sx) / n
	m := (pressureCPUCeiling - b) / k
	m = math.Max(m, peak)
	m = math.Min(m, peak*capacityPeakMultiple)
	return m, used, true
}

// medianFloat returns the median of values; ok=false when empty.
func medianFloat(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	mid := len(v) / 2
	if len(v)%2 == 1 {
		return v[mid], true
	}
	return (v[mid-1] + v[mid]) / 2, true
}

// rawPressure is the max of throughput/capacity, CPU busy/ceiling and a memory
// term (0 below 80%, 1 at 95%), clamped to [0,1]. The throughput term is
// skipped when capacityMbps <= 0.
func rawPressure(mbps, capacityMbps, busyPct, memPct float64) float64 {
	p := busyPct / pressureCPUCeiling
	if capacityMbps > 0 {
		p = math.Max(p, mbps/capacityMbps)
	}
	memTerm := math.Min(math.Max((memPct-80)/15, 0), 1)
	p = math.Max(p, memTerm)
	return math.Min(math.Max(p, 0), 1)
}

// nextPressure smooths raw with an EWMA against prev, unless prev is missing or
// older than pressureStaleSeconds, in which case raw is taken directly.
func nextPressure(prev float64, prevAt int64, raw float64, now int64) float64 {
	if prevAt <= 0 || now-prevAt > pressureStaleSeconds {
		return raw
	}
	return pressureEWMAAlpha*raw + (1-pressureEWMAAlpha)*prev
}

// quantizePressure floors p to a multiple of pressureBucket; the epsilon keeps
// values like 0.15 from falling into the bucket below due to float error.
func quantizePressure(p float64) float64 {
	buckets := math.Floor(p/pressureBucket + 1e-9)
	return buckets / math.Round(1/pressureBucket)
}

// pressureFresh reports whether u carries a pressure value newer than the
// stale window.
func pressureFresh(u *NodeUsage, now int64) bool {
	return u != nil && u.PressureAt > 0 && now-u.PressureAt <= pressureStaleSeconds
}
