package rule

import "fmt"

// Target is an opaque outbound index assigned by the engine builder.
// The rule engine stores and returns these values without interpreting them.
// Convention: 0 = direct, 1 = reject, 2+ = wire outbounds (k2v5, k2p, etc.).
type Target uint8

func (t Target) String() string {
	return fmt.Sprintf("outbound(%d)", t)
}

type MatchConfig struct {
	// Bundle-based (from k2b files).
	Preset  string   `yaml:"preset,omitempty"  json:"preset,omitempty"`
	Names   []string `yaml:"names,omitempty"   json:"names,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`

	// Inline host matching (compiled at engine build time).
	DomainSuffix []string `yaml:"domain_suffix,omitempty" json:"domain_suffix,omitempty"`
	IPCIDR       []string `yaml:"ip_cidr,omitempty"       json:"ip_cidr,omitempty"`

	// Connection metadata matching.
	ProcessName []string `yaml:"process_name,omitempty" json:"process_name,omitempty"`
	PackageName []string `yaml:"package_name,omitempty" json:"package_name,omitempty"`
	Network     string   `yaml:"network,omitempty"      json:"network,omitempty"`
	IPIsPrivate bool     `yaml:"ip_is_private,omitempty" json:"ip_is_private,omitempty"`

	// Catch-all.
	All bool `yaml:"all,omitempty" json:"all,omitempty"`
}

// HasCriteria reports whether any matching criterion is set.
func (m MatchConfig) HasCriteria() bool {
	return m.Preset != "" || len(m.Names) > 0 || len(m.Exclude) > 0 || m.All ||
		len(m.DomainSuffix) > 0 || len(m.IPCIDR) > 0 ||
		len(m.ProcessName) > 0 || len(m.PackageName) > 0 ||
		m.Network != "" || m.IPIsPrivate
}

// HasHostCriteria reports whether any host-based (domain/IP) criterion is set.
// Routes with only meta criteria (process, network) are skipped at DNS time.
func (m MatchConfig) HasHostCriteria() bool {
	return m.Preset != "" || len(m.Names) > 0 || len(m.Exclude) > 0 ||
		len(m.DomainSuffix) > 0 || len(m.IPCIDR) > 0 || m.IPIsPrivate
}

// Preset set-name mapping tracks the schema shipped by kaitu-io/k2-rules.
// Since k2b v2 (commit 77f117e, 2026-04), bundles consolidate services:
//   - overseas.k2b contains a single "overseas" set (full v2fly geolocation-!cn
//     + orphan sites, with @cn entries carved out via exclude-domains)
//   - cn-direct.k2b contains "cn-sites" + "geoip-cn" (bilibili merged into
//     cn-sites via v2fly geolocation-cn)
var presets = map[string][]string{
	"overseas":  {"overseas"},
	"cn-access": {"cn-sites", "geoip-cn"},
}

// ExpandPreset returns a copy of the preset's name list, or nil if unknown.
func ExpandPreset(name string) []string {
	p, ok := presets[name]
	if !ok {
		return nil
	}
	out := make([]string, len(p))
	copy(out, p)
	return out
}

// ExpandMatch expands a MatchConfig into a list of rule-source names.
// If m.All is true, returns nil (caller treats as match-everything).
func ExpandMatch(m MatchConfig) []string {
	if m.All {
		return nil
	}

	var names []string
	if m.Preset != "" {
		names = append(names, ExpandPreset(m.Preset)...)
	}
	names = append(names, m.Names...)

	if len(m.Exclude) > 0 {
		excl := make(map[string]struct{}, len(m.Exclude))
		for _, e := range m.Exclude {
			excl[e] = struct{}{}
		}
		filtered := names[:0]
		for _, n := range names {
			if _, excluded := excl[n]; !excluded {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}

	if names == nil {
		names = []string{}
	}
	return names
}
