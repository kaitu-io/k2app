package center

var (
	// Version 版本号（通过 ldflags 在构建时注入）
	Version = "dev"
	// GitCommit Git commit hash
	GitCommit = "unknown"
	// BuildTime 构建时间
	BuildTime = "unknown"
)
