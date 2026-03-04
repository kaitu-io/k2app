//go:build ios

package core

import (
	"runtime"
	"runtime/debug"
	"sync/atomic"
)

var connReleaseCount atomic.Int64

const (
	gcSampleInterval  = 8  // check heap every N-th connection release
	gcHeapThresholdMB = 20 // trigger GC if HeapInuse exceeds this
)

// onConnRelease is called after each TCP/UDP handler exits.
// On iOS, it periodically checks heap pressure and forces GC + FreeOSMemory
// to stay under the ~50 MB jetsam limit.
func onConnRelease() {
	if connReleaseCount.Add(1)%gcSampleInterval != 0 {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapInuse > gcHeapThresholdMB<<20 {
		runtime.GC()
		debug.FreeOSMemory()
	}
}
