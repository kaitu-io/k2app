package appbypass

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxEntryLen      = 256
	maxEntriesPerSet = 500
)

// compile normalizes a rawFile into a Preset: trim, optionally lowercase,
// validate entry bounds, dedup, sort.
func compile(raw *rawFile) (*Preset, error) {
	var p Preset
	p.Region = raw.Region

	var err error
	if p.Android.InstallerExact, err = normalize(raw.Android.InstallerExact, false); err != nil {
		return nil, fmt.Errorf("android.installer_exact: %w", err)
	}
	if p.Android.PackageExact, err = normalize(raw.Android.PackageExact, false); err != nil {
		return nil, fmt.Errorf("android.package_exact: %w", err)
	}
	if p.Android.PackagePrefix, err = normalize(raw.Android.PackagePrefix, false); err != nil {
		return nil, fmt.Errorf("android.package_prefix: %w", err)
	}
	if p.Desktop.ProcessExact, err = normalize(raw.Desktop.ProcessExact, true); err != nil {
		return nil, fmt.Errorf("desktop.process_exact: %w", err)
	}
	if p.Desktop.ProcessPrefix, err = normalize(raw.Desktop.ProcessPrefix, true); err != nil {
		return nil, fmt.Errorf("desktop.process_prefix: %w", err)
	}
	return &p, nil
}

func normalize(in []string, toLower bool) ([]string, error) {
	if len(in) > maxEntriesPerSet {
		return nil, fmt.Errorf("too many entries: %d > %d", len(in), maxEntriesPerSet)
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for i, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("empty entry at index %d", i)
		}
		if len(s) > maxEntryLen {
			return nil, fmt.Errorf("entry too long at index %d: %d > %d", i, len(s), maxEntryLen)
		}
		if toLower {
			s = strings.ToLower(s)
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
