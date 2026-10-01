package center

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClampOccurredAt(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		client time.Time
		want   time.Time
	}{
		{"past 1h kept", now.Add(-time.Hour), now.Add(-time.Hour)},
		{"future 1h clamped", now.Add(time.Hour), now},
		{"future 1min clamped", now.Add(time.Minute), now},
		{"future 1ns clamped", now.Add(time.Nanosecond), now},
		{"exactly now kept", now, now},
		{"exactly 7d kept", now.Add(-7 * 24 * time.Hour), now.Add(-7 * 24 * time.Hour)},
		{"past 8d clamped", now.Add(-8 * 24 * time.Hour), now},
		{"past 6d kept", now.Add(-6 * 24 * time.Hour), now.Add(-6 * 24 * time.Hour)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, tc.want.Equal(clampOccurredAt(tc.client, now)))
		})
	}
}
