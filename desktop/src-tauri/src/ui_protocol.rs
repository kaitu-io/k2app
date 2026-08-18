//
// `kaitu-ui://` URI-scheme handler — serves the webapp UI, disk (web-ota
// current bundle) first, embedded assets (asset_resolver) as fallback.
// Page origin: kaitu-ui://localhost (macOS) / http://kaitu-ui.localhost
// (Windows — Tauri's automatic mapping, see register_uri_scheme_protocol docs).
// The scheme name is a brand-neutral internal token (kaitu-icon:// precedent).

use std::path::{Path, PathBuf};
use tauri::http::Response;
use tauri::{Manager, UriSchemeContext, Wry};
use crate::web_ota;

pub const UI_SCHEME: &str = "kaitu-ui";

/// The URL the shell must boot the webapp at: the origin ROOT, never
/// `index.html`.
///
/// The webapp mounts a react-router `BrowserRouter` whose route table hangs off
/// `/` with no catch-all. Booting at `…/index.html` therefore hands the router
/// a location that matches nothing, and it renders an empty tree — a blank
/// window, with no error anywhere: the Rust side logs a clean boot, the
/// `ui_boot_ok` handshake still fires (it only proves the bundle's JS ran), and
/// the non-React layers (bridge, stores, pollers) keep working, so the logs look
/// completely healthy. The only trace is one react-router line,
/// `No routes matched location "/index.html"`.
///
/// The protocol handler maps `/` to index.html for both the on-disk OTA bundle
/// and the embedded assets, so the root is always servable.
pub fn ui_boot_url() -> String {
    ui_origin_url("")
}

/// Platform-correct absolute URL for a path served by the kaitu-ui protocol.
pub fn ui_origin_url(path_and_query: &str) -> String {
    let rest = path_and_query.trim_start_matches('/');
    #[cfg(target_os = "windows")]
    {
        format!("http://{UI_SCHEME}.localhost/{rest}")
    }
    #[cfg(not(target_os = "windows"))]
    {
        format!("{UI_SCHEME}://localhost/{rest}")
    }
}

/// MIME by extension — covers everything webapp/dist ships (js/webp/png/html/
/// json today, plus the usual web asset set for future-proofing).
pub fn mime_for_path(path: &str) -> &'static str {
    let ext = Path::new(path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    match ext.as_str() {
        "html" | "htm" => "text/html",
        "js" | "mjs" => "text/javascript",
        "css" => "text/css",
        "json" | "map" => "application/json",
        "png" => "image/png",
        "webp" => "image/webp",
        "svg" => "image/svg+xml",
        "jpg" | "jpeg" => "image/jpeg",
        "gif" => "image/gif",
        "ico" => "image/x-icon",
        "woff2" => "font/woff2",
        "woff" => "font/woff",
        "ttf" => "font/ttf",
        "wasm" => "application/wasm",
        "txt" => "text/plain",
        _ => "application/octet-stream",
    }
}

/// index.html must revalidate (it names the fingerprinted assets); everything
/// else in a bundle is content-addressed or replaceable wholesale.
pub fn cache_control_for(mime: &str) -> &'static str {
    if mime == "text/html" {
        "no-cache"
    } else {
        "public, max-age=86400"
    }
}

/// Map a request path to a file under `root`. Traversal-safe (checked after
/// percent-decoding); SPA fallback for extensionless routes.
pub fn resolve_disk_file(root: &Path, url_path: &str) -> Option<PathBuf> {
    use std::path::Component;

    let decoded = urlencoding::decode(url_path).ok()?;
    let rel = decoded.trim_start_matches('/');

    // For non-empty paths, validate components and segments.
    if !rel.is_empty() {
        // Require all path components to be plain names (no .. / . / drive prefixes / rooted paths).
        // This guards against Path::join semantics where absolute paths replace root.
        for component in Path::new(rel).components() {
            if !matches!(component, Component::Normal(_)) {
                return None;
            }
        }

        // Additional segment-level checks: each segment must be non-empty, no colons (drive letters),
        // and not . or .. (defense in depth).
        for seg in rel.split(['/', '\\']) {
            if seg.is_empty() || seg == ".." || seg == "." || seg.contains(':') {
                return None;
            }
        }
    }

    let candidate = if rel.is_empty() {
        root.join("index.html")
    } else {
        root.join(rel)
    };
    if candidate.is_file() {
        return Some(candidate);
    }
    if Path::new(rel).extension().is_none() {
        let index = root.join("index.html");
        if index.is_file() {
            return Some(index);
        }
    }
    None
}

fn ok_response(bytes: Vec<u8>, mime: &str, cache: &str) -> Response<Vec<u8>> {
    Response::builder()
        .status(200)
        .header("Content-Type", mime)
        .header("Cache-Control", cache)
        .body(bytes)
        .unwrap_or_else(|_| not_found())
}

fn not_found() -> Response<Vec<u8>> {
    Response::builder()
        .status(404)
        .body(Vec::new())
        .expect("static 404 response should always build")
}

/// kaitu-ui:// handler. Disk (web-ota current bundle) wins; embedded assets
/// via asset_resolver otherwise. A valid disk bundle must be self-consistent —
/// asset misses under a disk root 404 rather than mixing in embedded files.
pub fn handle_kaitu_ui(
    ctx: UriSchemeContext<'_, Wry>,
    request: tauri::http::Request<Vec<u8>>,
) -> Response<Vec<u8>> {
    let path = request.uri().path().to_string();
    let app = ctx.app_handle();

    // 1) On-disk OTA bundle
    if let Some(dirs) = web_ota::ota_dirs(app) {
        if let Some(root) = web_ota::serve_root(&dirs) {
            return match resolve_disk_file(&root, &path) {
                Some(file) => match std::fs::read(&file) {
                    Ok(bytes) => {
                        let mime = mime_for_path(&file.to_string_lossy());
                        ok_response(bytes, mime, cache_control_for(mime))
                    }
                    Err(e) => {
                        log::warn!("[kaitu-ui] read {} failed: {e}", file.display());
                        not_found()
                    }
                },
                None => not_found(),
            };
        }
    }

    // 2) Embedded assets (fresh install / rolled back to embedded)
    let resolver = app.asset_resolver();
    let asset_path = if path == "/" || path.is_empty() {
        "/index.html".to_string()
    } else {
        path.clone()
    };
    if let Some(asset) = resolver.get(asset_path) {
        let mime = asset.mime_type.clone();
        let cache = cache_control_for(&mime);
        return ok_response(asset.bytes, &mime, cache);
    }
    // SPA fallback for extensionless routes
    if Path::new(&path).extension().is_none() {
        if let Some(asset) = resolver.get("/index.html".to_string()) {
            return ok_response(asset.bytes, "text/html", "no-cache");
        }
    }
    not_found()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ui_origin_url_platform_shape() {
        let url = ui_origin_url("index.html");
        #[cfg(target_os = "windows")]
        assert_eq!(url, "http://kaitu-ui.localhost/index.html");
        #[cfg(not(target_os = "windows"))]
        assert_eq!(url, "kaitu-ui://localhost/index.html");
        // leading slash tolerated
        assert_eq!(ui_origin_url("/index.html"), url);
    }

    /// Every boot path (fresh install, post-migration, on-disk OTA bundle)
    /// navigates here, and the webapp's router only matches routes under `/`.
    /// A path segment in this URL is not a cosmetic difference — it is a blank
    /// window for every desktop user, and nothing else in the system reports
    /// it: 0.4.8 shipped `index.html` here and every layer still logged a
    /// healthy boot.
    #[test]
    fn boot_url_is_the_origin_root_not_a_file() {
        let boot = ui_boot_url();
        #[cfg(target_os = "windows")]
        assert_eq!(boot, "http://kaitu-ui.localhost/");
        #[cfg(not(target_os = "windows"))]
        assert_eq!(boot, "kaitu-ui://localhost/");

        // Stated as the property rather than only as a literal: whatever the
        // scheme/host become, the path must stay empty.
        let path = boot
            .split_once("//")
            .and_then(|(_, rest)| rest.split_once('/'))
            .map(|(_, p)| p)
            .expect("boot URL must have a host and a path");
        assert_eq!(path, "", "boot URL must address the origin root, got {boot}");
    }

    /// The handler that has to satisfy the above: the root must resolve to
    /// index.html on the on-disk OTA path, or booting at `/` would 404 instead
    /// of rendering.
    #[test]
    fn disk_root_resolves_to_index() {
        let tmp = tempfile::tempdir().unwrap();
        let root = tmp.path();
        std::fs::write(root.join("index.html"), "<html>root</html>").unwrap();
        assert_eq!(resolve_disk_file(root, "/"), Some(root.join("index.html")));
    }

    #[test]
    fn mime_covers_dist_extensions() {
        assert_eq!(mime_for_path("/index.html"), "text/html");
        assert_eq!(mime_for_path("/assets/app-abc.js"), "text/javascript");
        assert_eq!(mime_for_path("/assets/style.css"), "text/css");
        assert_eq!(mime_for_path("/version.json"), "application/json");
        assert_eq!(mime_for_path("/favicon.png"), "image/png");
        assert_eq!(mime_for_path("/images/x.webp"), "image/webp");
        assert_eq!(mime_for_path("/a.svg"), "image/svg+xml");
        assert_eq!(mime_for_path("/f.woff2"), "font/woff2");
        assert_eq!(mime_for_path("/x.wasm"), "application/wasm");
        assert_eq!(mime_for_path("/noext"), "application/octet-stream");
        assert_eq!(mime_for_path("/UP.HTML"), "text/html"); // case-insensitive
    }

    #[test]
    fn cache_policy() {
        assert_eq!(cache_control_for("text/html"), "no-cache");
        assert_eq!(cache_control_for("text/javascript"), "public, max-age=86400");
    }

    fn disk_root() -> tempfile::TempDir {
        let tmp = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(tmp.path().join("assets")).unwrap();
        std::fs::write(tmp.path().join("index.html"), "<html>root</html>").unwrap();
        std::fs::write(tmp.path().join("assets/app.js"), "js").unwrap();
        tmp
    }

    #[test]
    fn resolve_disk_file_basic() {
        let tmp = disk_root();
        let root = tmp.path();
        assert_eq!(resolve_disk_file(root, "/"), Some(root.join("index.html")));
        assert_eq!(resolve_disk_file(root, ""), Some(root.join("index.html")));
        assert_eq!(
            resolve_disk_file(root, "/assets/app.js"),
            Some(root.join("assets/app.js"))
        );
    }

    #[test]
    fn resolve_disk_file_spa_fallback() {
        let tmp = disk_root();
        let root = tmp.path();
        // extensionless route → SPA fallback
        assert_eq!(resolve_disk_file(root, "/account"), Some(root.join("index.html")));
        // asset miss with extension → 404, not index
        assert_eq!(resolve_disk_file(root, "/assets/missing.js"), None);
    }

    #[test]
    fn resolve_disk_file_rejects_traversal() {
        let tmp = disk_root();
        let root = tmp.path();
        assert_eq!(resolve_disk_file(root, "/../secret"), None);
        assert_eq!(resolve_disk_file(root, "/%2e%2e/secret"), None); // encoded, decoded before check
        assert_eq!(resolve_disk_file(root, "/a/../../b"), None);
    }

    #[test]
    fn resolve_disk_file_decodes_percent_encoding() {
        let tmp = disk_root();
        std::fs::write(tmp.path().join("has space.txt"), "x").unwrap();
        assert_eq!(
            resolve_disk_file(tmp.path(), "/has%20space.txt"),
            Some(tmp.path().join("has space.txt"))
        );
    }

    #[test]
    fn resolve_disk_file_rejects_drive_prefix() {
        let tmp = disk_root();
        let root = tmp.path();
        // Windows drive letters (uppercase and lowercase)
        assert_eq!(resolve_disk_file(root, "/C:/evil.txt"), None);
        assert_eq!(resolve_disk_file(root, "/c:/evil.txt"), None);
    }

    #[test]
    fn resolve_disk_file_rejects_encoded_drive_prefix() {
        let tmp = disk_root();
        let root = tmp.path();
        // Percent-encoded backslash after drive letter: /C:%5Cevil.txt → /C:\evil.txt
        assert_eq!(resolve_disk_file(root, "/C:%5Cevil.txt"), None);
    }

    #[test]
    fn resolve_disk_file_rejects_rooted_backslash() {
        let tmp = disk_root();
        let root = tmp.path();
        // Rooted path with backslash: /%5Cevil → /\evil
        assert_eq!(resolve_disk_file(root, "/%5Cevil"), None);
    }

    #[test]
    fn ok_response_sets_headers() {
        let resp = ok_response(b"<html></html>".to_vec(), "text/html", "no-cache");
        assert_eq!(resp.status(), 200);
        assert_eq!(resp.headers().get("Content-Type").unwrap(), "text/html");
        assert_eq!(resp.headers().get("Cache-Control").unwrap(), "no-cache");
    }

    #[test]
    fn not_found_is_empty_404() {
        let resp = not_found();
        assert_eq!(resp.status(), 404);
        assert!(resp.body().is_empty());
    }
}
