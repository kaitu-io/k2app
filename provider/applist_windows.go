//go:build windows

package provider

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	uninstall32 = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	uninstall64 = `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
	maxExeWalk  = 2 // depth into InstallLocation for .exe basenames
)

type windowsAppList struct{}

// NewAppListProvider constructs the production Windows provider.
func NewAppListProvider() AppListProvider { return &windowsAppList{} }

func (w *windowsAppList) ListInstalled(ctx context.Context) ([]InstalledApp, error) {
	deadline := time.Now().Add(2 * time.Second)
	roots := []struct {
		hive registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, uninstall32},
		{registry.LOCAL_MACHINE, uninstall64},
		{registry.CURRENT_USER, uninstall32},
	}
	var out []InstalledApp
	seen := make(map[string]struct{})
	for _, r := range roots {
		if time.Now().After(deadline) || ctx.Err() != nil {
			return out, nil
		}
		k, err := registry.OpenKey(r.hive, r.path, registry.READ|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subKeys, _ := k.ReadSubKeyNames(0)
		_ = k.Close()
		for _, sk := range subKeys {
			if ctx.Err() != nil {
				return out, nil
			}
			full := r.path + `\` + sk
			app, ok := readWindowsUninstallEntry(r.hive, full, sk)
			if !ok {
				continue
			}
			if _, dup := seen[app.ID]; dup {
				continue
			}
			seen[app.ID] = struct{}{}
			out = append(out, app)
		}
	}
	slog.Info("applist/windows: scan complete", "count", len(out))
	return out, nil
}

func readWindowsUninstallEntry(hive registry.Key, path, sub string) (InstalledApp, bool) {
	k, err := registry.OpenKey(hive, path, registry.QUERY_VALUE)
	if err != nil {
		return InstalledApp{}, false
	}
	defer k.Close()
	displayName, _, _ := k.GetStringValue("DisplayName")
	installLoc, _, _ := k.GetStringValue("InstallLocation")
	uninstallStr, _, _ := k.GetStringValue("UninstallString")
	parentKeyName, _, _ := k.GetStringValue("ParentKeyName")
	sysComp, _, _ := k.GetIntegerValue("SystemComponent")

	if isSystemComponent(displayName, sub, sysComp == 1, parentKeyName) {
		return InstalledApp{}, false
	}
	if displayName == "" {
		return InstalledApp{}, false
	}

	names := collectWindowsExeNames(installLoc, maxExeWalk)
	// Fallback: try the basename of UninstallString's first quoted path.
	if len(names) == 0 && uninstallStr != "" {
		if exe := extractExeFromUninstallString(uninstallStr); exe != "" {
			names = []string{strings.ToLower(exe)}
		}
	}
	if len(names) == 0 {
		return InstalledApp{}, false
	}
	return InstalledApp{
		ID:           sub,
		Label:        displayName,
		InstallPath:  installLoc,
		ProcessNames: names,
	}, true
}

// collectWindowsExeNames walks `root` up to `depth` directory levels deep
// and returns lower-cased .exe basenames. Returns nil if root is empty or
// unreadable.
func collectWindowsExeNames(root string, depth int) []string {
	if root == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	var walk func(dir string, currentDepth int)
	walk = func(dir string, currentDepth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				if currentDepth < depth {
					walk(filepath.Join(dir, e.Name()), currentDepth+1)
				}
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(strings.ToLower(name), ".exe") {
				continue
			}
			l := strings.ToLower(name)
			if _, dup := seen[l]; dup {
				continue
			}
			seen[l] = struct{}{}
			out = append(out, l)
		}
	}
	walk(root, 0)
	return out
}

// isSystemComponent reports whether the registry entry looks like a system
// update / hotfix / redistributable rather than a user-facing application.
func isSystemComponent(displayName, _ string, sysComp bool, parentKeyName string) bool {
	if sysComp {
		return true
	}
	if parentKeyName != "" {
		return true // it's an update slot
	}
	low := strings.ToLower(displayName)
	// Common noise — registry-as-program-list anti-patterns.
	noise := []string{"security update", "update for", "hotfix for", "kb50", "redistributable"}
	for _, n := range noise {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

// extractExeFromUninstallString returns the basename of the first path-like
// token in an UninstallString registry value. Handles quoted and unquoted
// forms.
func extractExeFromUninstallString(s string) string {
	// "C:\Program Files\X\unins.exe" /S → exe basename
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "\"") {
		if end := strings.Index(s[1:], "\""); end > 0 {
			return filepath.Base(s[1 : end+1])
		}
	}
	if space := strings.IndexByte(s, ' '); space > 0 {
		return filepath.Base(s[:space])
	}
	return filepath.Base(s)
}

var _ AppListProvider = (*windowsAppList)(nil)
