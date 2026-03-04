//go:build ios

package appext

import "runtime/debug"

func init() {
	// iOS NE jetsam limit ~50MB. Set Go heap budget to 35MB, leaving
	// headroom for C/ObjC runtime and system frameworks.
	// Without this, Go's GC triggers at 2× heap (GOGC=100), allowing
	// heap to grow from 20→40MB before GC runs — jetsam kills at 50MB first.
	debug.SetMemoryLimit(35 << 20)
}
