package provider

import "net/netip"

// ProcessSearcher finds the process that owns a network connection.
// Returns (processName, packageName, installerPackageName).
//   - processName: mac helper basename / win exe basename / linux /proc/comm / android empty
//   - packageName: Android packageName from UID; empty on desktop
//   - installerPackageName: Android only; package that installed packageName via
//     PackageManager.getInstallSourceInfo; empty on desktop or for system/sideloaded apps
type ProcessSearcher interface {
	FindProcess(network string, srcIP netip.Addr, srcPort uint16) (processName, packageName, installerPackageName string)
}

// NopProcessSearcher returns empty strings for all lookups.
// Used when platform doesn't support process detection.
type NopProcessSearcher struct{}

func (NopProcessSearcher) FindProcess(string, netip.Addr, uint16) (string, string, string) {
	return "", "", ""
}

// ProcessPathSearcher is an optional extension of ProcessSearcher for the
// desktop platforms, where "which application is this" is better answered by
// where the executable lives than by what it is called: every process of an
// app sits under its bundle / install directory, while basenames collide
// across apps (every Chromium app ships chrome_crashpad_handler, JDK-bundling
// apps ship java).
//
// processPath is the executable's full path in rule.NormalizeProcessPath form
// for the platform (Windows: lowercased), or "" when it cannot be read —
// callers then fall back to name matching alone.
type ProcessPathSearcher interface {
	ProcessSearcher
	FindProcessPath(network string, srcIP netip.Addr, srcPort uint16) (processName, processPath string)
}

// ConnProcessSearcher is an optional extension of ProcessSearcher for
// platforms whose kernel attribution needs the full connection 4-tuple —
// Android's ConnectivityManager.getConnectionOwnerUid takes (protocol,
// local, remote). Callers that know the destination should type-assert and
// prefer FindProcessConn; FindProcess stays the fallback when the
// destination is unknown or not an IP literal.
type ConnProcessSearcher interface {
	ProcessSearcher
	// FindProcessConn attributes a connection 4-tuple to its owning app.
	// packageNames lists every package sharing the owning UID ([0] is the
	// primary — Android sharedUserId means one UID can back several
	// packages); empty when unknown.
	FindProcessConn(network string, srcIP netip.Addr, srcPort uint16, dstIP netip.Addr, dstPort uint16) (processName string, packageNames []string, installerPackageName string)
}

// ConnectionOwnerResolver resolves a connection 4-tuple to the owning
// Android app UID via ConnectivityManager.getConnectionOwnerUid (API 29+;
// the active VpnService is explicitly permitted to call it). Implemented by
// the native Android layer via Go→Java callback. Returns a negative value
// when unknown: below API 29, lookup miss, or SecurityException.
type ConnectionOwnerResolver interface {
	OwnerUID(network string, srcIP string, srcPort int32, dstIP string, dstPort int32) int32
}

// PackageResolver resolves an Android UID to a package name.
// Implemented by the native Android layer via Go→Java callback.
// Nil on non-Android platforms.
type PackageResolver interface {
	PackageForUID(uid int32) string
	// PackagesForUID returns ALL packages sharing the UID, newline-joined
	// (gomobile interfaces cannot return []string). Empty when unknown.
	// Shared-UID apps (Play Store + GMS via sharedUserId) are why the full
	// set matters: routing is UID-granular, so a rule naming any one of
	// them must match the UID's traffic.
	PackagesForUID(uid int32) string
	// InstallerForPackage returns the package name of the installer that
	// delivered the given package, or "" if unknown / sideloaded / system.
	// Implemented on Android via PackageManager.getInstallSourceInfo;
	// cached in-process by the resolver (installer is install-time immutable).
	InstallerForPackage(packageName string) string
}
