// Measurement for spec.md §8.2 — traffic shape, the gap left after §6.
//
// Every other exposure in this design is a one-shot construction problem: build
// the ClientHello right, put the credential in the right field, pad the
// certificate. Shape is not. It leaks for as long as the connection lives, and
// no amount of care at handshake time touches it.
//
// The question this file answers is narrow and deliberately so: on the features
// a real-time classifier can actually compute, how far apart is "someone
// browsing the front" from "someone using the front's address as a tunnel"?
//
// It does NOT try to model VPN traffic. Both sides here are real TLS sessions
// to the same real host, differing only in usage pattern — which is exactly the
// variable under test. Modelling the payload would have made the model the
// finding, which is the mistake §13.4 already cost us once.
package spike

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Capture
// ---------------------------------------------------------------------------

// flowEvent is one TLS record as an on-path observer sees it: when, which way,
// how big. Nothing about contents — that is the whole point.
type flowEvent struct {
	at time.Duration
	up bool
	n  int
}

// flowRecorder wraps the TCP conn UNDER the TLS layer, so sizes are ciphertext
// record sizes, which is what an observer measures.
type flowRecorder struct {
	net.Conn
	start  time.Time
	mu     sync.Mutex
	events []flowEvent
}

func (f *flowRecorder) Read(p []byte) (int, error) {
	n, err := f.Conn.Read(p)
	if n > 0 {
		f.mu.Lock()
		f.events = append(f.events, flowEvent{time.Since(f.start), false, n})
		f.mu.Unlock()
	}
	return n, err
}

func (f *flowRecorder) Write(p []byte) (int, error) {
	n, err := f.Conn.Write(p)
	if n > 0 {
		f.mu.Lock()
		f.events = append(f.events, flowEvent{time.Since(f.start), true, n})
		f.mu.Unlock()
	}
	return n, err
}

func (f *flowRecorder) snapshot() []flowEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]flowEvent{}, f.events...)
}

// ---------------------------------------------------------------------------
// Features an on-path classifier can compute in real time
// ---------------------------------------------------------------------------

type shape struct {
	duration   time.Duration
	bytesUp    int
	bytesDown  int
	records    int
	maxIdle    time.Duration // longest gap with no traffic either way
	activeFrac float64       // fraction of 1-second buckets carrying anything
}

func shapeOf(events []flowEvent, total time.Duration) shape {
	var s shape
	s.duration = total
	s.records = len(events)

	buckets := map[int]bool{}
	var prev time.Duration
	for _, e := range events {
		if e.up {
			s.bytesUp += e.n
		} else {
			s.bytesDown += e.n
		}
		if gap := e.at - prev; gap > s.maxIdle {
			s.maxIdle = gap
		}
		prev = e.at
		buckets[int(e.at/time.Second)] = true
	}
	if gap := total - prev; gap > s.maxIdle {
		s.maxIdle = gap
	}
	if secs := int(total/time.Second) + 1; secs > 0 {
		s.activeFrac = float64(len(buckets)) / float64(secs)
	}
	return s
}

func (s shape) String() string {
	return fmt.Sprintf("时长=%-6v 上行=%-8d 下行=%-9d 记录数=%-5d 最长静默=%-7v 活跃占比=%.2f",
		s.duration.Round(time.Second), s.bytesUp, s.bytesDown, s.records,
		s.maxIdle.Round(100*time.Millisecond), s.activeFrac)
}

// ---------------------------------------------------------------------------
// Workloads
// ---------------------------------------------------------------------------

// plan describes a usage pattern. Both patterns run over ONE TLS connection to
// the SAME host, so every difference in the resulting shape comes from usage.
type plan struct {
	name     string
	requests int           // how many HTTP requests over the session
	think    time.Duration // pause between them
	hold     time.Duration // keep the connection open, idle, afterwards
}

// pageLoad is what a person visiting the front looks like: fetch, then the
// connection sits idle until it is reaped.
func pageLoad(hold time.Duration) plan {
	return plan{name: "浏览 front（取一次页面后闲置）", requests: 1, hold: hold}
}

// sustained is what a tunnel's outer connection looks like in aggregate:
// traffic keeps arriving for as long as the user is doing anything at all.
func sustained(n int, think time.Duration) plan {
	return plan{name: "隧道式（持续多次往返）", requests: n, think: think}
}

func runPlan(t *testing.T, addr, sni, path string, p plan) (shape, error) {
	t.Helper()

	raw, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return shape{}, err
	}
	rec := &flowRecorder{Conn: raw, start: time.Now()}
	defer rec.Close()

	conn := tls.Client(rec, &tls.Config{
		ServerName: sni,
		NextProtos: []string{"http/1.1"},
		MinVersion: tls.VersionTLS12,
		// Local runs use a self-signed front; network runs verify normally.
		InsecureSkipVerify: strings.HasPrefix(addr, "127.0.0.1"),
	})
	if err := conn.HandshakeContext(t.Context()); err != nil {
		return shape{}, err
	}

	br := bufio.NewReader(conn)
	for i := 0; i < p.requests; i++ {
		if i > 0 && p.think > 0 {
			time.Sleep(p.think)
		}
		req := "GET " + path + " HTTP/1.1\r\nHost: " + sni +
			"\r\nUser-Agent: Mozilla/5.0\r\nAccept: */*\r\nConnection: keep-alive\r\n\r\n"
		if _, err := conn.Write([]byte(req)); err != nil {
			return shape{}, err
		}
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			return shape{}, err
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			resp.Body.Close()
			return shape{}, err
		}
		resp.Body.Close()
	}
	if p.hold > 0 {
		time.Sleep(p.hold)
	}
	return shapeOf(rec.snapshot(), time.Since(rec.start)), nil
}

// ---------------------------------------------------------------------------
// Local front, so the pipeline is testable without the network
// ---------------------------------------------------------------------------

func localFront(t *testing.T, bodySize int) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{namedCert(t, "front.invalid")},
		NextProtos:   []string{"http/1.1"},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	body := strings.Repeat("x", bodySize)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					if _, err := http.ReadRequest(br); err != nil {
						return
					}
					fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

// TestShapePipeline exercises capture and features against a local front. It
// proves the instrument works; it proves nothing about real traffic, because
// both sides here are driven by this file.
func TestShapePipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("long: drives the full shaping pipeline against a local front")
	}
	addr := localFront(t, 120*1024)

	browse, err := runPlan(t, addr, "front.invalid", "/", pageLoad(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := runPlan(t, addr, "front.invalid", "/", sustained(6, 500*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("浏览:  %s", browse)
	t.Logf("隧道:  %s", tunnel)

	if browse.maxIdle <= tunnel.maxIdle {
		t.Fatalf("闲置期应产生更长的静默: browse=%v tunnel=%v", browse.maxIdle, tunnel.maxIdle)
	}
	if tunnel.bytesDown <= browse.bytesDown {
		t.Fatalf("持续使用应产生更多下行: browse=%d tunnel=%d", browse.bytesDown, tunnel.bytesDown)
	}
}

// TestRealFrontShapeAxis measures the axis, NOT the separability. The
// distinction is the same one §13.4 cost us: a number is only a finding if it
// is compared against the right null, and here the right null is "the
// distribution of real user sessions to this front" — which we have no vantage
// point to sample.
//
// So read the output this way:
//
//   - maxIdle and activeFrac separate by construction. I chose think=2s and
//     hold=20s. Those columns describe my two plans, not the world.
//   - byte volume and record count are NOT by construction: they follow from
//     the front's real page sizes. Those are the real numbers here.
//   - the sustained side is a FLOOR, not a ceiling. Ten requests over twenty
//     seconds is nothing next to an actual VPN session.
//
// What the test therefore establishes is the magnitude of the axis and that the
// instrument works. Whether our sessions fall outside the front's own user
// distribution is not answered here, and may not be answerable by us at all —
// see spec.md §14.
//
// Network-dependent, so opt in with K2T_NET=1.
func TestRealFrontShapeAxis(t *testing.T) {
	if os.Getenv("K2T_NET") != "1" {
		t.Skip("set K2T_NET=1 to measure real fronts over the network")
	}
	fronts := []struct{ addr, sni string }{
		{"www.apple.com:443", "www.apple.com"},
		{"www.microsoft.com:443", "www.microsoft.com"},
	}

	for _, f := range fronts {
		browse, err := runPlan(t, f.addr, f.sni, "/", pageLoad(20*time.Second))
		if err != nil {
			t.Logf("%-24s 浏览侧失败: %v", f.sni, err)
			continue
		}
		tunnel, err := runPlan(t, f.addr, f.sni, "/", sustained(10, 2*time.Second))
		if err != nil {
			t.Logf("%-24s 隧道侧失败: %v", f.sni, err)
			continue
		}
		t.Logf("")
		t.Logf("=== %s ===", f.sni)
		t.Logf("  浏览:  %s", browse)
		t.Logf("  隧道:  %s", tunnel)
		t.Logf("  下行倍数=%.1f×（真实量级）  静默比/活跃占比差由计划构造，不是发现",
			float64(tunnel.bytesDown)/float64(max(browse.bytesDown, 1)))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
