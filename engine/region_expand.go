package engine

import (
	"runtime"
	"strings"

	"github.com/kaitu-io/k2-rules/krs"
	"github.com/kaitu-io/k2/rule"
)

// expandRegionRoute converts one logical `match.region` route into up to
// two rule.RouteEntry items: a host entry (NamedSet refs over bundle.Sets)
// and a meta entry (process / package criteria from bundle.Apps[GOOS]).
//
// Returning two distinct entries (rather than one combined entry) is
// load-bearing: rule.Engine.matchConnLocked AND's host criteria with meta
// criteria within a single entry, so an entry with both would only match
// when BOTH a CN domain/IP AND a CN process were observed — far too narrow.
// The spec's OR semantics ("any match in cn.krs triggers direct") is
// implemented at the route-list level via first-match-wins.
//
// Returns (nil, nil) if bundle is nil or contributes neither host nor
// meta criteria. iOS returns (host, nil) — sandbox precludes process
// attribution but IP/domain matching still works.
func expandRegionRoute(b *krs.Bundle, target rule.Target) (host, meta *rule.RouteEntry) {
	if b == nil {
		return nil, nil
	}

	if len(b.Sets) > 0 {
		setRefs := make([]krs.Matcher, len(b.Sets))
		for i := range b.Sets {
			setRefs[i] = &b.Sets[i]
		}
		host = &rule.RouteEntry{
			Target: target,
			Sets:   setRefs,
		}
	}

	if b.Apps != nil {
		meta = buildMetaEntryFromApps(b.Apps, target)
	}
	return host, meta
}

// expandAppsRoute converts one logical `match.apps` route into a meta-only
// rule.RouteEntry by platform-dispatching the globs. darwin/windows/android
// route through buildMetaEntryFromApps; linux is built inline (krs has no
// Linux field); iOS falls through to nil (sandbox precludes attribution).
//
// Returns nil when globs is empty or the runtime platform has no place
// to put them (iOS on desktop; nothing on linux server).
func expandAppsRoute(globs []string, target rule.Target) *rule.RouteEntry {
	if len(globs) == 0 {
		return nil
	}
	apps := &krs.AppPatterns{}
	switch runtime.GOOS {
	case "darwin":
		apps.Darwin.Apps = append([]string(nil), globs...)
	case "windows":
		apps.Windows.Apps = lowerStrings(globs)
	case "android":
		apps.Android.Apps = append([]string(nil), globs...)
	case "linux":
		// krs.AppPatterns has no Linux field (region .krs bundles carry no
		// Linux app patterns), so Linux can't route through
		// buildMetaEntryFromApps. Build the meta entry directly here — only
		// the explicit match.apps path needs it, NOT the region path, so we
		// must not add a linux case to the shared buildMetaEntryFromApps.
		// Linux attributes by /proc/PID/exe basename, case-sensitive exact —
		// identical semantics to Darwin — matched by rule.processNameMatches.
		exact, prefix := rule.SplitGlobs(globs)
		if len(exact) == 0 && len(prefix) == 0 {
			return nil
		}
		return &rule.RouteEntry{
			Target:              target,
			ProcessNames:        exact,
			ProcessNamePrefixes: lowerStrings(prefix),
		}
	default:
		return nil
	}
	return buildMetaEntryFromApps(apps, target)
}

// buildMetaEntryFromApps assembles a meta-only RouteEntry from the
// platform-relevant subset of bundle.Apps via rule.SplitGlobs. Output
// fields mirror what the rule engine matches on: ProcessNames (exact),
// ProcessNamePrefixes (pre-lowercased), PackageNames (exact, case-preserved),
// PackageNamePrefixes (case-preserved per Android convention), InstallerPackages (exact).
//
// Returns nil when no criteria contributed. Caller is responsible for
// runtime.GOOS dispatch — this function consumes whatever Apps fields
// were populated.
func buildMetaEntryFromApps(apps *krs.AppPatterns, target rule.Target) *rule.RouteEntry {
	if apps == nil {
		return nil
	}
	entry := &rule.RouteEntry{Target: target}
	switch runtime.GOOS {
	case "darwin":
		// krs.MatchDarwinProcess is case-sensitive. rule.matchMeta's
		// ProcessNamePrefixes path lowercases the observed name and
		// compares against pre-lowercased prefix entries. Lowercasing
		// prefix entries here keeps both case-sensitive Darwin matcher
		// and case-insensitive prefix matcher seeing consistent inputs.
		// Exact ProcessNames stay case-preserved.
		exact, prefix := rule.SplitGlobs(apps.Darwin.Apps)
		entry.ProcessNames = exact
		entry.ProcessNamePrefixes = lowerStrings(prefix)
	case "windows":
		// krs.MatchWindowsProcess lowercases the query; krs writer
		// pre-lowercases Windows.Apps on encode. Mirror lowercasing
		// in case caller hands us non-canonical data.
		exact, prefix := rule.SplitGlobs(lowerStrings(apps.Windows.Apps))
		entry.ProcessNames = exact
		entry.ProcessNamePrefixes = prefix
	case "android":
		exact, prefix := rule.SplitGlobs(apps.Android.Apps)
		entry.PackageNames = exact
		entry.PackageNamePrefixes = prefix
		if len(apps.Android.Installers) > 0 {
			entry.InstallerPackages = append([]string(nil), apps.Android.Installers...)
		}
	}
	if len(entry.ProcessNames) == 0 && len(entry.ProcessNamePrefixes) == 0 &&
		len(entry.PackageNames) == 0 && len(entry.PackageNamePrefixes) == 0 &&
		len(entry.InstallerPackages) == 0 {
		return nil
	}
	return entry
}

// lowerStrings is a local helper. Two name collisions to dodge:
//
//   - engine/appbypass.go defines lowerSlice (deleted in Phase 7)
//   - engine/engine.go defines lowerCopy (callers in buildRouteEntries)
//
// Using a third distinct name keeps Phase 2 commit-clean without
// touching either. Phase 7 keeps lowerStrings as-is.
func lowerStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}
