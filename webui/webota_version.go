//go:build linux

package webui

import (
	"strconv"
	"strings"
)

// isNewerVersion reports whether remote is strictly newer than local.
//
// Semantics mirror the mobile web OTA gate (K2PluginUtils.isNewerVersion):
// dot-separated numeric segments compared left-to-right, missing segments
// treated as 0, non-numeric segments treated as 0, and a "-<pre>" suffix
// ranking below the same base version ("0.5.0" > "0.5.0-beta.1"); two
// pre-release suffixes compare numerically segment-by-segment.
func isNewerVersion(remote, local string) bool {
	rBase, rPre := splitVersion(remote)
	lBase, lPre := splitVersion(local)
	if c := compareSegments(rBase, lBase); c != 0 {
		return c > 0
	}
	switch {
	case rPre == "" && lPre != "":
		return true // stable beats same-base pre-release
	case rPre != "" && lPre == "":
		return false
	case rPre == "" && lPre == "":
		return false // equal
	}
	return compareSegments(parseSegments(rPre), parseSegments(lPre)) > 0
}

// meetsMinVersion reports whether have satisfies the minimum version min.
// Base versions only (pre-release suffix ignored); an empty/blank min
// always passes — old manifests without the field must not block.
// Mirrors K2PluginUtils.isCompatibleNativeVersion.
func meetsMinVersion(min, have string) bool {
	if strings.TrimSpace(min) == "" {
		return true
	}
	minBase, _ := splitVersion(min)
	haveBase, _ := splitVersion(have)
	return compareSegments(haveBase, minBase) >= 0
}

// meetsMinBridge reports whether the compiled bridge version satisfies
// the manifest's min_bridge gate. Zero (field absent in JSON) or a
// negative value always passes.
func meetsMinBridge(minBridge, have int) bool {
	return minBridge <= 0 || have >= minBridge
}

// splitVersion splits "1.2.3-beta.4" into base segments [1 2 3] and the
// pre-release suffix "beta.4" ("" when absent).
func splitVersion(v string) (base []int, pre string) {
	parts := strings.SplitN(v, "-", 2)
	if len(parts) > 1 {
		pre = parts[1]
	}
	return parseSegments(parts[0]), pre
}

// parseSegments parses dot-separated numeric segments; non-numeric
// segments become 0.
func parseSegments(s string) []int {
	fields := strings.Split(s, ".")
	out := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}

// compareSegments compares two segment slices, padding the shorter one
// with zeros. Returns -1/0/1.
func compareSegments(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		switch {
		case av > bv:
			return 1
		case av < bv:
			return -1
		}
	}
	return 0
}
