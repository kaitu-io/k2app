//go:build linux

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHandleSetCredentialPersistsConfig locks in Cleanup #1: applyCredential
// must write BOTH the source config (k2r.yml, read by `k2r run` on full
// restart) and the reconnect cache (state.json). A panel-set credential that
// only landed in state.json would be lost on the next full restart.
func TestHandleSetCredentialPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	g := &Gateway{}
	url := "k2subs://router-x:tok123@center.example/api/subs"
	cfg, err := g.applyCredential(url)
	if err != nil {
		t.Fatalf("applyCredential: %v", err)
	}
	if cfg == nil {
		t.Fatal("nil cfg")
	}
	st := loadState()
	if st == nil || st.Config == nil {
		t.Fatal("state.json not persisted")
	}
	if _, err := os.Stat(filepath.Join(dir, "k2r.yml")); err != nil {
		t.Fatalf("k2r.yml not persisted: %v", err)
	}
}

func TestHandleSetCredentialRejectsBadURL(t *testing.T) {
	dir := t.TempDir()
	prev := stateDirOverride
	stateDirOverride = dir
	t.Cleanup(func() { stateDirOverride = prev })

	g := &Gateway{}
	if _, err := g.applyCredential("not-a-url"); err == nil {
		t.Fatal("want error on bad url")
	}
}
