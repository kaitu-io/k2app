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

// PackageResolver resolves an Android UID to a package name.
// Implemented by the native Android layer via Go→Java callback.
// Nil on non-Android platforms.
type PackageResolver interface {
	PackageForUID(uid int32) string
	// InstallerForPackage returns the package name of the installer that
	// delivered the given package, or "" if unknown / sideloaded / system.
	// Implemented on Android via PackageManager.getInstallSourceInfo;
	// cached in-process by the resolver (installer is install-time immutable).
	InstallerForPackage(packageName string) string
}
