package center

import (
	"strconv"
	"strings"
)

// compareVersions compares two semver strings (major.minor.patch).
// Pre-release suffixes are stripped. Malformed input returns 0 (treat as equal).
func compareVersions(a, b string) int {
	pa := parseVersionParts(a)
	pb := parseVersionParts(b)
	if pa == nil || pb == nil {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] > pb[i] {
			return 1
		}
		if pa[i] < pb[i] {
			return -1
		}
	}
	return 0
}

// isVersionInRange checks if version is within [minVersion, maxVersion].
// Empty minVersion/maxVersion means no constraint. Empty/malformed version skips filtering (returns true).
func isVersionInRange(version, minVersion, maxVersion string) bool {
	if version == "" || parseVersionParts(version) == nil {
		return true // can't filter, show announcement
	}
	if minVersion != "" && compareVersions(version, minVersion) < 0 {
		return false
	}
	if maxVersion != "" && compareVersions(version, maxVersion) > 0 {
		return false
	}
	return true
}

// parseVersionParts parses "x.y.z" (ignoring pre-release suffix) into [major, minor, patch].
// Returns nil if malformed.
func parseVersionParts(v string) []int {
	if v == "" {
		return nil
	}
	// Strip pre-release suffix: "0.4.2-beta.1" -> "0.4.2"
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	result := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}
