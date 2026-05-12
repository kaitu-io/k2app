// desktop/src-tauri/src/icon_protocol.rs
//
// Tauri URI-scheme handler for `kaitu-icon://` — serves app icons to the
// webapp for the App Bypass UI. The list_running_apps command (Task 4.1)
// emits per-app `icon_url` strings of the form:
//
//   macOS:    kaitu-icon://bundle/<urlencoded-bundle-id>
//   Windows:  kaitu-icon://exe/<urlencoded-exe-path>
//
// The WebView resolves these via this handler. macOS uses NSWorkspace +
// NSBitmapImageRep to render the bundle's icon to PNG; Windows is a v1 stub
// (returns 404) — desktop devs without a Windows test rig should not be
// blocked by icon fidelity.

use tauri::http::Response;
use tauri::{UriSchemeContext, Wry};

#[cfg(target_os = "macos")]
fn icon_for_bundle(bundle_id: &str) -> Option<Vec<u8>> {
    use objc2_app_kit::{NSBitmapImageFileType, NSBitmapImageRep, NSWorkspace};
    use objc2_foundation::{NSDictionary, NSString};

    // SAFETY: NSWorkspace + NSBitmapImageRep are read-only metadata accessors
    // and safe to invoke from a worker thread. The Tauri protocol handler runs
    // off the main thread; we mutate no AppKit state.
    unsafe {
        let workspace = NSWorkspace::sharedWorkspace();
        let bundle_id_ns = NSString::from_str(bundle_id);

        // Resolve bundle id → app URL on disk. Returns None if not installed.
        let app_url = workspace.URLForApplicationWithBundleIdentifier(&bundle_id_ns)?;
        let path_ns = app_url.path()?;

        // NSWorkspace#iconForFile returns the Finder/Launch Services icon for
        // any path. Documented as never returning nil (falls back to a generic
        // icon), but we still go through NSImage → TIFF → bitmap rep, each of
        // which may return None for unusual images.
        let image = workspace.iconForFile(&path_ns);
        let tiff = image.TIFFRepresentation()?;
        let bitmap = NSBitmapImageRep::imageRepWithData(&tiff)?;
        let properties = NSDictionary::new();
        let png_data = bitmap.representationUsingType_properties(
            NSBitmapImageFileType::PNG,
            &properties,
        )?;

        // NSData → &[u8] is documented contiguous; copy into a Vec so the bytes
        // outlive the autorelease scope.
        Some(png_data.bytes().to_vec())
    }
}

#[cfg(target_os = "windows")]
fn icon_for_exe(_exe_path: &str) -> Option<Vec<u8>> {
    // V1: stub — Windows icons are not currently extracted. The webapp falls
    // back to a generic icon placeholder when this returns 404. A future
    // implementation can use ExtractIconExW + Gdiplus PNG encoding.
    None
}

/// Parsed components of a `kaitu-icon://` URI.
///
/// Pure helper extracted for unit testing.
#[derive(Debug, PartialEq, Eq)]
struct ParsedIconUri {
    kind: String,
    id: String,
}

fn parse_icon_uri(uri: &str) -> Option<ParsedIconUri> {
    let rest = uri.strip_prefix("kaitu-icon://")?;
    let mut parts = rest.splitn(2, '/');
    let kind = parts.next()?;
    let id_encoded = parts.next()?;
    if kind.is_empty() || id_encoded.is_empty() {
        return None;
    }
    let id = urlencoding::decode(id_encoded).ok()?.into_owned();
    Some(ParsedIconUri {
        kind: kind.to_string(),
        id,
    })
}

pub fn handle_kaitu_icon(
    _ctx: UriSchemeContext<'_, Wry>,
    request: tauri::http::Request<Vec<u8>>,
) -> Response<Vec<u8>> {
    let uri = request.uri().to_string();
    let payload: Option<Vec<u8>> = parse_icon_uri(&uri).and_then(|parsed| match parsed.kind.as_str() {
        #[cfg(target_os = "macos")]
        "bundle" => icon_for_bundle(&parsed.id),
        #[cfg(target_os = "windows")]
        "exe" => icon_for_exe(&parsed.id),
        _ => None,
    });

    match payload {
        Some(bytes) => Response::builder()
            .status(200)
            .header("Content-Type", "image/png")
            .header("Cache-Control", "public, max-age=86400")
            .body(bytes)
            .unwrap_or_else(|_| empty_404()),
        None => empty_404(),
    }
}

fn empty_404() -> Response<Vec<u8>> {
    Response::builder()
        .status(404)
        .body(Vec::new())
        .expect("static 404 response should always build")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_macos_bundle_uri() {
        let parsed = parse_icon_uri("kaitu-icon://bundle/com.apple.Safari").unwrap();
        assert_eq!(parsed.kind, "bundle");
        assert_eq!(parsed.id, "com.apple.Safari");
    }

    #[test]
    fn urldecodes_id() {
        // Windows exe path → percent-encoded slashes / colons / spaces
        let parsed = parse_icon_uri(
            "kaitu-icon://exe/C%3A%5CProgram%20Files%5CTestApp%5Ctest.exe",
        )
        .unwrap();
        assert_eq!(parsed.kind, "exe");
        assert_eq!(parsed.id, r"C:\Program Files\TestApp\test.exe");
    }

    #[test]
    fn rejects_missing_kind() {
        assert!(parse_icon_uri("kaitu-icon:///com.apple.Safari").is_none());
    }

    #[test]
    fn rejects_missing_id() {
        assert!(parse_icon_uri("kaitu-icon://bundle").is_none());
        assert!(parse_icon_uri("kaitu-icon://bundle/").is_none());
    }

    #[test]
    fn rejects_wrong_scheme() {
        assert!(parse_icon_uri("https://bundle/foo").is_none());
        assert!(parse_icon_uri("kaitu-icon:/bundle/foo").is_none());
    }

    #[test]
    fn empty_404_has_no_body() {
        let resp = empty_404();
        assert_eq!(resp.status(), 404);
        assert!(resp.body().is_empty());
    }
}
