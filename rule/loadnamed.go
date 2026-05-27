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

// NamedBundle pairs a parsed krs bundle with its source filename's
// region key (basename without ".krs"). Used by callers that need to
// map "user picked region cn" → "cn.krs's AppPatterns + sets".
type NamedBundle struct {
	Region string
	Bundle *krs.Bundle
}

// LoadNamed reads every *.krs in cacheDir, pairing each with its
// filename basename as a region key. Per-file parse errors are skipped
// with WARN; a missing directory returns (nil, nil).
func LoadNamed(cacheDir string) ([]NamedBundle, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []NamedBundle
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".krs") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, name))
		if err != nil {
			slog.Warn("krs: skip bundle (read failed)", "file", name, "err", err)
			continue
		}
		b, err := krs.ReadBundle(data)
		if err != nil {
			slog.Warn("krs: skip bundle (parse failed)", "file", name, "err", err)
			continue
		}
		out = append(out, NamedBundle{
			Region: strings.TrimSuffix(name, ".krs"),
			Bundle: b,
		})
	}
	return out, nil
}
