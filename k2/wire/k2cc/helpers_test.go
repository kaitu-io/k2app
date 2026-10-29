package k2cc

import "time"

// mockRTTStatsProvider is a mock implementation of RTT stats for testing
type mockRTTStatsProvider struct {
	smoothedRTT time.Duration
}

func (m *mockRTTStatsProvider) SmoothedRTT() time.Duration   { return m.smoothedRTT }
func (m *mockRTTStatsProvider) LatestRTT() time.Duration     { return m.smoothedRTT }
func (m *mockRTTStatsProvider) MinRTT() time.Duration        { return m.smoothedRTT }
func (m *mockRTTStatsProvider) MaxAckDelay() time.Duration   { return 25 * time.Millisecond }
func (m *mockRTTStatsProvider) MeanDeviation() time.Duration { return m.smoothedRTT / 4 }

func (m *mockRTTStatsProvider) PTO(includeMaxAckDelay bool) time.Duration {
	rttVar := m.smoothedRTT / 4
	if rttVar < time.Millisecond {
		rttVar = time.Millisecond
	}
	pto := m.smoothedRTT + 4*rttVar
	if includeMaxAckDelay {
		pto += 25 * time.Millisecond
	}
	return pto
}

func (m *mockRTTStatsProvider) UpdateRTT(sendDelta, ackDelay time.Duration) {
	m.smoothedRTT = sendDelta
}

func (m *mockRTTStatsProvider) SetMaxAckDelay(mad time.Duration) {}
func (m *mockRTTStatsProvider) SetInitialRTT(t time.Duration)    { m.smoothedRTT = t }
