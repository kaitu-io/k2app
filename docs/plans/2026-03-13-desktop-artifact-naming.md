# Desktop Artifact Naming Standardization

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Align all desktop build artifact filenames to the v0.3.22 convention: `Kaitu_{VERSION}_{ARCH}.{EXT}`

**Architecture:** Tauri generates default filenames that don't match our convention. The fix is to rename artifacts in build scripts after Tauri generates them. The web download page (`constants.ts`) already uses the correct convention — only the build pipeline needs fixing.

**Tech Stack:** Bash scripts, GitHub Actions YAML, Tauri config (JSON), Next.js constants

---

### Naming Convention (Single Source of Truth)

```
kaitu/desktop/{VERSION}/
  Kaitu_{VERSION}_universal.pkg              # macOS installer
  Kaitu_{VERSION}_universal.app.tar.gz       # macOS auto-update
  Kaitu_{VERSION}_universal.app.tar.gz.sig   # macOS signature
  Kaitu_{VERSION}_x64.exe                    # Windows installer
  Kaitu_{VERSION}_x64.exe.sig               # Windows signature
  cloudfront.latest.json                     # Update manifest (CloudFront CDN)
  d0.latest.json                             # Update manifest (D0 CDN)
```

Rule: `Kaitu_{VERSION}_{ARCH}.{EXT}` — underscore separated, no `-setup`, no hyphens in `Kaitu-VERSION`.

---

### Task 1: Fix macOS PKG naming in build-macos.sh

**Files:**
- Modify: `scripts/build-macos.sh:310-311` (PKG_UNSIGNED, PKG_SIGNED)

**Step 1: Change PKG filename variables**

In `scripts/build-macos.sh`, change lines 310-311 from:
```bash
PKG_UNSIGNED="$RELEASE_DIR/Kaitu-${VERSION}-unsigned.pkg"
PKG_SIGNED="$RELEASE_DIR/Kaitu-${VERSION}.pkg"
```
to:
```bash
PKG_UNSIGNED="$RELEASE_DIR/Kaitu_${VERSION}_universal-unsigned.pkg"
PKG_SIGNED="$RELEASE_DIR/Kaitu_${VERSION}_universal.pkg"
```

**Step 2: Verify no other references to old PKG name in the script**

Run: `grep -n 'Kaitu-' scripts/build-macos.sh`
Expected: No matches (lines 310-311 were the only ones).

---

### Task 2: Fix macOS tar.gz naming in build-macos.sh

**Files:**
- Modify: `scripts/build-macos.sh:390-400` (artifact collection)

**Step 3: Add rename step after copying tar.gz and sig**

Replace lines 390-400 (artifact collection section) from:
```bash
APP_TAR_GZ=$(find "$BUNDLE_DIR" -name '*.app.tar.gz' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_TAR_GZ" ]; then
  cp "$APP_TAR_GZ" "$RELEASE_DIR/"
  echo "Copied: $(basename "$APP_TAR_GZ")"
fi

APP_SIG=$(find "$BUNDLE_DIR" -name '*.app.tar.gz.sig' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_SIG" ]; then
  cp "$APP_SIG" "$RELEASE_DIR/"
  echo "Copied: $(basename "$APP_SIG")"
fi
```
to:
```bash
# Tauri generates Kaitu.app.tar.gz — rename to Kaitu_{VERSION}_universal.app.tar.gz
APP_TAR_GZ=$(find "$BUNDLE_DIR" -name '*.app.tar.gz' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_TAR_GZ" ]; then
  cp "$APP_TAR_GZ" "$RELEASE_DIR/Kaitu_${VERSION}_universal.app.tar.gz"
  echo "Renamed: $(basename "$APP_TAR_GZ") → Kaitu_${VERSION}_universal.app.tar.gz"
fi

APP_SIG=$(find "$BUNDLE_DIR" -name '*.app.tar.gz.sig' -maxdepth 1 2>/dev/null | head -1)
if [ -n "$APP_SIG" ]; then
  cp "$APP_SIG" "$RELEASE_DIR/Kaitu_${VERSION}_universal.app.tar.gz.sig"
  echo "Renamed: $(basename "$APP_SIG") → Kaitu_${VERSION}_universal.app.tar.gz.sig"
fi
```

---

### Task 3: Fix Windows EXE naming in Makefile

**Files:**
- Modify: `Makefile:74-75` (Windows artifact collection)

**Step 4: Rename Windows artifacts during collection**

Change lines 74-75 from:
```makefile
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe release/$(VERSION)/
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe.sig release/$(VERSION)/ 2>/dev/null || true
```
to:
```makefile
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe release/$(VERSION)/Kaitu_$(VERSION)_x64.exe
	@cp desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis/Kaitu_$(VERSION)_x64-setup.exe.sig release/$(VERSION)/Kaitu_$(VERSION)_x64.exe.sig 2>/dev/null || true
```

---

### Task 4: Fix publish-desktop.sh (latest.json generation + sig glob)

**Files:**
- Modify: `scripts/publish-desktop.sh:68,88-101,115-129`

**Step 5: Fix Windows sig file glob**

Change line 68 from:
```bash
WINDOWS_SIG=$(cat "${TMPDIR}"/*_x64-setup.exe.sig 2>/dev/null || echo "")
```
to:
```bash
WINDOWS_SIG=$(cat "${TMPDIR}"/*_x64.exe.sig 2>/dev/null || echo "")
```

**Step 6: Fix cloudfront.latest.json URLs**

Change the macOS URLs (lines 88-98) from `Kaitu.app.tar.gz` to `Kaitu_${VERSION}_universal.app.tar.gz`, and Windows URL (line 100) from `Kaitu_${VERSION}_x64-setup.exe` to `Kaitu_${VERSION}_x64.exe`:

```bash
cat > "${TMPDIR}/cloudfront.latest.json" << EOF
{
  "version": "${VERSION}",
  "notes": "See https://github.com/${REPO}/releases/tag/v${VERSION}",
  "pub_date": "${PUB_DATE}",
  "platforms": {
    "darwin-aarch64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "darwin-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "darwin-universal": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_universal.app.tar.gz",
      "signature": "${MACOS_SIG}"
    },
    "windows-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}/Kaitu_${VERSION}_x64.exe",
      "signature": "${WINDOWS_SIG}"
    }
  }
}
EOF
```

**Step 7: Fix d0.latest.json URLs (same pattern)**

Apply same changes to d0.latest.json block (lines 108-132) — replace `Kaitu.app.tar.gz` → `Kaitu_${VERSION}_universal.app.tar.gz` and `_x64-setup.exe` → `_x64.exe`, using `d0.all7.cc` domain.

---

### Task 5: Fix release-desktop.yml Slack notification

**Files:**
- Modify: `.github/workflows/release-desktop.yml:351-353`

**Step 8: Update Slack download URL patterns**

Change lines 351-353 from:
```bash
          if [ "${PLATFORM}" = "macOS" ]; then
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu-${VERSION}.pkg|macOS PKG>"
          else
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_x64-setup.exe|Windows Setup>"
```
to:
```bash
          if [ "${PLATFORM}" = "macOS" ]; then
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_universal.pkg|macOS PKG>"
          else
            DOWNLOAD_URL="<${CDN_BASE}/Kaitu_${VERSION}_x64.exe|Windows Setup>"
```

---

### Task 6: Fix changelog.json beta.1 entry

**Files:**
- Modify: `web/public/changelog.json`

**Step 9: Fix macOS URL in 0.4.0-beta.1 entry**

Change:
```json
"macos": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.4.0-beta.1/Kaitu-0.4.0-beta.1.pkg"
"macosBackup": "https://d0.all7.cc/kaitu/desktop/0.4.0-beta.1/Kaitu-0.4.0-beta.1.pkg"
```
to:
```json
"macos": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.4.0-beta.1/Kaitu_0.4.0-beta.1_universal.pkg"
"macosBackup": "https://d0.all7.cc/kaitu/desktop/0.4.0-beta.1/Kaitu_0.4.0-beta.1_universal.pkg"
```

> **Note:** The S3 file is `Kaitu-0.4.0-beta.1.pkg` (old name). Either rename on S3 or keep the old URL for this historical entry. Recommend keeping old URL since beta.1 is already superseded.

---

### Task 7: Document naming convention in CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (Key Conventions section)

**Step 10: Add naming convention entry**

Add to Key Conventions:
```
- **Desktop artifact naming**: `Kaitu_{VERSION}_{ARCH}.{EXT}` — underscore-separated. macOS: `_universal.pkg` / `_universal.app.tar.gz` / `.sig`. Windows: `_x64.exe` / `.sig`. S3 path: `kaitu/desktop/{VERSION}/`. Never use hyphen separator (`Kaitu-`) or `-setup` suffix.
```

---

### Task 8: Verify (dry run)

**Step 11: Run build-macos.sh with --single-arch --skip-notarization to verify filenames**

Run: `make build-macos-test`

Check: `ls release/*/` should show:
- `Kaitu_{VERSION}_universal.pkg` (or arch-specific in single-arch mode)
- `Kaitu_{VERSION}_universal.app.tar.gz`
- `Kaitu_{VERSION}_universal.app.tar.gz.sig`

**Step 12: Commit all changes**

```bash
git add scripts/build-macos.sh scripts/publish-desktop.sh Makefile \
  .github/workflows/release-desktop.yml CLAUDE.md
git commit -m "fix: standardize desktop artifact naming to Kaitu_{VERSION}_{ARCH}.{EXT}

Align build output filenames with v0.3.22 convention and web/constants.ts expectations:
- macOS PKG: Kaitu-{ver}.pkg → Kaitu_{ver}_universal.pkg
- macOS tar.gz: Kaitu.app.tar.gz → Kaitu_{ver}_universal.app.tar.gz
- Windows EXE: Kaitu_{ver}_x64-setup.exe → Kaitu_{ver}_x64.exe
- Update latest.json URL generation to match
- Document naming convention in CLAUDE.md"
```

---

### Post-Deploy: Rename existing S3 files (manual, optional)

After the next release is built with correct names, consider renaming beta.1/beta.2/beta.3 S3 files for consistency:

```bash
# Example for beta.3 (run manually):
VER=0.4.0-beta.3
aws s3 cp "s3://d0.all7.cc/kaitu/desktop/${VER}/Kaitu-${VER}.pkg" \
          "s3://d0.all7.cc/kaitu/desktop/${VER}/Kaitu_${VER}_universal.pkg"
aws s3 cp "s3://d0.all7.cc/kaitu/desktop/${VER}/Kaitu.app.tar.gz" \
          "s3://d0.all7.cc/kaitu/desktop/${VER}/Kaitu_${VER}_universal.app.tar.gz"
# etc.
```

Not critical since these versions are already published and superseded.
