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
}

// NewEngineConfig creates an EngineConfig with zero values.
func NewEngineConfig() *EngineConfig {
	return &EngineConfig{}
}
