package wire

import (
	"os"
	"sync"
	"time"
)

// readDeadline is a thread-safe read deadline for channel-based PacketConn types.
// Zero value is ready to use (no deadline set).
type readDeadline struct {
	mu sync.Mutex
	t  time.Time
}

// set stores the deadline. A zero value clears it.
func (d *readDeadline) set(t time.Time) {
	d.mu.Lock()
	d.t = t
	d.mu.Unlock()
}

// makeTimer returns a timer channel for use in ReadFrom's select.
//
// Three outcomes:
//   - No deadline set: returns (nil, nil, nil). Nil channel never fires in select.
//   - Deadline already passed: returns (nil, nil, os.ErrDeadlineExceeded).
//   - Deadline in future: returns (ch, timer, nil). Caller must defer timer.Stop().
func (d *readDeadline) makeTimer() (<-chan time.Time, *time.Timer, error) {
	d.mu.Lock()
	t := d.t
	d.mu.Unlock()

	if t.IsZero() {
		return nil, nil, nil
	}
	dur := time.Until(t)
	if dur <= 0 {
		return nil, nil, os.ErrDeadlineExceeded
	}
	timer := time.NewTimer(dur)
	return timer.C, timer, nil
}
