package wire

import (
	"bufio"
	"bytes"
	"testing"
)

func TestMetadataFrame_CookieRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	in := MetadataFrame{UDID: "d", Token: "t", Mode: "gateway", Version: "0.5.0", Cookie: "abc123"}
	if err := WriteMetadataFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadMetadataFrame(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("roundtrip: got %+v want %+v", out, in)
	}
}

// TestMetadataFrame_EmptyCookieOmitted: an empty cookie must not appear on the
// wire (cold auth is signalled by absence, and old servers must see the exact
// pre-cookie frame shape).
func TestMetadataFrame_EmptyCookieOmitted(t *testing.T) {
	var with, without bytes.Buffer
	if err := WriteMetadataFrame(&with, MetadataFrame{UDID: "d", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadataFrame(&without, MetadataFrame{UDID: "d", Token: "t", Cookie: ""}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(with.Bytes(), without.Bytes()) {
		t.Fatal("empty cookie changed the wire encoding")
	}
	if bytes.Contains(with.Bytes(), []byte("cookie")) {
		t.Fatal("cookie key emitted for empty cookie")
	}
}

func TestWriteMetadataResponse_WithCookie(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMetadataResponse(&buf, "ck-1", false); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(&buf)
	ok, code, msg, err := ReadStreamResponse(br) // old-client view first: a plain 0x00 status
	if err != nil || !ok || code != 0 || msg != "" {
		t.Fatalf("status: ok=%v code=%d msg=%q err=%v", ok, code, msg, err)
	}
	resp, err := ReadMetadataResponse(br)
	cookie := resp.Cookie
	if err != nil || cookie != "ck-1" {
		t.Fatalf("cookie=%q err=%v, want \"ck-1\", nil", cookie, err)
	}
}

func TestWriteMetadataResponse_EmptyCookieIsEmptyKV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMetadataResponse(&buf, "", false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), []byte{0x00, 0x00}) { // [status 0x00][field_count 0]
		t.Fatalf("wire bytes = %x, want 0000", buf.Bytes())
	}
	br := bufio.NewReader(&buf)
	if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
		t.Fatal("status parse failed")
	}
	resp, err := ReadMetadataResponse(br)
	cookie := resp.Cookie
	if err != nil || cookie != "" {
		t.Fatalf("cookie=%q err=%v, want \"\", nil", cookie, err)
	}
}

// TestReadMetadataResponse_BareOKFromOldServer: an old server writes exactly
// [0x00] and closes. After ReadStreamResponse consumes the status byte there
// is nothing left — ReadMetadataResponse must return ("", nil), not an error.
func TestReadMetadataResponse_BareOKFromOldServer(t *testing.T) {
	br := bufio.NewReader(bytes.NewReader([]byte{0x00}))
	if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
		t.Fatal("status parse failed")
	}
	resp, err := ReadMetadataResponse(br)
	cookie := resp.Cookie
	if err != nil || cookie != "" {
		t.Fatalf("old-server bare OK: cookie=%q err=%v, want \"\", nil", cookie, err)
	}
}

// TestReadMetadataResponse_MalformedKVIsError: once the field count parsed,
// a truncated kv is a protocol error (distinguishes a broken new server from
// an old server that sent nothing).
func TestReadMetadataResponse_MalformedKVIsError(t *testing.T) {
	// field_count=1 then truncated key.
	br := bufio.NewReader(bytes.NewReader([]byte{0x01, 0x06, 'c', 'o'}))
	if _, err := ReadMetadataResponse(br); err == nil {
		t.Fatal("truncated kv must return an error")
	}
}
