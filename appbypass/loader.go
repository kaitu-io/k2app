package appbypass

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	filenamePrefix = "app-bypass-"
	filenameSuffix = ".yaml"
)

// Load reads all app-bypass-<region>.yaml files under dir. Per-file
// errors (parse, version, validation, region mismatch) are logged + skipped —
// Load itself only returns error if dir cannot be read at all and the error
// is not "missing".
//
// Returns an empty PresetSet when dir is missing or contains no matching files.
func Load(dir string) (*PresetSet, error) {
	ps := &PresetSet{presets: make(map[string]*Preset)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ps, nil
		}
		return ps, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, filenamePrefix) || !strings.HasSuffix(name, filenameSuffix) {
			continue
		}
		region := strings.TrimSuffix(strings.TrimPrefix(name, filenamePrefix), filenameSuffix)
		if region == "" {
			continue
		}
		path := filepath.Join(dir, name)
		preset, err := loadOne(path, region)
		if err != nil {
			slog.Warn("appbypass: skip file", "file", name, "err", err)
			continue
		}
		ps.presets[region] = preset
	}
	return ps, nil
}

func loadOne(path, expectRegion string) (*Preset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := parseRaw(data)
	if err != nil {
		return nil, err
	}
	if raw.Region != expectRegion {
		return nil, fmt.Errorf("filename region mismatch: file=%s region=%s",
			filepath.Base(path), raw.Region)
	}
	return compile(raw)
}
