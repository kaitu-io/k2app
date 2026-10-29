//go:build linux

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ── TPROXY UDP flows ─────────────────────────────────────────────
//
// The packets a LAN client socket sends to one destination are one flow, and
// one flow is one tunnel session. Opening a session per packet gives every packet
// of an exchange a different egress source port (the server binds a socket
// per session), which breaks anything longer than a single request/reply —
// QUIC, WireGuard, games, VoIP — and leaves one idle session behind per
// packet (OpenWrt lab, 2026-09-29).

func lanSrc(port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(10, 30, 0, 2), Port: port}
}

func remoteDst(last byte, port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(44, 44, 44, last), Port: port}
}

// flowStarts records the flows a table opened.
type flowStarts struct {
	mu    sync.Mutex
	flows []*tproxyUDPConn
}

func (s *flowStarts) start(f *tproxyUDPConn) {
	s.mu.Lock()
	s.flows = append(s.flows, f)
	s.mu.Unlock()
}

func (s *flowStarts) all() []*tproxyUDPConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*tproxyUDPConn(nil), s.flows...)
}

func readPacket(t *testing.T, c *tproxyUDPConn) (string, string) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, to, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	return string(buf[:n]), to.String()
}

func TestUDPFlowTable_SameClientSocket_OneFlow(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts
	src, dst := lanSrc(40000), remoteDst(44, 9000)

	for i := 0; i < 5; i++ {
		// A fresh *net.UDPAddr per packet, as readUDPOrigDst produces.
		table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte(fmt.Sprintf("p%d", i)), starts.start)
	}

	flows := starts.all()
	if len(flows) != 1 {
		t.Fatalf("5 packets from one client socket opened %d flows, want 1", len(flows))
	}
	if table.len() != 1 {
		t.Fatalf("table holds %d flows, want 1", table.len())
	}
	if got := flows[0].PacketSourceAddr().String(); got != src.String() {
		t.Fatalf("flow source = %s, want %s", got, src)
	}
	for i := 0; i < 5; i++ {
		data, to := readPacket(t, flows[0])
		if data != fmt.Sprintf("p%d", i) || to != dst.String() {
			t.Fatalf("packet %d = %q to %s, want %q to %s", i, data, to, fmt.Sprintf("p%d", i), dst)
		}
	}
}

func TestUDPFlowTable_DifferentClientSockets_SeparateFlows(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("a"), starts.start)
	table.dispatch(nil, nil, lanSrc(40001), remoteDst(44, 9000), []byte("b"), starts.start)
	table.dispatch(nil, nil, &net.UDPAddr{IP: net.IPv4(10, 30, 0, 3), Port: 40000}, remoteDst(44, 9000), []byte("c"), starts.start)

	if n := len(starts.all()); n != 3 {
		t.Fatalf("3 client sockets opened %d flows, want 3", n)
	}
}

// Each destination is its own flow, so routing, sniffing and the reply source
// stay per destination — what the per-packet model decided, without its cost.
func TestUDPFlowTable_OneSocketTwoDestinations_TwoFlows(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	for i := 0; i < 3; i++ {
		table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("a"), starts.start)
		table.dispatch(nil, nil, lanSrc(40000), remoteDst(45, 9000), []byte("b"), starts.start)
	}

	flows := starts.all()
	if len(flows) != 2 {
		t.Fatalf("opened %d flows, want one per destination (2)", len(flows))
	}
	for i, want := range []struct{ data, dst string }{
		{"a", remoteDst(44, 9000).String()},
		{"b", remoteDst(45, 9000).String()},
	} {
		if got := flows[i].LocalAddr().String(); got != want.dst {
			t.Fatalf("flow %d destination = %s, want %s", i, got, want.dst)
		}
		for n := 0; n < 3; n++ {
			if data, to := readPacket(t, flows[i]); data != want.data || to != want.dst {
				t.Fatalf("flow %d packet %d = %q to %s, want %q to %s", i, n, data, to, want.data, want.dst)
			}
		}
	}
}

func TestUDPFlowTable_IPv4MappedSource_SameFlow(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	table.dispatch(nil, nil, &net.UDPAddr{IP: net.IPv4(10, 30, 0, 2).To4(), Port: 40000}, remoteDst(44, 9000), []byte("a"), starts.start)
	table.dispatch(nil, nil, &net.UDPAddr{IP: net.IPv4(10, 30, 0, 2).To16(), Port: 40000}, remoteDst(44, 9000), []byte("b"), starts.start)

	if n := len(starts.all()); n != 1 {
		t.Fatalf("4-byte and 16-byte forms of one source opened %d flows, want 1", n)
	}
}

func TestUDPFlowTable_ClosedFlow_NextPacketOpensNewFlow(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("a"), starts.start)
	first := starts.all()[0]
	first.Close()
	if table.len() != 0 {
		t.Fatalf("closed flow still in table (%d entries)", table.len())
	}

	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("b"), starts.start)
	flows := starts.all()
	if len(flows) != 2 || flows[1] == first {
		t.Fatalf("packet after close must open a new flow, have %d flows", len(flows))
	}
	if data, _ := readPacket(t, flows[1]); data != "b" {
		t.Fatalf("new flow got %q, want %q", data, "b")
	}
}

// dispatch must not hand a packet to a closed flow it still finds in the
// table: it replaces the flow and delivers there. The table is put into that
// state by hand; in production it is the window between a flow closing and
// its Close reaching the table.
func TestUDPFlowTable_ClosedFlowStillInTable_IsReplaced(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("a"), starts.start)
	stale := starts.all()[0]
	stale.table = nil // Close will not unregister: the table still points at it
	stale.Close()

	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("b"), starts.start)

	flows := starts.all()
	if len(flows) != 2 {
		t.Fatalf("have %d flows, want the stale one replaced", len(flows))
	}
	if data, _ := readPacket(t, flows[1]); data != "b" {
		t.Fatalf("replacement flow got %q, want %q", data, "b")
	}
	if table.len() != 1 {
		t.Fatalf("table holds %d flows, want only the replacement", table.len())
	}
}

// A flow that closes before its first packet is queued (shutdown racing the
// read loop) must not be started: a started flow is owned by a tunnel session
// that closes it, and this one would have no owner.
func TestUDPFlowTable_Feed_ClosedNewFlowIsNotStarted(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	key := newUDPFlowKey(lanSrc(40000), remoteDst(44, 9000))
	flow, created := table.lookup(key, nil, nil, lanSrc(40000), remoteDst(44, 9000))
	if !created || table.len() != 1 {
		t.Fatalf("lookup: created=%v len=%d", created, table.len())
	}
	flow.table = nil // keep it in the table through Close, as in the race
	flow.Close()

	if table.feed(flow, created, []byte("x"), starts.start) {
		t.Fatal("feed reported delivery into a closed flow")
	}
	if n := len(starts.all()); n != 0 {
		t.Fatalf("closed flow was started (%d starts)", n)
	}
	if table.len() != 0 {
		t.Fatalf("closed flow left in the table (%d)", table.len())
	}
}

func TestUDPFlowTable_StaleCloseDoesNotEvictNewerFlow(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("a"), starts.start)
	old := starts.all()[0]
	old.Close()
	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("b"), starts.start)

	table.remove(old.key, old) // a late cleanup of the old flow

	if table.len() != 1 {
		t.Fatalf("newer flow was evicted by the old flow's cleanup (%d entries)", table.len())
	}
}

func TestUDPFlowTable_CloseAll(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts
	table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("a"), starts.start)
	table.dispatch(nil, nil, lanSrc(40001), remoteDst(44, 9000), []byte("b"), starts.start)

	table.closeAll()

	if table.len() != 0 {
		t.Fatalf("%d flows left after closeAll", table.len())
	}
	for _, f := range starts.all() {
		if !f.closed() {
			t.Fatal("flow not closed")
		}
	}

	// A packet still in the read loop during shutdown must not open a flow
	// nobody will ever close.
	table.dispatch(nil, nil, lanSrc(40002), remoteDst(44, 9000), []byte("late"), starts.start)
	if n := len(starts.all()); n != 2 {
		t.Fatalf("a flow was opened after closeAll (%d flows)", n)
	}
	if table.len() != 0 {
		t.Fatalf("%d flows in a stopped table", table.len())
	}
}

func TestUDPFlowTable_FullTable_RefusesNewFlows(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts
	for i := 0; i < maxUDPFlows; i++ {
		table.dispatch(nil, nil, lanSrc(10000+i), remoteDst(44, 9000), []byte("x"), starts.start)
	}
	if table.len() != maxUDPFlows {
		t.Fatalf("table holds %d flows, want %d", table.len(), maxUDPFlows)
	}

	table.dispatch(nil, nil, lanSrc(10000+maxUDPFlows), remoteDst(44, 9000), []byte("x"), starts.start)
	if n := len(starts.all()); n != maxUDPFlows {
		t.Fatalf("opened %d flows, want the table capped at %d", n, maxUDPFlows)
	}
	if got := table.refused.Load(); got != 1 {
		t.Fatalf("refused = %d, want 1", got)
	}

	// An existing flow keeps working, and a closed one frees its place.
	table.dispatch(nil, nil, lanSrc(10000), remoteDst(44, 9000), []byte("y"), starts.start)
	starts.all()[1].Close()
	table.dispatch(nil, nil, lanSrc(10000+maxUDPFlows), remoteDst(44, 9000), []byte("z"), starts.start)
	if n := len(starts.all()); n != maxUDPFlows+1 {
		t.Fatalf("a freed place was not reused (%d flows opened)", n)
	}
}

// Flows are opened, fed and closed from different goroutines (read loop,
// tunnel). Whatever the interleaving, a flow that was started and then closed
// must leave the table, and one that is still open must still be in it.
func TestUDPFlowTable_ConcurrentDispatchAndClose(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts
	var wg, closers sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				table.dispatch(nil, nil, lanSrc(40000+i%4), remoteDst(44, 9000), []byte("x"), func(f *tproxyUDPConn) {
					starts.start(f)
					if i%3 == 0 {
						closers.Add(1)
						go func() { defer closers.Done(); f.Close() }()
					}
				})
			}
		}()
	}
	wg.Wait()
	closers.Wait()

	open := 0
	for _, f := range starts.all() {
		if !f.closed() {
			open++
		}
	}
	if table.len() != open {
		t.Fatalf("table holds %d flows but %d started flows are open", table.len(), open)
	}
	if open > 4 {
		t.Fatalf("%d open flows for 4 client sockets", open)
	}
}

// ── flow conn ────────────────────────────────────────────────────

// pipePacket ends an idle session through the read deadline. Without it a
// flow whose client went away would hold its tunnel slot forever.
func TestTProxyUDPConn_ReadFrom_HonoursReadDeadline(t *testing.T) {
	c := newTProxyUDPConn(nil, nil, lanSrc(40000), remoteDst(44, 9000), nil)
	defer c.Close()

	c.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	start := time.Now()
	_, _, err := c.ReadFrom(make([]byte, 64))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("ReadFrom = %v, want os.ErrDeadlineExceeded", err)
	}
	if !os.IsTimeout(err) {
		t.Fatal("deadline error must satisfy os.IsTimeout")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("deadline took %v", time.Since(start))
	}

	// A queued packet is delivered even when the deadline already passed.
	c.deliver([]byte("late"))
	c.SetReadDeadline(time.Now().Add(-time.Second))
	buf := make([]byte, 64)
	n, _, err := c.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "late" {
		t.Fatalf("queued packet = %q, %v", buf[:n], err)
	}
}

func TestTProxyUDPConn_Deliver_FullQueueDropsWithoutBlocking(t *testing.T) {
	c := newTProxyUDPConn(nil, nil, lanSrc(40000), remoteDst(44, 9000), nil)
	defer c.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < udpFlowQueue+10; i++ {
			if !c.deliver([]byte("x")) {
				t.Error("deliver reported a closed flow")
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deliver blocked on a full queue — it would stall the shared read loop")
	}
	if got := c.drops.Load(); got != 10 {
		t.Fatalf("drops = %d, want 10", got)
	}
}

func TestTProxyUDPConn_Deliver_ClosedFlowReportsFalse(t *testing.T) {
	c := newTProxyUDPConn(nil, nil, lanSrc(40000), remoteDst(44, 9000), nil)
	c.Close()
	if c.deliver([]byte("x")) {
		t.Fatal("deliver on a closed flow must report false")
	}
}

func TestTProxyUDPConn_DeliverCopiesPayload(t *testing.T) {
	c := newTProxyUDPConn(nil, nil, lanSrc(40000), remoteDst(44, 9000), nil)
	defer c.Close()

	buf := []byte("original")
	c.deliver(buf)
	copy(buf, "XXXXXXXX") // the read loop reuses its buffer for the next packet

	if data, _ := readPacket(t, c); data != "original" {
		t.Fatalf("queued packet = %q, want %q", data, "original")
	}
}

// ── reply source ─────────────────────────────────────────────────

// Whatever address the tunnel reports as the sender, the LAN client must see
// the reply coming from the destination it addressed. The tunnel's address can
// legitimately differ (the server resolved an SNI-upgraded hostname to another
// IP) or be someone else entirely (the egress port is full-cone); a reply
// socket is only ever bound to an address the LAN client chose.
func TestTProxyUDPConn_WriteTo_AlwaysRepliesFromTheFlowDestination(t *testing.T) {
	dst := remoteDst(44, 443)
	for _, from := range []net.Addr{
		nil,
		dst,
		&net.UDPAddr{IP: net.IPv4(203, 0, 113, 7), Port: 443},
		&net.UDPAddr{IP: net.IPv4(198, 51, 100, 9), Port: 31337},
	} {
		rep := &fakeReplier{w: &recordingWriter{}}
		c := newTProxyUDPConn(nil, rep, lanSrc(40000), dst, nil)

		if _, err := c.WriteTo([]byte("reply"), from); err != nil {
			t.Fatal(err)
		}
		if rep.askedFor == nil || rep.askedFor.String() != dst.String() {
			t.Fatalf("from=%v: reply source = %v, want %s", from, rep.askedFor, dst)
		}
		if rep.w.to.String() != lanSrc(40000).String() {
			t.Fatalf("from=%v: reply went to %v, want the LAN client", from, rep.w.to)
		}
		c.Close()
	}
}

// ── read loop ────────────────────────────────────────────────────

type flowHandler struct {
	mu    sync.Mutex
	conns []net.PacketConn
	dests []string
}

func (h *flowHandler) HandleTCP(conn net.Conn, _ string) { conn.Close() }

func (h *flowHandler) HandleUDP(conn net.PacketConn, dest string) {
	h.mu.Lock()
	h.conns = append(h.conns, conn)
	h.dests = append(h.dests, dest)
	h.mu.Unlock()
}

func (h *flowHandler) sessions() []net.PacketConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]net.PacketConn(nil), h.conns...)
}

// closingHandler gives up on every session at once, the way HandleUDP does on
// its early returns (no tunnel slot, IPv6 destination, blocked QUIC, rejected
// route, failed dial).
type closingHandler struct {
	flowHandler
}

func (h *closingHandler) HandleUDP(conn net.PacketConn, dest string) {
	h.flowHandler.HandleUDP(conn, dest)
	conn.Close()
}

// origDstListener is a loopback UDP socket that reports each packet's
// destination the way the TPROXY listener does, without needing root.
func origDstListener(t *testing.T) *net.UDPConn {
	t.Helper()
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		return c.Control(func(fd uintptr) {
			syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_RECVORIGDSTADDR, 1)
		})
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	return pc.(*net.UDPConn)
}

func TestGatewayProvider_UDPReadLoop_OneSessionPerClientSocket(t *testing.T) {
	listener := origDstListener(t)
	p := newGatewayProvider(nil, InterceptConfig{}, nil)
	h := &flowHandler{}
	p.startUDPRead(listener, h)
	defer func() {
		close(p.done)
		listener.Close()
		p.flows.closeAll()
	}()

	clientA, clientB := loopbackUDP(t), loopbackUDP(t)
	for i := 0; i < 5; i++ {
		if _, err := clientA.WriteTo([]byte(fmt.Sprintf("a%d", i)), listener.LocalAddr()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := clientB.WriteTo([]byte("b0"), listener.LocalAddr()); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for len(h.sessions()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	// Let any extra (wrong) session show up before counting.
	time.Sleep(100 * time.Millisecond)

	sessions := h.sessions()
	if len(sessions) != 2 {
		t.Fatalf("5 packets from one socket + 1 from another opened %d sessions, want 2", len(sessions))
	}

	var flowA *tproxyUDPConn
	for _, s := range sessions {
		f := s.(*tproxyUDPConn)
		if f.PacketSourceAddr().String() == clientA.LocalAddr().String() {
			flowA = f
		}
	}
	if flowA == nil {
		t.Fatal("no session for client A")
	}
	for i := 0; i < 5; i++ {
		data, to := readPacket(t, flowA)
		if data != fmt.Sprintf("a%d", i) {
			t.Fatalf("packet %d = %q", i, data)
		}
		if to != listener.LocalAddr().String() {
			t.Fatalf("packet %d addressed to %s, want %s", i, to, listener.LocalAddr())
		}
	}
}

// A session the tunnel abandons must not linger in the table swallowing the
// packets that follow: each one gets a fresh session and a fresh verdict.
func TestGatewayProvider_UDPReadLoop_AbandonedSessionLeavesTheTable(t *testing.T) {
	listener := origDstListener(t)
	p := newGatewayProvider(nil, InterceptConfig{}, nil)
	h := &closingHandler{}
	p.startUDPRead(listener, h)
	defer func() {
		close(p.done)
		listener.Close()
		p.flows.closeAll()
	}()

	client := loopbackUDP(t)
	for i := 0; i < 3; i++ {
		if _, err := client.WriteTo([]byte("x"), listener.LocalAddr()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for (len(h.sessions()) < i+1 || p.flows.len() != 0) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := len(h.sessions()); got != i+1 {
			t.Fatalf("after packet %d: %d sessions, want %d", i+1, got, i+1)
		}
		if n := p.flows.len(); n != 0 {
			t.Fatalf("after packet %d: abandoned session still in the table (%d)", i+1, n)
		}
	}
}

// ── queue budget ─────────────────────────────────────────────────

// With the tunnel out of slots every flow's session waits and nothing drains
// the queues. The packets held across all flows must stay bounded, and the
// budget must come back as soon as packets are read or their flow closes.
func TestUDPFlowTable_QueueBudget_BoundsPacketsAcrossFlows(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	flowsToFill := maxQueuedUDPPackets / udpFlowQueue
	for f := 0; f < flowsToFill; f++ {
		for i := 0; i < udpFlowQueue; i++ {
			table.dispatch(nil, nil, lanSrc(20000+f), remoteDst(44, 9000), []byte("x"), starts.start)
		}
	}
	if got := table.queued.Load(); got != maxQueuedUDPPackets {
		t.Fatalf("queued = %d, want the budget %d", got, maxQueuedUDPPackets)
	}

	table.dispatch(nil, nil, lanSrc(30000), remoteDst(44, 9000), []byte("over"), starts.start)
	flows := starts.all()
	over := flows[len(flows)-1]
	if got := over.drops.Load(); got != 1 {
		t.Fatalf("packet beyond the budget: drops = %d, want 1", got)
	}
	if got := table.queued.Load(); got != maxQueuedUDPPackets {
		t.Fatalf("queued = %d after a refused packet, want %d", got, maxQueuedUDPPackets)
	}

	// Reading one packet frees room for one.
	readPacket(t, flows[0])
	table.dispatch(nil, nil, lanSrc(30000), remoteDst(44, 9000), []byte("fits"), starts.start)
	if data, _ := readPacket(t, over); data != "fits" {
		t.Fatalf("got %q, want the packet queued after room was freed", data)
	}

	// Closing a flow frees everything it held.
	before := table.queued.Load()
	flows[1].Close()
	if got := table.queued.Load(); got != before-udpFlowQueue {
		t.Fatalf("queued = %d after closing a full flow, want %d", got, before-udpFlowQueue)
	}

	table.closeAll()
	if got := table.queued.Load(); got != 0 {
		t.Fatalf("queued = %d after closing every flow, want 0", got)
	}
}

// The budget must balance under concurrent dispatch, read and close — a leak
// would slowly starve every flow of queue room.
func TestUDPFlowTable_QueueBudget_BalancesUnderConcurrency(t *testing.T) {
	table := newUDPFlowTable()
	var wg, workers sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				table.dispatch(nil, nil, lanSrc(40000+i%6), remoteDst(44, 9000), []byte("x"), func(f *tproxyUDPConn) {
					workers.Add(1)
					go func() {
						defer workers.Done()
						buf := make([]byte, 16)
						for n := 0; n < 50; n++ {
							f.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
							if _, _, err := f.ReadFrom(buf); err != nil {
								break
							}
						}
						f.Close()
					}()
				})
			}
		}()
	}
	wg.Wait()
	workers.Wait()
	table.closeAll()

	if got := table.queued.Load(); got != 0 {
		t.Fatalf("queued = %d with every flow closed, want 0", got)
	}
	if n := table.len(); n != 0 {
		t.Fatalf("%d flows left", n)
	}
}

// A packet dropped because its own flow's queue is full was never queued, so
// it must not keep a place in the budget.
func TestUDPFlowTable_QueueBudget_FullFlowQueueDoesNotLeak(t *testing.T) {
	table := newUDPFlowTable()
	var starts flowStarts

	for i := 0; i < udpFlowQueue+10; i++ {
		table.dispatch(nil, nil, lanSrc(40000), remoteDst(44, 9000), []byte("x"), starts.start)
	}
	flow := starts.all()[0]
	if got := flow.drops.Load(); got != 10 {
		t.Fatalf("drops = %d, want 10", got)
	}
	if got := table.queued.Load(); got != udpFlowQueue {
		t.Fatalf("queued = %d, want only the %d packets actually held", got, udpFlowQueue)
	}
	flow.Close()
	if got := table.queued.Load(); got != 0 {
		t.Fatalf("queued = %d after close, want 0", got)
	}
}
