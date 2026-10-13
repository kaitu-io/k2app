//! macOS Dock icon after an icon change (0.4.12 replaced the Kaitu logo).
//!
//! The Dock keeps its own on-disk icon cache
//! (`$DARWIN_USER_CACHE_DIR/com.apple.dock.iconcache`). Verified on a Tahoe VM
//! upgrading 0.4.11 → 0.4.12: none of pkg upgrade, the updater's bundle swap +
//! `touch`, `lsregister -f`, renaming CFBundleIconFile, `killall Dock` or a
//! reboot made the Dock drop the old logo — Finder already showed the new one.
//! Only deleting that cache file and restarting the Dock did.
//!
//! Two layers:
//! 1. Every launch: set the running app's Dock tile image from the bundled
//!    icon, so the tile is right whatever the cache holds.
//! 2. Once per icon change: delete the Dock cache and restart the Dock, so a
//!    pinned, not-running Kaitu tile is right too. Costs one Dock flicker, so
//!    it is gated on a fingerprint of the bundled icon and skipped on a fresh
//!    install (nothing stale to replace).

use std::path::{Path, PathBuf};

use sha2::{Digest, Sha256};
use tauri::{AppHandle, Manager};

const FINGERPRINT_FILE: &str = "dock-icon-fingerprint";
const DOCK_CACHE_FILE: &str = "com.apple.dock.iconcache";
/// Written by earlier versions' first launch; their presence means a Dock cache
/// entry for the previous icon may exist.
const PRIOR_LAUNCH_MARKERS: &[&str] = &["storage.json", "storage-migrated"];

/// What to do about the Dock cache, given the stored and current fingerprints.
#[derive(Debug, PartialEq, Eq)]
enum CacheAction {
    /// Icon unchanged since the last refresh.
    Nothing,
    /// First launch ever: no stale entry, just remember the icon.
    Record,
    /// Icon differs from what an earlier launch showed: flush and remember.
    Flush,
}

fn decide(stored: Option<&str>, current: &str, launched_before: bool) -> CacheAction {
    match stored {
        Some(s) if s == current => CacheAction::Nothing,
        Some(_) => CacheAction::Flush,
        None if launched_before => CacheAction::Flush,
        None => CacheAction::Record,
    }
}

/// `<bundle>/Contents/Resources/<CFBundleIconFile>` for the running executable.
fn bundled_icon_path() -> Option<PathBuf> {
    let contents = std::env::current_exe().ok()?.parent()?.parent()?.to_path_buf();
    let info: plist::Dictionary = plist::from_file(contents.join("Info.plist")).ok()?;
    let name = info.get("CFBundleIconFile")?.as_string()?;
    let file = if name.ends_with(".icns") { name.to_string() } else { format!("{name}.icns") };
    let path = contents.join("Resources").join(file);
    path.is_file().then_some(path)
}

fn fingerprint(path: &Path) -> Option<String> {
    let bytes = std::fs::read(path).ok()?;
    Some(Sha256::digest(&bytes).iter().map(|b| format!("{b:02x}")).collect())
}

/// Must run on the main thread (Tauri's setup closure does).
fn set_dock_tile(icon: &Path) {
    use objc2::ClassType;
    use objc2_app_kit::{NSApplication, NSImage};
    use objc2_foundation::{MainThreadMarker, NSString};

    let Some(mtm) = MainThreadMarker::new() else {
        log::warn!("[dock-icon] not on the main thread, tile not set");
        return;
    };
    // SAFETY: main thread (checked above); NSImage is built from a file we own
    // inside the bundle and handed straight to NSApplication.
    unsafe {
        let Some(image) =
            NSImage::initWithContentsOfFile(NSImage::alloc(), &NSString::from_str(&icon.to_string_lossy()))
        else {
            log::warn!("[dock-icon] could not load {}", icon.display());
            return;
        };
        NSApplication::sharedApplication(mtm).setApplicationIconImage(Some(&image));
    }
}

fn dock_cache_path() -> Option<PathBuf> {
    let out = std::process::Command::new("/usr/bin/getconf")
        .arg("DARWIN_USER_CACHE_DIR")
        .output()
        .ok()?;
    let dir = String::from_utf8(out.stdout).ok()?;
    let dir = dir.trim();
    (!dir.is_empty()).then(|| PathBuf::from(dir).join(DOCK_CACHE_FILE))
}

fn flush_dock_cache() -> bool {
    let Some(cache) = dock_cache_path() else {
        log::warn!("[dock-icon] cannot resolve DARWIN_USER_CACHE_DIR");
        return false;
    };
    match std::fs::remove_file(&cache) {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => {
            log::warn!("[dock-icon] remove {}: {}", cache.display(), e);
            return false;
        }
    }
    // launchd restarts the Dock at once; it rebuilds the cache from bundles.
    let ok = std::process::Command::new("/usr/bin/killall")
        .arg("Dock")
        .status()
        .map(|s| s.success())
        .unwrap_or(false);
    log::info!("[dock-icon] Dock icon cache flushed, Dock restarted={}", ok);
    ok
}

pub fn refresh(app: &AppHandle) {
    // Dev builds run from target/, not a bundle — nothing to do.
    let Some(icon) = bundled_icon_path() else {
        log::debug!("[dock-icon] no bundled icon (not running from a .app)");
        return;
    };
    set_dock_tile(&icon);

    let Some(data_dir) = app.path().app_data_dir().ok() else { return };
    // Sampled now, before the webview loads: the webapp writes storage.json
    // within seconds of a first launch, which would make a fresh install look
    // like an upgrade if this were checked on the worker thread.
    let launched_before = PRIOR_LAUNCH_MARKERS.iter().any(|m| data_dir.join(m).exists());
    std::thread::spawn(move || {
        let Some(current) = fingerprint(&icon) else { return };
        let marker = data_dir.join(FINGERPRINT_FILE);
        let stored = std::fs::read_to_string(&marker).ok();
        let action = decide(stored.as_deref().map(str::trim), &current, launched_before);
        log::info!("[dock-icon] cache action: {:?}", action);
        let record = match action {
            CacheAction::Nothing => false,
            CacheAction::Record => true,
            // Leave the marker unwritten on failure so the next launch retries.
            CacheAction::Flush => flush_dock_cache(),
        };
        if record {
            let _ = std::fs::create_dir_all(&data_dir);
            if let Err(e) = std::fs::write(&marker, &current) {
                log::warn!("[dock-icon] write {}: {}", marker.display(), e);
            }
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unchanged_icon_does_nothing() {
        assert_eq!(decide(Some("abc"), "abc", true), CacheAction::Nothing);
    }

    #[test]
    fn changed_icon_flushes() {
        assert_eq!(decide(Some("old"), "new", true), CacheAction::Flush);
    }

    #[test]
    fn upgrade_from_version_without_fingerprint_flushes() {
        // 0.4.11 → 0.4.12: no marker yet, but the app ran before.
        assert_eq!(decide(None, "new", true), CacheAction::Flush);
    }

    #[test]
    fn fresh_install_only_records() {
        assert_eq!(decide(None, "new", false), CacheAction::Record);
    }
}
