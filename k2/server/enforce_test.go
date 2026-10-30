package server

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	smux "github.com/xtaci/smux/v2"
)

// TestServer_RejectsUnauthenticatedProxyStream: k2s has no permissive mode. A
// client that opens a proxy stream without first presenting metadata auth must
// not be served, whatever the config says.
func TestServer_RejectsUnauthenticatedProxyStream(t *testing.T) {
	srv, err := New(&config.ServerConfig{Listen: "127.0.0.1:0", Auth: testAuthConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	srv.handler.AllowPrivateIP = true

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		for {
			c, err := echoLn.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	// Dispatch accepted proxy streams to the handler, as Run() does; without
	// it a served stream would just sit unanswered and look like a rejection.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.acceptTCPLoop(ctx, srv.tcpwsSrv)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go srv.tcpwsSrv.HandleConn(serverConn)

	sess, err := smux.Client(clientConn, wire.DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := wire.WriteStreamHeader(stream, wire.StreamTypeTCP, echoLn.Addr().String()); err != nil {
		t.Fatal(err)
	}
	ok, _, _, err := wire.ReadStreamResponse(bufio.NewReader(stream))
	if err == nil && ok {
		t.Fatal("unauthenticated proxy stream was served; k2s must always enforce device auth")
	}
}

// TestProvisionedAuthValidator: zero-config `k2s run` admits exactly the
// auth.json credential in its connect URL and nothing else.
func TestProvisionedAuthValidator(t *testing.T) {
	v := provisionedAuthValidator(wire.AuthInfo{UDID: "u1", Token: "t1"})
	cases := []struct {
		token, udid string
		want        bool
	}{
		{"t1", "u1", true},
		{"t2", "u1", false},
		{"t1", "u2", false},
		{"", "", false},
		{"x", "y", false}, // the old accept-all admitted any two non-empty strings
	}
	for _, c := range cases {
		if got := v(c.token, c.udid, "").OK; got != c.want {
			t.Errorf("validator(%q,%q).OK = %v, want %v", c.token, c.udid, got, c.want)
		}
	}
	if empty := provisionedAuthValidator(wire.AuthInfo{}); empty("", "", "").OK {
		t.Error("an empty provisioned credential must admit nobody")
	}
}

// TestNew_CertDirWithoutAuthUsesProvisionedCredential: cert_dir alone (the
// zero-config path) must still start, with auth.json as the credential.
func TestNew_CertDirWithoutAuthUsesProvisionedCredential(t *testing.T) {
	srv, err := New(&config.ServerConfig{Listen: ":0", CertDir: t.TempDir()})
	if err != nil {
		t.Fatalf("cert_dir-only config must start: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	if srv.provisionedAuth == nil || srv.provisionedAuth.UDID == "" || srv.provisionedAuth.Token == "" {
		t.Fatal("cert_dir provisioning must record the auth.json credential")
	}
}
