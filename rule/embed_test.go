package rule

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestEmbeddedRules_IsUsableTarball(t *testing.T) {
	if len(embeddedRules) == 0 {
		t.Fatal("embeddedRules is empty — placeholder missing or build-fetch wrote nothing")
	}
	gz, err := gzip.NewReader(bytes.NewReader(embeddedRules))
	if err != nil {
		t.Fatalf("embeddedRules is not gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	krs := 0
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		names = append(names, hdr.Name)
		if strings.HasSuffix(hdr.Name, ".krs") {
			krs++
		}
	}
	if krs == 0 {
		t.Fatalf("embedded archive has 0 .krs (names=%v)", names)
	}
	found := false
	for _, n := range names {
		if n == "cn.krs" || strings.HasSuffix(n, "/cn.krs") {
			found = true
		}
	}
	if !found {
		t.Errorf("embedded archive missing cn.krs (names=%v)", names)
	}
}
