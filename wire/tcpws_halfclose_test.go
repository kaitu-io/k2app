package wire

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// TCP-WS half-close (smux K2 patch 2) is enabled per session by the
// "smux_hc" metadata key: the client offers it in the request, the server
// in the response, and each side then lets the other's first FIN leave the
// stream writable.

func TestMetadataFrame_SmuxHalfCloseRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	in := MetadataFrame{UDID: "d", Token: "t", SmuxHalfClose: true}
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

// Absent capability is absent on the wire: old servers see the same frame.
func TestMetadataFrame_SmuxHalfCloseOmittedWhenFalse(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteMetadataFrame(&buf, MetadataFrame{UDID: "d", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("smux_hc")) {
		t.Fatal("smux_hc emitted without the capability")
	}
}

func TestWriteMetadataResponse_SmuxHalfClose(t *testing.T) {
	for _, hc := range []bool{false, true} {
		var buf bytes.Buffer
		if err := WriteMetadataResponse(&buf, "ck", hc); err != nil {
			t.Fatal(err)
		}
		br := bufio.NewReader(&buf)
		if ok, _, _, err := ReadStreamResponse(br); err != nil || !ok {
			t.Fatalf("hc=%v: status ok=%v err=%v", hc, ok, err)
		}
		resp, err := ReadMetadataResponse(br)
		if err != nil || resp.Cookie != "ck" || resp.SmuxHalfClose != hc {
			t.Fatalf("hc=%v: got %+v err=%v", hc, resp, err)
		}
	}
}

type halfCloser interface{ CloseWrite() error }

func tcpwsHalfClosePayload() []byte {
	return bytes.Repeat([]byte("0123456789abcdef"), (1<<20)/16) // > smux initial window
}

// Request/response: the client half-closes after its request and still
// receives a response larger than one window. Before the patch the client's
// CloseWrite was a full Close and the response was dropped.
func TestTCPWS_HalfClose_ResponseAfterClientFIN(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()
	want := tcpwsHalfClosePayload()

	served := make(chan error, 1)
	go func() {
		conn, _, err := server.AcceptTCP(context.Background())
		if err != nil {
			served <- err
			return
		}
		defer conn.Close()
		if err := WriteStreamResponse(conn, nil); err != nil {
			served <- err
			return
		}
		req, err := io.ReadAll(conn) // ends at the client's FIN
		if err != nil || string(req) != "req" {
			served <- io.ErrUnexpectedEOF
			return
		}
		_, err = conn.Write(want)
		served <- err
	}()

	conn, err := client.DialTCP(context.Background(), "example.com:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("req")); err != nil {
		t.Fatal(err)
	}
	if err := conn.(halfCloser).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("response after CloseWrite: %d bytes, err=%v; want %d", len(got), err, len(want))
	}
	if err := <-served; err != nil {
		t.Fatalf("server: %v", err)
	}
}

// Upload after the server half-closes: the client keeps writing past one
// window, which needs the server's capability from the metadata response.
func TestTCPWS_HalfClose_UploadAfterServerFIN(t *testing.T) {
	client, server, cleanup := setupTCPWSPair(t)
	defer cleanup()
	want := tcpwsHalfClosePayload()

	uploaded := make(chan []byte, 1)
	go func() {
		conn, _, err := server.AcceptTCP(context.Background())
		if err != nil {
			uploaded <- nil
			return
		}
		defer conn.Close()
		if err := WriteStreamResponse(conn, nil); err != nil {
			uploaded <- nil
			return
		}
		conn.(halfCloser).CloseWrite()
		b, _ := io.ReadAll(conn)
		uploaded <- b
	}()

	conn, err := client.DialTCP(context.Background(), "example.com:80", nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if b, err := io.ReadAll(conn); err != nil || len(b) != 0 {
		t.Fatalf("waiting for server FIN: %d bytes, err=%v", len(b), err)
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if n, err := conn.Write(want); err != nil || n != len(want) {
		t.Fatalf("upload after server FIN: n=%d err=%v; want %d", n, err, len(want))
	}
	conn.Close()
	select {
	case got := <-uploaded:
		if !bytes.Equal(got, want) {
			t.Fatalf("server received %d bytes; want %d", len(got), len(want))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server upload read timed out")
	}
}
