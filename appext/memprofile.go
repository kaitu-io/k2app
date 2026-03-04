package appext

import (
	"encoding/json"
	"runtime"
	"strings"
)

// MemorySnapshot returns a JSON memory profile aggregated by component.
// Useful for diagnosing iOS NE jetsam kills — call periodically or on memory warning.
//
// Output example:
//
//	{
//	  "heap_inuse_mb": 12.5,
//	  "heap_sys_mb": 24.0,
//	  "sys_mb": 30.2,
//	  "goroutines": 42,
//	  "components": {
//	    "quic-go":   {"inuse_bytes": 4194304, "alloc_bytes": 8388608, "alloc_objects": 1024},
//	    "sing-tun":  {"inuse_bytes": 2097152, ...},
//	    "k2rule":    {"inuse_bytes": 1048576, ...},
//	    "wire":      {"inuse_bytes": 524288, ...},
//	    "smux":      {"inuse_bytes": 262144, ...},
//	    "engine":    {"inuse_bytes": 131072, ...},
//	    "other":     {"inuse_bytes": 3145728, ...}
//	  }
//	}
//
// gomobile exports this as AppextMemorySnapshot (ObjC) / Appext.memorySnapshot (Java).
func MemorySnapshot() string {
	// 1. Overall runtime stats.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// 2. Per-allocation-site heap profile.
	n, _ := runtime.MemProfile(nil, true)
	records := make([]runtime.MemProfileRecord, n+50) // pad for concurrent allocs
	n, ok := runtime.MemProfile(records, true)
	if !ok {
		records = make([]runtime.MemProfileRecord, n*2)
		n, _ = runtime.MemProfile(records, true)
	}
	records = records[:n]

	// 3. Aggregate by component using import path prefixes.
	type compStats struct {
		InUseBytes   int64 `json:"inuse_bytes"`
		AllocBytes   int64 `json:"alloc_bytes"`
		AllocObjects int64 `json:"alloc_objects"`
	}

	components := map[string]*compStats{
		"quic-go":  {},
		"sing-tun": {},
		"sing":     {},
		"k2rule":   {},
		"wire":     {},
		"smux":     {},
		"engine":   {},
		"dns":      {},
		"core":     {},
		"appext":   {},
		"runtime":  {},
		"other":    {},
	}

	classifyStack := func(stk []uintptr) string {
		frames := runtime.CallersFrames(stk)
		for {
			frame, more := frames.Next()
			fn := frame.Function
			if fn == "" {
				if !more {
					break
				}
				continue
			}
			// Match by import path segments.
			switch {
			case strings.Contains(fn, "quic-go"):
				return "quic-go"
			case strings.Contains(fn, "sing-tun") || strings.Contains(fn, "sing/tun"):
				return "sing-tun"
			case strings.Contains(fn, "sagernet/sing"):
				return "sing"
			case strings.Contains(fn, "k2rule"):
				return "k2rule"
			case strings.Contains(fn, "/wire"):
				return "wire"
			case strings.Contains(fn, "xtaci/smux"):
				return "smux"
			case strings.Contains(fn, "/engine"):
				return "engine"
			case strings.Contains(fn, "/dns") || strings.Contains(fn, "miekg/dns"):
				return "dns"
			case strings.Contains(fn, "/core"):
				return "core"
			case strings.Contains(fn, "/appext"):
				return "appext"
			case strings.Contains(fn, "runtime."):
				return "runtime"
			}
			if !more {
				break
			}
		}
		return "other"
	}

	for i := range records {
		r := &records[i]
		comp := classifyStack(r.Stack())
		cs := components[comp]
		cs.InUseBytes += r.InUseBytes()
		cs.AllocBytes += r.AllocBytes
		cs.AllocObjects += r.AllocObjects
	}

	// Remove empty components.
	for k, v := range components {
		if v.InUseBytes == 0 && v.AllocBytes == 0 {
			delete(components, k)
		}
	}

	result := map[string]any{
		"heap_inuse_mb": float64(ms.HeapInuse) / (1 << 20),
		"heap_sys_mb":   float64(ms.HeapSys) / (1 << 20),
		"sys_mb":        float64(ms.Sys) / (1 << 20),
		"stack_mb":      float64(ms.StackInuse) / (1 << 20),
		"goroutines":    runtime.NumGoroutine(),
		"gc_cycles":     ms.NumGC,
		"components":    components,
	}

	b, _ := json.Marshal(result)
	return string(b)
}
