//go:build linux

package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Boot verification for the Linux web OTA.
//
// The other three shells verify a bundle at PROCESS start: the app boots,
// the webapp confirms it rendered, the marker clears. A Go daemon has no
// such moment — it runs for weeks and the bundle is exercised whenever a
// browser loads the page. So "boot" here means "a document was served",
// and the rollback judgement has to tolerate two things the app shells
// never see: nobody visiting for days (must NOT roll back — untested is
// not broken), and several tabs opening at once (must NOT roll back on the
// count alone — the confirm may still be in flight).
//
// Hence the two-part judgement: served at least twice AND long enough
// after the first serve that a healthy bundle would have confirmed.

func TestShouldRollbackBoot(t *testing.T) {
	const v = "0.4.9.1234"
	cases := []struct {
		name string
		st   bootState
		now  int64
		want bool
	}{
		{
			// Nobody has loaded the page yet. Untested is not broken.
			name: "never served does not roll back",
			st:   bootState{Version: v},
			now:  10_000,
		},
		{
			// First load. The confirm is milliseconds away if the bundle is fine.
			name: "single serve does not roll back",
			st:   bootState{Version: v, FirstServedUnix: 10_000, ServedCount: 1},
			now:  10_001,
		},
		{
			// Two tabs opened together: count is met but the first confirm
			// has not had time to land. Rolling back here would discard a
			// perfectly good bundle.
			name: "two serves inside the grace window does not roll back",
			st:   bootState{Version: v, FirstServedUnix: 10_000, ServedCount: 2},
			now:  10_005,
		},
		{
			// User saw a white screen, waited, reloaded. Both conditions met.
			name: "two serves after the grace window rolls back",
			st:   bootState{Version: v, FirstServedUnix: 10_000, ServedCount: 2},
			now:  10_000 + bootRollbackMinAgeSec,
			want: true,
		},
		{
			// A long-idle marker with only one serve still must not fire:
			// the single visitor may simply have closed the tab.
			name: "one serve long ago does not roll back",
			st:   bootState{Version: v, FirstServedUnix: 10_000, ServedCount: 1},
			now:  10_000 + 86_400,
		},
	}
	for _, tc := range cases {
		if got := shouldRollbackBoot(tc.st, tc.now); got != tc.want {
			t.Errorf("%s: shouldRollbackBoot = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestBootStateRoundTripAndConfirm(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})

	if st := o.readBootState(); st != nil {
		t.Fatalf("fresh dir: readBootState = %+v, want nil", st)
	}
	if err := o.armBootPending("0.4.9.1234"); err != nil {
		t.Fatalf("armBootPending: %v", err)
	}
	st := o.readBootState()
	if st == nil || st.Version != "0.4.9.1234" || st.ServedCount != 0 {
		t.Fatalf("after arm: readBootState = %+v", st)
	}

	o.ConfirmBootOK()
	if st := o.readBootState(); st != nil {
		t.Fatalf("after confirm: readBootState = %+v, want nil", st)
	}
}

// The marker must NOT live inside current/ — rollback removes that
// directory, and the version it names is the one thing rollback needs.
func TestBootMarkerLivesOutsideCurrent(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})
	if err := o.armBootPending("0.4.9.1234"); err != nil {
		t.Fatalf("armBootPending: %v", err)
	}
	if p := o.bootStatePath(); filepath.Dir(p) != dir {
		t.Errorf("boot marker at %s, want directly under %s", p, dir)
	}
}

// A serve records the first-serve timestamp once and counts every serve.
func TestNoteIndexServedAccumulates(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if err := o.armBootPending("0.4.9.1234"); err != nil {
		t.Fatalf("armBootPending: %v", err)
	}

	o.NoteIndexServed()
	first := o.readBootState()
	if first == nil || first.ServedCount != 1 || first.FirstServedUnix == 0 {
		t.Fatalf("after one serve: %+v", first)
	}

	o.NoteIndexServed()
	second := o.readBootState()
	if second == nil || second.ServedCount != 2 {
		t.Fatalf("after two serves: %+v", second)
	}
	if second.FirstServedUnix != first.FirstServedUnix {
		t.Errorf("FirstServedUnix moved: %d → %d", first.FirstServedUnix, second.FirstServedUnix)
	}
}

// NoteIndexServed on a confirmed bundle (no marker) is a no-op and must
// never create one — otherwise every page view would re-arm the check.
func TestNoteIndexServedWithoutMarkerIsNoop(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	o.NoteIndexServed()
	if st := o.readBootState(); st != nil {
		t.Fatalf("readBootState = %+v, want nil", st)
	}
}

// The full rollback: previous/ is restored over current/, the failed
// version is recorded, and the marker is cleared so the restored bundle
// is not itself judged.
func TestRollbackRestoresPreviousAndQuarantines(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})

	mustWriteBundle(t, filepath.Join(dir, "current"), "0.4.9.1234", "BAD")
	mustWriteBundle(t, filepath.Join(dir, "previous"), "0.4.9.1000", "GOOD")

	if err := o.rollbackBoot("0.4.9.1234"); err != nil {
		t.Fatalf("rollbackBoot: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "current", "index.html"))
	if err != nil {
		t.Fatalf("read restored index: %v", err)
	}
	if string(got) != "GOOD" {
		t.Errorf("current/index.html = %q, want the previous bundle", got)
	}
	if v := o.quarantinedVersion(); v != "0.4.9.1234" {
		t.Errorf("quarantinedVersion = %q, want the failed version", v)
	}
	if st := o.readBootState(); st != nil {
		t.Errorf("marker survived rollback: %+v", st)
	}
}

// With no previous/ the only safe fallback is the embedded bundle, which
// the handler serves as soon as current/ is gone.
func TestRollbackWithoutPreviousClearsCurrent(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})
	mustWriteBundle(t, filepath.Join(dir, "current"), "0.4.9.1234", "BAD")

	if err := o.rollbackBoot("0.4.9.1234"); err != nil {
		t.Fatalf("rollbackBoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "current")); !os.IsNotExist(err) {
		t.Errorf("current/ still present after rollback (err=%v)", err)
	}
	if v := o.quarantinedVersion(); v != "0.4.9.1234" {
		t.Errorf("quarantinedVersion = %q", v)
	}
}

// The quarantine gate, mirroring desktop evaluate_manifest and the mobile
// planAutoUpdate: the exact version that failed is refused until the CDN
// publishes something newer.
func TestShouldApplyRefusesQuarantinedVersion(t *testing.T) {
	dir := t.TempDir()
	o := NewWebOTA(WebOTAConfig{Dir: dir, ShellVersion: "0.4.9"})
	mustWriteBundle(t, filepath.Join(dir, "current"), "0.4.9.1234", "BAD")
	if err := o.rollbackBoot("0.4.9.1234"); err != nil {
		t.Fatalf("rollbackBoot: %v", err)
	}

	bad := WebOTAManifest{Version: "0.4.9.1234", MinLinux: "0.4.9", MinBridge: 1}
	if ok, reason := o.shouldApply(&bad); ok {
		t.Errorf("quarantined version applied (reason=%q)", reason)
	}

	newer := WebOTAManifest{Version: "0.4.9.1235", MinLinux: "0.4.9", MinBridge: 1}
	if ok, reason := o.shouldApply(&newer); !ok {
		t.Errorf("newer version blocked: %s", reason)
	}
}

func mustWriteBundle(t *testing.T, dir, version, index string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "version.txt"), []byte(version+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// --- serve-side hook ---

// The hook must fire only when an OTA bundle's document is actually
// handed to a browser. Counting an embedded-fallback serve would put a
// bundle on trial that was never shown; counting `?ui=embedded` would
// punish the very escape hatch a user reaches for when a bundle is broken.
func TestIndexServedHookFiresOnlyForOTADocuments(t *testing.T) {
	fakeEmbed(t)
	overrideDir := writeOverride(t)

	var served int
	h := NewWebappHandlerWithOTA("0.4.9", "abc", "amd64", overrideDir, func() { served++ })
	if h == nil {
		t.Fatal("handler is nil")
	}

	// OTA document → counts.
	if body := getBody(t, h, "/"); !strings.Contains(body, "OTA-UI") {
		t.Fatalf("expected OTA document, got %q", body)
	}
	if served != 1 {
		t.Fatalf("OTA document: served = %d, want 1", served)
	}

	// Escape hatch → embedded, must not count.
	if body := getBody(t, h, "/?ui=embedded"); !strings.Contains(body, "EMBEDDED-UI") {
		t.Fatalf("expected embedded document, got %q", body)
	}
	if served != 1 {
		t.Errorf("?ui=embedded: served = %d, want it unchanged at 1", served)
	}

	// Asset requests are not documents.
	getBody(t, h, "/assets/ota-only.js")
	if served != 1 {
		t.Errorf("asset request: served = %d, want it unchanged at 1", served)
	}
}

// No override on disk: every document is the embedded one, so nothing is
// ever on trial and the hook must stay silent.
func TestIndexServedHookSilentWithoutOverride(t *testing.T) {
	fakeEmbed(t)
	var served int
	h := NewWebappHandlerWithOTA("0.4.9", "abc", "amd64", "", func() { served++ })
	if h == nil {
		t.Fatal("handler is nil")
	}
	getBody(t, h, "/")
	if served != 0 {
		t.Errorf("served = %d, want 0", served)
	}
}

// --- /api/ui-boot-ok ---

func TestUIBootOkHandlerClearsMarker(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if err := o.armBootPending("0.4.9.1234"); err != nil {
		t.Fatal(err)
	}
	h := NewUIBootOkHandler(o)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/ui-boot-ok", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if st := o.readBootState(); st != nil {
		t.Errorf("marker survived: %+v", st)
	}
}

func TestUIBootOkHandlerRejectsGET(t *testing.T) {
	o := NewWebOTA(WebOTAConfig{Dir: t.TempDir(), ShellVersion: "0.4.9"})
	if err := o.armBootPending("0.4.9.1234"); err != nil {
		t.Fatal(err)
	}
	h := NewUIBootOkHandler(o)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/ui-boot-ok", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	if st := o.readBootState(); st == nil {
		t.Error("GET cleared the marker")
	}
}

func getBody(t *testing.T, h http.Handler, target string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Body.String()
}
