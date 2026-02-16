package center

import (
	"crypto/ed25519"
	"encoding/pem"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestEncodeEd25519PrivateKeyToPEM(t *testing.T) {
	// Generate a test keypair
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("failed to generate keypair: %v", err)
	}

	// Encode to PEM
	pemBytes, err := encodeEd25519PrivateKeyToPEM(privateKey)
	if err != nil {
		t.Fatalf("failed to encode private key to PEM: %v", err)
	}

	// Verify it's valid PEM
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}

	if block.Type != "OPENSSH PRIVATE KEY" {
		t.Errorf("expected PEM type 'OPENSSH PRIVATE KEY', got '%s'", block.Type)
	}

	// Try to parse it with ssh library
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("failed to parse private key with ssh library: %v", err)
	}

	// Verify the public key matches
	parsedPubKey := signer.PublicKey()
	sshPubKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatalf("failed to create SSH public key: %v", err)
	}

	if string(parsedPubKey.Marshal()) != string(sshPubKey.Marshal()) {
		t.Error("parsed public key doesn't match original")
	}

	t.Logf("Successfully generated and parsed Ed25519 SSH keypair")
	t.Logf("Public key: %s", ssh.MarshalAuthorizedKey(sshPubKey))
}

func TestAppendString(t *testing.T) {
	b := []byte{}
	b = appendString(b, "test")

	expected := []byte{0, 0, 0, 4, 't', 'e', 's', 't'}
	if len(b) != len(expected) {
		t.Errorf("expected length %d, got %d", len(expected), len(b))
	}

	for i := range expected {
		if b[i] != expected[i] {
			t.Errorf("byte %d: expected %d, got %d", i, expected[i], b[i])
		}
	}
}

func TestAppendBytes(t *testing.T) {
	b := []byte{}
	data := []byte{1, 2, 3}
	b = appendBytes(b, data)

	expected := []byte{0, 0, 0, 3, 1, 2, 3}
	if len(b) != len(expected) {
		t.Errorf("expected length %d, got %d", len(expected), len(b))
	}

	for i := range expected {
		if b[i] != expected[i] {
			t.Errorf("byte %d: expected %d, got %d", i, expected[i], b[i])
		}
	}
}
