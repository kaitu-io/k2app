package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.4.2", "0.4.1", 1},
		{"0.4.1", "0.4.2", -1},
		{"0.4.2", "0.4.2", 0},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1},
		// Pre-release suffix ignored
		{"0.4.2-beta.1", "0.4.2", 0},
		{"0.4.2-beta.1", "0.4.1", 1},
		// Malformed input returns 0 (no filtering)
		{"", "0.4.2", 0},
		{"0.4.2", "", 0},
		{"invalid", "0.4.2", 0},
		{"0.4", "0.4.2", 0},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.want, compareVersions(tt.a, tt.b))
		})
	}
}

func TestIsVersionInRange(t *testing.T) {
	tests := []struct {
		version, minV, maxV string
		want                bool
	}{
		{"0.4.2", "", "", true},           // no constraints
		{"0.4.2", "0.4.2", "", true},      // exact min
		{"0.4.2", "0.4.3", "", false},     // below min
		{"0.4.2", "", "0.4.2", true},      // exact max
		{"0.4.3", "", "0.4.2", false},     // above max
		{"0.4.2", "0.4.1", "0.4.3", true}, // in range
		{"", "0.4.1", "0.4.3", true},      // empty version = no filtering
		{"invalid", "0.4.1", "", true},     // malformed = no filtering
	}
	for _, tt := range tests {
		t.Run(tt.version+"_in_"+tt.minV+"_"+tt.maxV, func(t *testing.T) {
			assert.Equal(t, tt.want, isVersionInRange(tt.version, tt.minV, tt.maxV))
		})
	}
}
