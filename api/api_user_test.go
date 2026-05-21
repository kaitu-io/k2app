package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDataUserWithDevice_HasPassword(t *testing.T) {
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
			u := &User{PasswordHash: tt.hash}
			got := buildDataUserWithDevice(u, nil)
			require.NotNil(t, got)
			assert.Equal(t, tt.expected, got.HasPassword)
		})
	}
}
