package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildDataUserWithDevice_HasPassword(t *testing.T) {
	m := SetupMockDB(t)
	// Swap getDB so GetActiveSubscriptions (called inside buildDataUserWithDevice)
	// uses the mock instead of the real db.Get() which panics without a config.
	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		{"empty hash → false", "", false},
		{"hash set → true", "$2a$10$abcdef", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Expect the SELECT query from GetActiveSubscriptions and return empty.
			m.Mock.ExpectQuery(`SELECT`).WillReturnRows(m.Mock.NewRows(nil))
			u := &User{PasswordHash: tt.hash}
			got := buildDataUserWithDevice(u, nil)
			require.NotNil(t, got)
			assert.Equal(t, tt.expected, got.HasPassword)
		})
	}
}
