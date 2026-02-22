package mobile

// EngineConfig holds platform-specific paths and settings for Engine.Start().
// All path fields are provided by the native host (iOS/Android).
// gomobile exports this as MobileEngineConfig (ObjC) / mobile.EngineConfig (Java).
type EngineConfig struct {
	CacheDir string // required: k2rule cache etc. iOS: NSCachesDirectory, Android: context.getCacheDir()
	LogDir   string // optional: file log directory. empty = no file logging (EventHandler.OnLog only)
}

// NewEngineConfig creates an EngineConfig with zero values.
func NewEngineConfig() *EngineConfig {
	return &EngineConfig{}
}
