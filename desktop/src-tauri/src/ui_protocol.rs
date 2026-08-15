//
// `kaitu-ui://` URI-scheme handler — serves the webapp UI, disk (web-ota
// current bundle) first, embedded assets (asset_resolver) as fallback.
// Page origin: kaitu-ui://localhost (macOS) / http://kaitu-ui.localhost
// (Windows — Tauri's automatic mapping, see register_uri_scheme_protocol docs).
// The scheme name is a brand-neutral internal token (kaitu-icon:// precedent).

use std::path::{Path, PathBuf};

pub const UI_SCHEME: &str = "kaitu-ui";

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
    let decoded = urlencoding::decode(url_path).ok()?;
    let rel = decoded.trim_start_matches('/');
    if rel.split(['/', '\\']).any(|seg| seg == "..") {
        return None;
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
}
