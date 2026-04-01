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

func TestFilterAnnouncementsForClient(t *testing.T) {
	// Simulate DB result already sorted by priority DESC, id DESC
	announcements := []Announcement{
		{ID: 3, Message: "OTT promo", Priority: 10, MinVersion: "0.4.2", MaxVersion: ""},
		{ID: 2, Message: "Maintenance", Priority: 5, MinVersion: "", MaxVersion: ""},
		{ID: 1, Message: "Old v0.3 only", Priority: 0, MinVersion: "", MaxVersion: "0.3.99"},
	}

	t.Run("no version filter — returns all", func(t *testing.T) {
		result := filterAnnouncementsForClient(announcements, "")
		assert.Len(t, result, 3)
		assert.Equal(t, "3", result[0].ID)
		assert.Equal(t, "2", result[1].ID)
		assert.Equal(t, "1", result[2].ID)
	})

	t.Run("v0.4.2 — filters out old v0.3-only", func(t *testing.T) {
		result := filterAnnouncementsForClient(announcements, "0.4.2")
		assert.Len(t, result, 2)
		assert.Equal(t, "3", result[0].ID) // OTT promo (priority 10, minVersion 0.4.2)
		assert.Equal(t, "2", result[1].ID) // Maintenance (priority 5, no version constraint)
	})

	t.Run("v0.3.0 — filters out OTT promo (needs 0.4.2+)", func(t *testing.T) {
		result := filterAnnouncementsForClient(announcements, "0.3.0")
		assert.Len(t, result, 2)
		assert.Equal(t, "2", result[0].ID) // Maintenance
		assert.Equal(t, "1", result[1].ID) // Old v0.3 only
	})

	t.Run("v0.4.0 — above maxVersion 0.3.99, below minVersion 0.4.2", func(t *testing.T) {
		result := filterAnnouncementsForClient(announcements, "0.4.0")
		assert.Len(t, result, 1)
		assert.Equal(t, "2", result[0].ID) // Only maintenance (no constraints)
	})

	t.Run("empty input — returns nil", func(t *testing.T) {
		result := filterAnnouncementsForClient(nil, "0.4.2")
		assert.Nil(t, result)
	})

	t.Run("field mapping correctness", func(t *testing.T) {
		input := []Announcement{{
			ID: 42, Message: "test", LinkURL: "https://kaitu.io", LinkText: "click",
			OpenMode: "webview", AuthMode: "ott", Priority: 5,
			MinVersion: "0.4.0", MaxVersion: "0.5.0", ExpiresAt: 1234567890,
		}}
		result := filterAnnouncementsForClient(input, "0.4.2")
		assert.Len(t, result, 1)
		r := result[0]
		assert.Equal(t, "42", r.ID)
		assert.Equal(t, "test", r.Message)
		assert.Equal(t, "https://kaitu.io", r.LinkURL)
		assert.Equal(t, "click", r.LinkText)
		assert.Equal(t, "webview", r.OpenMode)
		assert.Equal(t, "ott", r.AuthMode)
		assert.Equal(t, 5, r.Priority)
		assert.Equal(t, "0.4.0", r.MinVersion)
		assert.Equal(t, "0.5.0", r.MaxVersion)
		assert.Equal(t, int64(1234567890), r.ExpiresAt)
	})
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
