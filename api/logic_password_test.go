package center

import (
	"testing"
)

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  string
	}{
		{"valid password", "Password123", ""},
		{"too short", "Pass1", "password_too_short"},
		{"no letter", "12345678", "password_needs_letter"},
		{"no number", "Password", "password_needs_number"},
		{"exactly 8 chars", "Pass1234", ""},
		{"special chars ok", "Pass123!", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidatePasswordStrength(tt.password)
			if got != tt.wantErr {
				t.Errorf("ValidatePasswordStrength(%q) = %q, want %q", tt.password, got, tt.wantErr)
			}
		})
	}
}

func TestUserPasswordHashAndVerify(t *testing.T) {
	password := "TestPassword123"

	hash, err := UserPasswordHash(password)
	if err != nil {
		t.Fatalf("UserPasswordHash failed: %v", err)
	}

	if hash == "" {
		t.Error("UserPasswordHash returned empty hash")
	}

	if !UserPasswordVerify(password, hash) {
		t.Error("UserPasswordVerify failed for correct password")
	}

	if UserPasswordVerify("wrongpassword", hash) {
		t.Error("UserPasswordVerify succeeded for wrong password")
	}
}
