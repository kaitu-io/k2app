package center

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"net"
	"testing"
)

func TestValidateSSLIPDomain_IPv4(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		wantIP     string
		wantErr    bool
	}{
		{
			name:    "valid IPv4 sslip.io",
			domain:  "203-0-113-50.sslip.io",
			wantIP:  "203.0.113.50",
			wantErr: false,
		},
		{
			name:    "valid IPv4 nip.io",
			domain:  "192-168-1-1.nip.io",
			wantIP:  "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "invalid domain format",
			domain:  "example.com",
			wantErr: true,
		},
		{
			name:    "invalid IP in domain",
			domain:  "999-999-999-999.sslip.io",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := ValidateSSLIPDomain(tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSSLIPDomain(%q) expected error, got nil", tt.domain)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateSSLIPDomain(%q) unexpected error: %v", tt.domain, err)
				return
			}
			if ip.String() != tt.wantIP {
				t.Errorf("ValidateSSLIPDomain(%q) = %v, want %v", tt.domain, ip, tt.wantIP)
			}
		})
	}
}

func TestValidateSSLIPDomain_IPv6(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantIP  string
		wantErr bool
	}{
		{
			name:    "valid IPv6 with double dash",
			domain:  "2001-db8--1.sslip.io",
			wantIP:  "2001:db8::1",
			wantErr: false,
		},
		{
			name:    "valid IPv6 full",
			domain:  "2001-0db8-0000-0000-0000-0000-0000-0001.sslip.io",
			wantIP:  "2001:db8::1",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := ValidateSSLIPDomain(tt.domain)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateSSLIPDomain(%q) expected error, got nil", tt.domain)
				}
				return
			}
			if err != nil {
				t.Errorf("ValidateSSLIPDomain(%q) unexpected error: %v", tt.domain, err)
				return
			}
			expectedIP := net.ParseIP(tt.wantIP)
			if !ip.Equal(expectedIP) {
				t.Errorf("ValidateSSLIPDomain(%q) = %v, want %v", tt.domain, ip, tt.wantIP)
			}
		})
	}
}

func TestParsePublicKey_ECDSA(t *testing.T) {
	// Generate an ECDSA key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ECDSA key: %v", err)
	}

	// Encode public key to PKIX format
	pubKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to marshal public key: %v", err)
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyDER)

	// Parse the public key
	parsedKey, keyType, err := ParsePublicKey(pubKeyB64)
	if err != nil {
		t.Fatalf("ParsePublicKey failed: %v", err)
	}

	if keyType != "ecdsa" {
		t.Errorf("Expected key type 'ecdsa', got %q", keyType)
	}

	// Verify the parsed key matches the original
	ecdsaKey, ok := parsedKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("Parsed key is not ECDSA")
	}

	if !ecdsaKey.Equal(&privateKey.PublicKey) {
		t.Error("Parsed public key does not match original")
	}
}

func TestGenerateChallenge(t *testing.T) {
	challenge, err := GenerateChallenge()
	if err != nil {
		t.Fatalf("GenerateChallenge failed: %v", err)
	}

	// Check nonce length (32 bytes = 64 hex chars)
	if len(challenge.Nonce) != 64 {
		t.Errorf("Nonce length = %d, want 64 hex chars", len(challenge.Nonce))
	}

	// Check timestamp is recent
	if challenge.Timestamp == 0 {
		t.Error("Timestamp should not be zero")
	}

	// Check expiry is after creation
	if challenge.ExpiresAt <= challenge.Timestamp {
		t.Error("ExpiresAt should be after Timestamp")
	}
}

func TestVerifySignature_ECDSA(t *testing.T) {
	// Generate an ECDSA key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ECDSA key: %v", err)
	}

	// Generate a challenge
	challenge, err := GenerateChallenge()
	if err != nil {
		t.Fatalf("GenerateChallenge failed: %v", err)
	}

	// Decode nonce and sign it
	nonceBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		nonceBytes[i] = hexCharToByte(challenge.Nonce[i*2])<<4 | hexCharToByte(challenge.Nonce[i*2+1])
	}

	hash := sha256.Sum256(nonceBytes)
	sig, err := ecdsa.SignASN1(rand.Reader, privateKey, hash[:])
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	signatureB64 := base64.StdEncoding.EncodeToString(sig)

	// Verify signature
	err = VerifySignature(&privateKey.PublicKey, challenge.Nonce, signatureB64)
	if err != nil {
		t.Errorf("VerifySignature failed: %v", err)
	}
}

func TestVerifySignature_Invalid(t *testing.T) {
	// Generate an ECDSA key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate ECDSA key: %v", err)
	}

	// Generate a challenge
	challenge, err := GenerateChallenge()
	if err != nil {
		t.Fatalf("GenerateChallenge failed: %v", err)
	}

	// Create an invalid signature (random bytes)
	invalidSig := make([]byte, 64)
	rand.Read(invalidSig)
	invalidSigB64 := base64.StdEncoding.EncodeToString(invalidSig)

	// Verify should fail
	err = VerifySignature(&privateKey.PublicKey, challenge.Nonce, invalidSigB64)
	if err == nil {
		t.Error("VerifySignature should fail with invalid signature")
	}
}

func TestCSRRequestStore(t *testing.T) {
	store := &CSRRequestStore{
		requests: make(map[string]*CSRRequest),
	}

	// Create a test request
	req := &CSRRequest{
		ID:      "test-id-123",
		Domains: []string{"1-2-3-4.sslip.io"},
	}

	// Store
	store.Store(req)

	// Get
	retrieved, found := store.Get("test-id-123")
	if !found {
		t.Error("Request not found after Store")
	}
	if retrieved.ID != req.ID {
		t.Errorf("Retrieved ID = %q, want %q", retrieved.ID, req.ID)
	}

	// Delete
	store.Delete("test-id-123")
	_, found = store.Get("test-id-123")
	if found {
		t.Error("Request should not be found after Delete")
	}
}

// hexCharToByte converts a hex character to its byte value
func hexCharToByte(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}
