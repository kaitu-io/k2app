// Package appbypass loads region-specific app-bypass pattern YAML files
// (app-bypass-<region>.yaml) and exposes them as compiled Preset structs
// ready to inject into rule.MatchConfig.
//
// See docs/superpowers/specs/2026-05-25-app-bypass-engine-managed-design.md §4.
package appbypass

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SupportedVersion is the highest schema version this reader understands.
// Files with a higher version are rejected with ErrUnsupportedVersion so
// the daemon logs + skips rather than misinterpreting unknown fields.
const SupportedVersion = 1

// ErrUnsupportedVersion is returned by parseRaw when version > SupportedVersion.
var ErrUnsupportedVersion = errors.New("appbypass: unsupported schema version")

// rawFile mirrors the on-disk YAML one-to-one. Compile step normalizes
// into a Preset.
type rawFile struct {
	Version     int        `yaml:"version"`
	Region      string     `yaml:"region"`
	Description string     `yaml:"description,omitempty"`
	Android     rawAndroid `yaml:"android,omitempty"`
	Desktop     rawDesktop `yaml:"desktop,omitempty"`
}

type rawAndroid struct {
	InstallerExact []string `yaml:"installer_exact,omitempty"`
	PackageExact   []string `yaml:"package_exact,omitempty"`
	PackagePrefix  []string `yaml:"package_prefix,omitempty"`
}

type rawDesktop struct {
	ProcessExact  []string `yaml:"process_exact,omitempty"`
	ProcessPrefix []string `yaml:"process_prefix,omitempty"`
}

// parseRaw strict-decodes YAML into rawFile and gates version + required fields.
// KnownFields(true) makes typos at the publisher side fail loudly.
func parseRaw(data []byte) (*rawFile, error) {
	var raw rawFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("appbypass: parse yaml: %w", err)
	}
	if raw.Version <= 0 {
		return nil, fmt.Errorf("appbypass: missing or invalid version field")
	}
	if raw.Version > SupportedVersion {
		return nil, fmt.Errorf("%w: file has %d, reader supports up to %d",
			ErrUnsupportedVersion, raw.Version, SupportedVersion)
	}
	if raw.Region == "" {
		return nil, fmt.Errorf("appbypass: missing region field")
	}
	return &raw, nil
}
