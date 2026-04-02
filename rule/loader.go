package rule

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Load reads all .k2b bundle files from cacheDir.
// Returns nil (not error) if dir is empty or doesn't exist.
// Skips corrupt files with a warning log.
func Load(cacheDir string) ([]*Bundle, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var bundles []*Bundle
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".k2b") {
			continue
		}
		path := filepath.Join(cacheDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("rule: skip bundle", "file", e.Name(), "err", err)
			continue
		}
		b, err := ReadBundle(data)
		if err != nil {
			slog.Warn("rule: skip bundle", "file", e.Name(), "err", err)
			continue
		}
		bundles = append(bundles, b)
	}
	return bundles, nil
}

// Index builds a name→*BundleSet lookup from all loaded bundles.
// If the same name appears in multiple bundles, the last one wins.
func Index(bundles []*Bundle) map[string]*BundleSet {
	idx := make(map[string]*BundleSet)
	for _, b := range bundles {
		for i := range b.Sets {
			idx[b.Sets[i].Name] = &b.Sets[i]
		}
	}
	return idx
}
