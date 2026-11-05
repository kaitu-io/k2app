package self_test

import (
	"context"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	quicproxy "github.com/kaitu-io/kuic/integrationtests/tools/proxy"

	"github.com/stretchr/testify/require"
)

// recvLossRun downloads payloadSize bytes from a server through a proxy that
// drops 1-RTT (short header) packets at random: downRate of server→client
// packets, upRate of client→server packets. It returns the client's
// receive-side estimate 1 − received/span from ConnectionStats and the drop
// fraction the proxy actually applied to server→client 1-RTT packets.
func recvLossRun(t *testing.T, downRate, upRate float64, payloadSize int) (estimate, actualDown float64, stats quic.ConnectionStats) {
	t.Helper()
	ln, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(nil))
	require.NoError(t, err)
	defer ln.Close()

	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		str, err := conn.OpenStream()
		if err != nil {
			return
		}
		_, _ = str.Write(make([]byte, payloadSize))
		_ = str.Close()
	}()

	var mu sync.Mutex
	rng := rand.New(rand.NewSource(42))
	var downSeen, downDropped int
	proxy := &quicproxy.Proxy{
		Conn:       newUDPConnLocalhost(t),
		ServerAddr: ln.Addr().(*net.UDPAddr),
		DelayPacket: func(quicproxy.Direction, net.Addr, net.Addr, []byte) time.Duration {
			return scaleDuration(2 * time.Millisecond)
		},
		DropPacket: func(dir quicproxy.Direction, _, _ net.Addr, packet []byte) bool {
			if len(packet) == 0 || packet[0]&0x80 != 0 { // long header: never drop the handshake
				return false
			}
			mu.Lock()
			defer mu.Unlock()
			drop := false
			switch dir {
			case quicproxy.DirectionOutgoing:
				drop = rng.Float64() < downRate
				downSeen++
				if drop {
					downDropped++
				}
			case quicproxy.DirectionIncoming:
				drop = rng.Float64() < upRate
			}
			return drop
		},
	}
	require.NoError(t, proxy.Start())
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := quic.Dial(ctx, newUDPConnLocalhost(t), proxy.LocalAddr(), getTLSClientConfig(), getQuicConfig(nil))
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")

	str, err := conn.AcceptStream(ctx)
	require.NoError(t, err)
	n, err := io.Copy(io.Discard, str)
	require.NoError(t, err)
	require.Equal(t, int64(payloadSize), n)

	stats = conn.ConnectionStats()
	require.NotZero(t, stats.PacketNumberSpan1RTT)
	estimate = 1 - float64(stats.PacketsReceived1RTT)/float64(stats.PacketNumberSpan1RTT)
	mu.Lock()
	if downSeen > 0 {
		actualDown = float64(downDropped) / float64(downSeen)
	}
	mu.Unlock()
	return estimate, actualDown, stats
}

// The client's receive-side estimate tracks the loss actually applied to the
// server→client direction — the direction a downloading client cannot see in
// its own sender-side loss statistics.
func TestRecvLoss_EstimateTracksDownlinkLoss(t *testing.T) {
	est, actual, stats := recvLossRun(t, 0.2, 0, 2<<20)
	t.Logf("downlink 20%%: estimate=%.3f actual=%.3f received=%d span=%d", est, actual, stats.PacketsReceived1RTT, stats.PacketNumberSpan1RTT)
	require.InDelta(t, actual, est, 0.03)
	require.Greater(t, est, 0.1)
}

// Negative control: a clean path reads as (nearly) lossless. The only
// structural bias is the sender's packet-number skipping (≤ 1 per 256).
func TestRecvLoss_CleanPathReadsLossless(t *testing.T) {
	est, _, stats := recvLossRun(t, 0, 0, 2<<20)
	t.Logf("clean: estimate=%.4f received=%d span=%d", est, stats.PacketsReceived1RTT, stats.PacketNumberSpan1RTT)
	require.Less(t, est, 0.01)
}

// Direction discriminator: loss on the client→server direction only must not
// show up as receive-side loss (it is the sender-side statistics' job).
func TestRecvLoss_UplinkLossIsNotDownlinkLoss(t *testing.T) {
	est, _, stats := recvLossRun(t, 0, 0.3, 1<<20)
	t.Logf("uplink-only 30%%: estimate=%.4f received=%d span=%d", est, stats.PacketsReceived1RTT, stats.PacketNumberSpan1RTT)
	require.Less(t, est, 0.02)
}
