//go:build linux

package webui

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Boot verification for web OTA bundles — the Linux half of the invariant
// the desktop and mobile shells enforce.
//
// WHY THE JUDGEMENT DIFFERS FROM THE APP SHELLS
// =============================================
// Tauri/iOS/Android verify at PROCESS start: the app launches, the webapp
// confirms it rendered, the marker clears, and an unconfirmed marker on the
// next launch means the bundle failed. A Go daemon has no equivalent moment
// — it runs for weeks, and the bundle is only exercised when a browser loads
// the page. Two situations follow that the app shells never face:
//
//   - Nobody visits for days. The marker sits there unconfirmed, but the
//     bundle is untested, not broken. Rolling back would discard a good
//     bundle for lack of a visitor.
//   - Several tabs load at once. The serve count jumps before the first
//     confirm can land, so the count alone cannot mean failure.
//
// Hence the two-part rule in shouldRollbackBoot: served at least twice AND
// far enough past the first serve that a healthy bundle would have
// confirmed. A white screen makes the user reload, which satisfies both; a
// burst of simultaneous tabs satisfies only the first.
//
// Linux also keeps its `?ui=embedded` escape hatch, so this automation is a
// convenience rather than the last line of defence it is on mobile. That is
// deliberate weighting: the cost of a wrong rollback (a good bundle
// quarantined, updates stuck) is real, so the rule errs toward not firing.
//
// BACKWARD COMPATIBILITY. The marker is only ever written by apply(), so
// only bundles installed by a daemon carrying this code are ever judged.
// Such a bundle is necessarily newer than this daemon's release, hence
// carries the webapp that calls /api/ui-boot-ok. A pre-existing current/
// from an older daemon has no marker and is never touched.

const (
	// bootRollbackMinServes is how many document serves must accumulate
	// with no confirmation before the bundle is considered broken. One is
	// not enough: the first serve is exactly when a healthy bundle is still
	// on its way to confirming.
	bootRollbackMinServes = 2

	// bootRollbackMinAgeSec is how long after the FIRST serve the verdict
	// may be reached. It exists to separate "the user stared at a white
	// screen and reloaded" from "two tabs opened in the same instant",
	// which are indistinguishable by count alone.
	bootRollbackMinAgeSec = 20

	bootStateFile   = ".boot-pending"
	quarantineFile  = "quarantined-version.txt"
	bootStatePerms  = 0644
	quarantinePerms = 0644
)

// bootState is the on-disk marker: which bundle is awaiting confirmation
// and how the browser has exercised it so far.
type bootState struct {
	// Version is the bundle awaiting confirmation — recorded here because
	// rollback deletes current/, taking current/version.txt with it, and
	// the version is precisely what the quarantine needs.
	Version string `json:"version"`
	// FirstServedUnix is when the document was first handed to a browser;
	// 0 means it never has been.
	FirstServedUnix int64 `json:"first_served_unix,omitempty"`
	// ServedCount counts document serves since the bundle was applied.
	ServedCount int `json:"served_count,omitempty"`
}

// shouldRollbackBoot is the verdict, split out as a pure function so the
// two-part rule above is testable without a filesystem or a clock.
func shouldRollbackBoot(st bootState, nowUnix int64) bool {
	if st.ServedCount < bootRollbackMinServes || st.FirstServedUnix == 0 {
		return false
	}
	return nowUnix-st.FirstServedUnix >= bootRollbackMinAgeSec
}

// bootStatePath is the marker's location: directly under cfg.Dir, NEVER
// inside current/ — rollback removes that directory wholesale, and the
// marker names the version being rolled back.
func (o *WebOTA) bootStatePath() string { return filepath.Join(o.cfg.Dir, bootStateFile) }

func (o *WebOTA) quarantinePath() string { return filepath.Join(o.cfg.Dir, quarantineFile) }

// readBootState returns the marker, or nil when none is pending (also when
// it is unreadable or corrupt — an unusable marker must not strand the
// bundle in permanent limbo).
func (o *WebOTA) readBootState() *bootState {
	data, err := os.ReadFile(o.bootStatePath())
	if err != nil {
		return nil
	}
	var st bootState
	if err := json.Unmarshal(data, &st); err != nil {
		slog.Warn("webui: corrupt web OTA boot marker — ignoring", "err", err)
		return nil
	}
	if st.Version == "" {
		return nil
	}
	return &st
}

func (o *WebOTA) writeBootState(st bootState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(o.bootStatePath(), data, bootStatePerms)
}

// armBootPending marks a freshly applied bundle as awaiting confirmation.
// Called by apply() after the pending → current swap succeeds.
func (o *WebOTA) armBootPending(version string) error {
	if err := os.MkdirAll(o.cfg.Dir, 0755); err != nil {
		return err
	}
	return o.writeBootState(bootState{Version: version})
}

// ConfirmBootOK clears the marker: the webapp rendered. Called by the
// /api/ui-boot-ok handler, which the webapp hits only after ReactDOM has
// rendered — never during bridge init, which is the mistake that made this
// whole mechanism a no-op on the mobile shells.
func (o *WebOTA) ConfirmBootOK() {
	st := o.readBootState()
	if st == nil {
		return
	}
	if err := os.Remove(o.bootStatePath()); err != nil && !os.IsNotExist(err) {
		slog.Warn("webui: clearing web OTA boot marker failed", "err", err)
		return
	}
	slog.Info("DIAG: webota-boot-ok", "version", st.Version, "serves", st.ServedCount)
}

// NoteIndexServed records that a document was handed to a browser, and
// rolls back once the verdict is reached. No-op when nothing is pending,
// so a confirmed bundle costs one failed stat per page load and never
// re-arms itself.
func (o *WebOTA) NoteIndexServed() {
	st := o.readBootState()
	if st == nil {
		return
	}
	now := time.Now().Unix()
	if st.FirstServedUnix == 0 {
		st.FirstServedUnix = now
	}
	st.ServedCount++
	if err := o.writeBootState(*st); err != nil {
		slog.Warn("webui: updating web OTA boot marker failed", "err", err)
		return
	}
	if !shouldRollbackBoot(*st, now) {
		return
	}
	slog.Warn("DIAG: webota-boot-fail",
		"version", st.Version, "serves", st.ServedCount,
		"ageS", now-st.FirstServedUnix)
	if err := o.rollbackBoot(st.Version); err != nil {
		slog.Warn("webui: web OTA rollback failed", "version", st.Version, "err", err)
	}
}

// rollbackBoot discards the unconfirmed bundle: previous/ is restored over
// current/ (or current/ is simply removed, leaving the handler to serve the
// embedded bundle), the version is quarantined, and the marker is cleared
// so the restored bundle is not itself put on trial.
func (o *WebOTA) rollbackBoot(version string) error {
	current := o.CurrentDir()
	previous := filepath.Join(o.cfg.Dir, "previous")

	// Move the bad bundle aside first so a failure restoring previous/
	// cannot leave the broken one in place.
	discarded := filepath.Join(o.cfg.Dir, "rolled-back")
	os.RemoveAll(discarded)
	if _, err := os.Stat(current); err == nil {
		if err := os.Rename(current, discarded); err != nil {
			return err
		}
	}
	if _, err := os.Stat(previous); err == nil {
		if err := os.Rename(previous, current); err != nil {
			// current/ is already gone; the embedded bundle takes over.
			slog.Warn("webui: restoring previous web bundle failed", "err", err)
		}
	}
	os.RemoveAll(discarded)

	if version != "" {
		if err := os.WriteFile(o.quarantinePath(), []byte(version+"\n"), quarantinePerms); err != nil {
			slog.Warn("webui: recording quarantined web version failed", "err", err)
		}
	}
	if err := os.Remove(o.bootStatePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	slog.Warn("DIAG: webota-rolled-back", "version", version)
	return nil
}

// quarantinedVersion is the bundle version that already failed to boot and
// must not be applied again, or "" when nothing is quarantined.
func (o *WebOTA) quarantinedVersion() string {
	data, err := os.ReadFile(o.quarantinePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
