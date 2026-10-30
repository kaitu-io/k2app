package smux

import (
	"container/heap"
	"testing"
)

func TestShaper(t *testing.T) {
	w1 := writeRequest{prio: 10}
	w2 := writeRequest{prio: 10}
	w3 := writeRequest{prio: 20}
	w4 := writeRequest{prio: 100}

	var reqs shaperHeap
	heap.Push(&reqs, w4)
	heap.Push(&reqs, w3)
	heap.Push(&reqs, w2)
	heap.Push(&reqs, w1)

	var lastPrio uint64
	for len(reqs) > 0 {
		w := heap.Pop(&reqs).(writeRequest)
		if w.prio < lastPrio {
			t.Fatal("incorrect shaper priority")
		}

		t.Log("prio:", w.prio)
		lastPrio = w.prio
	}
}

// Equal priorities leave in enqueue order: two window updates for one stream
// (both prio 0) must not overtake each other, or the peer keeps the stale one.
func TestShaper_EqualPriorityIsFIFO(t *testing.T) {
	var reqs shaperHeap
	for _, seq := range []uint64{5, 1, 9, 3, 7, 2, 8, 4, 6} {
		heap.Push(&reqs, writeRequest{prio: 0, seq: seq})
	}
	heap.Push(&reqs, writeRequest{prio: 10, seq: 0})
	for want := uint64(1); want <= 9; want++ {
		if w := heap.Pop(&reqs).(writeRequest); w.seq != want {
			t.Fatalf("popped seq %d, want %d", w.seq, want)
		}
	}
}
