//go:build ios

package appext

import "runtime/debug"

func init() {
	// iOS NE jetsam limit ~50MB. Aggressive GC + hard heap budget to stay safe.
	// GOGC=10: trigger GC at 10% heap growth (vs default 100%). More frequent
	// but smaller GC cycles — smoother memory curve, prevents spikes.
	// MemoryLimit=35MB: hard ceiling leaving 15MB for C/ObjC/system frameworks.
	// Together they form a dual safety net (sing-box uses same GOGC=10 strategy).
	debug.SetGCPercent(10)
	debug.SetMemoryLimit(35 << 20)
}
