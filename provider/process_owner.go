package provider

import (
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// uidCacheTTL bounds how long a UID→packages mapping is reused. The mapping
// only changes on install/uninstall, so this is generous compared to the
// 4-tuple cache — it exists to avoid one binder round-trip (getPackagesForUid)
// per new connection.
const uidCacheTTL = 60 * time.Second

// OwnerProcessSearcher attributes connections to Android apps via
// ConnectivityManager.getConnectionOwnerUid (through ConnectionOwnerResolver,
// a Go→Java callback), then resolves UID→packages via PackageResolver.
//
// This replaces the /proc-scanning LinuxProcessSearcher path on Android 10+,
// where SELinux and hidepid hide /proc/net/tcp* and other apps' /proc/PID/fd
// from the VPN process — the /proc path silently returns empty there, which
// made every package_name rule (App Bypass force-proxy, region app globs) a
// no-op. getConnectionOwnerUid is the platform-sanctioned replacement: the
// active VpnService is explicitly permitted to call it (API 29+).
//
// fallback (the /proc searcher) still serves Android ≤9 (minSdk 24), where
// getConnectionOwnerUid does not exist and the resolver returns a negative
// UID — and the destination-less FindProcess entry point.
type OwnerProcessSearcher struct {
	owner    ConnectionOwnerResolver
	packages PackageResolver // may be nil: UIDs then resolve to no packages
	fallback ProcessSearcher // may be nil: no /proc fallback

	cache *processCache // 4-tuple → joined packages/installer (2s TTL, keyed by src like every searcher)

	uidMu    deadlock.Mutex
	uidCache map[int32]uidCacheEntry
}

type uidCacheEntry struct {
	packages  []string
	installer string
	expires   time.Time
}

// NewOwnerProcessSearcher wires the getConnectionOwnerUid-based searcher.
// owner must be non-nil; packages and fallback may be nil.
func NewOwnerProcessSearcher(owner ConnectionOwnerResolver, packages PackageResolver, fallback ProcessSearcher) *OwnerProcessSearcher {
	return &OwnerProcessSearcher{
		owner:    owner,
		packages: packages,
		fallback: fallback,
		cache:    newProcessCache(processCacheCap, processCacheTTL),
		uidCache: make(map[int32]uidCacheEntry),
	}
}

// FindProcess (destination-less ProcessSearcher entry point) cannot call
// getConnectionOwnerUid — the platform API needs the remote address — so it
// delegates straight to the /proc fallback. Both in-tree callers
// (core/tunnel.go routeConn, engine/dns_handler.go queryMeta) know their
// destination and use FindProcessConn instead.
func (s *OwnerProcessSearcher) FindProcess(network string, srcIP netip.Addr, srcPort uint16) (string, string, string) {
	if s.fallback == nil {
		return "", "", ""
	}
	return s.fallback.FindProcess(network, srcIP, srcPort)
}

// FindProcessConn implements ConnProcessSearcher. processName stays empty on
// the owner path (Android attribution is package-based); packageNames carries
// every package sharing the owning UID.
func (s *OwnerProcessSearcher) FindProcessConn(network string, srcIP netip.Addr, srcPort uint16, dstIP netip.Addr, dstPort uint16) (string, []string, string) {
	if !srcIP.IsValid() || !dstIP.IsValid() {
		name, pkg, installer := s.FindProcess(network, srcIP, srcPort)
		return name, singletonPackages(pkg), installer
	}
	// Same canonicalization contract as LinuxProcessSearcher: 4-in-6 ::ffff:
	// forms must produce the same cache key and the same platform query as
	// their IPv4 originals.
	srcIP, dstIP = srcIP.Unmap(), dstIP.Unmap()

	// Cache by source socket (same key shape as every other searcher): one
	// src socket belongs to one UID for its lifetime, so dst need not be in
	// the key. Values store the newline-joined package set.
	key := cacheKey(network, srcIP, srcPort)
	if name, joined, installer, ok := s.cache.get(key); ok {
		return name, splitPackages(joined), installer
	}

	start := time.Now()
	uid := s.owner.OwnerUID(network, srcIP.String(), int32(srcPort), dstIP.String(), int32(dstPort))
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		slog.Info("DIAG: process-lookup-slow",
			"platform", "android-owner",
			"network", network,
			"latencyMs", elapsed.Milliseconds())
	}

	if uid < 0 {
		// Below API 29 or lookup miss: /proc fallback (works on Android ≤9).
		name, pkg, installer := s.FindProcess(network, srcIP, srcPort)
		s.cache.set(key, name, pkg, installer)
		return name, singletonPackages(pkg), installer
	}

	pkgs, installer := s.resolveUID(uid)
	s.cache.set(key, "", strings.Join(pkgs, "\n"), installer)
	return "", pkgs, installer
}

// resolveUID maps UID → (all packages, primary installer) with its own
// longer-lived cache (mapping changes only on install/uninstall).
func (s *OwnerProcessSearcher) resolveUID(uid int32) ([]string, string) {
	now := time.Now()
	s.uidMu.Lock()
	if e, ok := s.uidCache[uid]; ok && now.Before(e.expires) {
		s.uidMu.Unlock()
		return e.packages, e.installer
	}
	s.uidMu.Unlock()

	var pkgs []string
	var installer string
	if s.packages != nil {
		pkgs = splitPackages(s.packages.PackagesForUID(uid))
		if len(pkgs) == 0 {
			pkgs = singletonPackages(s.packages.PackageForUID(uid))
		}
		if len(pkgs) > 0 {
			installer = s.packages.InstallerForPackage(pkgs[0])
		}
	}

	s.uidMu.Lock()
	if len(s.uidCache) >= processCacheCap {
		for k, e := range s.uidCache {
			if now.After(e.expires) {
				delete(s.uidCache, k)
			}
		}
	}
	s.uidCache[uid] = uidCacheEntry{packages: pkgs, installer: installer, expires: now.Add(uidCacheTTL)}
	s.uidMu.Unlock()
	return pkgs, installer
}

func splitPackages(joined string) []string {
	if joined == "" {
		return nil
	}
	parts := strings.Split(joined, "\n")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func singletonPackages(pkg string) []string {
	if pkg == "" {
		return nil
	}
	return []string{pkg}
}
