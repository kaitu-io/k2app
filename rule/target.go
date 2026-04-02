package rule

import (
	"fmt"
	"strings"
)

type Target uint8

const (
	TargetDirect Target = 0
	TargetProxy  Target = 1
	TargetReject Target = 2
)

func (t Target) String() string {
	switch t {
	case TargetDirect:
		return "direct"
	case TargetProxy:
		return "proxy"
	case TargetReject:
		return "reject"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// ParseVia converts a route "via" string to a Target.
func ParseVia(via string) Target {
	switch {
	case strings.HasPrefix(via, "k2v5://"):
		return TargetProxy
	case via == "reject":
		return TargetReject
	default:
		return TargetDirect
	}
}

type MatchConfig struct {
	Preset  string   `yaml:"preset,omitempty"  json:"preset,omitempty"`
	Names   []string `yaml:"names,omitempty"   json:"names,omitempty"`
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	All     bool     `yaml:"all,omitempty"     json:"all,omitempty"`
}

func (m MatchConfig) HasCriteria() bool {
	return m.Preset != "" || len(m.Names) > 0 || len(m.Exclude) > 0 || m.All
}

var presets = map[string][]string{
	"overseas": {
		"google", "facebook", "telegram", "twitter", "netflix",
		"discord", "spotify", "tiktok", "openai", "github",
		"cloudflare", "fastly",
	},
	"cn-access": {
		"cn-sites", "bilibili", "geoip-cn",
	},
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
