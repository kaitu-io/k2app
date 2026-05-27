package rule

import (
	"log/slog"
	"strings"
)

// SplitGlobs partitions krs single-`*` glob patterns into the (exact,
// prefix) shape consumed by rule.MatchConfig.{ProcessName,
// ProcessNamePrefix} (and the Package/Installer equivalents).
//
// Mapping rules:
//   - no '*' → exact (one entry)
//   - trailing '*' only ("X*") → prefix "X"
//   - any other glob (leading '*', middle '*', or just "*") → skipped
//     with a WARN log
//
// **Transitional helper.** Plan B retires this once engine.Start
// consumes bundle.Apps directly via match.region. Verified safe
// against current k2-rules/app-bypass/cn.yaml data: no middle-star
// or leading-star patterns exist in production rule data as of
// 2026-05-27.
func SplitGlobs(globs []string) (exact, prefix []string) {
	for _, g := range globs {
		switch {
		case g == "" || g == "*":
			slog.Warn("rule.SplitGlobs: invalid glob, skipping", "glob", g)
		case !strings.Contains(g, "*"):
			exact = append(exact, g)
		case strings.HasSuffix(g, "*") && !strings.Contains(g[:len(g)-1], "*"):
			prefix = append(prefix, strings.TrimSuffix(g, "*"))
		default:
			slog.Warn("rule.SplitGlobs: middle/leading-star glob not expressible in MatchConfig, skipping",
				"glob", g)
		}
	}
	return
}
