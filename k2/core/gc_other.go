//go:build !ios

package core

// onConnRelease is a no-op on non-iOS platforms where memory is not constrained.
func onConnRelease() {}
