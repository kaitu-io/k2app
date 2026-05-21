package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		userInputs []string
		wantErr    string
	}{
		{"too short — under 10", "Pass1234", nil, "password_too_short"},
		{"exactly 10 chars — boundary", "k7N#mq2P!x", nil, ""},
		{"common dictionary word", "Password12", nil, "password_too_weak"},
		{"keyboard pattern", "qwerty1234A", nil, "password_too_weak"},
		{"repeated chars", "aaaaaaaaaaa1A", nil, "password_too_weak"},
		{"contains user email local part", "alice12345!", []string{"alice@example.com"}, "password_too_weak"},
		{"strong passphrase ok", "correct horse battery staple", nil, ""},
		{"random strong ok", "k7N#mq2P!xT9", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidatePasswordStrength(tt.password, tt.userInputs)
			assert.Equal(t, tt.wantErr, got)
		})
	}
}

func TestUserPasswordHashAndVerify(t *testing.T) {
	password := "k7N#mq2P!xT9"

	hash, err := UserPasswordHash(password)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)

	assert.True(t, UserPasswordVerify(password, hash), "verify should succeed for correct password")
	assert.False(t, UserPasswordVerify("wrongpassword", hash), "verify should fail for wrong password")
}
