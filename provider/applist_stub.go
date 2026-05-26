//go:build (!darwin && !windows && !linux) || android || ios

package provider

// NewAppListProvider returns a nil-impl provider on platforms without an
// applist source (iOS sandbox, android gomobile build, or unrecognized GOOS).
func NewAppListProvider() AppListProvider {
	return nilProvider{}
}
