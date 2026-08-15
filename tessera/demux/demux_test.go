package demux

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
)

func listenUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc
}

func TestNewValidatesConfig(t *testing.T) {
	sock := listenUDP(t)
	front := sock.LocalAddr().(*net.UDPAddr)
	ok := func(d []byte) bool { return false }

	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"无 socket", Config{Front: front, Classify: ok}},
		{"无 front", Config{Conn: sock, Classify: ok}},
		{"无判别器", Config{Conn: sock, Front: front}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatal("应报错而非静默接受 —— 缺任何一项都会让节点无壳开服")
			}
		})
	}
}

// TestProbeFrontDetectsARealQUICServer is the startup gate of spec §4.6. It has
// to distinguish "a QUIC server is listening" from "a UDP socket exists", which
// is why the probe sends a packet and waits for an answer instead of dialing:
// a UDP dial to a dead port succeeds.
func TestProbeFrontDetectsARealQUICServer(t *testing.T) {
	t.Run("真 QUIC 服务端应通过", func(t *testing.T) {
		addr := serveQUIC(t)
		if err := ProbeFront(addr, 3*time.Second); err != nil {
			t.Fatalf("应探测成功: %v", err)
		}
	})

	t.Run("端口上没人应失败", func(t *testing.T) {
		// Bind a port then release it, so the address is well-formed but dead.
		sock, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		dead := sock.LocalAddr().(*net.UDPAddr)
		sock.Close()

		if err := ProbeFront(dead, 500*time.Millisecond); err == nil {
			t.Fatal("死端口应失败 —— 这正是 net.DialUDP 成功而探测必须失败的情形")
		}
	})

	t.Run("沉默的 UDP 服务应失败", func(t *testing.T) {
		silent := listenUDP(t)
		if err := ProbeFront(silent.LocalAddr().(*net.UDPAddr), 500*time.Millisecond); err == nil {
			t.Fatal("只开着 socket 不说 QUIC 的服务应失败")
		}
	})

	t.Run("回显非 QUIC 字节的服务应失败", func(t *testing.T) {
		echo := listenUDP(t)
		go func() {
			buf := make([]byte, 2048)
			for {
				n, addr, err := echo.ReadFromUDP(buf)
				if err != nil {
					return
				}
				echo.WriteToUDP([]byte("definitely not quic")[:min(n, 19)], addr)
			}
		}()
		if err := ProbeFront(echo.LocalAddr().(*net.UDPAddr), 2*time.Second); err == nil {
			t.Fatal("回显任意字节的服务不应被当成 QUIC front")
		}
	})
}

// serveQUIC starts a bare QUIC listener and returns its address.
func serveQUIC(t *testing.T) *net.UDPAddr {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "probe.invalid"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	sock := listenUDP(t)
	ln, err := quic.Listen(sock, &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		NextProtos:   []string{"h3"},
		MinVersion:   tls.VersionTLS13,
	}, &quic.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			if _, err := ln.Accept(context.Background()); err != nil {
				return
			}
		}
	}()
	return sock.LocalAddr().(*net.UDPAddr)
}

// TestRelayLimitRefusesRatherThanGrows pins the overload behaviour. Dropping is
// what a busy server does, so it costs no camouflage; growing without bound
// would let any stranger allocate node memory a socket at a time.
func TestRelayLimitRefusesRatherThanGrows(t *testing.T) {
	front := listenUDP(t)
	node := listenUDP(t)

	c, err := New(Config{
		Conn:      node,
		Front:     front.LocalAddr().(*net.UDPAddr),
		Classify:  func([]byte) bool { return false }, // everyone is a stranger
		MaxRelays: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	go func() {
		buf := make([]byte, 2048)
		for {
			if _, _, err := c.ReadFrom(buf); err != nil {
				return
			}
		}
	}()

	nodeAddr := node.LocalAddr().(*net.UDPAddr)
	for range 5 {
		sock, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer sock.Close()
		if _, err := sock.WriteToUDP([]byte("stranger"), nodeAddr); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for c.Stats().Relayed.Load()+c.Stats().Refused.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := c.Stats().Relayed.Load(); got != 2 {
		t.Errorf("转发流数 = %d，期望正好 2（MaxRelays）", got)
	}
	if got := c.Stats().Refused.Load(); got != 3 {
		t.Errorf("拒绝流数 = %d，期望 3", got)
	}
}
