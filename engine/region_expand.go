package engine

import (
	"log/slog"
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

// expandRegionRouteDisk is the mmap-backed counterpart to expandRegionRoute,
// used on the NE (NetworkExtension) path. It builds ONLY the host entry
// (domain/IP) from the disk bundle's matchers; region-Apps meta is omitted by
// design. This is exact parity with iOS — the only NE platform — where
// expandRegionRoute already returns (host, nil) because the sandbox precludes
// process attribution. krs.DiskBundle also exposes no Apps accessor, so there
// is nothing to lose. Returns nil when the bundle is nil or carries no sets.
func expandRegionRouteDisk(b *krs.DiskBundle, target rule.Target) *rule.RouteEntry {
	if b == nil {
		return nil
	}
	sets := b.Sets() // []krs.Matcher, valid until the bundle is unmapped
	if len(sets) == 0 {
		return nil
	}
	return &rule.RouteEntry{
		Target: target,
		Sets:   sets,
	}
}

// expandAppsRoute converts one logical "these applications" route —
// `match.apps` name globs plus `match.app_paths` directories — into a single
// meta-only rule.RouteEntry. Names and paths land in the same entry, so they
// are OR'd (rule.matchMeta's process group): a process matches by living
// inside one of the directories OR by its name.
//
// Globs are platform-dispatched: darwin/windows/android route through
// buildMetaEntryFromApps; linux is built inline (krs has no Linux field);
// iOS contributes nothing (sandbox precludes attribution).
//
// Returns nil when neither half contributes a criterion on this platform.
func expandAppsRoute(globs, paths []string, target rule.Target) *rule.RouteEntry {
	return expandAppsRouteFor(runtime.GOOS, globs, paths, target)
}

// expandAppsRouteFor is the goos-parameterized body of expandAppsRoute, split
// out so tests can exercise every platform branch on any host.
func expandAppsRouteFor(goos string, globs, paths []string, target rule.Target) *rule.RouteEntry {
	entry := appGlobsEntryFor(goos, globs, target)
	if prefixes := appPathPrefixesFor(goos, paths); len(prefixes) > 0 {
		if entry == nil {
			entry = &rule.RouteEntry{Target: target}
		}
		entry.ProcessPathPrefixes = prefixes
	}
	return entry
}

// appPathPrefixesFor compiles match.app_paths into ProcessPathPrefixes.
// Desktop platforms only — the mobile searchers report no executable path.
// Directories rule.NormalizeProcessDir rejects (relative, a filesystem or
// drive root) are dropped with a warning rather than becoming a prefix that
// captures every process.
func appPathPrefixesFor(goos string, paths []string) []string {
	switch goos {
	case "darwin", "windows", "linux":
	default:
		return nil
	}
	var out []string
	for _, p := range paths {
		dir := rule.NormalizeProcessDir(goos, p)
		if dir == "" {
			slog.Warn("engine: match.app_paths entry is not a usable directory — skipped", "path", p)
			continue
		}
		out = append(out, dir)
	}
	return out
}

// appGlobsEntryFor builds the name half of an apps route. Returns nil when
// globs is empty or the platform has no place to put them.
func appGlobsEntryFor(goos string, globs []string, target rule.Target) *rule.RouteEntry {
	if len(globs) == 0 {
		return nil
	}
	apps := &krs.AppPatterns{}
	switch goos {
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
	return buildMetaEntryFromAppsFor(goos, apps, target)
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
	return buildMetaEntryFromAppsFor(runtime.GOOS, apps, target)
}

// buildMetaEntryFromAppsFor is the goos-parameterized body of
// buildMetaEntryFromApps, split out so tests can exercise every platform
// branch on any host (the android branch would otherwise never run in CI).
func buildMetaEntryFromAppsFor(goos string, apps *krs.AppPatterns, target rule.Target) *rule.RouteEntry {
	if apps == nil {
		return nil
	}
	entry := &rule.RouteEntry{Target: target}
	switch goos {
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
		// apps.Android.Installers is deliberately NOT compiled into routing.
		// Installer (= which store delivered the app) is the UI classifier's
		// default-badge signal (appext/classify.go), but as a kernel-level
		// route criterion it is far too coarse: on Chinese-ROM devices
		// virtually every app — Microsoft Edge included — reports the OEM
		// store (com.xiaomi.market …) as installer, so an installer→direct
		// meta route would force entire foreign browsers out of the tunnel.
		// It never routed before 2026-09 either: attribution was a no-op on
		// Android 10+ (the /proc path returned empty), so dropping it here
		// regresses nothing while the owner-UID searcher makes meta live.
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
