package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// resolveIncludeInData unmarshals YAML data into a map, resolves any "include"
// directive, and re-marshals the merged result. If no "include" key is present,
// the original data is returned unchanged.
func resolveIncludeInData(data []byte) ([]byte, error) {
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return data, nil // let the caller's unmarshal report the error
	}

	pattern, ok := m["include"].(string)
	if !ok || pattern == "" {
		return data, nil
	}

	// Remove "include" before merge so it doesn't persist in the final config.
	delete(m, "include")

	if err := ResolveInclude(pattern, m); err != nil {
		return nil, err
	}

	merged, err := yaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("include: re-marshal: %w", err)
	}
	return merged, nil
}

// ResolveInclude resolves a glob pattern, reads each matching YAML file in
// sorted (alphabetical) order, and merges the key-value pairs into base.
// Later files override keys from earlier files (shallow merge).
// Nested includes (an "include" key inside an included file) are not processed.
// If the glob matches no files, the function returns nil without modifying base.
func ResolveInclude(pattern string, base map[string]any) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("include: invalid glob pattern %q: %w", pattern, err)
	}

	// No matches — nothing to do.
	if len(matches) == 0 {
		return nil
	}

	// Process files in sorted order so the merge is deterministic.
	sort.Strings(matches)

	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("include: read %s: %w", path, err)
		}

		var m map[string]any
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("include: parse %s: %w", path, err)
		}

		// Shallow-merge into base; later keys overwrite earlier ones.
		for k, v := range m {
			base[k] = v
		}
	}

	return nil
}
