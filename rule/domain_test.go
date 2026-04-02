package rule

import (
	"encoding/binary"
	"testing"
)

func buildTestDomainSection(domains []string) domainSection {
	data := buildDomainData(domains)
	if len(data) == 0 {
		return domainSection{}
	}
	count := int(binary.LittleEndian.Uint32(data[0:4]))
	return domainSection{data: data, count: count}
}

func TestDomainMatchSuffix(t *testing.T) {
	ds := buildTestDomainSection([]string{"google.com"})

	tests := []struct {
		domain string
		want   bool
	}{
		{"google.com", true},
		{"www.google.com", true},
		{"mail.google.com", true},
		{"a.b.c.google.com", true},
		{"example.com", false},
		{"notgoogle.com", false},
		{"google.com.evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := ds.Match(tt.domain)
			if got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestDomainMatchEmpty(t *testing.T) {
	ds := buildTestDomainSection(nil)
	if ds.Match("google.com") {
		t.Error("empty section should match nothing")
	}

	ds2 := buildTestDomainSection([]string{})
	if ds2.Match("google.com") {
		t.Error("empty section should match nothing")
	}
}

func TestDomainMatchCaseInsensitive(t *testing.T) {
	ds := buildTestDomainSection([]string{"google.com"})

	tests := []struct {
		domain string
		want   bool
	}{
		{"GOOGLE.COM", true},
		{"Google.Com", true},
		{"WWW.GOOGLE.COM", true},
		{"www.Google.COM", true},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := ds.Match(tt.domain)
			if got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestDomainNoFalsePrefix(t *testing.T) {
	ds := buildTestDomainSection([]string{"google.com"})

	// "oogle.com" is NOT a suffix match for "google.com"
	if ds.Match("oogle.com") {
		t.Error("oogle.com should NOT match google.com suffix rule")
	}
}

func TestDomainMultipleEntries(t *testing.T) {
	ds := buildTestDomainSection([]string{
		"google.com",
		"facebook.com",
		"twitter.com",
	})

	tests := []struct {
		domain string
		want   bool
	}{
		{"google.com", true},
		{"www.facebook.com", true},
		{"api.twitter.com", true},
		{"example.com", false},
		{"google.org", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := ds.Match(tt.domain)
			if got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestReverseString(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"abc", "cba"},
		{"", ""},
		{"a", "a"},
		{".google.com", "moc.elgoog."},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := reverseString(tt.in)
			if got != tt.want {
				t.Errorf("reverseString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
