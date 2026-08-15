// Package demux splits one UDP port into two fates — spec §4.6 and §6.2.
//
// Datagrams from flows the node authenticated surface through ReadFrom, so a
// QUIC server listening on this net.PacketConn only ever sees its own clients.
// Everything else is forwarded verbatim to the front and never reaches the QUIC
// stack at all.
//
// Note what is absent: no TLS termination, no CRYPTO frame reassembly, no
// connection ID tracking, no decryption. After the decision the node is a UDP
// NAT. That makes the borrowed shell on QUIC cleaner than on TCP, where the
// relay has to peek a ClientHello out of a byte stream and then splice it.
//
// A prober therefore completes a real handshake with the real front, gets the
// front's real certificate chain and its real responses. There is no forged
// material anywhere in that exchange, because the node produced none.
package demux

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kaitu-io/tessera/credential"
	"github.com/kaitu-io/tessera/quicwire"
)

// maxDatagram is the largest UDP payload QUIC permits (RFC 9000 §14). Reading
// into anything smaller would silently truncate: net.UDPConn discards the tail
// of an oversized datagram, so a relayed flow would be corrupted in a way that
// looks like packet loss rather than like a bug.
const maxDatagram = 65527

// DefaultIdleTimeout is how long a relayed flow may sit silent before its
// socket is reclaimed.
const DefaultIdleTimeout = 60 * time.Second

// DefaultMaxRelays bounds concurrently relayed flows.
const DefaultMaxRelays = 4096

// Classifier decides whether a flow's first datagram came from one of our
// clients. It must be safe for concurrent use.
type Classifier func(datagram []byte) bool

// TokenClassifier is the §4.5 decision carried over the QUIC token: parse the
// Initial header, open the credential.
//
// Anything that is not a parseable v1 Initial is not ours. That includes a
// short header packet, which is what an authenticated flow's datagrams look
// like after a NAT rebinding moves it to a new 4-tuple — see the note on
// migration in Conn's documentation.
func TokenClassifier(o *credential.Opener) Classifier {
	return func(d []byte) bool {
		initial, ok := quicwire.ParseInitial(d)
		if !ok {
			return false
		}
		_, err := o.OpenToken(initial.Token)
		return err == nil
	}
}

// Config configures a demultiplexing listener.
type Config struct {
	// Conn is the node's UDP socket. Conn takes ownership: closing the Conn
	// closes it.
	Conn *net.UDPConn
	// Front is where unauthenticated flows go. It must be reachable; a node
	// that cannot reach its front has no shell to hide behind and must not
	// start.
	Front *net.UDPAddr
	// Classify runs once per client 4-tuple, on its first datagram.
	Classify Classifier
	// MaxRelays bounds concurrently relayed flows. Zero picks a default.
	MaxRelays int
	// IdleTimeout reclaims silent relayed flows. Zero picks a default.
	IdleTimeout time.Duration
	// Logger receives operational events. Nil discards them.
	Logger *slog.Logger
}

// Conn is a net.PacketConn surfacing only authenticated flows.
//
// # Connection migration
//
// The decision is per 4-tuple. An authenticated flow whose source address
// changes — NAT rebinding, a phone moving between networks — arrives as a new
// 4-tuple whose first datagram is a short header packet carrying no token, so
// it is classified as a stranger and relayed to the front. The connection
// breaks.
//
// This is a known limitation, not an oversight, and it is pinned by a test so
// that fixing it is a deliberate change. The fix is for the node to recognise
// its own connection IDs in short header packets; that needs the node's
// connection ID generator to mark them, which is a separate piece of work.
type Conn struct {
	sock        *net.UDPConn
	front       *net.UDPAddr
	classify    Classifier
	maxRelays   int
	idleTimeout time.Duration
	log         *slog.Logger

	mu     sync.Mutex
	relays map[string]*relay
	local  map[string]time.Time

	closeOnce sync.Once
	closed    chan struct{}
	bufs      sync.Pool

	stats Stats
}

// Stats counts what the demultiplexer did. All fields are safe to read
// concurrently.
type Stats struct {
	Authenticated atomic.Int64
	Relayed       atomic.Int64
	// Refused counts stranger flows dropped because MaxRelays was reached.
	// Dropping is what an overloaded server does, so it costs no camouflage,
	// but a persistently non-zero count means the limit is undersized.
	Refused atomic.Int64
}

type relay struct {
	conn     *net.UDPConn
	lastSeen atomic.Int64 // unix nanos
}

// New builds a demultiplexing listener.
func New(cfg Config) (*Conn, error) {
	if cfg.Conn == nil {
		return nil, errors.New("demux: nil socket")
	}
	if cfg.Front == nil {
		return nil, errors.New("demux: nil front address")
	}
	if cfg.Classify == nil {
		return nil, errors.New("demux: nil classifier")
	}
	maxRelays := cfg.MaxRelays
	if maxRelays <= 0 {
		maxRelays = DefaultMaxRelays
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	c := &Conn{
		sock:        cfg.Conn,
		front:       cfg.Front,
		classify:    cfg.Classify,
		maxRelays:   maxRelays,
		idleTimeout: idle,
		log:         log,
		relays:      map[string]*relay{},
		local:       map[string]time.Time{},
		closed:      make(chan struct{}),
		bufs:        sync.Pool{New: func() any { b := make([]byte, maxDatagram); return &b }},
	}
	go c.reapIdle()
	return c, nil
}

// Stats returns the counters. The returned pointer is live.
func (c *Conn) Stats() *Stats { return &c.stats }

// ReadFrom returns the next datagram belonging to an authenticated flow,
// relaying everything else on the way. It satisfies net.PacketConn.
func (c *Conn) ReadFrom(p []byte) (int, net.Addr, error) {
	bufp := c.bufs.Get().(*[]byte)
	defer c.bufs.Put(bufp)
	buf := *bufp

	for {
		n, addr, err := c.sock.ReadFromUDP(buf)
		if err != nil {
			return 0, nil, err
		}
		key := addr.String()

		r, mine := c.route(key, buf[:n], addr)
		if mine {
			return copy(p, buf[:n]), addr, nil
		}
		if r == nil {
			continue // refused; drop as an overloaded server would
		}
		r.lastSeen.Store(time.Now().UnixNano())
		if _, err := r.conn.Write(buf[:n]); err != nil {
			// The front went away. The flow stalls, which is what a stranger
			// would see from any server whose upstream broke.
			c.log.Debug("tessera/demux: relay write failed", "peer", key, "err", err)
		}
	}
}

// route resolves a flow to its fate, deciding on first sight. It returns
// mine=true for the authenticated path, or the relay to forward to. A nil
// relay with mine=false means the flow was refused.
func (c *Conn) route(key string, first []byte, addr *net.UDPAddr) (*relay, bool) {
	c.mu.Lock()
	if _, ok := c.local[key]; ok {
		c.local[key] = time.Now()
		c.mu.Unlock()
		return nil, true
	}
	if r, ok := c.relays[key]; ok {
		c.mu.Unlock()
		return r, false
	}
	c.mu.Unlock()

	// Classify outside the lock: it runs an X25519 and an AEAD open, and
	// holding the map lock across that would serialise every flow behind the
	// slowest one. A concurrent duplicate is harmless — both racers reach the
	// same verdict, and the loser's relay socket is closed below.
	if c.classify(first) {
		c.mu.Lock()
		c.local[key] = time.Now()
		c.mu.Unlock()
		c.stats.Authenticated.Add(1)
		return nil, true
	}

	c.mu.Lock()
	if r, ok := c.relays[key]; ok { // lost a race
		c.mu.Unlock()
		return r, false
	}
	if len(c.relays) >= c.maxRelays {
		c.mu.Unlock()
		c.stats.Refused.Add(1)
		return nil, false
	}
	c.mu.Unlock()

	conn, err := net.DialUDP("udp", nil, c.front)
	if err != nil {
		c.log.Warn("tessera/demux: cannot reach front", "front", c.front, "err", err)
		c.stats.Refused.Add(1)
		return nil, false
	}
	r := &relay{conn: conn}
	r.lastSeen.Store(time.Now().UnixNano())

	c.mu.Lock()
	if existing, ok := c.relays[key]; ok { // lost the race while dialing
		c.mu.Unlock()
		conn.Close()
		return existing, false
	}
	c.relays[key] = r
	c.mu.Unlock()

	c.stats.Relayed.Add(1)
	go c.pumpBack(r, addr)
	return r, false
}

// pumpBack carries the front's datagrams back to the client. They leave from
// the node's own socket, so to the client the front's QUIC endpoint simply is
// the node — nothing is spoofed.
func (c *Conn) pumpBack(r *relay, client *net.UDPAddr) {
	bufp := c.bufs.Get().(*[]byte)
	defer c.bufs.Put(bufp)
	buf := *bufp

	for {
		n, err := r.conn.Read(buf)
		if err != nil {
			return
		}
		r.lastSeen.Store(time.Now().UnixNano())
		if _, err := c.sock.WriteToUDP(buf[:n], client); err != nil {
			return
		}
	}
}

func (c *Conn) reapIdle() {
	t := time.NewTicker(c.idleTimeout / 2)
	defer t.Stop()
	for {
		select {
		case <-c.closed:
			return
		case now := <-t.C:
			cutoff := now.Add(-c.idleTimeout)
			c.mu.Lock()
			for key, r := range c.relays {
				if time.Unix(0, r.lastSeen.Load()).Before(cutoff) {
					r.conn.Close()
					delete(c.relays, key)
				}
			}
			for key, seen := range c.local {
				if seen.Before(cutoff) {
					delete(c.local, key)
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *Conn) WriteTo(p []byte, addr net.Addr) (int, error) { return c.sock.WriteTo(p, addr) }
func (c *Conn) LocalAddr() net.Addr                          { return c.sock.LocalAddr() }
func (c *Conn) SetDeadline(t time.Time) error                { return c.sock.SetDeadline(t) }
func (c *Conn) SetReadDeadline(t time.Time) error            { return c.sock.SetReadDeadline(t) }
func (c *Conn) SetWriteDeadline(t time.Time) error           { return c.sock.SetWriteDeadline(t) }

// Close releases the node's socket and every relay socket.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		c.mu.Lock()
		for key, r := range c.relays {
			r.conn.Close()
			delete(c.relays, key)
		}
		c.mu.Unlock()
		err = c.sock.Close()
	})
	return err
}

// ProbeFront checks that the front actually speaks QUIC at the given address,
// before the node starts serving. A node whose front is unreachable has no
// shell to hide behind and must fail loudly rather than quietly serve a path
// that identifies it (spec §4.6).
//
// The probe sends an Initial packet bearing a version no endpoint implements.
// RFC 9000 §6.1 requires a server to answer that with a Version Negotiation
// packet, so a reply proves reachability and that something QUIC is listening —
// which merely opening a UDP socket does not, UDP having no connection to
// establish and no error to report.
func ProbeFront(front *net.UDPAddr, timeout time.Duration) error {
	conn, err := net.DialUDP("udp", nil, front)
	if err != nil {
		return fmt.Errorf("demux: cannot open socket toward front %s: %w", front, err)
	}
	defer conn.Close()

	if _, err := conn.Write(versionNegotiationProbe()); err != nil {
		return fmt.Errorf("demux: cannot send to front %s: %w", front, err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	buf := make([]byte, maxDatagram)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("demux: front %s did not answer a QUIC probe: %w", front, err)
	}
	// A Version Negotiation packet is a long header carrying version 0.
	if n < 5 || buf[0]&0x80 == 0 || binary.BigEndian.Uint32(buf[1:5]) != 0 {
		return fmt.Errorf("demux: front %s answered %d bytes that are not QUIC version negotiation", front, n)
	}
	return nil
}

// versionNegotiationProbe builds an Initial packet with a reserved version.
//
// It is padded to 1200 bytes because RFC 9000 §14.1 lets a server discard
// anything smaller that might start a connection; an unpadded probe would come
// back as "unreachable" against a perfectly good front.
func versionNegotiationProbe() []byte {
	var dcid, scid [8]byte
	rand.Read(dcid[:])
	rand.Read(scid[:])

	d := make([]byte, 0, 1200)
	d = append(d, 0xc0)
	// 0x?a?a?a?a is the reserved pattern of RFC 9000 §15, guaranteed to name no
	// real version, so no endpoint can mistake this for a connection attempt.
	d = binary.BigEndian.AppendUint32(d, 0x0a0a0a0a)
	d = append(d, byte(len(dcid)))
	d = append(d, dcid[:]...)
	d = append(d, byte(len(scid)))
	d = append(d, scid[:]...)
	d = append(d, 0x00) // token length 0
	return append(d, make([]byte, 1200-len(d))...)
}

var _ net.PacketConn = (*Conn)(nil)
