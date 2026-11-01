package center

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func linearSamples(n int, intercept, slope float64) []capacitySample {
	out := make([]capacitySample, 0, n)
	for i := 0; i < n; i++ {
		mbps := float64(i) * 100 / float64(n-1)
		out = append(out, capacitySample{Mbps: mbps, BusyPct: intercept + slope*mbps})
	}
	return out
}

func TestFitNodeCapacity_Linear(t *testing.T) {
	m, used, ok := fitNodeCapacity(linearSamples(60, 10, 0.5))
	require.True(t, ok)
	assert.InDelta(t, 140, m, 0.5)
	assert.Equal(t, 60, used)
}

func TestFitNodeCapacity_TooFewBusySamples(t *testing.T) {
	s := []capacitySample{}
	for i := 0; i < 29; i++ {
		s = append(s, capacitySample{Mbps: 20 + float64(i), BusyPct: 10 + 0.5*(20+float64(i))})
	}
	for i := 0; i < 20; i++ {
		s = append(s, capacitySample{Mbps: float64(i), BusyPct: 10 + 0.5*float64(i)})
	}
	_, _, ok := fitNodeCapacity(s)
	assert.False(t, ok)
}

func TestFitNodeCapacity_NoBusySamples(t *testing.T) {
	s := []capacitySample{}
	for i := 0; i < 50; i++ {
		s = append(s, capacitySample{Mbps: 5, BusyPct: 10})
	}
	_, _, ok := fitNodeCapacity(s)
	assert.False(t, ok)
}

func TestFitNodeCapacity_ExcludesHighSteal(t *testing.T) {
	base := linearSamples(60, 10, 0.5)
	m0, _, ok := fitNodeCapacity(base)
	require.True(t, ok)
	with := append([]capacitySample{}, base...)
	with = append(with,
		capacitySample{Mbps: 50, BusyPct: 95, StealPct: 6},
		capacitySample{Mbps: 90, BusyPct: 5, StealPct: 20},
		capacitySample{Mbps: 400, BusyPct: 5, StealPct: 5.1},
	)
	m1, used, ok := fitNodeCapacity(with)
	require.True(t, ok)
	assert.Equal(t, 60, used)
	assert.InDelta(t, m0, m1, 1e-9)
}

func TestFitNodeCapacity_NonPositiveSlope(t *testing.T) {
	s := []capacitySample{}
	for i := 0; i < 40; i++ {
		s = append(s, capacitySample{Mbps: 20 + float64(i), BusyPct: 60 - 0.2*float64(i)})
	}
	_, _, ok := fitNodeCapacity(s)
	assert.False(t, ok)
}

func TestFitNodeCapacity_CappedAtFiveTimesPeak(t *testing.T) {
	s := []capacitySample{}
	for i := 0; i < 40; i++ {
		mbps := 20 + float64(i)*10/39 // peak 30
		s = append(s, capacitySample{Mbps: mbps, BusyPct: 5 + 0.001*mbps})
	}
	m, _, ok := fitNodeCapacity(s)
	require.True(t, ok)
	assert.InDelta(t, 150, m, 1e-9)
}

func TestFitNodeCapacity_FloorAtPeak(t *testing.T) {
	// busy = 70 + 2*Mbps => M=5, but observed peak is 100
	m, _, ok := fitNodeCapacity(linearSamples(60, 70, 2))
	require.True(t, ok)
	assert.InDelta(t, 100, m, 1e-9)
}

func TestRawPressure_TakesMax(t *testing.T) {
	assert.InDelta(t, 0.5, rawPressure(50, 100, 20, 50), 1e-9)
	assert.InDelta(t, 0.9, rawPressure(10, 100, 72, 50), 1e-9)
}

func TestRawPressure_NormalMemoryIgnored(t *testing.T) {
	assert.InDelta(t, 0.1, rawPressure(10, 100, 5, 75), 1e-9)
	assert.InDelta(t, 0.5, rawPressure(10, 100, 5, 87.5), 1e-9)
	assert.InDelta(t, 1.0, rawPressure(10, 100, 5, 95), 1e-9)
}

func TestRawPressure_NoCapacity(t *testing.T) {
	assert.InDelta(t, 0.25, rawPressure(500, 0, 20, 50), 1e-9)
	assert.InDelta(t, 0.5, rawPressure(500, 0, 20, 87.5), 1e-9)
}

func TestNextPressure_EWMA(t *testing.T) {
	assert.InDelta(t, 0.65, nextPressure(0.5, 1000, 1, 1060), 1e-9)
}

func TestNextPressure_StaleResets(t *testing.T) {
	assert.InDelta(t, 1.0, nextPressure(0.5, 1000, 1, 1601), 1e-9)
	assert.InDelta(t, 1.0, nextPressure(0.5, 0, 1, 1000), 1e-9)
}

func TestPressureFresh(t *testing.T) {
	assert.True(t, pressureFresh(&NodeUsage{PressureAt: 1000}, 1600))
	assert.False(t, pressureFresh(&NodeUsage{PressureAt: 1000}, 1601))
	assert.False(t, pressureFresh(&NodeUsage{}, 1000))
	assert.False(t, pressureFresh(nil, 1000))
}

func TestMedianFloat(t *testing.T) {
	_, ok := medianFloat(nil)
	assert.False(t, ok)
	v, ok := medianFloat([]float64{3, 1, 2})
	assert.True(t, ok)
	assert.Equal(t, 2.0, v)
	v, _ = medianFloat([]float64{4, 1, 2, 3})
	assert.Equal(t, 2.5, v)
}

func TestQuantizePressure(t *testing.T) {
	cases := map[float64]float64{0.02: 0, 0.03: 0, 0.05: 0.05, 0.15: 0.15, 0.149: 0.10, 1: 1, 0.3: 0.3, 0.35: 0.35}
	for in, want := range cases {
		got := quantizePressure(in)
		assert.True(t, math.Abs(got-want) < 1e-9, "in=%v got=%v want=%v", in, got, want)
	}
}
