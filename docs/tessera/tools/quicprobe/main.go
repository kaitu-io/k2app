// quicprobe tests whether a candidate front's name is on the GFW's QUIC SNI
// blocklist -- the second, SEPARATE list (spec.md §2, ~58k FQDNs, since
// 2024-04-07). Passing the TCP gate (tools/sni_gate_probe.py) says nothing
// about this one.
//
// # Why this is harder than the TCP probe, and how the verdict is read
//
// TCP SNI blocking injects an RST -- a loud, unambiguous signal. QUIC SNI
// blocking DROPS packets: the GFW decrypts the Initial, matches the SNI, and
// silently discards every UDP packet on (srcIP, dstIP, dstPort) for ~180s.
// There is no reset. A blocked name therefore looks exactly like a timeout.
//
// So the verdict does not key on "timeout vs not". It keys on whether the
// server answered AT ALL:
//
//   - handshake completes, OR fails with a TLS-layer error (bad cert, ALPN
//     mismatch, CRYPTO_ERROR) -> the Initial REACHED the server and it replied.
//     The name is NOT blocked. The target is just an echo point; we do not care
//     that the cert does not match the SNI.
//   - the dial times out with no server response at all -> the Initial was
//     dropped. Suspected blocked -- but a dead/QUIC-less target looks identical,
//     which is what the controls and the poison re-check exist to separate.
//
// # Traps handled
//
//   - RESIDUAL BLOCKING on (srcIP,dstIP,dstPort), source-port-independent, so a
//     new UDP socket does NOT escape it. After a timeout we re-probe the same
//     target with a benign SNI; if that also times out the target is poisoned
//     and the reading is void. Targets are rotated regardless.
//   - NO POSITIVE CONTROL = NO RESULT. Known-blocked names must time out and
//     known-good names must answer, or the method is not working from here and
//     every candidate reading is meaningless. Controls gate the run.
//
// # KNOWN LIMITATION: production CDN targets do not work (measured 2026-07-28)
//
// Run against Cloudflare/Google/Fastly edges, the verdict is dominated by the
// per-target UDP path quality, not by the SNI. From a Hangzhou node the same
// benign SNI scored 8/8 to a Cloudflare IP and 0/8 to Google and Fastly IPs,
// while a blocked SNI (youtube) scored 6/8 -- indistinguishable. QUIC blocking
// is packet DROP, and "dropped by the GFW for its SNI" cannot be separated from
// "this UDP path drops anyway" when the target is not under our control. See
// spec.md §14.5.2.
//
// The correct instrument is a SINGLE self-controlled overseas QUIC server that
// answers ANY SNI, bound to UDP/443, so the path is constant and the SNI is the
// only variable. Not yet built. The `baseline` mode below is what diagnosed
// this and stays useful for that.
//
// # What it cannot see (even done right)
//
// One province, one ISP (per the 2026-07-28 decision provincial walls are out
// of scope, spec.md §8.3). Snapshot only -- the list drifts, so this belongs in
// the §9 daily measurement.
//
// Usage: quicprobe <overseas-quic-ip> [...]          -- gate probe (see caveat)
//        quicprobe baseline <sni> <overseas-quic-ip> [...]  -- path-quality diag
// Targets must run QUIC/HTTP3 on UDP/443.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/apernet/quic-go"
)

const benign = "www.microsoft.com"

var (
	positive = []string{"www.youtube.com", "www.facebook.com", "www.google.com"}
	negative = []string{"www.microsoft.com", "www.cloudflare.com"}
	candidates = []string{
		"s3.amazonaws.com", "storage.googleapis.com", "objects.githubusercontent.com",
		"cdn.jsdelivr.net", "unpkg.com", "cdnjs.cloudflare.com", "dl.google.com",
		"swcdn.apple.com", "updates.cdn-apple.com", "download.windowsupdate.com",
		"dl.delivery.mp.microsoft.com", "www.apple.com",
	}
)

type verdict int

const (
	answered verdict = iota // server replied (handshake ok or TLS-layer error) -> NOT blocked
	silent                  // no reply before deadline -> suspected blocked
	void                    // target poisoned by residual blocking -> reading discarded
)

func (v verdict) String() string {
	switch v {
	case answered:
		return "通"
	case silent:
		return "封?"
	default:
		return "void"
	}
}

// probe returns whether the server answered a QUIC Initial carrying this SNI.
func probe(ip, sni string, timeout time.Duration) verdict {
	tlsConf := &tls.Config{
		ServerName:         sni,
		NextProtos:         []string{"h3"},
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := quic.DialAddr(ctx, net.JoinHostPort(ip, "443"), tlsConf,
		&quic.Config{HandshakeIdleTimeout: timeout})
	if err == nil {
		conn.CloseWithError(0, "")
		return answered
	}
	// A reply that fails at the TLS layer still proves the Initial arrived.
	if serverReplied(err) {
		return answered
	}
	return silent
}

// serverReplied distinguishes "server sent something back" from "silence". Only
// a genuine timeout / no-network-activity means the Initial was dropped; a TLS
// alert, CRYPTO_ERROR, ALPN failure, or version negotiation all mean bytes came
// back and the name is not blocked.
func serverReplied(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, sig := range []string{
		"no recent network activity", "timeout", "deadline",
		"handshake did not complete",
	} {
		if strings.Contains(s, sig) {
			return false
		}
	}
	// Anything else (CRYPTO_ERROR, tls:, alert, alpn, no compatible quic version)
	// is a reply.
	return true
}

// measure adds the poison re-check that a raw probe lacks.
func measure(ip, sni string, timeout time.Duration) verdict {
	v := probe(ip, sni, timeout)
	if v != silent {
		return v
	}
	if probe(ip, benign, timeout) == silent {
		return void // benign also silent -> target poisoned, not this SNI
	}
	return silent
}

type state struct{ i int }

func (st *state) run(names []string, tag string, targets []string, timeout time.Duration) map[string]verdict {
	out := map[string]verdict{}
	for _, name := range names {
		ip := targets[st.i%len(targets)]
		st.i++
		v := measure(ip, name, timeout)
		out[name] = v
		fmt.Printf("  %-8s %-32s %-6s %s\n", tag, name, v, ip)
		time.Sleep(3 * time.Second)
	}
	return out
}

func count(m map[string]verdict, want verdict) int {
	n := 0
	for _, v := range m {
		if v == want {
			n++
		}
	}
	return n
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: quicprobe [baseline <sni>] <overseas-quic-ip> [...]")
		os.Exit(2)
	}
	// baseline mode: dial ONE legitimate SNI (never blocklisted) many times per
	// target, report the QUIC handshake success rate. If this is low, the
	// out-of-China UDP path is the problem and no SNI verdict is readable.
	if os.Args[1] == "baseline" {
		sni := os.Args[2]
		targets := os.Args[3:]
		fmt.Printf("基线：SNI=%s，每靶子 8 次，测 UDP/QUIC 出境是否通\n\n", sni)
		for _, ip := range targets {
			ok := 0
			for i := 0; i < 8; i++ {
				if probe(ip, sni, 6*time.Second) == answered {
					ok++
				}
				time.Sleep(4 * time.Second)
			}
			fmt.Printf("  %-18s %d/8 握手成功\n", ip, ok)
		}
		return
	}
	targets := os.Args[1:]
	timeout := 6 * time.Second
	fmt.Printf("目标(QUIC/UDP 443): %s\n\n", strings.Join(targets, ", "))
	st := &state{}

	fmt.Println("== 阳性对照（已知 QUIC 封锁，必须 封?）==")
	pos := st.run(positive, "对照/封", targets, timeout)
	fmt.Println("\n== 阴性对照（已知可达，必须 通）==")
	neg := st.run(negative, "对照/通", targets, timeout)

	hits, misses := count(pos, silent), count(neg, answered)
	fmt.Printf("\n阳性 封? %d/%d   阴性 通 %d/%d\n", hits, len(positive), misses, len(negative))
	if hits < len(positive) {
		fmt.Println("→ 阳性对照未全部超时：本观测点测不到 QUIC SNI 封锁（或靶子不支持 QUIC）。停。")
		os.Exit(1)
	}
	if misses < len(negative) {
		fmt.Println("→ 阴性对照未全部应答：靶子或路径有问题。停。")
		os.Exit(1)
	}
	fmt.Println("→ 方法成立。")
	fmt.Println()

	fmt.Println("== 候选 front（QUIC 路径）==")
	st.run(candidates, "候选", targets, timeout)
	fmt.Println("\n注：仅本省本运营商，单次快照。QUIC 与 TCP 是两张独立黑名单。")
}
