package smux

import (
	"container/heap"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultAcceptBacklog = 1024
)

var (
	ErrInvalidProtocol  = errors.New("invalid protocol")
	ErrConsumed         = errors.New("peer consumed more than sent")
	ErrGoAway           = errors.New("stream id overflows, should start a new connection")
	ErrTimeout          = errors.New("timeout")
	ErrInvalidOperation = errors.New("invalid parameters on poll")
	ErrWouldBlock       = errors.New("operation would block on IO")
)

type writeRequest struct {
	prio   uint64
	frame  Frame
	result chan writeResult
}

type writeResult struct {
	n   int
	err error
}

type buffersWriter interface {
	WriteBuffers(v [][]byte) (n int, err error)
}

// Session defines a multiplexed connection for streams
type Session struct {
	conn io.ReadWriteCloser

	config           *Config
	nextStreamID     uint32 // next stream identifier
	nextStreamIDLock sync.Mutex

	bucket       int32         // token bucket
	bucketNotify chan struct{} // used for waiting for tokens

	streams    map[uint32]*Stream // all streams in this session
	streamLock sync.Mutex         // locks streams

	die     chan struct{} // flag session has died
	dieOnce sync.Once

	// socket error handling
	socketReadError      atomic.Value
	socketWriteError     atomic.Value
	chSocketReadError    chan struct{}
	chSocketWriteError   chan struct{}
	socketReadErrorOnce  sync.Once
	socketWriteErrorOnce sync.Once

	// smux protocol errors
	protoError     atomic.Value
	chProtoError   chan struct{}
	protoErrorOnce sync.Once

	chAccepts chan *Stream

	dataReady int32 // flag data has arrived

	goAway int32 // flag id exhausted

	halfClose int32 // peer keeps reading after its first FIN (SetPeerHalfClose)

	// receive-window auto-tuning (Config.MinStreamBuffer, k2 patch 6)
	autoTune bool
	grown    int64 // window growth granted to live streams, beyond MinStreamBuffer
	growPool int64 // cap on grown

	deadline atomic.Value

	shaper chan writeRequest // a shaper for writing
	writes chan writeRequest
}

func newSession(config *Config, conn io.ReadWriteCloser, client bool) *Session {
	s := new(Session)
	s.die = make(chan struct{})
	s.conn = conn
	s.config = config
	s.streams = make(map[uint32]*Stream)
	s.chAccepts = make(chan *Stream, defaultAcceptBacklog)
	s.bucket = int32(config.MaxReceiveBuffer)
	if config.MinStreamBuffer > 0 && config.MinStreamBuffer < config.MaxStreamBuffer {
		s.autoTune = true
		s.growPool = int64(config.MaxReceiveBuffer / 2)
	}
	s.bucketNotify = make(chan struct{}, 1)
	s.shaper = make(chan writeRequest)
	s.writes = make(chan writeRequest)
	s.chSocketReadError = make(chan struct{})
	s.chSocketWriteError = make(chan struct{})
	s.chProtoError = make(chan struct{})

	if client {
		s.nextStreamID = 1
	} else {
		s.nextStreamID = 0
	}

	go s.shaperLoop()
	go s.recvLoop()
	go s.sendLoop()
	go s.keepalive()
	return s
}

// OpenStream is used to create a new stream
func (s *Session) OpenStream() (*Stream, error) {
	if s.IsClosed() {
		return nil, io.ErrClosedPipe
	}

	// generate stream id
	s.nextStreamIDLock.Lock()
	if s.goAway > 0 {
		s.nextStreamIDLock.Unlock()
		return nil, ErrGoAway
	}

	s.nextStreamID += 2
	sid := s.nextStreamID
	if sid == sid%2 { // stream-id overflows
		s.goAway = 1
		s.nextStreamIDLock.Unlock()
		return nil, ErrGoAway
	}
	s.nextStreamIDLock.Unlock()

	stream := newStream(sid, s.config.MaxFrameSize, s)

	// Register before the SYN goes out: a peer that accepts and writes at
	// once can otherwise land its PSH/FIN in recvLoop before the stream is
	// in s.streams, where it is dropped as a frame for an unknown stream
	// (K2_PATCHES.md patch 5).
	s.streamLock.Lock()
	select {
	case <-s.chSocketWriteError:
		s.streamLock.Unlock()
		return nil, s.socketWriteError.Load().(error)
	case <-s.die:
		s.streamLock.Unlock()
		return nil, io.ErrClosedPipe
	default:
		s.streams[sid] = stream
	}
	s.streamLock.Unlock()

	if _, err := s.writeFrame(newFrame(cmdSYN, sid)); err != nil {
		s.streamClosed(sid)
		return nil, err
	}
	if err := stream.sendInitialWindow(); err != nil {
		stream.Close()
		return nil, err
	}
	return stream, nil
}

// Open returns a generic ReadWriteCloser
func (s *Session) Open() (io.ReadWriteCloser, error) {
	return s.OpenStream()
}

// AcceptStream is used to block until the next available stream
// is ready to be accepted.
func (s *Session) AcceptStream() (*Stream, error) {
	var deadline <-chan time.Time
	if d, ok := s.deadline.Load().(time.Time); ok && !d.IsZero() {
		timer := time.NewTimer(time.Until(d))
		defer timer.Stop()
		deadline = timer.C
	}

	select {
	case stream := <-s.chAccepts:
		if err := stream.sendInitialWindow(); err != nil {
			stream.Close()
			return nil, err
		}
		return stream, nil
	case <-deadline:
		return nil, ErrTimeout
	case <-s.chSocketReadError:
		return nil, s.socketReadError.Load().(error)
	case <-s.chProtoError:
		return nil, s.protoError.Load().(error)
	case <-s.die:
		return nil, io.ErrClosedPipe
	}
}

// Accept Returns a generic ReadWriteCloser instead of smux.Stream
func (s *Session) Accept() (io.ReadWriteCloser, error) {
	return s.AcceptStream()
}

// Close is used to close the session and all streams.
func (s *Session) Close() error {
	var once bool
	s.dieOnce.Do(func() {
		close(s.die)
		once = true
	})

	if once {
		s.streamLock.Lock()
		for k := range s.streams {
			s.streams[k].sessionClose()
		}
		s.streamLock.Unlock()
		return s.conn.Close()
	} else {
		return io.ErrClosedPipe
	}
}

// notifyBucket notifies recvLoop that bucket is available
func (s *Session) notifyBucket() {
	select {
	case s.bucketNotify <- struct{}{}:
	default:
	}
}

// The three notify*Error helpers close the session too: recvLoop or
// sendLoop has exited, so the session can never move data again, and
// leaving it open let OpenStream succeed on it and writers wait on window
// updates that could not arrive (K2_PATCHES.md patch 5).
func (s *Session) notifyReadError(err error) {
	s.socketReadErrorOnce.Do(func() {
		s.socketReadError.Store(err)
		close(s.chSocketReadError)
	})
	s.Close()
}

func (s *Session) notifyWriteError(err error) {
	s.socketWriteErrorOnce.Do(func() {
		s.socketWriteError.Store(err)
		close(s.chSocketWriteError)
	})
	s.Close()
}

func (s *Session) notifyProtoError(err error) {
	s.protoErrorOnce.Do(func() {
		s.protoError.Store(err)
		close(s.chProtoError)
	})
	s.Close()
}

// IsClosed does a safe check to see if we have shutdown
func (s *Session) IsClosed() bool {
	select {
	case <-s.die:
		return true
	default:
		return false
	}
}

// NumStreams returns the number of currently open streams
func (s *Session) NumStreams() int {
	if s.IsClosed() {
		return 0
	}
	s.streamLock.Lock()
	defer s.streamLock.Unlock()
	return len(s.streams)
}

// SetDeadline sets a deadline used by Accept* calls.
// A zero time value disables the deadline.
func (s *Session) SetDeadline(t time.Time) error {
	s.deadline.Store(t)
	return nil
}

// LocalAddr satisfies net.Conn interface
func (s *Session) LocalAddr() net.Addr {
	if ts, ok := s.conn.(interface {
		LocalAddr() net.Addr
	}); ok {
		return ts.LocalAddr()
	}
	return nil
}

// RemoteAddr satisfies net.Conn interface
func (s *Session) RemoteAddr() net.Addr {
	if ts, ok := s.conn.(interface {
		RemoteAddr() net.Addr
	}); ok {
		return ts.RemoteAddr()
	}
	return nil
}

// SetPeerHalfClose records whether the peer supports half-close (k2 patch 2):
// whether its first FIN on a stream leaves it reading. smux has no handshake
// of its own, so the caller learns this out of band. Until it is set, a
// peer's first FIN ends our writes on that stream, as in upstream.
func (s *Session) SetPeerHalfClose(v bool) {
	var n int32
	if v {
		n = 1
	}
	atomic.StoreInt32(&s.halfClose, n)
}

func (s *Session) peerHalfClose() bool { return atomic.LoadInt32(&s.halfClose) == 1 }

// notify the session that a stream has closed
func (s *Session) streamClosed(sid uint32) {
	s.streamLock.Lock()
	if stream, ok := s.streams[sid]; ok {
		n, grown := stream.recycleTokens()
		if n > 0 { // return remaining tokens to the bucket
			if atomic.AddInt32(&s.bucket, int32(n)) > 0 {
				s.notifyBucket()
			}
		}
		if grown > 0 {
			atomic.AddInt64(&s.grown, -int64(grown))
		}
	}
	delete(s.streams, sid)
	s.streamLock.Unlock()
}

// reserveGrowth grants a stream with receive window cur up to cur more
// (doubling it), limited by MaxStreamBuffer and by what is left of the
// session's growth pool. It returns the bytes granted, possibly 0.
func (s *Session) reserveGrowth(cur uint32) uint32 {
	want := int64(cur)
	if max := int64(s.config.MaxStreamBuffer) - int64(cur); want > max {
		want = max
	}
	for want > 0 {
		grown := atomic.LoadInt64(&s.grown)
		d := s.growPool - grown
		if d <= 0 {
			return 0
		}
		if d > want {
			d = want
		}
		if atomic.CompareAndSwapInt64(&s.grown, grown, grown+d) {
			return uint32(d)
		}
	}
	return 0
}

// returnTokens is called by stream to return token after read
func (s *Session) returnTokens(n int) {
	if atomic.AddInt32(&s.bucket, int32(n)) > 0 {
		s.notifyBucket()
	}
}

// recvLoop keeps on reading from underlying connection if tokens are available
func (s *Session) recvLoop() {
	var hdr rawHeader
	var updHdr updHeader

	for {
		for atomic.LoadInt32(&s.bucket) <= 0 && !s.IsClosed() {
			select {
			case <-s.bucketNotify:
			case <-s.die:
				return
			}
		}

		// read header first
		if _, err := io.ReadFull(s.conn, hdr[:]); err == nil {
			atomic.StoreInt32(&s.dataReady, 1)
			if hdr.Version() != version {
				s.notifyProtoError(ErrInvalidProtocol)
				return
			}
			sid := hdr.StreamID()
			// Only PSH and UPD carry a payload, and UPD's is fixed. A
			// length on any other frame would be parsed as the next header
			// and desync the stream (K2_PATCHES.md patch 5).
			switch hdr.Cmd() {
			case cmdNOP, cmdSYN, cmdFIN:
				if hdr.Length() != 0 {
					s.notifyProtoError(ErrInvalidProtocol)
					return
				}
			case cmdUPD:
				if hdr.Length() != szCmdUPD {
					s.notifyProtoError(ErrInvalidProtocol)
					return
				}
			}
			switch hdr.Cmd() {
			case cmdNOP:
			case cmdSYN:
				var accepted *Stream
				s.streamLock.Lock()
				if _, ok := s.streams[sid]; !ok {
					accepted = newStream(sid, s.config.MaxFrameSize, s)
					s.streams[sid] = accepted
				}
				s.streamLock.Unlock()
				// Hand off outside streamLock: a full accept backlog blocks
				// recvLoop (backpressure), and holding the lock meanwhile
				// also blocked every Stream.Close and OpenStream.
				if accepted != nil {
					select {
					case s.chAccepts <- accepted:
					case <-s.die:
						return
					}
				}
			case cmdFIN:
				s.streamLock.Lock()
				if stream, ok := s.streams[sid]; ok {
					stream.fin()
					stream.notifyReadEvent()
				}
				s.streamLock.Unlock()
			case cmdPSH:
				if hdr.Length() > 0 {
					newbuf := defaultAllocator.Get(int(hdr.Length()))
					if written, err := io.ReadFull(s.conn, newbuf); err == nil {
						s.streamLock.Lock()
						if stream, ok := s.streams[sid]; ok {
							stream.pushBytes(newbuf)
							atomic.AddInt32(&s.bucket, -int32(written))
							stream.notifyReadEvent()
						} else {
							defaultAllocator.Put(newbuf)
						}
						s.streamLock.Unlock()
					} else {
						s.notifyReadError(err)
						return
					}
				}
			case cmdUPD:
				if _, err := io.ReadFull(s.conn, updHdr[:]); err == nil {
					// Write computes the remaining window in int32; a window
					// past MaxInt32 goes negative there and the writer waits
					// forever for an update that would fix it.
					if updHdr.Window() > math.MaxInt32 {
						s.notifyProtoError(ErrInvalidProtocol)
						return
					}
					s.streamLock.Lock()
					if stream, ok := s.streams[sid]; ok {
						stream.update(updHdr.Consumed(), updHdr.Window())
					}
					s.streamLock.Unlock()
				} else {
					s.notifyReadError(err)
					return
				}
			default:
				s.notifyProtoError(ErrInvalidProtocol)
				return
			}
		} else {
			s.notifyReadError(err)
			return
		}
	}
}

func (s *Session) keepalive() {
	tickerPing := time.NewTicker(s.config.KeepAliveInterval)
	tickerTimeout := time.NewTicker(s.config.KeepAliveTimeout)
	defer tickerPing.Stop()
	defer tickerTimeout.Stop()
	for {
		select {
		case <-tickerPing.C:
			s.writeFrameInternal(newFrame(cmdNOP, 0), tickerPing.C, 0)
			s.notifyBucket() // force a signal to the recvLoop
		case <-tickerTimeout.C:
			if !atomic.CompareAndSwapInt32(&s.dataReady, 1, 0) {
				s.Close()
				return
			}
		case <-s.die:
			return
		}
	}
}

// shaper shapes the sending sequence among streams
func (s *Session) shaperLoop() {
	var reqs shaperHeap
	var next writeRequest
	var chWrite chan writeRequest

	for {
		if len(reqs) > 0 {
			chWrite = s.writes
			next = heap.Pop(&reqs).(writeRequest)
		} else {
			chWrite = nil
		}

		select {
		case <-s.die:
			return
		case r := <-s.shaper:
			if chWrite != nil { // next is valid, reshape
				heap.Push(&reqs, next)
			}
			heap.Push(&reqs, r)
		case chWrite <- next:
		}
	}
}

func (s *Session) sendLoop() {
	var buf []byte
	var n int
	var err error
	var vec [][]byte // vector for writeBuffers

	// A peer that keeps sending NOPs but stops reading would otherwise block
	// this loop in Write forever: keepalive only watches the receive side,
	// so nothing ever closed the session (K2_PATCHES.md patch 5). Bound
	// every write by KeepAliveTimeout. The deadline is pushed forward only
	// once less than half of it is left, not per frame.
	dl, _ := s.conn.(interface{ SetWriteDeadline(time.Time) error })
	var writeDeadline time.Time
	timeout := s.config.KeepAliveTimeout

	bw, ok := s.conn.(buffersWriter)
	if ok {
		buf = make([]byte, headerSize)
		vec = make([][]byte, 2)
	} else {
		buf = make([]byte, (1<<16)+headerSize)
	}

	for {
		select {
		case <-s.die:
			return
		case request := <-s.writes:
			buf[0] = request.frame.ver
			buf[1] = request.frame.cmd
			binary.LittleEndian.PutUint16(buf[2:], uint16(len(request.frame.data)))
			binary.LittleEndian.PutUint32(buf[4:], request.frame.sid)

			if dl != nil {
				if now := time.Now(); writeDeadline.Sub(now) < timeout/2 {
					writeDeadline = now.Add(timeout)
					dl.SetWriteDeadline(writeDeadline)
				}
			}

			if len(vec) > 0 {
				vec[0] = buf[:headerSize]
				vec[1] = request.frame.data
				n, err = bw.WriteBuffers(vec)
			} else {
				copy(buf[headerSize:], request.frame.data)
				n, err = s.conn.Write(buf[:headerSize+len(request.frame.data)])
			}

			if err == nil && n < headerSize+len(request.frame.data) {
				// A short write without an error leaves a partial frame
				// on the wire; the stream is desynced, treat it as fatal.
				err = io.ErrShortWrite
			}
			n -= headerSize
			if n < 0 {
				n = 0
			}

			result := writeResult{
				n:   n,
				err: err,
			}

			request.result <- result
			close(request.result)

			// store conn error
			if err != nil {
				s.notifyWriteError(err)
				return
			}
		}
	}
}

// writeFrame writes the frame to the underlying connection
// and returns the number of bytes written if successful
func (s *Session) writeFrame(f Frame) (n int, err error) {
	return s.writeFrameInternal(f, nil, 0)
}

// internal writeFrame version to support deadline used in keepalive
func (s *Session) writeFrameInternal(f Frame, deadline <-chan time.Time, prio uint64) (int, error) {
	req := writeRequest{
		prio:   prio,
		frame:  f,
		result: make(chan writeResult, 1),
	}
	select {
	case s.shaper <- req:
	case <-s.die:
		return 0, io.ErrClosedPipe
	case <-s.chSocketWriteError:
		return 0, s.socketWriteError.Load().(error)
	case <-deadline:
		return 0, ErrTimeout
	}

	select {
	case result := <-req.result:
		return result.n, result.err
	case <-s.die:
		return 0, io.ErrClosedPipe
	case <-s.chSocketWriteError:
		return 0, s.socketWriteError.Load().(error)
	case <-deadline:
		return 0, ErrTimeout
	}
}
