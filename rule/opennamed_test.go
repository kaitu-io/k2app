package rule

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaitu-io/k2-rules/krs"
)

func writeKRSFile(t *testing.T, dir, name string, b *krs.Bundle) {
	t.Helper()
	var buf bytes.Buffer
	if err := krs.WriteBundle(&buf, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenNamed_MatchesAndCloses(t *testing.T) {
	dir := t.TempDir()
	writeKRSFile(t, dir, "cn.krs", &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "cn", DomainSuffixes: []string{"qq.com"}, CIDRs: []string{"1.1.1.0/24"}},
	}})
	writeKRSFile(t, dir, "os.krs", &krs.Bundle{Sets: []krs.NamedSet{
		{Name: "os", DomainSuffixes: []string{"google.com"}},
	}})

	named, closers, err := OpenNamed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != 2 || len(closers) != 2 {
		t.Fatalf("want 2 bundles + 2 closers, got %d / %d", len(named), len(closers))
	}

	idx := IndexDisk(named)
	if !idx["cn"].MatchDomainReversed(krs.ReversedParents("www.qq.com")) {
		t.Error("cn should match www.qq.com")
	}
	if idx["cn"].MatchDomainReversed(krs.ReversedParents("google.com")) {
		t.Error("cn should not match google.com")
	}
	if !idx["cn"].MatchIP(netip.MustParseAddr("1.1.1.1")) {
		t.Error("cn should match IP 1.1.1.1")
	}
	if !idx["os"].MatchDomainReversed(krs.ReversedParents("google.com")) {
		t.Error("os should match google.com")
	}

	for _, c := range closers {
		if err := c(); err != nil {
			t.Errorf("close: %v", err)
		}
	}
}

func TestOpenNamed_MissingDir(t *testing.T) {
	named, closers, err := OpenNamed(filepath.Join(t.TempDir(), "nope"))
	if err != nil || named != nil || closers != nil {
		t.Fatalf("missing dir: want (nil,nil,nil), got (%v,%v,%v)", named, closers, err)
	}
}
