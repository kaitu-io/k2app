package center

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// SSH keypair secret keys
const (
	SecretKeySSHPrivateKey = "node_ssh_private_key"
	SecretKeySSHPublicKey  = "node_ssh_public_key"
)

// SSHKeypair represents an SSH keypair for node installation
type SSHKeypair struct {
	PrivateKey string    // PEM encoded private key
	PublicKey  string    // OpenSSH format public key (ssh-ed25519 AAAA...)
	UpdatedAt  time.Time // Last updated time
}

// GetOrCreateSSHKeypair retrieves existing SSH keypair or creates a new one
func GetOrCreateSSHKeypair(ctx context.Context) (*SSHKeypair, error) {
	log.Infof(ctx, "getting or creating SSH keypair for node installation")

	// Try to get existing keypair
	keypair, err := getSSHKeypair(ctx)
	if err == nil {
		log.Infof(ctx, "found existing SSH keypair")
		return keypair, nil
	}

	// If not found, create new keypair
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Infof(ctx, "no SSH keypair found, generating new one")
		return generateAndSaveSSHKeypair(ctx)
	}

	log.Errorf(ctx, "failed to get SSH keypair: %v", err)
	return nil, fmt.Errorf("failed to get SSH keypair: %w", err)
}

// RotateSSHKeypair generates a new SSH keypair and replaces the existing one
func RotateSSHKeypair(ctx context.Context) (*SSHKeypair, error) {
	log.Infof(ctx, "rotating SSH keypair")
	return generateAndSaveSSHKeypair(ctx)
}

// getSSHKeypair retrieves the SSH keypair from database
func getSSHKeypair(ctx context.Context) (*SSHKeypair, error) {
	// Get private key
	updatedAt, privateKeyPEM, err := GetSecret(ctx, SecretKeySSHPrivateKey)
	if err != nil {
		return nil, err
	}

	// Get public key
	_, publicKey, err := GetSecret(ctx, SecretKeySSHPublicKey)
	if err != nil {
		return nil, err
	}

	return &SSHKeypair{
		PrivateKey: string(privateKeyPEM),
		PublicKey:  string(publicKey),
		UpdatedAt:  *updatedAt,
	}, nil
}

// generateAndSaveSSHKeypair generates a new Ed25519 SSH keypair and saves it
func generateAndSaveSSHKeypair(ctx context.Context) (*SSHKeypair, error) {
	log.Infof(ctx, "generating new Ed25519 SSH keypair")

	// Generate Ed25519 keypair
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Errorf(ctx, "failed to generate Ed25519 keypair: %v", err)
		return nil, fmt.Errorf("failed to generate Ed25519 keypair: %w", err)
	}

	// Convert to PEM format for private key
	privateKeyPEM, err := encodeEd25519PrivateKeyToPEM(privateKey)
	if err != nil {
		log.Errorf(ctx, "failed to encode private key to PEM: %v", err)
		return nil, fmt.Errorf("failed to encode private key: %w", err)
	}

	// Convert to OpenSSH format for public key
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		log.Errorf(ctx, "failed to create SSH public key: %v", err)
		return nil, fmt.Errorf("failed to create SSH public key: %w", err)
	}
	publicKeyStr := string(ssh.MarshalAuthorizedKey(sshPublicKey))

	// Save to database (encrypted)
	if err := SetSecret(ctx, SecretKeySSHPrivateKey, privateKeyPEM); err != nil {
		log.Errorf(ctx, "failed to save SSH private key: %v", err)
		return nil, fmt.Errorf("failed to save SSH private key: %w", err)
	}

	if err := SetSecret(ctx, SecretKeySSHPublicKey, []byte(publicKeyStr)); err != nil {
		log.Errorf(ctx, "failed to save SSH public key: %v", err)
		return nil, fmt.Errorf("failed to save SSH public key: %w", err)
	}

	log.Infof(ctx, "SSH keypair generated and saved successfully")

	return &SSHKeypair{
		PrivateKey: string(privateKeyPEM),
		PublicKey:  publicKeyStr,
		UpdatedAt:  time.Now(),
	}, nil
}

// encodeEd25519PrivateKeyToPEM encodes an Ed25519 private key to OpenSSH PEM format
func encodeEd25519PrivateKeyToPEM(privateKey ed25519.PrivateKey) ([]byte, error) {
	// OpenSSH format for Ed25519 private keys
	// Reference: https://github.com/openssh/openssh-portable/blob/master/PROTOCOL.key

	publicKey := privateKey.Public().(ed25519.PublicKey)

	// Random check bytes (must match in decryption)
	checkBytes := make([]byte, 4)
	if _, err := rand.Read(checkBytes); err != nil {
		return nil, err
	}

	// Build the private key section
	// uint32 check1, uint32 check2, string keytype, string pubkey, string privkey+pubkey, string comment
	privSection := []byte{}

	// checkint (repeated twice, must match)
	privSection = append(privSection, checkBytes...)
	privSection = append(privSection, checkBytes...)

	// keytype string "ssh-ed25519"
	keytype := "ssh-ed25519"
	privSection = appendString(privSection, keytype)

	// public key
	privSection = appendBytes(privSection, publicKey)

	// private key (64 bytes: 32 private + 32 public)
	privSection = appendBytes(privSection, privateKey)

	// comment (empty)
	privSection = appendString(privSection, "")

	// padding to block size (8 bytes for unencrypted)
	for i := 1; len(privSection)%8 != 0; i++ {
		privSection = append(privSection, byte(i))
	}

	// Build the full key blob
	// "openssh-key-v1\0" + ciphername + kdfname + kdfoptions + numkeys + pubkey + privkey
	blob := []byte("openssh-key-v1\x00")

	// cipher: "none" (unencrypted)
	blob = appendString(blob, "none")

	// kdf: "none"
	blob = appendString(blob, "none")

	// kdfoptions: empty string
	blob = appendBytes(blob, []byte{})

	// number of keys: 1
	blob = append(blob, 0, 0, 0, 1)

	// public key blob
	pubKeyBlob := []byte{}
	pubKeyBlob = appendString(pubKeyBlob, "ssh-ed25519")
	pubKeyBlob = appendBytes(pubKeyBlob, publicKey)
	blob = appendBytes(blob, pubKeyBlob)

	// private key section (length-prefixed)
	blob = appendBytes(blob, privSection)

	// Encode as PEM
	pemBlock := &pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: blob,
	}

	return pem.EncodeToMemory(pemBlock), nil
}

// appendString appends a length-prefixed string
func appendString(b []byte, s string) []byte {
	return appendBytes(b, []byte(s))
}

// appendBytes appends length-prefixed bytes
func appendBytes(b []byte, data []byte) []byte {
	// 4-byte big-endian length
	length := uint32(len(data))
	b = append(b, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
	return append(b, data...)
}

// GetSSHPublicKeyForDisplay returns the public key in display format
func GetSSHPublicKeyForDisplay(ctx context.Context) (string, error) {
	keypair, err := GetOrCreateSSHKeypair(ctx)
	if err != nil {
		return "", err
	}
	return keypair.PublicKey, nil
}

// GetSSHPrivateKeyBase64 returns the private key as base64 (for admin use only)
func GetSSHPrivateKeyBase64(ctx context.Context) (string, error) {
	keypair, err := GetOrCreateSSHKeypair(ctx)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString([]byte(keypair.PrivateKey)), nil
}
