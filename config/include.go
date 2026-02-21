package config

// ResolveInclude resolves a glob pattern relative to the current directory,
// reads each matching YAML file in sorted order, and merges them into base.
// Later files override earlier keys. Nested includes are not supported.
func ResolveInclude(pattern string, base map[string]any) error {
	return nil // RED: not implemented
}
