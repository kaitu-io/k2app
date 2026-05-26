package appbypass

// Preset is one region's compiled bypass patterns.
type Preset struct {
	Region  string
	Android AndroidPatterns
	Desktop DesktopPatterns
}

// AndroidPatterns target packageName + installerPackageName (case-sensitive).
type AndroidPatterns struct {
	InstallerExact []string
	PackageExact   []string
	PackagePrefix  []string
}

// DesktopPatterns target the process_name returned by ProcessSearcher.
// All entries pre-lowercased at compile time.
type DesktopPatterns struct {
	ProcessExact  []string
	ProcessPrefix []string
}

// PresetSet is the result of Load() — all presets indexed by region.
type PresetSet struct {
	presets map[string]*Preset
}

// Get returns the preset for region (e.g. "cn"), or nil.
func (s *PresetSet) Get(region string) *Preset {
	if s == nil {
		return nil
	}
	return s.presets[region]
}

// Empty reports whether no presets are loaded.
func (s *PresetSet) Empty() bool {
	return s == nil || len(s.presets) == 0
}
