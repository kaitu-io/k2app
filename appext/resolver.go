package appext

// PackageResolver resolves Android UID to package name and identifies the
// installer source for a given package. Implemented by native Android layer
// (Java/Kotlin) calling:
//   - context.getPackageManager().getPackagesForUid(uid) → PackageForUID
//   - context.getPackageManager().getInstallSourceInfo(pkg) → InstallerForPackage
//
// gomobile exports this as AppextPackageResolver (ObjC) / appext.PackageResolver (Java).
type PackageResolver interface {
	PackageForUID(uid int32) string
	// PackagesForUID returns ALL packages sharing the UID, newline-joined
	// (gomobile interfaces cannot return string arrays). Kotlin:
	// packageManager.getPackagesForUid(uid)?.joinToString("\n") ?: "".
	// Shared-UID apps are why the full set matters — Play Store and GMS
	// share one UID via sharedUserId, and routing is UID-granular.
	PackagesForUID(uid int32) string
	// InstallerForPackage returns the installer package that delivered the
	// given package, or "" if unknown / sideloaded / system. Required for
	// App Bypass v2 — Chinese app stores (com.xiaomi.market, com.huawei.appmarket)
	// are a strong signal for bypassing the VPN tunnel.
	InstallerForPackage(packageName string) string
}

// ConnectionOwnerResolver resolves a connection 4-tuple to the owning app
// UID. Implemented by the native Android layer (Kotlin) via
// ConnectivityManager.getConnectionOwnerUid — API 29+, and the caller must
// be the active VpnService (which K2VpnService is). Return a negative value
// when unknown: below API 29, Process.INVALID_UID, or any exception.
//
// network is "tcp" or "udp"; addresses are IP literals (IPv4 or IPv6).
//
// gomobile exports this as AppextConnectionOwnerResolver (ObjC) /
// appext.ConnectionOwnerResolver (Java).
type ConnectionOwnerResolver interface {
	OwnerUID(network string, srcIP string, srcPort int32, dstIP string, dstPort int32) int32
}
