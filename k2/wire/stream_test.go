package wire

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
)

// ============================================================================
// QUIC Varint Encoding (RFC 9000 §16)
// ============================================================================

func TestVarintEncode(t *testing.T) {
	tests := []struct {
		name string
		val  uint64
		want []byte
	}{
		{"zero", 0, []byte{0x00}},
		{"one", 1, []byte{0x01}},
		{"max 1-byte", 63, []byte{0x3f}},
		{"min 2-byte", 64, []byte{0x40, 0x40}},
		{"port 443", 443, []byte{0x41, 0xbb}},
		{"max 2-byte", 16383, []byte{0x7f, 0xff}},
		{"min 4-byte", 16384, []byte{0x80, 0x00, 0x40, 0x00}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AppendVarint(nil, tt.val)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("AppendVarint(%d) = %x, want %x", tt.val, got, tt.want)
			}
		})
	}
}

func TestVarintDecode(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		want    uint64
		wantN   int
		wantErr bool
	}{
		{"zero", []byte{0x00}, 0, 1, false},
		{"one", []byte{0x01}, 1, 1, false},
		{"max 1-byte", []byte{0x3f}, 63, 1, false},
		{"min 2-byte", []byte{0x40, 0x40}, 64, 2, false},
		{"port 443", []byte{0x41, 0xbb}, 443, 2, false},
		{"empty", []byte{}, 0, 0, true},
		{"truncated 2-byte", []byte{0x40}, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bytes.NewReader(tt.data)
			got, n, err := ReadVarint(r)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ReadVarint = %d, want %d", got, tt.want)
			}
			if n != tt.wantN {
				t.Errorf("ReadVarint consumed %d bytes, want %d", n, tt.wantN)
			}
		})
	}
}

func TestVarintRoundtrip(t *testing.T) {
	values := []uint64{0, 1, 63, 64, 255, 443, 16383, 16384, 65535, 100000}
	for _, v := range values {
		encoded := AppendVarint(nil, v)
		r := bytes.NewReader(encoded)
		decoded, _, err := ReadVarint(r)
		if err != nil {
			t.Fatalf("roundtrip %d: %v", v, err)
		}
		if decoded != v {
			t.Errorf("roundtrip: encoded %d, decoded %d", v, decoded)
		}
	}
}

// ============================================================================
// Stream Header (TCP/UDP)
// ============================================================================

func TestWriteStreamHeader(t *testing.T) {
	var buf bytes.Buffer
	addr := "1.2.3.4:443"

	err := WriteStreamHeader(&buf, StreamTypeTCP, addr)
	if err != nil {
		t.Fatalf("WriteStreamHeader: %v", err)
	}

	data := buf.Bytes()
	// type byte
	if data[0] != 0x01 {
		t.Errorf("type = 0x%02x, want 0x01", data[0])
	}
	// addr_len is varint encoding of len("1.2.3.4:443") = 11
	if data[1] != 11 {
		t.Errorf("addr_len = %d, want 11", data[1])
	}
	// addr
	if string(data[2:13]) != addr {
		t.Errorf("addr = %q, want %q", string(data[2:13]), addr)
	}
}

func TestReadStreamHeader(t *testing.T) {
	var buf bytes.Buffer
	addr := "192.168.1.1:8080"
	WriteStreamHeader(&buf, StreamTypeUDP, addr)

	typ, gotAddr, err := ReadStreamHeader(&buf)
	if err != nil {
		t.Fatalf("ReadStreamHeader: %v", err)
	}
	if typ != StreamTypeUDP {
		t.Errorf("type = %d, want %d", typ, StreamTypeUDP)
	}
	if gotAddr != addr {
		t.Errorf("addr = %q, want %q", gotAddr, addr)
	}
}

func TestStreamHeaderRoundtrip(t *testing.T) {
	addrs := []string{
		"1.2.3.4:443",
		"192.168.0.1:8080",
		"[2001:db8::1]:443",
		"10.0.0.1:53",
	}

	for _, addr := range addrs {
		t.Run(addr, func(t *testing.T) {
			var buf bytes.Buffer
			WriteStreamHeader(&buf, StreamTypeTCP, addr)

			typ, gotAddr, err := ReadStreamHeader(&buf)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if typ != StreamTypeTCP {
				t.Errorf("type = %d, want TCP", typ)
			}
			if gotAddr != addr {
				t.Errorf("addr = %q, want %q", gotAddr, addr)
			}
		})
	}
}

// ============================================================================
// TCP Stream Response
// ============================================================================

func TestWriteReadStreamResponseOK(t *testing.T) {
	var buf bytes.Buffer
	WriteStreamResponse(&buf, nil) // success

	ok, _, errMsg, err := ReadStreamResponse(&buf)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if !ok {
		t.Error("expected ok=true")
	}
	if errMsg != "" {
		t.Errorf("errMsg = %q, want empty", errMsg)
	}
}

func TestWriteReadStreamResponseError(t *testing.T) {
	var buf bytes.Buffer
	WriteStreamResponse(&buf, net.UnknownNetworkError("connection refused"))

	ok, _, errMsg, err := ReadStreamResponse(&buf)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
}

// ============================================================================
// StreamConn wrapper
// ============================================================================

func TestStreamConn_ReadWrite(t *testing.T) {
	// Simulate a bidirectional stream with a pipe
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)

	// Client goroutine: send header, read response, then send data
	go func() {
		if err := WriteStreamHeader(client, StreamTypeTCP, "1.2.3.4:443"); err != nil {
			errCh <- err
			return
		}
		// Read response before sending data (avoids net.Pipe deadlock)
		clientBuf := bufio.NewReader(client)
		ok, _, _, err := ReadStreamResponse(clientBuf)
		if err != nil {
			errCh <- err
			return
		}
		if !ok {
			errCh <- io.ErrUnexpectedEOF
			return
		}
		_, err = client.Write([]byte("hello"))
		errCh <- err
	}()

	// Server side: read header
	serverBuf := bufio.NewReader(server)
	typ, addr, err := ReadStreamHeader(serverBuf)
	if err != nil {
		t.Fatalf("ReadStreamHeader: %v", err)
	}
	if typ != StreamTypeTCP || addr != "1.2.3.4:443" {
		t.Fatalf("unexpected header: type=%d addr=%q", typ, addr)
	}

	// Server side: send OK response
	if err := WriteStreamResponse(server, nil); err != nil {
		t.Fatalf("WriteStreamResponse: %v", err)
	}

	// Read data
	buf := make([]byte, 10)
	n, err := serverBuf.Read(buf)
	if err != nil {
		t.Fatalf("Read data: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("data = %q, want %q", string(buf[:n]), "hello")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("client error: %v", err)
	}
}

// ============================================================================
// StreamError 0x02 wire format
// ============================================================================

func TestWriteReadStreamResponse_StreamError(t *testing.T) {
	// 0x02 coded format roundtrip: code preserved
	var buf bytes.Buffer
	WriteStreamResponse(&buf, &StreamError{Code: 403, Message: "blocked: reserved IP"})

	ok, errCode, errMsg, err := ReadStreamResponse(&buf)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
	if errCode != 403 {
		t.Errorf("errCode = %d, want 403", errCode)
	}
	if errMsg != "blocked: reserved IP" {
		t.Errorf("errMsg = %q, want %q", errMsg, "blocked: reserved IP")
	}
}

func TestWriteReadStreamResponse_LegacyCode0(t *testing.T) {
	// Legacy 0x01 format: code should be 0
	var buf bytes.Buffer
	WriteStreamResponse(&buf, net.UnknownNetworkError("dial failed"))

	ok, errCode, errMsg, err := ReadStreamResponse(&buf)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
	if errCode != 0 {
		t.Errorf("errCode = %d, want 0 for legacy format", errCode)
	}
	if errMsg == "" {
		t.Error("expected non-empty errMsg")
	}
}

func TestReadStreamResponse_UnknownStatus(t *testing.T) {
	// Old client reading 0x02 is handled, but 0x03 should fail gracefully
	buf := bytes.NewReader([]byte{0x03})
	_, _, _, err := ReadStreamResponse(buf)
	if err == nil {
		t.Fatal("expected error for unknown status byte")
	}
}

func TestStreamErrorCodes_Roundtrip(t *testing.T) {
	// Test various server error codes through the wire format
	codes := []int{400, 401, 402, 403, 502, 503}
	for _, code := range codes {
		var buf bytes.Buffer
		WriteStreamResponse(&buf, &StreamError{Code: code, Message: "test"})

		ok, gotCode, _, err := ReadStreamResponse(&buf)
		if err != nil {
			t.Fatalf("code %d: ReadStreamResponse: %v", code, err)
		}
		if ok {
			t.Errorf("code %d: expected ok=false", code)
		}
		if gotCode != code {
			t.Errorf("code %d: got code %d", code, gotCode)
		}
	}
}

// ============================================================================
// H3 Custom Frame (0x4B32)
// ============================================================================

func TestWriteH3ProxyFrameType(t *testing.T) {
	var buf bytes.Buffer
	WriteH3ProxyFrameType(&buf)

	data := buf.Bytes()
	// 0x4B32 in varint encoding = 2 bytes (14-bit value: 0x4B32)
	r := bytes.NewReader(data)
	val, _, err := ReadVarint(r)
	if err != nil {
		t.Fatalf("ReadVarint: %v", err)
	}
	if val != H3FrameTypeK2Proxy {
		t.Errorf("frame type = 0x%x, want 0x%x", val, H3FrameTypeK2Proxy)
	}
}

func TestReadH3ProxyStream(t *testing.T) {
	// Simulate a complete H3 proxy stream:
	// [frame_type:varint=0x4B32][type:1][addr_len:varint][addr]
	var buf bytes.Buffer
	WriteH3ProxyFrameType(&buf)
	WriteStreamHeader(&buf, StreamTypeTCP, "8.8.8.8:53")

	// Read back
	r := bytes.NewReader(buf.Bytes())

	// Read frame type
	ft, _, err := ReadVarint(r)
	if err != nil {
		t.Fatalf("ReadVarint frame type: %v", err)
	}
	if ft != H3FrameTypeK2Proxy {
		t.Fatalf("frame type = 0x%x, want 0x4B32", ft)
	}

	// Read stream header
	typ, addr, err := ReadStreamHeader(r)
	if err != nil {
		t.Fatalf("ReadStreamHeader: %v", err)
	}
	if typ != StreamTypeTCP || addr != "8.8.8.8:53" {
		t.Errorf("got type=%d addr=%q", typ, addr)
	}

	// Should be at EOF
	_, err = r.ReadByte()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

// ============================================================================
// MetadataFrame Round-Trip
// ============================================================================

func TestMetadataFrame_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		meta MetadataFrame
	}{
		{
			name: "full frame with mode",
			meta: MetadataFrame{UDID: "abc123", Token: "jwt-token-here", Mode: "gateway"},
		},
		{
			name: "no mode (normal client)",
			meta: MetadataFrame{UDID: "device1", Token: "secret"},
		},
		{
			name: "normal client with version",
			meta: MetadataFrame{UDID: "device1", Token: "secret", Version: "0.4.8"},
		},
		{
			name: "full frame with mode and version",
			meta: MetadataFrame{UDID: "abc", Token: "jwt", Mode: "gateway", Version: "1.2.3"},
		},
		{
			name: "empty fields",
			meta: MetadataFrame{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteMetadataFrame(&buf, tt.meta); err != nil {
				t.Fatalf("WriteMetadataFrame: %v", err)
			}

			br := bufio.NewReader(&buf)
			got, err := ReadMetadataFrame(br)
			if err != nil {
				t.Fatalf("ReadMetadataFrame: %v", err)
			}

			if got.UDID != tt.meta.UDID {
				t.Errorf("UDID = %q, want %q", got.UDID, tt.meta.UDID)
			}
			if got.Token != tt.meta.Token {
				t.Errorf("Token = %q, want %q", got.Token, tt.meta.Token)
			}
			if got.Mode != tt.meta.Mode {
				t.Errorf("Mode = %q, want %q", got.Mode, tt.meta.Mode)
			}
			if got.Version != tt.meta.Version {
				t.Errorf("Version = %q, want %q", got.Version, tt.meta.Version)
			}
		})
	}
}

// TestMetadataFrame_ForwardCompat verifies an old reader (one that doesn't know
// the "version" key) tolerates a frame carrying it — the kv format skips unknown
// keys. This pins the backward-compatibility guarantee for the phased rollout:
// a new client sending version must never break an un-upgraded node.
func TestMetadataFrame_ForwardCompat(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMetadataFrame(&buf, MetadataFrame{UDID: "u", Token: "t", Version: "9.9.9"}); err != nil {
		t.Fatalf("WriteMetadataFrame: %v", err)
	}
	// Simulate an old reader: parse the same wire bytes but only read udid/token.
	br := bufio.NewReader(&buf)
	fieldCount, _, err := ReadVarint(br)
	if err != nil {
		t.Fatalf("read field count: %v", err)
	}
	seen := map[string]string{}
	for i := uint64(0); i < fieldCount; i++ {
		kl, _, _ := ReadVarint(br)
		kb := make([]byte, kl)
		io.ReadFull(br, kb)
		vl, _, _ := ReadVarint(br)
		vb := make([]byte, vl)
		io.ReadFull(br, vb)
		seen[string(kb)] = string(vb)
	}
	if seen["udid"] != "u" || seen["token"] != "t" {
		t.Errorf("old reader failed to extract udid/token: %v", seen)
	}
	if seen["version"] != "9.9.9" {
		t.Errorf("version field not present on wire: %v", seen)
	}
}
