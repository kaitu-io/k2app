package engine

import (
	"reflect"
	"testing"

	"github.com/kaitu-io/k2/rule"
)

func TestDNSDroppedMetaFields(t *testing.T) {
	cases := []struct {
		name string
		m    rule.MatchConfig
		want []string
	}{
		{"domain+port", rule.MatchConfig{DomainSuffix: []string{"x.com"}, Port: []string{"443"}}, []string{"port"}},
		{"names+process", rule.MatchConfig{Names: []string{"cn-sites"}, ProcessName: []string{"chrome.exe"}}, []string{"process_name"}},
		{"preset+protocol+network", rule.MatchConfig{Preset: "cn", Protocol: []string{"quic"}, Network: "udp"}, []string{"protocol", "network"}},

		// IP-only host criteria are exempt: a DNS-time domain query can never
		// match them, so the meta half still ANDs at connection time.
		{"ipcidr+port", rule.MatchConfig{IPCIDR: []string{"10.0.0.0/8"}, Port: []string{"443"}}, nil},
		{"ip_is_private+port", rule.MatchConfig{IPIsPrivate: true, Port: []string{"443"}}, nil},

		// Meta-only and host-only routes are fine as-is.
		{"port-only", rule.MatchConfig{Port: []string{"443"}}, nil},
		{"domain-only", rule.MatchConfig{DomainSuffix: []string{"x.com"}}, nil},

		// region expands into separate host and meta entries — a different
		// (documented) composition, not a dropped AND. Out of scope here.
		{"region+port", rule.MatchConfig{Region: "cn", Port: []string{"443"}}, nil},
	}
	for _, c := range cases {
		if got := dnsDroppedMetaFields(c.m); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: dnsDroppedMetaFields = %v, want %v", c.name, got, c.want)
		}
	}
}
