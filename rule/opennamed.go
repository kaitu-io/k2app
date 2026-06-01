package rule

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaitu-io/k2-rules/krs"
)

// NamedDiskBundle pairs an mmap-backed bundle with its region key (the source
// filename's basename without ".krs"). It is the disk-backed counterpart to
// NamedBundle: the constitution forbids LoadNamed/ReadBundle on the NE runtime
// path because they expand the full rule corpus onto the dirty heap (~19 MB).
// OpenNamed maps the same files instead, keeping resident dirty heap O(set
// count) (~tens of KB) — the rule data stays in reclaimable file-backed pages.
type NamedDiskBundle struct {
	Region string
	Bundle *krs.DiskBundle
}

// OpenNamed mmaps every *.krs in cacheDir. Per-file Open errors are skipped
// with WARN (mirrors LoadNamed's partial-boot tolerance: a corrupt bundle must
// not block the others). Returns the bundles plus one unmap closer per opened
// bundle — the engine holds these and runs them on Close/Reload. A missing
// cacheDir returns (nil, nil, nil).
func OpenNamed(cacheDir string) ([]NamedDiskBundle, []func() error, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var out []NamedDiskBundle
	var closers []func() error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".krs") {
			continue
		}
		db, err := krs.Open(filepath.Join(cacheDir, name))
		if err != nil {
			slog.Warn("krs: skip bundle (mmap open failed)", "file", name, "err", err)
			continue
		}
		out = append(out, NamedDiskBundle{
			Region: strings.TrimSuffix(name, ".krs"),
			Bundle: db,
		})
		closers = append(closers, db.Close)
	}
	return out, closers, nil
}

// ListRegions returns the region key (basename without ".krs") of every *.krs
// in cacheDir WITHOUT parsing any bundle. Callers that only need the set of
// present regions (existence checks, config validation) use this instead of
// LoadNamed to avoid expanding the full rule corpus onto the heap. A missing
// or unreadable dir returns nil.
func ListRegions(cacheDir string) []string {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".krs") {
			out = append(out, strings.TrimSuffix(n, ".krs"))
		}
	}
	return out
}

// IndexDisk builds a name->Matcher lookup across opened disk bundles. On name
// collision later bundles win (mirrors krs.Index, with a WARN — a silent
// overwrite would mask a curation mistake). Names come from SetNames();
// matchers from Sets() (both bundle-ordered). Sets() allocates a fresh slice
// per call, so it is called exactly once here at setup — never per lookup.
func IndexDisk(named []NamedDiskBundle) map[string]krs.Matcher {
	idx := make(map[string]krs.Matcher)
	for _, nb := range named {
		if nb.Bundle == nil {
			continue
		}
		names := nb.Bundle.SetNames()
		sets := nb.Bundle.Sets()
		for i, n := range names {
			if i >= len(sets) {
				break
			}
			if _, exists := idx[n]; exists {
				slog.Warn("krs: duplicate set name across bundles (last wins)",
					"name", n, "region", nb.Region)
			}
			idx[n] = sets[i]
		}
	}
	return idx
}
