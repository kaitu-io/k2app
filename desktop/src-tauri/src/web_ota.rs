//! Web OTA — hot-update of the webapp UI bundle (desktop shell).
//!
//! Spec: docs/superpowers/specs/2026-08-14-web-ota-design.md §5.2.
//! Directory layout under {app_data_dir}/web-ota/:
//!   pending/    downloaded+verified bundle awaiting swap
//!   current/    the active bundle (current/version.txt = applied UI version)
//!   previous/   the last-known-good bundle (rollback target)
//!   quarantine/ bundles that failed to boot (.boot-pending still set at next launch)
//! Version-compare semantics mirror mobile K2PluginUtils.kt exactly.

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
}
