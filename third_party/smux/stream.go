package smux

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Stream implements net.Conn
type Stream struct {
	id   uint32
	sess *Session

	buffers [][]byte
	heads   [][]byte // slice heads kept for recycle

	bufferLock sync.Mutex
	frameSize  int

	// notify a read event
	chReadEvent chan struct{}

	// flag the stream has closed
	die     chan struct{}
	dieOnce sync.Once

	// FIN command. A stream ends with two FINs from each side (k2 patch 2):
	// the first closes that side's write direction, the second its read
	// direction. Upstream sent a single FIN, from Close.
	chFinEvent   chan struct{} // peer's first FIN: peer stopped writing
	chPeerClosed chan struct{} // peer's second FIN: peer stopped reading too
	finsRecv     uint32        // FINs received, saturating at 3; written only by recvLoop

	// our write direction (CloseWrite)
	writeClosed     chan struct{}
	writeClosedOnce sync.Once
	finMu           sync.Mutex
	finsSent        int // guarded by finMu

	// deadlines
	readDeadline  atomic.Value
	writeDeadline atomic.Value

	// per stream sliding window control
	numRead    uint32 // number of consumed bytes
	numWritten uint32 // count num of bytes written
	incr       uint32 // counting for sending

	// receive window we advertise (k2 patch 6); guarded by bufferLock
	rcvWnd       uint32
	tuneStart    time.Time // start of the current auto-tuning sample
	tuneBytes    uint32    // bytes consumed since tuneStart
	tuneReleased bool      // growth returned to the session; never grow again

	// UPD command
	peerConsumed uint32        // num of bytes the peer has consumed
	peerWindow   uint32        // peer window, initialized to 256KB, updated by peer
	chUpdate     chan struct{} // notify of remote data consuming and window update
}

// newStream initiates a Stream struct
func newStream(id uint32, frameSize int, sess *Session) *Stream {
	s := new(Stream)
	s.id = id
	s.chReadEvent = make(chan struct{}, 1)
	s.chUpdate = make(chan struct{}, 1)
	s.frameSize = frameSize
	s.sess = sess
	s.die = make(chan struct{})
	s.chFinEvent = make(chan struct{})
	s.chPeerClosed = make(chan struct{})
	s.writeClosed = make(chan struct{})
	s.peerWindow = initialPeerWindow // set to initial window size
	s.rcvWnd = uint32(sess.config.MaxStreamBuffer)
	if sess.autoTune {
		s.rcvWnd = uint32(sess.config.MinStreamBuffer)
		s.tuneStart = time.Now()
	}
	return s
}

// tuneInterval is the auto-tuning sample: a stream whose reader consumes
// its whole window within it gets the window doubled. A window-limited
// sender delivers one window per round trip, so any path with RTT under
// tuneInterval grows; a stream that is not window-limited grows only to
// about its rate × tuneInterval.
const tuneInterval = time.Second

// consumedLocked accounts n bytes read by the user and decides whether to
// send a window update; it returns the update's consumed and window values
// (ok=false: none due). Caller holds bufferLock.
func (s *Stream) consumedLocked(n int) (consumed, window uint32, ok bool) {
	s.numRead += uint32(n)
	s.incr += uint32(n)
	if s.sess.autoTune && !s.tuneReleased && n > 0 {
		s.tuneBytes += uint32(n)
		if s.tuneBytes >= s.rcvWnd {
			now := time.Now()
			if now.Sub(s.tuneStart) <= tuneInterval {
				if d := s.sess.reserveGrowth(s.rcvWnd); d > 0 {
					s.rcvWnd += d
					ok = true // tell the sender now, not at half a window
				}
			}
			s.tuneStart, s.tuneBytes = now, 0
		}
	}
	// in an ideal environment:
	// if more than half of buffer has consumed, send read ack to peer
	// based on round-trip time of ACK, continous flowing data
	// won't slow down because of waiting for ACK, as long as the
	// consumer keeps on reading data
	// s.numRead == n also notify window at the first read
	if s.incr >= s.rcvWnd/2 || (n > 0 && s.numRead == uint32(n)) {
		ok = true
	}
	if !ok {
		return 0, 0, false
	}
	s.incr = 0
	return s.numRead, s.rcvWnd, true
}

// sendInitialWindow tells the peer our window right after SYN (opener) or
// accept (acceptor). Without it the peer assumes initialPeerWindow, 256 KB,
// until our first read, which with auto-tuning is far more than we grant.
// The opener's update is queued right behind its SYN, ahead of any data,
// and the acceptor's goes out before AcceptStream returns, so before its
// first reply: either way the peer applies it before it sends us data.
func (s *Stream) sendInitialWindow() error {
	if !s.sess.autoTune {
		return nil
	}
	s.bufferLock.Lock()
	consumed, window := s.numRead, s.rcvWnd
	s.bufferLock.Unlock()
	return s.sendWindowUpdate(consumed, window)
}

// ID returns the unique stream ID.
func (s *Stream) ID() uint32 {
	return s.id
}

// Read implements net.Conn
func (s *Stream) Read(b []byte) (n int, err error) {
	for {
		n, err = s.TryRead(b)
		if err == ErrWouldBlock {
			if ew := s.waitRead(); ew != nil {
				return 0, ew
			}
		} else {
			return n, err
		}
	}
}

// TryRead is the nonblocking version of Read
func (s *Stream) TryRead(b []byte) (n int, err error) {
	if len(b) == 0 {
		return 0, nil
	}

	s.bufferLock.Lock()
	if len(s.buffers) > 0 {
		n = copy(b, s.buffers[0])
		s.buffers[0] = s.buffers[0][n:]
		if len(s.buffers[0]) == 0 {
			s.buffers[0] = nil
			s.buffers = s.buffers[1:]
			// full recycle
			defaultAllocator.Put(s.heads[0])
			s.heads = s.heads[1:]
		}
	}
	consumed, window, notify := s.consumedLocked(n)
	s.bufferLock.Unlock()

	if n > 0 {
		s.sess.returnTokens(n)
		if notify {
			err := s.sendWindowUpdate(consumed, window)
			return n, err
		} else {
			return n, nil
		}
	}

	select {
	case <-s.die:
		return 0, io.EOF
	default:
		return 0, ErrWouldBlock
	}
}

// WriteTo implements io.WriteTo
func (s *Stream) WriteTo(w io.Writer) (n int64, err error) {
	for {
		var buf []byte
		s.bufferLock.Lock()
		if len(s.buffers) > 0 {
			buf = s.buffers[0]
			s.buffers = s.buffers[1:]
			s.heads = s.heads[1:]
		}
		consumed, window, notify := s.consumedLocked(len(buf))
		s.bufferLock.Unlock()

		if buf != nil {
			nw, ew := w.Write(buf)
			s.sess.returnTokens(len(buf))
			defaultAllocator.Put(buf)
			if nw > 0 {
				n += int64(nw)
			}

			if ew == nil && nw < len(buf) {
				ew = io.ErrShortWrite // the rest of buf would be lost silently
			}
			if ew != nil {
				return n, ew
			}

			if notify {
				if err := s.sendWindowUpdate(consumed, window); err != nil {
					return n, err
				}
			}
		} else if ew := s.waitRead(); ew != nil {
			if ew == io.EOF {
				// io.WriterTo: reaching EOF is success. io.Copy hands this
				// error straight back to its caller, so EOF here made a
				// clean peer FIN look like a broken stream.
				return n, nil
			}
			return n, ew
		}
	}
}

func (s *Stream) sendWindowUpdate(consumed, window uint32) error {
	var timer *time.Timer
	var deadline <-chan time.Time
	if d, ok := s.readDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	frame := newFrame(cmdUPD, s.id)
	var hdr updHeader
	binary.LittleEndian.PutUint32(hdr[:], consumed)
	binary.LittleEndian.PutUint32(hdr[4:], window)
	frame.data = hdr[:]
	_, err := s.sess.writeFrameInternal(frame, deadline, 0)
	return err
}

func (s *Stream) waitRead() error {
	var timer *time.Timer
	var deadline <-chan time.Time
	if d, ok := s.readDeadline.Load().(time.Time); ok && !d.IsZero() {
		timer = time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case <-s.chReadEvent:
		return nil
	case <-s.chFinEvent:
		// Data and FIN can both be pending when select runs; it picks a ready
		// case at random. Report EOF only once the buffer is drained, or the
		// peer's last bytes are dropped (upstream fix for xtaci/smux#82).
		s.bufferLock.Lock()
		defer s.bufferLock.Unlock()
		if len(s.buffers) > 0 {
			return nil
		}
		return io.EOF
	case <-s.sess.chSocketReadError:
		return s.sess.socketReadError.Load().(error)
	case <-s.sess.chProtoError:
		return s.sess.protoError.Load().(error)
	case <-deadline:
		return ErrTimeout
	case <-s.die:
		return io.ErrClosedPipe
	}

}

// Write implements net.Conn
//
// Note that the behavior when multiple goroutines write concurrently is not deterministic,
// frames may interleave in random way.
func (s *Stream) Write(b []byte) (n int, err error) {
	// check empty input
	if len(b) == 0 {
		return 0, nil
	}

	// check if stream has closed
	select {
	case <-s.die:
		return 0, io.ErrClosedPipe
	case <-s.writeClosed:
		return 0, io.ErrClosedPipe
	case <-s.chPeerClosed:
		return 0, io.EOF
	default:
	}

	// A peer that supports half-close keeps reading after its first FIN, so
	// only its second FIN (chPeerClosed) ends our writes. Any other peer's
	// single FIN is a full close and must still end them, as upstream did:
	// that peer will never send another window update.
	peerFin := s.chFinEvent
	if s.sess.peerHalfClose() {
		peerFin = nil
	}

	// The write deadline is re-read on every pass, so a SetWriteDeadline
	// from another goroutine (which wakes us via chUpdate) applies to a
	// writer already blocked on the window (K2_PATCHES.md patch 5).
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	// frame split and transmit process
	sent := 0
	frame := newFrame(cmdPSH, s.id)

	for {
		var deadline <-chan time.Time
		if timer != nil {
			timer.Stop()
			timer = nil
		}
		if d, ok := s.writeDeadline.Load().(time.Time); ok && !d.IsZero() {
			timer = time.NewTimer(time.Until(d))
			deadline = timer.C
		}

		// per stream sliding window control
		// [.... [consumed... numWritten] ... win... ]
		// [.... [consumed...................+rmtwnd]]
		var bts []byte
		// note:
		// even if uint32 overflow, this math still works:
		// eg1: uint32(0) - uint32(math.MaxUint32) = 1
		// eg2: int32(uint32(0) - uint32(1)) = -1
		// security check for misbehavior
		inflight := int32(atomic.LoadUint32(&s.numWritten) - atomic.LoadUint32(&s.peerConsumed))
		if inflight < 0 {
			return 0, ErrConsumed
		}

		win := int32(atomic.LoadUint32(&s.peerWindow)) - inflight
		if win > 0 {
			if win > int32(len(b)) {
				bts = b
				b = nil
			} else {
				bts = b[:win]
				b = b[win:]
			}

			for len(bts) > 0 {
				sz := len(bts)
				if sz > s.frameSize {
					sz = s.frameSize
				}
				frame.data = bts[:sz]
				bts = bts[sz:]
				n, err := s.sess.writeFrameInternal(frame, deadline, uint64(atomic.LoadUint32(&s.numWritten)))
				atomic.AddUint32(&s.numWritten, uint32(sz))
				sent += n
				if err != nil {
					return sent, err
				}
			}
		}

		// if there is any data remaining to be sent
		// wait until stream closes, window changes or deadline reached
		// this blocking behavior will inform upper layer to do flow control
		if len(b) > 0 {
			select {
			case <-peerFin: // peer fully closed, future window update is impossible
				return sent, io.EOF
			case <-s.chPeerClosed:
				return sent, io.EOF
			case <-s.writeClosed:
				return sent, io.ErrClosedPipe
			case <-s.die:
				return sent, io.ErrClosedPipe
			case <-deadline:
				return sent, ErrTimeout
			case <-s.sess.chSocketWriteError:
				return sent, s.sess.socketWriteError.Load().(error)
			case <-s.chUpdate:
				continue
			}
		} else {
			return sent, nil
		}
	}
}

// CloseWrite closes the write direction: it sends FIN and leaves the stream
// readable until Close. Calling it more than once is a no-op.
func (s *Stream) CloseWrite() error {
	select {
	case <-s.die:
		return io.ErrClosedPipe
	default:
	}
	s.writeClosedOnce.Do(func() { close(s.writeClosed) })
	return s.sendFINs(1)
}

// Close implements net.Conn. It closes both directions, so it brings the FINs
// sent for this stream to two: the peer's reads end at the first and its
// writes at the second. A peer running upstream smux treats every FIN after
// the first as a no-op.
func (s *Stream) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})

	if once {
		s.writeClosedOnce.Do(func() { close(s.writeClosed) })
		err := s.sendFINs(2)
		s.sess.streamClosed(s.id)
		return err
	} else {
		return io.ErrClosedPipe
	}
}

// sendFINs sends FINs until want have been sent for this stream. The count
// is claimed under finMu and the frames are written outside it.
func (s *Stream) sendFINs(want int) error {
	s.finMu.Lock()
	n := want - s.finsSent
	if n > 0 {
		s.finsSent = want
	}
	s.finMu.Unlock()

	var err error
	for i := 0; i < n; i++ {
		if _, e := s.sess.writeFrame(newFrame(cmdFIN, s.id)); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// GetDieCh returns a readonly chan which can be readable
// when the stream is to be closed.
func (s *Stream) GetDieCh() <-chan struct{} {
	return s.die
}

// SetReadDeadline sets the read deadline as defined by
// net.Conn.SetReadDeadline.
// A zero time value disables the deadline.
func (s *Stream) SetReadDeadline(t time.Time) error {
	s.readDeadline.Store(t)
	s.notifyReadEvent() // a blocked reader re-reads the deadline
	return nil
}

// SetWriteDeadline sets the write deadline as defined by
// net.Conn.SetWriteDeadline.
// A zero time value disables the deadline.
func (s *Stream) SetWriteDeadline(t time.Time) error {
	s.writeDeadline.Store(t)
	select { // a blocked writer re-reads the deadline
	case s.chUpdate <- struct{}{}:
	default:
	}
	return nil
}

// SetDeadline sets both read and write deadlines as defined by
// net.Conn.SetDeadline.
// A zero time value disables the deadlines.
func (s *Stream) SetDeadline(t time.Time) error {
	if err := s.SetReadDeadline(t); err != nil {
		return err
	}
	if err := s.SetWriteDeadline(t); err != nil {
		return err
	}
	return nil
}

// session closes
func (s *Stream) sessionClose() { s.dieOnce.Do(func() { close(s.die) }) }

// LocalAddr satisfies net.Conn interface
func (s *Stream) LocalAddr() net.Addr {
	if ts, ok := s.sess.conn.(interface {
		LocalAddr() net.Addr
	}); ok {
		return ts.LocalAddr()
	}
	return nil
}

// RemoteAddr satisfies net.Conn interface
func (s *Stream) RemoteAddr() net.Addr {
	if ts, ok := s.sess.conn.(interface {
		RemoteAddr() net.Addr
	}); ok {
		return ts.RemoteAddr()
	}
	return nil
}

// pushBytes append buf to buffers
func (s *Stream) pushBytes(buf []byte) (written int, err error) {
	s.bufferLock.Lock()
	s.buffers = append(s.buffers, buf)
	s.heads = append(s.heads, buf)
	s.bufferLock.Unlock()
	return
}

// recycleTokens transform remaining bytes to tokens(will truncate buffer)
// It also hands back the window growth this stream holds, which the
// caller returns to the session's pool.
func (s *Stream) recycleTokens() (n int, grown uint32) {
	s.bufferLock.Lock()
	for k := range s.buffers {
		n += len(s.buffers[k])
		defaultAllocator.Put(s.heads[k])
	}
	s.buffers = nil
	s.heads = nil
	if s.sess.autoTune && !s.tuneReleased {
		grown = s.rcvWnd - uint32(s.sess.config.MinStreamBuffer)
		s.tuneReleased = true
	}
	s.bufferLock.Unlock()
	return
}

// notify read event
func (s *Stream) notifyReadEvent() {
	select {
	case s.chReadEvent <- struct{}{}:
	default:
	}
}

// update command
func (s *Stream) update(consumed uint32, window uint32) {
	atomic.StoreUint32(&s.peerConsumed, consumed)
	atomic.StoreUint32(&s.peerWindow, window)
	select {
	case s.chUpdate <- struct{}{}:
	default:
	}
}

// fin records a FIN from the peer: the first closes the peer's write
// direction, the second its read direction; further FINs are no-ops. The
// count saturates so a peer flooding FINs can never wrap it back to 1 and
// close chFinEvent twice (a panic in recvLoop takes the whole process down).
// 3 means "more than two", kept distinct so tests can spot a stray FIN.
// Called only from recvLoop.
func (s *Stream) fin() {
	n := atomic.LoadUint32(&s.finsRecv)
	if n >= 3 {
		return
	}
	atomic.StoreUint32(&s.finsRecv, n+1)
	switch n {
	case 0:
		close(s.chFinEvent)
	case 1:
		close(s.chPeerClosed)
	}
}
