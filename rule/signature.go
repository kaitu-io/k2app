package rule

import (
	"fmt"
	"os"
	"strings"
)

// BundleSignature fingerprints the bundle set currently on disk in cacheDir:
// every *.krs (name, size, mtime) plus the manifest version in
// bundles.version. Two equal signatures mean the same set; any swap
// (atomicSwap renames freshly extracted files, so mtimes always move), seed,
// or manifest-version change yields a different one. touchVersionFile only
// bumps bundles.version's mtime, which is deliberately not part of it.
//
// Returns "" when the directory is unreadable or holds no .krs — callers treat
// that as "nothing loadable", never as a set to switch to.
func BundleSignature(cacheDir string) string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".krs") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // vanished mid-swap; the next check sees the settled set
		}
		fmt.Fprintf(&b, "%s:%d:%d;", name, info.Size(), info.ModTime().UnixNano())
		n++
	}
	if n == 0 {
		return ""
	}
	b.WriteString("v=")
	b.WriteString(readLocalManifestVersion(cacheDir))
	return b.String()
}
