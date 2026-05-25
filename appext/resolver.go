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
	// InstallerForPackage returns the installer package that delivered the
	// given package, or "" if unknown / sideloaded / system. Required for
	// App Bypass v2 — Chinese app stores (com.xiaomi.market, com.huawei.appmarket)
	// are a strong signal for bypassing the VPN tunnel.
	InstallerForPackage(packageName string) string
}
