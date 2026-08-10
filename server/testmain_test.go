package server

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// quic-go background listeners — always running when Transport is open.
		goleak.IgnoreTopFunction("github.com/kaitu-io/kuic.(*packetHandlerMap).listen"),
		goleak.IgnoreTopFunction("github.com/kaitu-io/kuic.(*Transport).listen"),
		goleak.IgnoreTopFunction("github.com/kaitu-io/kuic.(*Transport).runReceiveLoop"),
		// smux session background goroutines — keepalive and shapers.
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).keepalive"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).sendLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).recvLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).shaperLoop"),
		// net/http h2 client read loop — lingering from ProbePublicIP in auto-provisioning tests.
		// Go stdlib HTTP/2 connections spawn a readLoop goroutine that outlives the response.
		goleak.IgnoreAnyFunction("net/http.(*http2ClientConn).readLoop"),
	)
}
