package k2p

import (
	"bytes"
	"io"
	"testing"
)

func TestWriteReadAuthFrame(t *testing.T) {
	secret := "my-super-secret"
	var buf bytes.Buffer

	if err := WriteAuthFrame(&buf, secret); err != nil {
		t.Fatalf("WriteAuthFrame: %v", err)
	}

	got, err := ReadAuthFrame(&buf)
	if err != nil {
		t.Fatalf("ReadAuthFrame: %v", err)
	}
	if got != secret {
		t.Errorf("got %q, want %q", got, secret)
	}
}

func TestReadAuthFrame_BadVersion(t *testing.T) {
	// Construct frame with version 0x02.
	var buf bytes.Buffer
	buf.WriteByte(0x02)
	buf.WriteByte(0x00)
	buf.WriteByte(0x03)
	buf.WriteString("abc")

	_, err := ReadAuthFrame(&buf)
	if err == nil {
		t.Fatal("expected error for bad version, got nil")
	}
}

func TestHashVerifySecret(t *testing.T) {
	password := "test-password"

	hash, err := HashSecret(password)
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}

	if !VerifySecret(password, hash) {
		t.Error("VerifySecret returned false for correct password")
	}
	if VerifySecret("wrong-password", hash) {
		t.Error("VerifySecret returned true for wrong password")
	}
}

func TestWriteAuthFrame_EmptySecret(t *testing.T) {
	var buf bytes.Buffer

	if err := WriteAuthFrame(&buf, ""); err != nil {
		t.Fatalf("WriteAuthFrame empty: %v", err)
	}

	got, err := ReadAuthFrame(&buf)
	if err != nil {
		t.Fatalf("ReadAuthFrame: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestWriteAuthFrame_SecretTooLong(t *testing.T) {
	secret := string(make([]byte, 65536)) // 65536 bytes > max 65535
	var buf bytes.Buffer
	err := WriteAuthFrame(&buf, secret)
	if err == nil {
		t.Fatal("expected error for secret > 65535 bytes, got nil")
	}
}

func TestReadAuthFrame_Truncated(t *testing.T) {
	// Only write header, no body.
	var buf bytes.Buffer
	buf.WriteByte(authVersion)
	buf.WriteByte(0x00)
	buf.WriteByte(0x05) // claims 5 bytes but writes none

	_, err := ReadAuthFrame(&buf)
	if err == nil {
		t.Fatal("expected error for truncated frame, got nil")
	}
	if err != io.ErrUnexpectedEOF && err.Error() == "" {
		t.Logf("got expected error: %v", err)
	}
}
