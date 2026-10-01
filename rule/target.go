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

	// Region selects a loaded *.krs bundle by basename (e.g. "cn").
	// Expanded by engine.buildRouteEntries into one host route over
	// bundle.Sets + one meta route over bundle.Apps[GOOS] via
	// rule.SplitGlobs. Empty = no region match. Plan B's single smart-bypass
	// routing vocabulary, replacing the old top-level field + cn-access preset.
	Region string `yaml:"region,omitempty" json:"region,omitempty"`

	// Inline host matching (compiled at engine build time).
	DomainSuffix []string `yaml:"domain_suffix,omitempty" json:"domain_suffix,omitempty"`
	IPCIDR       []string `yaml:"ip_cidr,omitempty"       json:"ip_cidr,omitempty"`

	// Apps lists single-* glob patterns matched against the running
	// process's executable basename (desktop) or installed package id
	// (Android). Platform-dispatched at engine.buildRouteEntries via
	// runtime.GOOS — patterns aimed at the wrong platform are silently
	// ignored. Used by Plan C Tier-1 force-override routes; emitted by
	// webapp app-routes.store.
	Apps []string `yaml:"apps,omitempty" json:"apps,omitempty"`

	// AppPaths lists directories (macOS .app bundle, Windows install dir);
	// a process whose executable lives anywhere under one of them matches.
	// Desktop only. OR'd with Apps within the same route — one logical
	// "these applications" criterion — so a route may carry both: paths for
	// everything inside the bundle, names for what runs outside it.
	// Emitted by webapp app-routes.store.
	AppPaths []string `yaml:"app_paths,omitempty" json:"app_paths,omitempty"`

	// Connection metadata matching.
	ProcessName []string `yaml:"process_name,omitempty" json:"process_name,omitempty"`
	PackageName []string `yaml:"package_name,omitempty" json:"package_name,omitempty"`

	// v2 (app-bypass): prefix matching + Android installer source.
	ProcessNamePrefix []string `yaml:"process_name_prefix,omitempty" json:"process_name_prefix,omitempty"`
	PackageNamePrefix []string `yaml:"package_name_prefix,omitempty" json:"package_name_prefix,omitempty"`
	InstallerPackage  []string `yaml:"installer_package,omitempty"  json:"installer_package,omitempty"`

	Network     string `yaml:"network,omitempty"      json:"network,omitempty"`
	IPIsPrivate bool   `yaml:"ip_is_private,omitempty" json:"ip_is_private,omitempty"`

	// Port matches the destination port: "443" or "27000-28000".
	// Meta criterion — connection-time only, never part of DNS-time host
	// matching (a DNS query carries no destination port).
	Port []string `yaml:"port,omitempty" json:"port,omitempty"`

	// Protocol matches the sniffed application protocol: "stun", "dtls",
	// "quic", "bittorrent". Meta criterion, and only known AFTER the first
	// packet is sniffed — it therefore participates in the post-sniff
	// re-route, not in the initial routing decision.
	Protocol []string `yaml:"protocol,omitempty" json:"protocol,omitempty"`

	// SrcIPCIDR matches the connection's LAN source address (CIDR list,
	// OR within list). Multi-slot gateway mode pins each slot subnet to
	// its outbound. Meta criterion — never participates in DNS-time
	// host matching.
	SrcIPCIDR []string `yaml:"src_ip_cidr,omitempty" json:"src_ip_cidr,omitempty"`

	// Catch-all.
	All bool `yaml:"all,omitempty" json:"all,omitempty"`
}

// HasCriteria reports whether any matching criterion is set.
func (m MatchConfig) HasCriteria() bool {
	return m.Preset != "" || len(m.Names) > 0 || len(m.Exclude) > 0 || m.All ||
		m.Region != "" || len(m.Apps) > 0 || len(m.AppPaths) > 0 ||
		len(m.DomainSuffix) > 0 || len(m.IPCIDR) > 0 ||
		len(m.ProcessName) > 0 || len(m.PackageName) > 0 ||
		len(m.ProcessNamePrefix) > 0 || len(m.PackageNamePrefix) > 0 ||
		len(m.InstallerPackage) > 0 ||
		m.Network != "" || m.IPIsPrivate || len(m.SrcIPCIDR) > 0 || len(m.Port) > 0 ||
		len(m.Protocol) > 0
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
//
// Country-specific presets follow the "{cc}-access" naming convention and
// expand to [{cc}-sites, geoip-{cc}], served from the per-country
// {cc}-direct.k2b bundle published by kaitu-io/k2-rules (17 countries:
// cn, ir, ru, tr, pk, vn, mm, eg, id, sa, ae, th, bd, by, tm, kz, uz).
//
// Note on geoip-cn: as of k2-rules sources.go (post HTTPDNS-anchors merge),
// the "geoip-cn" set blends two data sources — Loyalsoldier mainland CIDRs
// plus build-time pre-resolved HTTPDNS server PoP IPs (often Tencent HK).
// The set is intentionally a superset of pure geoip data so existing client
// binaries pick up HTTPDNS anchors on the next daily bundle refresh without
// needing this preset map to change. See k2-rules sources.go for details.
//
// "games" is a standalone service bundle (not a "{cc}-access" country
// package): overseas game-publisher domains + ASN IP ranges, served from
// games.krs as the sets [games-sites, games-ip]. Referencing the "games"
// preset pulls in this extra bundle on top of whatever country presets are
// already selected.
//
// The set names "ir-blocked" and "ru-blocked" are reserved for a future
// force-proxy feature (IP+domain data from runetfreedom / citizenlab). They
// are downloaded as part of ir-direct.k2b / ru-direct.k2b but not yet
// exposed via any preset here. Similarly, "tencent-overseas" (from
// tencent-overseas.krs) is published by kaitu-io/k2-rules but is a
// build-internal set — deliberately not exposed via any preset here.
var presets = map[string][]string{
	"overseas":  {"overseas"},
	"cn-access": {"cn-sites", "geoip-cn"},
	"ir-access": {"ir-sites", "geoip-ir"},
	"ru-access": {"ru-sites", "geoip-ru"},
	"tr-access": {"tr-sites", "geoip-tr"},
	"pk-access": {"pk-sites", "geoip-pk"},
	"vn-access": {"vn-sites", "geoip-vn"},
	"mm-access": {"mm-sites", "geoip-mm"},
	"eg-access": {"eg-sites", "geoip-eg"},
	"id-access": {"id-sites", "geoip-id"},
	"sa-access": {"sa-sites", "geoip-sa"},
	"ae-access": {"ae-sites", "geoip-ae"},
	"th-access": {"th-sites", "geoip-th"},
	"bd-access": {"bd-sites", "geoip-bd"},
	"by-access": {"by-sites", "geoip-by"},
	"tm-access": {"tm-sites", "geoip-tm"},
	"kz-access": {"kz-sites", "geoip-kz"},
	"uz-access": {"uz-sites", "geoip-uz"},
	"games":     {"games-sites", "games-ip"},
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
