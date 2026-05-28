package provider

import "context"

// InstalledApp is one row in the platform's installed-applications list.
// Fields are populated best-effort — empty strings mean the platform
// doesn't expose that data.
type InstalledApp struct {
	// ID is the platform-stable identifier:
	//   - macOS:   absolute .app bundle path  ("/Applications/WeChat.app")
	//   - Windows: Uninstall registry sub-key ("{12345678-1234-...}")
	//   - Linux:   .desktop file path         ("/usr/share/applications/firefox.desktop")
	//   - Android: package name              ("com.tencent.mm")
	ID string

	// Label is the user-visible name (CFBundleName, DisplayName, Name=).
	Label string

	// InstallerPackageName is Android-only (packageInstaller's package name).
	// Empty on desktop platforms.
	InstallerPackageName string

	// InstallPath is the install location:
	//   - macOS: .app bundle path (same as ID)
	//   - Windows: InstallLocation registry value (may be empty)
	//   - Linux: directory containing Exec=... target
	InstallPath string

	// ProcessNames is the matcher input — see spec §4.3 Case Selection.
	//   - macOS: every basename inside the .app's MacOS dir + Contents/Helpers/*.app/Contents/MacOS
	//   - Windows: every .exe basename under InstallLocation (depth ≤ 2)
	//   - Linux: Exec= field's first token's basename
	//   - Android: [packageName]
	// Lower-cased for desktop entries since the matcher is case-insensitive.
	ProcessNames []string
}

// AppListProvider enumerates installed user applications. Used only by
// the daemon's classify-apps / get-presets discovery actions — routing
// does NOT depend on this list, so a transient enumeration failure cannot
// break VPN flows.
type AppListProvider interface {
	// ListInstalled returns the current installed-apps snapshot.
	// Implementations should respect ctx cancellation; cost should stay
	// under ~500ms for typical user installs.
	ListInstalled(ctx context.Context) ([]InstalledApp, error)
}

// nilProvider is the build-tag fallback used on platforms without a
// useful applist source (iOS sandbox, android gomobile path, or any
// future GOOS that doesn't compile a per-OS file).
type nilProvider struct{}

func (nilProvider) ListInstalled(_ context.Context) ([]InstalledApp, error) {
	return nil, nil
}
