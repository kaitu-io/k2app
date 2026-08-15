//! Web OTA — hot-update of the webapp UI bundle (desktop shell).
//!
//! Spec: docs/superpowers/specs/2026-08-14-web-ota-design.md §5.2.
//! Directory layout under {app_data_dir}/web-ota/:
//!   pending/    downloaded+verified bundle awaiting swap
//!   current/    the active bundle (current/version.txt = applied UI version)
//!   previous/   the last-known-good bundle (rollback target)
//!   quarantine/ bundles that failed to boot (.boot-pending still set at next launch)
//! Version-compare semantics mirror mobile K2PluginUtils.kt exactly.

use std::path::PathBuf;

use chrono;

/// Compile-time bridge API version of this shell. Bumped in lockstep with the
/// webapp's BRIDGE_API_VERSION (guarded by the contract-guard plan; this file
/// only consumes it via the manifest `min_bridge` gate).
pub const DESKTOP_BRIDGE_VERSION: u32 = 1;

/// Split "0.4.8.1234-beta.2" into (base segments, optional prerelease segments).
/// Non-numeric segments parse as 0 — mirrors K2PluginUtils.splitVersion.
fn split_version(v: &str) -> (Vec<u64>, Option<Vec<u64>>) {
    let mut parts = v.trim().splitn(2, '-');
    let base = parts
        .next()
        .unwrap_or("")
        .split('.')
        .map(|s| s.parse::<u64>().unwrap_or(0))
        .collect();
    let pre = parts
        .next()
        .map(|p| p.split('.').map(|s| s.parse::<u64>().unwrap_or(0)).collect());
    (base, pre)
}

fn compare_segments(a: &[u64], b: &[u64]) -> std::cmp::Ordering {
    let n = a.len().max(b.len());
    for i in 0..n {
        let av = a.get(i).copied().unwrap_or(0);
        let bv = b.get(i).copied().unwrap_or(0);
        if av != bv {
            return av.cmp(&bv);
        }
    }
    std::cmp::Ordering::Equal
}

/// True iff `remote` is strictly newer than `local`.
/// Same base: a release is newer than any prerelease of that base.
pub fn is_newer_version(remote: &str, local: &str) -> bool {
    use std::cmp::Ordering::*;
    let (rb, rp) = split_version(remote);
    let (lb, lp) = split_version(local);
    match compare_segments(&rb, &lb) {
        Greater => true,
        Less => false,
        Equal => match (rp, lp) {
            (None, Some(_)) => true,
            (Some(_), None) | (None, None) => false,
            (Some(r), Some(l)) => compare_segments(&r, &l) == Greater,
        },
    }
}

/// Gate check: does `current` satisfy a `min_*` requirement?
/// Base segments only (0.4.9-beta.1 satisfies min 0.4.9); None/empty passes.
pub fn meets_min_base(min: Option<&str>, current: &str) -> bool {
    let Some(min) = min.map(str::trim).filter(|s| !s.is_empty()) else {
        return true;
    };
    let (min_base, _) = split_version(min);
    let (cur_base, _) = split_version(current);
    compare_segments(&cur_base, &min_base) != std::cmp::Ordering::Less
}

/// Web OTA manifest — the subset of {CDN}/{brand}/web/latest.json the desktop
/// shell consumes. Foreign fields (min_native, min_linux, released_at) are
/// other consumers' and are ignored (additive JSON, spec §3.2).
#[derive(Debug, Clone, serde::Deserialize)]
pub struct WebManifest {
    pub version: String,
    pub url: String,
    pub hash: String,
    #[serde(default)]
    pub size: Option<u64>,
    #[serde(default)]
    pub sig: Option<String>,
    #[serde(default)]
    pub min_bridge: Option<u32>,
    #[serde(default)]
    pub min_desktop: Option<String>,
}

#[derive(Debug, PartialEq, Eq)]
pub enum Gate {
    Apply,
    Skip(&'static str),
}

/// Desktop gate: sig mandatory → min_bridge → min_desktop → strictly newer.
pub fn evaluate_manifest(
    m: &WebManifest,
    app_version: &str,
    bridge_version: u32,
    local_ui_version: &str,
) -> Gate {
    if m.sig.as_deref().map(str::trim).filter(|s| !s.is_empty()).is_none() {
        return Gate::Skip("missing sig");
    }
    if let Some(min) = m.min_bridge {
        if bridge_version < min {
            return Gate::Skip("min_bridge not satisfied");
        }
    }
    if !meets_min_base(m.min_desktop.as_deref(), app_version) {
        return Gate::Skip("min_desktop not satisfied");
    }
    if !is_newer_version(&m.version, local_ui_version) {
        return Gate::Skip("remote not newer than local");
    }
    Gate::Apply
}

/// Filesystem layout of the web-ota state directory. Root is injected so
/// everything below is unit-testable against a tempdir.
pub struct OtaDirs {
    pub root: PathBuf,
}

impl OtaDirs {
    pub fn new(root: PathBuf) -> Self {
        Self { root }
    }
    pub fn pending(&self) -> PathBuf {
        self.root.join("pending")
    }
    pub fn current(&self) -> PathBuf {
        self.root.join("current")
    }
    pub fn previous(&self) -> PathBuf {
        self.root.join("previous")
    }
    pub fn quarantine(&self) -> PathBuf {
        self.root.join("quarantine")
    }
    pub fn version_file(&self) -> PathBuf {
        self.current().join("version.txt")
    }
    pub fn boot_pending(&self) -> PathBuf {
        self.root.join(".boot-pending")
    }
}

/// The disk directory to serve the UI from, or None → embedded fallback.
pub fn serve_root(d: &OtaDirs) -> Option<PathBuf> {
    let cur = d.current();
    if cur.join("index.html").is_file() {
        Some(cur)
    } else {
        None
    }
}

/// Applied UI version: current/version.txt, falling back to the app version
/// (fresh install / no OTA yet) — same semantics as mobile web-update/version.txt.
pub fn local_ui_version(d: &OtaDirs, app_version: &str) -> String {
    std::fs::read_to_string(d.version_file())
        .map(|s| s.trim().to_string())
        .ok()
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| app_version.to_string())
}

/// Promote a verified pending bundle: current → previous, pending → current.
/// Caller must have written pending/version.txt before calling.
pub fn apply_pending(d: &OtaDirs) -> Result<(), String> {
    let pending = d.pending();
    if !pending.join("index.html").is_file() {
        return Err("pending bundle has no index.html".to_string());
    }
    let previous = d.previous();
    if previous.exists() {
        std::fs::remove_dir_all(&previous).map_err(|e| format!("clear previous: {e}"))?;
    }
    let current = d.current();
    if current.exists() {
        std::fs::rename(&current, &previous).map_err(|e| format!("current -> previous: {e}"))?;
    }
    std::fs::rename(&pending, &current).map_err(|e| format!("pending -> current: {e}"))
}

/// Written just before the shell boots the on-disk UI; cleared by the webapp's
/// ui_boot_ok call. Still present at next startup ⇒ the UI never booted.
pub fn mark_boot_pending(d: &OtaDirs) {
    let _ = std::fs::create_dir_all(&d.root);
    if let Err(e) = std::fs::write(d.boot_pending(), "1") {
        log::error!("[web-ota] failed to write .boot-pending: {e}");
    }
}

pub fn clear_boot_pending(d: &OtaDirs) {
    let _ = std::fs::remove_file(d.boot_pending());
}

#[derive(Debug, PartialEq, Eq)]
pub enum BootCheck {
    Clean,
    RolledBackToPrevious,
    RolledBackToEmbedded,
}

/// Startup rollback: consume a stale .boot-pending, quarantine the bundle that
/// failed to boot, and restore previous/ (or fall back to embedded assets).
pub fn startup_rollback(d: &OtaDirs) -> BootCheck {
    if !d.boot_pending().is_file() {
        return BootCheck::Clean;
    }
    let _ = std::fs::remove_file(d.boot_pending());
    let current = d.current();
    if current.exists() {
        let ver = local_ui_version(d, "unknown");
        let _ = std::fs::create_dir_all(d.quarantine());
        let stamp = chrono::Utc::now().format("%Y%m%d%H%M%S");
        let target = d.quarantine().join(format!("{ver}-{stamp}"));
        if let Err(e) = std::fs::rename(&current, &target) {
            // Last resort: a bundle we can't move must not be served again.
            log::error!("[web-ota] quarantine rename failed ({e}), deleting current");
            let _ = std::fs::remove_dir_all(&current);
        }
    }
    let previous = d.previous();
    if previous.join("index.html").is_file() && std::fs::rename(&previous, d.current()).is_ok() {
        return BootCheck::RolledBackToPrevious;
    }
    BootCheck::RolledBackToEmbedded
}

/// Verify sha256 of `data` against a manifest hash field ("sha256:<hex>" or bare hex).
pub fn verify_sha256(data: &[u8], expected: &str) -> Result<(), String> {
    use sha2::{Digest, Sha256};
    let want = expected.trim().to_lowercase();
    let want = want.strip_prefix("sha256:").unwrap_or(&want);
    let digest = Sha256::digest(data);
    let got: String = digest.iter().map(|b| format!("{b:02x}")).collect();
    if got == want {
        Ok(())
    } else {
        Err(format!("sha256 mismatch: got {got}, want {want}"))
    }
}

/// Verify a minisign signature. Both arguments are base64 of the full minisign
/// text files — identical encoding to tauri-plugin-updater's verify_signature,
/// so the CI can sign web.zip with the existing TAURI_SIGNING_PRIVATE_KEY and
/// we reuse the tauri.conf.json updater pubkey verbatim.
pub fn verify_minisign(data: &[u8], sig_b64: &str, pubkey_b64: &str) -> Result<(), String> {
    use base64::Engine;
    let b64 = base64::engine::general_purpose::STANDARD;
    let pk_txt = String::from_utf8(
        b64.decode(pubkey_b64.trim())
            .map_err(|e| format!("pubkey base64: {e}"))?,
    )
    .map_err(|e| format!("pubkey utf8: {e}"))?;
    let sig_txt = String::from_utf8(
        b64.decode(sig_b64.trim())
            .map_err(|e| format!("sig base64: {e}"))?,
    )
    .map_err(|e| format!("sig utf8: {e}"))?;
    let pk = minisign_verify::PublicKey::decode(&pk_txt).map_err(|e| format!("pubkey decode: {e}"))?;
    let sig = minisign_verify::Signature::decode(&sig_txt).map_err(|e| format!("sig decode: {e}"))?;
    // allow_legacy=true mirrors tauri-plugin-updater (updater.rs:1456)
    pk.verify(data, &sig, true).map_err(|e| format!("minisign verify: {e}"))
}

/// Extract a verified web.zip into `dest`. Zip top level is the dist content
/// (spec §3.3). Rejects zip-slip entries; requires index.html to be present.
pub fn extract_zip_to(data: &[u8], dest: &std::path::Path) -> Result<(), String> {
    let reader = std::io::Cursor::new(data);
    let mut archive = zip::ZipArchive::new(reader).map_err(|e| format!("open zip: {e}"))?;
    std::fs::create_dir_all(dest).map_err(|e| format!("create dest: {e}"))?;
    for i in 0..archive.len() {
        let mut entry = archive.by_index(i).map_err(|e| format!("zip entry {i}: {e}"))?;
        let Some(rel) = entry.enclosed_name() else {
            return Err(format!("unsafe zip entry name: {}", entry.name()));
        };
        let out = dest.join(rel);
        if entry.is_dir() {
            std::fs::create_dir_all(&out).map_err(|e| format!("mkdir {}: {e}", out.display()))?;
            continue;
        }
        if let Some(parent) = out.parent() {
            std::fs::create_dir_all(parent).map_err(|e| format!("mkdir {}: {e}", parent.display()))?;
        }
        let mut file =
            std::fs::File::create(&out).map_err(|e| format!("create {}: {e}", out.display()))?;
        std::io::copy(&mut entry, &mut file).map_err(|e| format!("write {}: {e}", out.display()))?;
    }
    if !dest.join("index.html").is_file() {
        return Err("bundle missing index.html".to_string());
    }
    Ok(())
}

/// Resolve a manifest `url` field: absolute http(s) passes through, otherwise
/// join onto the source's download base — the manifest's own directory
/// (mirrors K2PluginUtils.resolveDownloadURL + fetchManifest's baseURL).
pub fn resolve_download_url(url: &str, download_base: &str) -> String {
    if url.starts_with("http://") || url.starts_with("https://") {
        return url.to_string();
    }
    format!(
        "{}/{}",
        download_base.trim_end_matches('/'),
        url.trim_start_matches('/')
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn newer_by_base_segments() {
        assert!(is_newer_version("0.4.9", "0.4.8"));
        assert!(is_newer_version("0.4.8.1", "0.4.8"));
        assert!(!is_newer_version("0.4.8", "0.4.8.1"));
        assert!(!is_newer_version("0.4.8", "0.4.8"));
        assert!(is_newer_version("0.5.0", "0.4.9.9999"));
    }

    #[test]
    fn missing_segments_pad_zero() {
        assert!(!is_newer_version("0.4.8.0", "0.4.8"));
        assert!(!is_newer_version("0.4.8", "0.4.8.0"));
    }

    #[test]
    fn release_beats_prerelease_same_base() {
        assert!(is_newer_version("0.4.8", "0.4.8-beta.3"));
        assert!(!is_newer_version("0.4.8-beta.3", "0.4.8"));
        assert!(is_newer_version("0.4.8-beta.4", "0.4.8-beta.3"));
        assert!(!is_newer_version("0.4.8-beta.3", "0.4.8-beta.3"));
    }

    #[test]
    fn nonnumeric_segments_are_zero() {
        // "abc" parses to 0 — same as mobile toIntOrNull() ?: 0
        assert!(!is_newer_version("0.abc.1", "0.0.1"));
        assert!(is_newer_version("0.abc.2", "0.0.1"));
    }

    #[test]
    fn min_base_gate() {
        assert!(meets_min_base(None, "0.4.8"));
        assert!(meets_min_base(Some(""), "0.4.8"));
        assert!(meets_min_base(Some("0.4.9"), "0.4.9"));
        assert!(meets_min_base(Some("0.4.9"), "0.4.9-beta.1")); // base-only compare
        assert!(meets_min_base(Some("0.4.9"), "0.5.0"));
        assert!(!meets_min_base(Some("0.4.9"), "0.4.8"));
    }

    const MANIFEST_JSON: &str = r#"{
        "version": "0.4.9.1300",
        "url": "0.4.9.1300/web.zip",
        "hash": "sha256:6a3c087f6be29c69c1cebbbf6e4bf6ed24c108ff8ae2948b1b5e3b03d2edcabc",
        "size": 1234567,
        "released_at": "2026-08-14T12:00:00Z",
        "min_native": "0.4.8",
        "sig": "dGVzdC1zaWduYXR1cmU=",
        "min_bridge": 1,
        "min_desktop": "0.4.9",
        "min_linux": "0.4.9"
    }"#;

    fn manifest() -> WebManifest {
        serde_json::from_str(MANIFEST_JSON).expect("manifest must parse with unknown fields")
    }

    #[test]
    fn manifest_parses_and_ignores_foreign_fields() {
        let m = manifest();
        assert_eq!(m.version, "0.4.9.1300");
        assert_eq!(m.url, "0.4.9.1300/web.zip");
        assert_eq!(m.min_bridge, Some(1));
        assert_eq!(m.min_desktop.as_deref(), Some("0.4.9"));
        assert_eq!(m.size, Some(1234567));
    }

    #[test]
    fn manifest_optional_fields_absent() {
        let m: WebManifest = serde_json::from_str(
            r#"{"version":"0.4.9.1","url":"0.4.9.1/web.zip","hash":"sha256:ab"}"#,
        )
        .unwrap();
        assert_eq!(m.sig, None);
        assert_eq!(m.min_bridge, None);
        assert_eq!(m.min_desktop, None);
    }

    #[test]
    fn gate_applies_when_all_pass() {
        let m = manifest();
        assert_eq!(evaluate_manifest(&m, "0.4.9", 1, "0.4.9.1200"), Gate::Apply);
    }

    #[test]
    fn gate_rejects_missing_sig() {
        let mut m = manifest();
        m.sig = None;
        assert_eq!(
            evaluate_manifest(&m, "0.4.9", 1, "0.4.9.1200"),
            Gate::Skip("missing sig")
        );
        m.sig = Some("  ".into());
        assert_eq!(
            evaluate_manifest(&m, "0.4.9", 1, "0.4.9.1200"),
            Gate::Skip("missing sig")
        );
    }

    #[test]
    fn gate_rejects_min_bridge() {
        let mut m = manifest();
        m.min_bridge = Some(2);
        assert_eq!(
            evaluate_manifest(&m, "0.4.9", 1, "0.4.9.1200"),
            Gate::Skip("min_bridge not satisfied")
        );
    }

    #[test]
    fn gate_rejects_min_desktop() {
        let m = manifest();
        assert_eq!(
            evaluate_manifest(&m, "0.4.8", 1, "0.4.8.1200"),
            Gate::Skip("min_desktop not satisfied")
        );
    }

    #[test]
    fn gate_rejects_not_newer() {
        let m = manifest();
        assert_eq!(
            evaluate_manifest(&m, "0.4.9", 1, "0.4.9.1300"),
            Gate::Skip("remote not newer than local")
        );
        assert_eq!(
            evaluate_manifest(&m, "0.4.9", 1, "0.4.9.1400"),
            Gate::Skip("remote not newer than local")
        );
    }

    #[test]
    fn dirs_layout_matches_contract() {
        let d = OtaDirs::new(std::path::PathBuf::from("/tmp/x/web-ota"));
        assert!(d.pending().ends_with("web-ota/pending"));
        assert!(d.current().ends_with("web-ota/current"));
        assert!(d.previous().ends_with("web-ota/previous"));
        assert!(d.quarantine().ends_with("web-ota/quarantine"));
        assert!(d.version_file().ends_with("web-ota/current/version.txt"));
        assert!(d.boot_pending().ends_with("web-ota/.boot-pending"));
    }

    #[test]
    fn serve_root_requires_index_html() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        assert_eq!(serve_root(&d), None); // no current at all
        std::fs::create_dir_all(d.current()).unwrap();
        assert_eq!(serve_root(&d), None); // current without index.html
        std::fs::write(d.current().join("index.html"), "<html></html>").unwrap();
        assert_eq!(serve_root(&d), Some(d.current()));
    }

    #[test]
    fn local_ui_version_falls_back_to_app_version() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        assert_eq!(local_ui_version(&d, "0.4.8"), "0.4.8");
        std::fs::create_dir_all(d.current()).unwrap();
        std::fs::write(d.version_file(), "0.4.8.1234\n").unwrap();
        assert_eq!(local_ui_version(&d, "0.4.8"), "0.4.8.1234"); // trimmed
        std::fs::write(d.version_file(), "  ").unwrap();
        assert_eq!(local_ui_version(&d, "0.4.8"), "0.4.8"); // blank → fallback
    }

    fn seed_bundle(dir: &std::path::Path, marker: &str) {
        std::fs::create_dir_all(dir).unwrap();
        std::fs::write(dir.join("index.html"), format!("<html>{marker}</html>")).unwrap();
        std::fs::write(dir.join("version.txt"), marker).unwrap();
    }

    #[test]
    fn apply_pending_first_install() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        seed_bundle(&d.pending(), "v1");
        apply_pending(&d).unwrap();
        assert!(!d.pending().exists());
        assert_eq!(local_ui_version(&d, "app"), "v1");
        assert!(!d.previous().exists()); // nothing to demote on first install
    }

    #[test]
    fn apply_pending_rotates_current_to_previous() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        seed_bundle(&d.current(), "v1");
        seed_bundle(&d.pending(), "v2");
        apply_pending(&d).unwrap();
        assert_eq!(local_ui_version(&d, "app"), "v2");
        assert_eq!(
            std::fs::read_to_string(d.previous().join("version.txt")).unwrap(),
            "v1"
        );
    }

    #[test]
    fn apply_pending_drops_stale_previous() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        seed_bundle(&d.previous(), "v0");
        seed_bundle(&d.current(), "v1");
        seed_bundle(&d.pending(), "v2");
        apply_pending(&d).unwrap();
        assert_eq!(local_ui_version(&d, "app"), "v2");
        assert_eq!(
            std::fs::read_to_string(d.previous().join("version.txt")).unwrap(),
            "v1"
        );
    }

    #[test]
    fn apply_pending_rejects_bundle_without_index() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        std::fs::create_dir_all(d.pending()).unwrap();
        std::fs::write(d.pending().join("app.js"), "x").unwrap();
        seed_bundle(&d.current(), "v1");
        assert!(apply_pending(&d).is_err());
        // current untouched on failure
        assert_eq!(local_ui_version(&d, "app"), "v1");
    }

    #[test]
    fn boot_pending_mark_and_clear() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        mark_boot_pending(&d);
        assert!(d.boot_pending().is_file());
        clear_boot_pending(&d);
        assert!(!d.boot_pending().exists());
    }

    #[test]
    fn startup_rollback_noop_without_marker() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        seed_bundle(&d.current(), "v2");
        assert_eq!(startup_rollback(&d), BootCheck::Clean);
        assert_eq!(local_ui_version(&d, "app"), "v2"); // untouched
    }

    #[test]
    fn startup_rollback_quarantines_and_restores_previous() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        seed_bundle(&d.previous(), "v1");
        seed_bundle(&d.current(), "v2");
        mark_boot_pending(&d);
        assert_eq!(startup_rollback(&d), BootCheck::RolledBackToPrevious);
        assert!(!d.boot_pending().exists()); // marker consumed
        assert_eq!(local_ui_version(&d, "app"), "v1"); // previous promoted
        assert!(!d.previous().exists());
        // v2 landed in quarantine, name prefixed by its version
        let quarantined: Vec<_> = std::fs::read_dir(d.quarantine())
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .collect();
        assert_eq!(quarantined.len(), 1);
        assert!(quarantined[0].starts_with("v2-"), "got {:?}", quarantined);
    }

    #[test]
    fn startup_rollback_to_embedded_when_no_previous() {
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        seed_bundle(&d.current(), "v2");
        mark_boot_pending(&d);
        assert_eq!(startup_rollback(&d), BootCheck::RolledBackToEmbedded);
        assert_eq!(serve_root(&d), None); // falls through to embedded assets
    }

    #[test]
    fn startup_rollback_marker_without_current() {
        // Marker set but current was never created (e.g. manual web-ota wipe):
        // must not panic, must consume the marker.
        let tmp = tempfile::tempdir().unwrap();
        let d = OtaDirs::new(tmp.path().join("web-ota"));
        std::fs::create_dir_all(&d.root).unwrap();
        mark_boot_pending(&d);
        assert_eq!(startup_rollback(&d), BootCheck::RolledBackToEmbedded);
        assert!(!d.boot_pending().exists());
    }

    #[test]
    fn sha256_accepts_prefixed_and_bare_hex() {
        // sha256("test") is a well-known vector
        let h = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08";
        assert!(verify_sha256(b"test", h).is_ok());
        assert!(verify_sha256(b"test", &format!("sha256:{h}")).is_ok());
        assert!(verify_sha256(b"test", &format!("SHA256:{}", h.to_uppercase())).is_ok());
        assert!(verify_sha256(b"tampered", h).is_err());
        assert!(verify_sha256(b"test", "sha256:deadbeef").is_err());
    }

    #[test]
    fn minisign_verifies_known_vector_and_rejects_tampering() {
        use base64::Engine;
        let pk_file = "untrusted comment: minisign public key\nRWQf6LRCGA9i53mlYecO4IzT51TGPpvWucNSCh1CBM0QTaLn73Y7GFO3\n";
        let sig_file = "untrusted comment: signature from minisign secret key\nRUQf6LRCGA9i559r3g7V1qNyJDApGip8MfqcadIgT9CuhV3EMhHoN1mGTkUidF/z7SrlQgXdy8ofjb7bNJJylDOocrCo8KLzZwo=\ntrusted comment: timestamp:1556193335\tfile:test\ny/rUw2y8/hOUYjZU71eHp/Wo1KZ40fGy2VJEDl34XMJM+TX48Ss/17u3IvIfbVR1FkZZSNCisQbuQY+bHwhEBg==\n";
        let pk_b64 = base64::engine::general_purpose::STANDARD.encode(pk_file);
        let sig_b64 = base64::engine::general_purpose::STANDARD.encode(sig_file);

        assert!(verify_minisign(b"test", &sig_b64, &pk_b64).is_ok());
        assert!(verify_minisign(b"tampered", &sig_b64, &pk_b64).is_err());
    }

    #[test]
    fn minisign_rejects_garbage_inputs() {
        assert!(verify_minisign(b"test", "not-base64!!!", "also-not").is_err());
        use base64::Engine;
        let junk = base64::engine::general_purpose::STANDARD.encode("not a minisign file");
        assert!(verify_minisign(b"test", &junk, &junk).is_err());
    }

    fn make_zip(entries: &[(&str, &str)]) -> Vec<u8> {
        use std::io::Write;
        let mut buf = std::io::Cursor::new(Vec::new());
        {
            let mut w = zip::ZipWriter::new(&mut buf);
            let opts = zip::write::SimpleFileOptions::default()
                .compression_method(zip::CompressionMethod::Deflated);
            for (name, content) in entries {
                w.start_file(*name, opts).unwrap();
                w.write_all(content.as_bytes()).unwrap();
            }
            w.finish().unwrap();
        }
        buf.into_inner()
    }

    #[test]
    fn extract_zip_roundtrip() {
        let tmp = tempfile::tempdir().unwrap();
        let dest = tmp.path().join("pending");
        let data = make_zip(&[
            ("index.html", "<html>ok</html>"),
            ("debug.html", "<html>dbg</html>"),
            ("assets/app-abc123.js", "console.log(1)"),
        ]);
        extract_zip_to(&data, &dest).unwrap();
        assert_eq!(
            std::fs::read_to_string(dest.join("index.html")).unwrap(),
            "<html>ok</html>"
        );
        assert_eq!(
            std::fs::read_to_string(dest.join("assets/app-abc123.js")).unwrap(),
            "console.log(1)"
        );
    }

    #[test]
    fn extract_zip_rejects_bundle_without_index() {
        let tmp = tempfile::tempdir().unwrap();
        let dest = tmp.path().join("pending");
        let data = make_zip(&[("app.js", "x")]);
        assert!(extract_zip_to(&data, &dest).is_err());
    }

    #[test]
    fn extract_zip_rejects_traversal_entries() {
        let tmp = tempfile::tempdir().unwrap();
        let dest = tmp.path().join("pending");
        let data = make_zip(&[("../evil.html", "pwn"), ("index.html", "ok")]);
        assert!(extract_zip_to(&data, &dest).is_err());
        assert!(!tmp.path().join("evil.html").exists());
    }

    #[test]
    fn extract_zip_rejects_corrupt_data() {
        let tmp = tempfile::tempdir().unwrap();
        assert!(extract_zip_to(b"definitely not a zip", &tmp.path().join("p")).is_err());
    }

    #[test]
    fn resolve_download_url_relative_and_absolute() {
        // url is manifest-directory-relative: "{version}/web.zip" (contract table)
        assert_eq!(
            resolve_download_url("0.4.9.1300/web.zip", "https://d0.all7.cc/kaitu/web"),
            "https://d0.all7.cc/kaitu/web/0.4.9.1300/web.zip"
        );
        assert_eq!(
            resolve_download_url("0.4.9.1300/web.zip", "https://d0.all7.cc/kaitu/web/beta"),
            "https://d0.all7.cc/kaitu/web/beta/0.4.9.1300/web.zip"
        );
        assert_eq!(
            resolve_download_url("/x.zip", "https://d0.all7.cc/kaitu/web/"),
            "https://d0.all7.cc/kaitu/web/x.zip"
        );
        assert_eq!(
            resolve_download_url(
                "https://elsewhere.example/web.zip",
                "https://d0.all7.cc/kaitu/web"
            ),
            "https://elsewhere.example/web.zip"
        );
    }
}
