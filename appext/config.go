package appext

// EngineConfig holds platform-specific paths and settings for Engine.Start().
// All path fields are provided by the native host (iOS/Android).
// gomobile exports this as AppextEngineConfig (ObjC) / appext.EngineConfig (Java).
type EngineConfig struct {
	CacheDir        string          // required: k2rule cache etc. iOS: NSCachesDirectory, Android: context.getCacheDir()
	LogDir          string          // optional: file log directory. empty = no file logging (EventHandler.OnLog only)
	Debug           bool            // when true and LogDir is set, also copy logs to stderr (Xcode/logcat)
	SocketProtector SocketProtector // optional: marks socket FDs for VPN exclusion (Android VpnService.protect)
	PackageResolver PackageResolver // optional: Android UID→package name
	// ConnectionOwner resolves connection 4-tuples to app UIDs
	// (ConnectivityManager.getConnectionOwnerUid). Android only; when set,
	// per-app rules (App Bypass force-proxy, region app globs) attribute via
	// the platform API instead of the /proc path that Android 10+ SELinux
	// blocks. Nil on iOS.
	ConnectionOwner ConnectionOwnerResolver
}

// NewEngineConfig creates an EngineConfig with zero values.
func NewEngineConfig() *EngineConfig {
	return &EngineConfig{}
}
