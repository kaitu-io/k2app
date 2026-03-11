package wire

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// quic-go background listeners — always running when Transport is open.
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*packetHandlerMap).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).listen"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).runReceiveLoop"),
		goleak.IgnoreTopFunction("github.com/apernet/quic-go.(*Transport).runSendQueue"),
		// smux session background goroutines — keepalive and shapers.
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).keepalive"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).sendLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).recvLoop"),
		goleak.IgnoreTopFunction("github.com/xtaci/smux/v2.(*Session).shaperLoop"),
		// net/http transport dial goroutines — ECH provider DNS-over-HTTPS cleanup lag.
		// Use IgnoreAnyFunction because the goroutine top may be internal/poll.runtime_pollWait.
		goleak.IgnoreAnyFunction("net/http.(*Transport).dialConnFor"),
	)
}
