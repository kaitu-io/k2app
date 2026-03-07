//go:build windows

package config

import "testing"

// TestIsRoot_CI calls IsRoot() and logs the result. We cannot assert a specific
// value because the test may run elevated (e.g. in a CI service account) or
// unelevated (developer workstation). The test documents the behavior and
// verifies no runtime error occurs.
func TestIsRoot_CI(t *testing.T) {
	elevated := IsRoot()
	t.Logf("IsRoot() = %v (elevated=%v)", elevated, elevated)
}

// TestIsRoot_NoPanic calls IsRoot() multiple times to verify it does not panic
// on repeated invocations (e.g. token handle leak or double-close).
func TestIsRoot_NoPanic(t *testing.T) {
	for i := 0; i < 3; i++ {
		_ = IsRoot()
	}
	t.Log("IsRoot() called 3 times without panic")
}
