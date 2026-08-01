package wire

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type fakeMetaTransport struct {
	stream io.ReadWriteCloser
	h3     bool
}

func (f fakeMetaTransport) openMetaStream(ctx context.Context) (io.ReadWriteCloser, error) {
	return f.stream, nil
}
func (f fakeMetaTransport) writesH3FrameType() bool { return f.h3 }

// runFakeMetaServer reads the client's metadata request off the pipe and
// answers with respond(). Returns the parsed request frame via a channel.
func runFakeMetaServer(t *testing.T, server net.Conn, expectH3 bool, respond func(w io.Writer)) <-chan MetadataFrame {
	t.Helper()
	got := make(chan MetadataFrame, 1)
	go func() {
		defer server.Close()
		br := bufio.NewReader(server)
		if expectH3 {
			ft, _, err := ReadVarint(br)
			if err != nil || ft != H3FrameTypeK2Proxy {
				t.Errorf("H3 frame type: got %v err=%v", ft, err)
				return
			}
		}
		st, _, err := ReadStreamHeader(br)
		if err != nil || st != StreamTypeMeta {
			t.Errorf("stream header: type=%v err=%v", st, err)
			return
		}
		meta, err := ReadMetadataFrame(br)
		if err != nil {
			t.Errorf("read frame: %v", err)
			return
		}
		got <- meta
		respond(server)
	}()
	return got
}

func TestSendMetadata_SuccessWithCookie(t *testing.T) {
	client, server := net.Pipe()
	runFakeMetaServer(t, server, true, func(w io.Writer) {
		WriteMetadataResponse(w, "server-cookie")
	})
	client.SetReadDeadline(time.Now().Add(2 * time.Second))

	cookie, err := sendMetadata(context.Background(), fakeMetaTransport{stream: client, h3: true},
		MetadataFrame{UDID: "d", Token: "t"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cookie != "server-cookie" {
		t.Fatalf("cookie = %q, want server-cookie", cookie)
	}
}

// TestSendMetadata_OldServerBareOK: an old server answers a bare [0x00] —
// success, empty cookie, no error.
func TestSendMetadata_OldServerBareOK(t *testing.T) {
	client, server := net.Pipe()
	runFakeMetaServer(t, server, false, func(w io.Writer) {
		w.Write([]byte{0x00})
	})
	client.SetReadDeadline(time.Now().Add(2 * time.Second))

	cookie, err := sendMetadata(context.Background(), fakeMetaTransport{stream: client, h3: false},
		MetadataFrame{UDID: "d", Token: "t"})
	if err != nil || cookie != "" {
		t.Fatalf("cookie=%q err=%v, want \"\", nil", cookie, err)
	}
}

func TestSendMetadata_CodedRejectIsStreamError(t *testing.T) {
	client, server := net.Pipe()
	runFakeMetaServer(t, server, false, func(w io.Writer) {
		WriteStreamResponse(w, &StreamError{Code: 402, Message: "subscription expired"})
	})
	client.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, err := sendMetadata(context.Background(), fakeMetaTransport{stream: client, h3: false},
		MetadataFrame{UDID: "d", Token: "t"})
	var se *StreamError
	if !errors.As(err, &se) || se.Code != 402 {
		t.Fatalf("err = %v, want *StreamError{402}", err)
	}
	if !isMetadataReject(err) {
		t.Fatal("coded rejection must be a metadata reject")
	}
}

func TestSendMetadata_LegacyRejectIsErrStreamRejected(t *testing.T) {
	client, server := net.Pipe()
	runFakeMetaServer(t, server, false, func(w io.Writer) {
		WriteStreamResponse(w, errors.New("unauthorized"))
	})
	client.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, err := sendMetadata(context.Background(), fakeMetaTransport{stream: client, h3: false},
		MetadataFrame{UDID: "d", Token: "t"})
	if !errors.Is(err, ErrStreamRejected) {
		t.Fatalf("err = %v, want ErrStreamRejected", err)
	}
	if !isMetadataReject(err) {
		t.Fatal("legacy rejection must be a metadata reject")
	}
}

// TestSendMetadata_TransportErrorIsNotReject: a transport failure must never
// look like a rejection — the caller's best-effort branch depends on it.
func TestSendMetadata_TransportErrorIsNotReject(t *testing.T) {
	client, server := net.Pipe()
	server.Close() // every write fails
	_, err := sendMetadata(context.Background(), fakeMetaTransport{stream: client, h3: false},
		MetadataFrame{UDID: "d", Token: "t"})
	if err == nil {
		t.Fatal("want a transport error")
	}
	if isMetadataReject(err) {
		t.Fatalf("transport error misclassified as rejection: %v", err)
	}
}

// TestQUICClient_MetadataRejectFailsConnect: under enforce, Center's explicit
// 401 must fail connect() with the structured code — not be swallowed until a
// later DialTCP limps into a closed connection (spec §5.2 认证拒绝不再被吞掉).
func TestQUICClient_MetadataRejectFailsConnect(t *testing.T) {
	_, addr, cleanup := startRawQUICServer(t, vReject(401), true)
	defer cleanup()

	client := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port, UDID: "d", Token: "t"}, nil)
	client.tlsConfig = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.DialTCP(ctx, "example.com:80", nil)
	if err == nil {
		t.Fatal("connect must fail on explicit metadata rejection")
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("err = %v, want ErrMetadataRejected in chain", err)
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("err = %v, want embedded *StreamError{401}", err)
	}
}

// TestTCPWSClient_MetadataRejectFailsConnect — the TCP-WS twin.
func TestTCPWSClient_MetadataRejectFailsConnect(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "d", Token: "t"}
	client, _, cleanup := setupTCPWSEnforcePair(t, cfg, true, vReject(401))
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.DialTCP(ctx, "example.com:80", nil)
	if err == nil {
		t.Fatal("connect must fail on explicit metadata rejection")
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("err = %v, want ErrMetadataRejected in chain", err)
	}
	var se *StreamError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("err = %v, want embedded *StreamError{401}", err)
	}
}

// TestQUICClient_PermissiveCredentialProblemStillConnects: rollout guard — a
// permissive server always answers OK, so a client with bad credentials keeps
// connecting until enforce flips (spec §5.2 note).
func TestQUICClient_PermissiveCredentialProblemStillConnects(t *testing.T) {
	server, addr, cleanup := startRawQUICServer(t, vReject(401), false)
	defer cleanup()

	client := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port, UDID: "d", Token: "t"}, nil)
	client.tlsConfig = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}
	defer client.Close()

	// Drain AcceptTCP so the proxy stream completes server-side.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if conn, _, err := server.AcceptTCP(ctx); err == nil {
			WriteStreamResponse(conn, nil)
			conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatalf("permissive rollout must not fail connect on credential problems: %v", err)
	}
	conn.Close()
}

// --- Fast-follow to Task 5 review: legacy (0x01) metadata rejection must be
// proxy-layer-visible too. -------------------------------------------------
//
// The coded (0x02) form was already covered transitively — TestSendMetadata_
// CodedRejectIsStreamError proves *StreamError comes back, and *StreamError
// self-implements IsProxyLayer() (wire/stream.go), so it's automatically
// recognized by isProxyLayerError() even wrapped inside ErrMetadataRejected,
// with no extra wrapping needed at the connect() call site. The legacy form
// used a bare ErrStreamRejected sentinel (no IsProxyLayer()), so without the
// explicit &ProxyLayerError{...} wrap added at the quic.go/tcpws.go call
// sites, TransportManager's isProxyLayerError() would miss it and needlessly
// retry via TCP-WS against a server that will repeat the identical rejection.
//
// vReject(0) (AuthVerdict{OK:false, Code:0}, the "no opinion" state — see
// wire/auth.go's four-state doc) is what drives the server down the legacy
// branch: quic.go:1401 / tcpws.go:824 both branch on `verdict.Code > 0` for
// coded vs legacy, and 0 fails that check.

// TestQUICClient_LegacyMetadataRejectIsProxyLayerError proves a legacy-form
// (uncoded) rejection from QUICClient.connect() is recognized by
// isProxyLayerError() — the signal TransportManager uses to skip TCP-WS
// fallback (transport.go DialTCP/DialUDP).
func TestQUICClient_LegacyMetadataRejectIsProxyLayerError(t *testing.T) {
	_, addr, cleanup := startRawQUICServer(t, vReject(0), true)
	defer cleanup()

	client := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port, UDID: "d", Token: "t"}, nil)
	client.tlsConfig = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.DialTCP(ctx, "example.com:80", nil)
	if err == nil {
		t.Fatal("connect must fail on explicit legacy metadata rejection")
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("err = %v, want ErrMetadataRejected in chain", err)
	}
	if !errors.Is(err, ErrStreamRejected) {
		t.Fatalf("err = %v, want ErrStreamRejected (legacy form) in chain", err)
	}
	if !isProxyLayerError(err) {
		t.Fatalf("err = %v, want isProxyLayerError == true (TransportManager must skip TCP-WS fallback)", err)
	}
}

// TestTCPWSClient_LegacyMetadataRejectIsProxyLayerError — the TCP-WS twin.
func TestTCPWSClient_LegacyMetadataRejectIsProxyLayerError(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "d", Token: "t"}
	client, _, cleanup := setupTCPWSEnforcePair(t, cfg, true, vReject(0))
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := client.DialTCP(ctx, "example.com:80", nil)
	if err == nil {
		t.Fatal("connect must fail on explicit legacy metadata rejection")
	}
	if !errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("err = %v, want ErrMetadataRejected in chain", err)
	}
	if !errors.Is(err, ErrStreamRejected) {
		t.Fatalf("err = %v, want ErrStreamRejected (legacy form) in chain", err)
	}
	if !isProxyLayerError(err) {
		t.Fatalf("err = %v, want isProxyLayerError == true (TransportManager must skip TCP-WS fallback)", err)
	}
}
