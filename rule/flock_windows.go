//go:build windows

package rule

import "context"

// acquireLock is a no-op on Windows. Only one k2.exe desktop process runs;
// the mobile plugin/NE split exists solely on iOS/Android (Unix family).
func acquireLock(_ context.Context, _ string) (func(), error) {
	return func() {}, nil
}
