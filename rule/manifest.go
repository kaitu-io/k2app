package rule

import "time"

// Manifest mirrors k2-rules dist/manifest.json (schema v2). Only the fields
// the client consumes are modeled; unknown fields are ignored by json.
type Manifest struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Version       string                    `json:"version"` // RFC3339 UTC, monotonic
	Bundles       map[string]ManifestBundle `json:"bundles"`
}

// ManifestBundle is one region's published integrity metadata.
type ManifestBundle struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	RuleCount int    `json:"ruleCount"`
}

// parseVersion parses an RFC3339 manifest version. Unparseable → zero time
// (treated as the oldest possible), so a malformed/legacy version never wins
// newest-wins and never blocks an upgrade.
func parseVersion(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// remoteNewer reports whether remote version a is strictly newer than local
// version b. Numeric (time) comparison, not string — robust to format drift.
func remoteNewer(a, b string) bool {
	return parseVersion(a).After(parseVersion(b))
}
