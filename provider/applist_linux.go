//go:build linux && !android

package provider

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// linuxAppList walks XDG application directories and parses .desktop files.
type linuxAppList struct {
	scanRoots []string
}

// NewAppListProvider constructs the production Linux provider with the
// standard XDG application directories.
func NewAppListProvider() AppListProvider { return &linuxAppList{} }

func (l *linuxAppList) ListInstalled(ctx context.Context) ([]InstalledApp, error) {
	roots := l.scanRoots
	if roots == nil {
		roots = []string{
			"/usr/share/applications",
			"/usr/local/share/applications",
		}
		if home := os.Getenv("HOME"); home != "" {
			roots = append(roots, filepath.Join(home, ".local/share/applications"))
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	var out []InstalledApp
	seen := make(map[string]struct{})
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Warn("applist/linux: ReadDir failed", "root", root, "err", err)
			}
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil || time.Now().After(deadline) {
				return out, nil
			}
			if !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			full := filepath.Join(root, e.Name())
			if _, dup := seen[full]; dup {
				continue
			}
			seen[full] = struct{}{}
			app, ok := parseLinuxDesktop(full)
			if !ok {
				continue
			}
			out = append(out, app)
		}
	}
	slog.Info("applist/linux: scan complete", "count", len(out))
	return out, nil
}

func parseLinuxDesktop(path string) (InstalledApp, bool) {
	f, err := os.Open(path)
	if err != nil {
		return InstalledApp{}, false
	}
	defer f.Close()

	var (
		inEntry bool
		name    string
		exec    string
		entType string
		hidden  bool
		noDisp  bool
	)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inEntry = (line == "[Desktop Entry]")
			continue
		}
		if !inEntry {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "Name":
			if name == "" {
				name = v
			}
		case "Exec":
			exec = v
		case "Type":
			entType = v
		case "Hidden":
			hidden = strings.EqualFold(v, "true")
		case "NoDisplay":
			noDisp = strings.EqualFold(v, "true")
		}
	}
	if entType != "" && entType != "Application" {
		return InstalledApp{}, false
	}
	if hidden || noDisp {
		return InstalledApp{}, false
	}
	if name == "" || exec == "" {
		return InstalledApp{}, false
	}

	cmd := firstExecToken(exec)
	if cmd == "" {
		return InstalledApp{}, false
	}
	procName := strings.ToLower(filepath.Base(cmd))

	return InstalledApp{
		ID:           path,
		Label:        name,
		InstallPath:  filepath.Dir(cmd),
		ProcessNames: []string{procName},
	}, true
}

// firstExecToken returns the first whitespace-separated token of Exec=,
// stripping surrounding quotes. Field codes (%u / %f / %U / %F / %i / %c
// / %k) appear after the first token so this is sufficient — they are
// dropped by virtue of selecting only the first token.
func firstExecToken(exec string) string {
	exec = strings.TrimSpace(exec)
	// Unquote leading "...".
	if strings.HasPrefix(exec, "\"") {
		if end := strings.Index(exec[1:], "\""); end > 0 {
			return exec[1 : end+1]
		}
	}
	if sp := strings.IndexByte(exec, ' '); sp > 0 {
		return exec[:sp]
	}
	return exec
}

var _ AppListProvider = (*linuxAppList)(nil)
