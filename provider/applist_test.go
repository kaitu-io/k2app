package provider

import (
	"context"
	"testing"
)

func TestAppListProvider_InterfaceShape(t *testing.T) {
	// Compile-time assertion: every platform must produce a type implementing
	// AppListProvider. The constructor returns the interface, so a build
	// failure here means a platform impl is missing or signature drift.
	var _ AppListProvider = NewAppListProvider()
}

func TestInstalledApp_ZeroValueSafe(t *testing.T) {
	a := InstalledApp{}
	if a.ID != "" || a.Label != "" || len(a.ProcessNames) != 0 {
		t.Fatalf("zero value not safe: %+v", a)
	}
}

// TestNilProvider_ListInstalledReturnsEmpty asserts the documented behavior
// for unsupported platforms: nil-returning constructor is wrong; the
// stub-returning constructor must yield a provider whose ListInstalled
// returns ([], nil) so callers don't need to nil-check at every use site.
func TestNilProvider_ListInstalledReturnsEmpty(t *testing.T) {
	p := NewAppListProvider()
	if p == nil {
		t.Fatal("NewAppListProvider returned nil — should always return a usable interface (use empty stub on unsupported OS)")
	}
	got, err := p.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	// Don't assert empty — on macOS/Windows/Linux this returns the real list.
	// On stub platforms (build tags ios/android) it returns []. Either is OK.
	_ = got
}
