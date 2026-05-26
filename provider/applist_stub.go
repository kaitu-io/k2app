//go:build ios || (linux && android) || (!darwin && !windows && !linux)

package provider

// NewAppListProvider returns a nil-impl provider on platforms without an
// applist source (iOS sandbox, android gomobile build, or unrecognized GOOS).
func NewAppListProvider() AppListProvider {
	return nilProvider{}
}
