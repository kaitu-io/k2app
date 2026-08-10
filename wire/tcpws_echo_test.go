package wire

import (
	"bufio"
	"bytes"
	"testing"
)

func TestHandleEcho_ReflectsNonce(t *testing.T) {
	nonce := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	var out bytes.Buffer
	br := bufio.NewReader(bytes.NewReader(nonce))
	handleEcho(&out, br)
	if !bytes.Equal(out.Bytes(), nonce) {
		t.Fatalf("echo reflected %v, want %v", out.Bytes(), nonce)
	}
}
