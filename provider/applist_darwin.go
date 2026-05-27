//go:build darwin && !ios

package provider

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"howett.net/plist"
)

// darwinAppList scans top-level .app bundles in /Applications and ~/Applications.
// Daemon runs as root, but parsing top-level Info.plist is fine without user
// session (filesystem readable). We do NOT use NSWorkspace — daemon has no
// GUI session attachment.
type darwinAppList struct {
	// scanRoots is overrideable for tests; nil falls back to platform defaults.
	scanRoots []string
}

// NewAppListProvider constructs the production macOS provider with the
// standard scan roots (~/Applications resolved via $HOME — daemon root
// context typically /var/root, so $HOME may be empty; that's OK, just no
// per-user list).
func NewAppListProvider() AppListProvider {
	return &darwinAppList{}
}

type macInfoPlist struct {
	BundleIdentifier  string `plist:"CFBundleIdentifier"`
	BundleName        string `plist:"CFBundleName"`
	BundleDisplayName string `plist:"CFBundleDisplayName"`
	BundleExecutable  string `plist:"CFBundleExecutable"`
}

func (d *darwinAppList) ListInstalled(ctx context.Context) ([]InstalledApp, error) {
	roots := d.scanRoots
	if roots == nil {
		roots = []string{"/Applications"}
		if home := os.Getenv("HOME"); home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
	}
	deadline := time.Now().Add(2 * time.Second) // hard ceiling — preview latency budget
	var out []InstalledApp
	seen := make(map[string]struct{})
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("applist/darwin: ReadDir failed", "root", root, "err", err)
			}
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				return out, nil // partial result on cancel
			}
			if time.Now().After(deadline) {
				slog.Warn("applist/darwin: scan deadline reached", "scanned", len(out))
				return out, nil
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".app") {
				continue
			}
			bundlePath := filepath.Join(root, name)
			if _, dup := seen[bundlePath]; dup {
				continue
			}
			seen[bundlePath] = struct{}{}
			app, ok := parseDarwinBundle(bundlePath)
			if !ok {
				continue
			}
			out = append(out, app)
		}
	}
	slog.Info("applist/darwin: scan complete", "count", len(out))
	return out, nil
}

func parseDarwinBundle(bundlePath string) (InstalledApp, bool) {
	infoPath := filepath.Join(bundlePath, "Contents", "Info.plist")
	f, err := os.Open(infoPath)
	if err != nil {
		return InstalledApp{}, false
	}
	defer f.Close()

	var info macInfoPlist
	dec := plist.NewDecoder(f)
	if err := dec.Decode(&info); err != nil {
		return InstalledApp{}, false
	}

	label := info.BundleDisplayName
	if label == "" {
		label = info.BundleName
	}
	if label == "" {
		label = strings.TrimSuffix(filepath.Base(bundlePath), ".app")
	}

	names := collectDarwinProcessNames(bundlePath, info.BundleExecutable)
	if len(names) == 0 {
		return InstalledApp{}, false
	}

	return InstalledApp{
		ID:           bundlePath,
		Label:        label,
		InstallPath:  bundlePath,
		ProcessNames: names,
	}, true
}

// collectDarwinProcessNames returns case-preserved basenames of executables
// inside the bundle. Includes the primary CFBundleExecutable + any helper
// .app's CFBundleExecutable found at typical helper paths.
//
// Case is preserved because the krs library's MatchDarwinProcess is
// case-sensitive (Darwin process names are case-sensitive by macOS
// convention — "WeChat" ≠ "wechat"). Dedup uses a lowercased key so
// "TestApp" and "TestApp" with quirky case from CFBundleExecutable don't
// duplicate, but the emitted slice keeps the original casing for the
// matcher.
func collectDarwinProcessNames(bundlePath, primary string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(name string) {
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	add(primary)

	// Helper apps live in:
	//   .app/Contents/Frameworks/*.app
	//   .app/Contents/Helpers/*.app
	//   .app/Contents/Library/LoginItems/*.app
	helperRoots := []string{
		filepath.Join(bundlePath, "Contents", "Frameworks"),
		filepath.Join(bundlePath, "Contents", "Helpers"),
		filepath.Join(bundlePath, "Contents", "Library", "LoginItems"),
	}
	for _, hr := range helperRoots {
		entries, err := os.ReadDir(hr)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			helperInfo := filepath.Join(hr, e.Name(), "Contents", "Info.plist")
			f, err := os.Open(helperInfo)
			if err != nil {
				continue
			}
			var info macInfoPlist
			_ = plist.NewDecoder(f).Decode(&info)
			_ = f.Close()
			add(info.BundleExecutable)
		}
	}
	return out
}

// ensure constructor signature matches interface
var _ AppListProvider = (*darwinAppList)(nil)
var _ AppListProvider = nilProvider{}
