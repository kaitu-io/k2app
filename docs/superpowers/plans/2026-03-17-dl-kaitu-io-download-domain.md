# dl.kaitu.io Download Domain Migration + Install Page Improvements

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate user-facing download URLs from CDN domains to `dl.kaitu.io`, create unified install script, and improve the `/install` page with Linux support and beta-first version logic.

**Architecture:** New CloudFront distribution fronting existing S3 bucket with `dl.kaitu.io` alternate domain. Website CDN constants updated as primary, old CloudFront kept as backup. Unified shell script at `web/public/i/k2` handles Linux/macOS install. Install page gets Linux platform card, beta priority, and CLI command display.

**Tech Stack:** AWS (ACM, CloudFront, Route53), Next.js (React), Shell scripting, i18n (next-intl)

**Spec:** `docs/superpowers/specs/2026-03-17-dl-kaitu-io-download-domain.md`

---

## File Map

### Create
- `web/public/i/k2` — Unified desktop client install script (Linux AppImage + macOS PKG)

### Modify
- `web/src/lib/constants.ts` — CDN_PRIMARY/BACKUP + getDownloadLinks() add Linux
- `web/src/app/[locale]/install/InstallClient.tsx` — Version logic rewrite, Linux platform, CLI command
- `web/src/app/[locale]/install/page.tsx` — Pass betaVersion prop properly
- `web/src/app/[locale]/support/page.tsx` — PDF guide URLs
- `web/public/i/k2s` — CDN URLs
- `web/messages/zh-CN/install.json` — New i18n keys (Linux, CLI, beta badge)
- `web/messages/en-US/install.json` — Same
- `scripts/generate-changelog.js` — CDN URLs
- `.github/workflows/release-desktop.yml` — Slack notification URL
- `.github/workflows/build-mobile.yml` — Slack notification URL

### Delete
- `web/public/install.sh` — Replaced by `i/k2` and `i/k2s`
- `scripts/install-linux.sh` — Logic merged into `web/public/i/k2`

---

## Task 1: AWS Infrastructure — CloudFront Distribution for dl.kaitu.io

**Files:** None (AWS CLI operations)

**Dependency:** Must complete before code changes can be tested.

- [ ] **Step 1: Request ACM certificate for dl.kaitu.io**

```bash
aws acm request-certificate \
  --domain-name dl.kaitu.io \
  --validation-method DNS \
  --region us-east-1 \
  --output json
```

Save the `CertificateArn` from output.

- [ ] **Step 2: Get DNS validation record**

```bash
aws acm describe-certificate \
  --certificate-arn <CERT_ARN> \
  --region us-east-1 \
  --query "Certificate.DomainValidationOptions[0].ResourceRecord" \
  --output json
```

- [ ] **Step 3: Create DNS validation CNAME in Route53**

```bash
aws route53 change-resource-record-sets \
  --hosted-zone-id Z0765916OB6BGV85HULS \
  --change-batch '{
    "Changes": [{
      "Action": "UPSERT",
      "ResourceRecordSet": {
        "Name": "<VALIDATION_CNAME_NAME>",
        "Type": "CNAME",
        "TTL": 300,
        "ResourceRecords": [{"Value": "<VALIDATION_CNAME_VALUE>"}]
      }
    }]
  }'
```

- [ ] **Step 4: Wait for certificate validation**

```bash
aws acm wait certificate-validated \
  --certificate-arn <CERT_ARN> \
  --region us-east-1
```

Expected: Command returns after certificate is issued (may take 1-5 minutes).

- [ ] **Step 5: Create CloudFront distribution**

Replicate existing distribution `E3W144CRNT652P` config:
- Origin: `d0.all7.cc.s3.ap-northeast-1.amazonaws.com`
- OAC: `E1O5EA08J84DTW` (same OAC, same bucket)
- CachePolicyId: `658327ea-f89d-4fab-a63d-7e88639e58f6` (CachingOptimized)
- Compress: true
- ViewerProtocolPolicy: redirect-to-https
- AllowedMethods: GET, HEAD
- PriceClass: PriceClass_All
- HttpVersion: http2
- Alternate domain: `dl.kaitu.io`
- ACM cert: the newly created certificate

```bash
aws cloudfront create-distribution --distribution-config '{
  "CallerReference": "dl-kaitu-io-2026-03-17",
  "Comment": "dl.kaitu.io - official download domain",
  "Enabled": true,
  "HttpVersion": "http2",
  "PriceClass": "PriceClass_All",
  "DefaultRootObject": "",
  "Origins": {
    "Quantity": 1,
    "Items": [{
      "Id": "S3-d0-all7-cc",
      "DomainName": "d0.all7.cc.s3.ap-northeast-1.amazonaws.com",
      "OriginAccessControlId": "E1O5EA08J84DTW",
      "S3OriginConfig": { "OriginAccessIdentity": "" }
    }]
  },
  "DefaultCacheBehavior": {
    "TargetOriginId": "S3-d0-all7-cc",
    "ViewerProtocolPolicy": "redirect-to-https",
    "CachePolicyId": "658327ea-f89d-4fab-a63d-7e88639e58f6",
    "Compress": true,
    "AllowedMethods": {
      "Quantity": 2,
      "Items": ["HEAD", "GET"],
      "CachedMethods": { "Quantity": 2, "Items": ["HEAD", "GET"] }
    }
  },
  "Aliases": {
    "Quantity": 1,
    "Items": ["dl.kaitu.io"]
  },
  "ViewerCertificate": {
    "ACMCertificateArn": "<CERT_ARN>",
    "SSLSupportMethod": "sni-only",
    "MinimumProtocolVersion": "TLSv1.2_2021"
  },
  "Restrictions": {
    "GeoRestriction": { "RestrictionType": "none", "Quantity": 0 }
  }
}'
```

Save `Distribution.DomainName` (e.g., `dXXXXXXX.cloudfront.net`) from output.

- [ ] **Step 6: Create DNS CNAME for dl.kaitu.io**

```bash
aws route53 change-resource-record-sets \
  --hosted-zone-id Z0765916OB6BGV85HULS \
  --change-batch '{
    "Changes": [{
      "Action": "UPSERT",
      "ResourceRecordSet": {
        "Name": "dl.kaitu.io",
        "Type": "CNAME",
        "TTL": 300,
        "ResourceRecords": [{"Value": "<DISTRIBUTION_DOMAIN_NAME>"}]
      }
    }]
  }'
```

- [ ] **Step 7: Verify dl.kaitu.io serves files**

```bash
# Wait for distribution to deploy (~5 min)
aws cloudfront wait distribution-deployed --id <DISTRIBUTION_ID>

# Test download
curl -sI "https://dl.kaitu.io/kaitu/desktop/cloudfront.latest.json" | head -5
```

Expected: HTTP 200 with JSON response.

- [ ] **Step 8: Commit infrastructure notes**

No code files changed — record the distribution ID and domain in a commit message for reference:

```bash
git commit --allow-empty -m "infra: dl.kaitu.io CloudFront distribution created

Distribution ID: <ID>
Domain: <DOMAIN>.cloudfront.net
ACM Cert: <CERT_ARN>
OAC: E1O5EA08J84DTW (shared with existing distribution)"
```

---

## Task 2: CDN URL Migration — constants, scripts, workflows

**Files:**
- Modify: `web/src/lib/constants.ts:7-8` (CDN_PRIMARY, CDN_BACKUP)
- Modify: `web/src/lib/constants.ts:10-21` (getDownloadLinks — add Linux)
- Modify: `web/src/app/[locale]/support/page.tsx` (PDF guide URLs, search for `d13jc1jqzlg4yt`)
- Modify: `web/public/i/k2s:9-10` (CDN_PRIMARY, CDN_FALLBACK)
- Modify: `scripts/generate-changelog.js:18-19` (CDN_PRIMARY, CDN_BACKUP)
- Modify: `.github/workflows/release-desktop.yml:412` (CDN_BASE)
- Modify: `.github/workflows/build-mobile.yml:337` (CDN_BASE)
- Delete: `web/public/install.sh`
- Delete: `scripts/install-linux.sh`

- [ ] **Step 1: Update `web/src/lib/constants.ts`**

```typescript
// Line 7-8: Change CDN domains
export const CDN_PRIMARY = 'https://dl.kaitu.io/kaitu/desktop';
export const CDN_BACKUP = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';

// Line 10-21: Add linux to getDownloadLinks()
export function getDownloadLinks(version: string) {
  return {
    windows: {
      primary: `${CDN_PRIMARY}/${version}/Kaitu_${version}_x64.exe`,
      backup: `${CDN_BACKUP}/${version}/Kaitu_${version}_x64.exe`,
    },
    macos: {
      primary: `${CDN_PRIMARY}/${version}/Kaitu_${version}_universal.pkg`,
      backup: `${CDN_BACKUP}/${version}/Kaitu_${version}_universal.pkg`,
    },
    linux: {
      primary: `${CDN_PRIMARY}/${version}/Kaitu_${version}_amd64.AppImage`,
      backup: `${CDN_BACKUP}/${version}/Kaitu_${version}_amd64.AppImage`,
    },
  };
}
```

- [ ] **Step 2: Update `web/src/app/[locale]/support/page.tsx`**

Search for `d13jc1jqzlg4yt.cloudfront.net` and replace with `dl.kaitu.io` in PDF guide URLs.

- [ ] **Step 3: Update `web/public/i/k2s` lines 9-10**

```bash
CDN_PRIMARY="https://dl.kaitu.io/kaitu/k2"
CDN_FALLBACK="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
```

- [ ] **Step 4: Update `scripts/generate-changelog.js` lines 18-19**

```javascript
const CDN_PRIMARY = 'https://dl.kaitu.io/kaitu/desktop';
const CDN_BACKUP = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';
```

- [ ] **Step 5: Update `.github/workflows/release-desktop.yml` line 412**

```yaml
CDN_BASE="https://dl.kaitu.io/kaitu/desktop/${VERSION}"
```

- [ ] **Step 6: Update `.github/workflows/build-mobile.yml` line 337**

```yaml
CDN_BASE="https://dl.kaitu.io/kaitu/android"
```

- [ ] **Step 7: Delete obsolete files**

```bash
rm web/public/install.sh
rm scripts/install-linux.sh
```

- [ ] **Step 8: Regenerate changelog**

```bash
cd web && node ../scripts/generate-changelog.js
```

Verify `web/public/changelog.json` now contains `dl.kaitu.io` URLs instead of old CDN.

- [ ] **Step 9: Commit**

```bash
git add web/src/lib/constants.ts web/src/app/*/support/page.tsx \
  web/public/i/k2s scripts/generate-changelog.js web/public/changelog.json \
  .github/workflows/release-desktop.yml .github/workflows/build-mobile.yml
git rm web/public/install.sh scripts/install-linux.sh
git commit -m "feat: migrate download URLs to dl.kaitu.io

CDN_PRIMARY → dl.kaitu.io, CDN_BACKUP → d13jc1jqzlg4yt.cloudfront.net.
Add Linux to getDownloadLinks(). Delete obsolete install.sh and
install-linux.sh. Update CI Slack notification URLs."
```

---

## Task 3: Unified Install Script `web/public/i/k2`

**Files:**
- Create: `web/public/i/k2`
- Reference: `scripts/install-linux.sh` (source for Linux logic, being deleted in Task 2)
- Reference: `web/public/i/k2s` (pattern reference for CDN fallback, checksum, etc.)

- [ ] **Step 1: Create `web/public/i/k2`**

Merge Linux logic from `scripts/install-linux.sh` + new macOS PKG logic. Follow `i/k2s` patterns for `fetch()`, `download()`, `sha256_cmd()`, CDN fallback.

```bash
#!/bin/sh
# Kaitu Desktop Client Installer
# Linux: AppImage + k2 daemon + systemd service
# macOS: PKG installer
#
# Usage: curl -fsSL https://kaitu.io/i/k2 | sudo bash
set -e

CDN_PRIMARY="https://dl.kaitu.io/kaitu"
CDN_FALLBACK="https://d13jc1jqzlg4yt.cloudfront.net/kaitu"
MANIFEST=""

# --- Helpers ---

info()  { echo "[kaitu] $*"; }
error() { echo "[kaitu] ERROR: $*" >&2; exit 1; }

fetch() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "$url"
    else
        echo "Error: curl or wget required" >&2
        exit 1
    fi
}

download() {
    url="$1"; dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fSL -o "$dest" "$url"
    else
        wget -qO "$dest" "$url"
    fi
}

download_with_fallback() {
    dest="$1"; primary="$2"; fallback="$3"
    if ! download "$primary" "$dest" 2>/dev/null; then
        if ! download "$fallback" "$dest" 2>/dev/null; then
            error "Failed to download from both CDN endpoints"
        fi
    fi
}

detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"
    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)      error "Unsupported OS: $OS. Use https://kaitu.io/install for Windows." ;;
    esac
    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)             error "Unsupported architecture: $ARCH" ;;
    esac
}

get_latest_version() {
    local channel="beta"
    for arg in "$@"; do
        case "$arg" in
            --channel=*) channel="${arg#*=}" ;;
        esac
    done

    local manifest_path="desktop"
    if [ "$channel" = "beta" ]; then
        manifest_path="desktop/beta"
    fi

    MANIFEST=$(fetch "${CDN_PRIMARY}/${manifest_path}/cloudfront.latest.json" 2>/dev/null || true)
    VERSION=$(echo "$MANIFEST" | grep '"version"' | head -1 | sed 's/.*"version" *: *"//;s/".*//' || true)

    if [ -z "$VERSION" ]; then
        MANIFEST=$(fetch "${CDN_FALLBACK}/${manifest_path}/cloudfront.latest.json" 2>/dev/null || true)
        VERSION=$(echo "$MANIFEST" | grep '"version"' | head -1 | sed 's/.*"version" *: *"//;s/".*//' || true)
    fi

    if [ -z "$VERSION" ]; then
        error "Could not determine latest version"
    fi
}

# --- Linux ---

check_linux_deps() {
    # webkit2gtk-4.1
    if ! (ldconfig -p 2>/dev/null | grep -q "libwebkit2gtk-4.1") && \
       ! (command -v pkg-config >/dev/null 2>&1 && pkg-config --exists webkit2gtk-4.1 2>/dev/null); then
        echo ""
        echo "webkit2gtk-4.1 is required but not installed."
        echo ""
        echo "Install it for your distribution:"
        echo "  Ubuntu/Debian:  sudo apt install libwebkit2gtk-4.1-0"
        echo "  Fedora:         sudo dnf install webkit2gtk4.1"
        echo "  Arch:           sudo pacman -S webkit2gtk-4.1"
        echo "  openSUSE:       sudo zypper install webkit2gtk-4.1"
        echo ""
        error "Install webkit2gtk-4.1 and re-run this script."
    fi

    # libfuse2
    if ! ldconfig -p 2>/dev/null | grep -q "libfuse.so.2"; then
        echo ""
        echo "libfuse2 is required to run AppImage but not installed."
        echo ""
        echo "Install it for your distribution:"
        echo "  Ubuntu 22.04:   sudo apt install libfuse2"
        echo "  Ubuntu 24.04:   sudo apt install libfuse2t64"
        echo "  Fedora:         sudo dnf install fuse-libs"
        echo "  Arch:           sudo pacman -S fuse2"
        echo ""
        error "Install libfuse2 and re-run this script."
    fi
}

install_linux() {
    if [ "$ARCH" != "amd64" ]; then
        error "Linux desktop currently only supports amd64."
    fi

    check_linux_deps

    INSTALL_DIR="/opt/kaitu"
    mkdir -p "$INSTALL_DIR"

    info "Downloading Kaitu AppImage v${VERSION}..."
    download_with_fallback "${INSTALL_DIR}/Kaitu.AppImage" \
        "${CDN_PRIMARY}/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage" \
        "${CDN_FALLBACK}/desktop/${VERSION}/Kaitu_${VERSION}_amd64.AppImage"
    chmod +x "${INSTALL_DIR}/Kaitu.AppImage"

    info "Downloading k2 daemon..."
    download_with_fallback "${INSTALL_DIR}/k2" \
        "${CDN_PRIMARY}/desktop/${VERSION}/k2-linux-amd64" \
        "${CDN_FALLBACK}/desktop/${VERSION}/k2-linux-amd64"
    chmod +x "${INSTALL_DIR}/k2"

    ln -sf "${INSTALL_DIR}/k2" /usr/local/bin/k2
    info "k2 available at /usr/local/bin/k2"

    info "Installing k2 systemd service..."
    "${INSTALL_DIR}/k2" service install

    # Desktop entry
    REAL_USER="${SUDO_USER:-$(whoami)}"
    REAL_HOME=$(eval echo "~${REAL_USER}")
    DESKTOP_DIR="${REAL_HOME}/.local/share/applications"
    mkdir -p "$DESKTOP_DIR"

    cat > "${DESKTOP_DIR}/kaitu.desktop" << ENTRY
[Desktop Entry]
Name=Kaitu
Comment=Kaitu VPN
Exec=${INSTALL_DIR}/Kaitu.AppImage
Icon=${INSTALL_DIR}/kaitu.png
Type=Application
Categories=Network;VPN;
StartupWMClass=kaitu
ENTRY
    chown "${REAL_USER}:" "${DESKTOP_DIR}/kaitu.desktop"

    # Extract icon (best-effort)
    (
        cd /tmp
        "${INSTALL_DIR}/Kaitu.AppImage" --appimage-extract "*.png" 2>/dev/null || true
        ICON=$(find squashfs-root -name "*.png" -path "*/256x256/*" -print -quit 2>/dev/null)
        [ -z "$ICON" ] && ICON=$(find squashfs-root -maxdepth 1 -name "*.png" -print -quit 2>/dev/null)
        [ -n "$ICON" ] && cp "$ICON" "${INSTALL_DIR}/kaitu.png"
        rm -rf squashfs-root
    ) 2>/dev/null || true

    # Uninstaller
    cat > /usr/local/bin/kaitu-uninstall << 'UNINSTALL'
#!/usr/bin/env bash
set -euo pipefail
if [ "$(id -u)" -ne 0 ]; then echo "Run with sudo: sudo kaitu-uninstall"; exit 1; fi
echo "Uninstalling Kaitu..."
systemctl stop k2 2>/dev/null || true
systemctl disable k2 2>/dev/null || true
/opt/kaitu/k2 service uninstall 2>/dev/null || true
rm -rf /opt/kaitu
rm -f /usr/local/bin/k2 /usr/local/bin/kaitu-uninstall
for home_dir in /home/*/; do rm -f "${home_dir}.local/share/applications/kaitu.desktop" 2>/dev/null || true; done
rm -f /root/.local/share/applications/kaitu.desktop 2>/dev/null || true
if [ "${1:-}" = "--purge" ]; then
  for home_dir in /home/*/; do rm -rf "${home_dir}.local/share/kaitu" "${home_dir}.cache/k2" 2>/dev/null || true; done
  rm -rf /var/log/k2 2>/dev/null || true
  echo "Purged all data and logs."
fi
echo "Kaitu uninstalled."
UNINSTALL
    chmod +x /usr/local/bin/kaitu-uninstall

    info ""
    info "=== Installation complete ==="
    info "  GUI:       ${INSTALL_DIR}/Kaitu.AppImage"
    info "  CLI:       k2 (in PATH)"
    info "  Service:   systemctl status k2"
    info "  Uninstall: sudo kaitu-uninstall"
    info ""
    info "Launch Kaitu from your application menu or run:"
    info "  ${INSTALL_DIR}/Kaitu.AppImage"
}

# --- macOS ---

install_macos() {
    info "Downloading Kaitu PKG v${VERSION}..."
    TMP_PKG="/tmp/Kaitu_${VERSION}_universal.pkg"
    download_with_fallback "$TMP_PKG" \
        "${CDN_PRIMARY}/desktop/${VERSION}/Kaitu_${VERSION}_universal.pkg" \
        "${CDN_FALLBACK}/desktop/${VERSION}/Kaitu_${VERSION}_universal.pkg"

    info "Installing Kaitu..."
    installer -pkg "$TMP_PKG" -target /
    rm -f "$TMP_PKG"

    info ""
    info "=== Installation complete ==="
    info "  Launch Kaitu from Applications or Spotlight."
}

# --- Main ---

main() {
    if [ "$(id -u)" -ne 0 ]; then
        error "Run with sudo: curl -fsSL https://kaitu.io/i/k2 | sudo bash"
    fi

    detect_platform
    get_latest_version "$@"

    info "Installing Kaitu v${VERSION} (${OS}/${ARCH})..."

    case "$OS" in
        linux)  install_linux ;;
        darwin) install_macos ;;
    esac
}

main "$@"
```

- [ ] **Step 2: Verify script is valid shell**

```bash
bash -n web/public/i/k2
```

Expected: No output (no syntax errors).

- [ ] **Step 3: Commit**

```bash
git add web/public/i/k2
git commit -m "feat: create unified install script web/public/i/k2

Detects OS via uname: Linux gets AppImage + k2 daemon + systemd,
macOS gets PKG via installer command. CDN primary dl.kaitu.io
with d13jc1jqzlg4yt.cloudfront.net fallback.
Replaces scripts/install-linux.sh (deleted in previous commit)."
```

---

## Task 4: Install Page — i18n Keys

**Files:**
- Modify: `web/messages/zh-CN/install.json`
- Modify: `web/messages/en-US/install.json`
- Modify: all other locale files (`ja`, `zh-TW`, `zh-HK`, `en-AU`, `en-GB`) — copy from en-US

New keys needed:

| Key | zh-CN | en-US |
|-----|-------|-------|
| `install.linux` | `Linux` | `Linux` |
| `install.linuxVersion` | `Ubuntu / Fedora / Arch` | `Ubuntu / Fedora / Arch` |
| `install.downloadAppImage` | `Linux 桌面版` | `Linux Desktop` |
| `install.cliInstall` | `或通过命令行安装` | `Or install via command line` |
| `install.cliCommand` | `curl -fsSL https://kaitu.io/i/k2 \| sudo bash` | `curl -fsSL https://kaitu.io/i/k2 \| sudo bash` |
| `install.copied` | `已复制` | `Copied` |
| `install.beta` | `公测版` | `Beta` |
| `install.alsoAvailableStable` | `也可以下载稳定版 v{version}` | `Stable version v{version} also available` |
| `install.linuxCliRecommended` | `推荐使用命令行安装，自动检查依赖` | `CLI install recommended — auto-checks dependencies` |

- [ ] **Step 1: Add keys to `web/messages/zh-CN/install.json`**

Add the new keys inside the `"install"` object.

- [ ] **Step 2: Add keys to `web/messages/en-US/install.json`**

- [ ] **Step 3: Copy en-US keys to remaining 5 locales**

Files: `web/messages/ja/install.json`, `web/messages/zh-TW/install.json`, `web/messages/zh-HK/install.json`, `web/messages/en-AU/install.json`, `web/messages/en-GB/install.json`

- [ ] **Step 4: Commit**

```bash
git add web/messages/*/install.json
git commit -m "i18n: add Linux platform and CLI install keys to install namespace"
```

---

## Task 5: Install Page — InstallClient.tsx Rewrite

**Files:**
- Modify: `web/src/app/[locale]/install/InstallClient.tsx`
- Modify: `web/src/app/[locale]/install/page.tsx` (ensure betaVersion prop passed)

- [ ] **Step 1: Update `page.tsx` — pass both versions**

Verify `page.tsx` already passes `betaVersion` and `stableVersion` to `<InstallClient>`. Currently it does (line 70). No change needed unless the prop handling is wrong.

- [ ] **Step 2: Rewrite version logic in `InstallClient.tsx`**

Replace lines 106-116 (the `showBetaAndStable` hack):

```typescript
export default function InstallClient({ betaVersion, stableVersion: serverStable }: InstallClientProps) {
  const t = useTranslations();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [downloadState, setDownloadState] = useState<DownloadState>('detecting');
  const [countdown, setCountdown] = useState(5);
  const [copied, setCopied] = useState(false);

  // Single source of truth: CDN manifest. No build-time fallback.
  // ISR guarantees at least one successful fetch is cached. If both are null,
  // the page will error — deploy and visit once to prime the cache.
  const displayVersion = betaVersion || serverStable!;
  const isBeta = !!(betaVersion && betaVersion !== serverStable);
  const downloadLinks = getDownloadLinks(displayVersion);
  const stableDownloadLinks = isBeta && serverStable ? getDownloadLinks(serverStable) : null;
```

Delete `showBetaAndStable`, `betaLinks`, `stableLinks`, `effectiveStable` variables entirely.
Remove `DESKTOP_VERSION` import from constants (no longer used by this file).

- [ ] **Step 3: Update `getPrimaryLink` to handle Linux**

```typescript
const getPrimaryLink = useCallback((deviceInfo: DeviceInfo | null) => {
  if (!deviceInfo) return null;
  switch (deviceInfo.type) {
    case 'windows': return downloadLinks.windows.primary;
    case 'macos': return downloadLinks.macos.primary;
    case 'linux': return downloadLinks.linux.primary;
    default: return null;
  }
}, [downloadLinks]);
```

- [ ] **Step 4: Update auto-download logic for Linux**

Linux detected as desktop but should NOT auto-download. Modify the `useEffect` that sets initial downloadState:

```typescript
useEffect(() => {
  const deviceInfo = detectDevice();
  setDevice(deviceInfo);
  if (deviceInfo.type === 'linux') {
    // Linux: show CLI command instead of auto-download
    setDownloadState('cancelled');
  } else if (deviceInfo.isDesktop) {
    setDownloadState('ready');
  } else {
    setDownloadState('unavailable');
  }
}, []);
```

- [ ] **Step 5: Add CLI command copy helper**

```typescript
const copyCliCommand = useCallback(async () => {
  try {
    await navigator.clipboard.writeText('curl -fsSL https://kaitu.io/i/k2 | sudo bash');
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  } catch {
    // Fallback: select text
  }
}, []);
```

- [ ] **Step 6: Update Hero card for beta badge**

In the hero section, after the version label, add beta badge:

```tsx
<p className="text-sm text-muted-foreground mb-2">
  {t('install.install.latestVersion', { version: displayVersion })}
  {isBeta && (
    <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-primary/15 text-primary">
      {t('install.install.beta')}
    </span>
  )}
</p>
```

- [ ] **Step 7: Update Hero for Linux — show CLI command instead of countdown**

Add a new state card for Linux detected users (after the `cancelled` card):

```tsx
{/* Linux: CLI install as primary */}
{device?.type === 'linux' && (
  <Card className="p-8 mb-8">
    <div className="text-center">
      <h3 className="text-lg font-semibold text-foreground mb-2">
        {t('install.install.cliInstall')}
      </h3>
      <p className="text-xs text-muted-foreground mb-3">
        {t('install.install.linuxCliRecommended')}
      </p>
      <div className="flex items-center justify-center gap-2 bg-muted rounded-lg px-4 py-3 mb-4 font-mono text-sm">
        <code>curl -fsSL https://kaitu.io/i/k2 | sudo bash</code>
        <Button variant="ghost" size="sm" onClick={copyCliCommand}>
          {copied ? <CheckCircle className="w-4 h-4 text-success" /> : <Copy className="w-4 h-4" />}
        </Button>
      </div>
      <Button variant="outline" onClick={startDownload}>
        <Download className="w-4 h-4 mr-2" />
        {t('install.install.downloadAppImage')}
      </Button>
    </div>
  </Card>
)}
```

Add `Copy` to lucide-react imports.

- [ ] **Step 8: Add Linux platform card to grid**

Insert between macOS and iOS cards:

```tsx
{/* Linux */}
<PlatformCard
  platform="linux"
  name={t('install.install.linux')}
  subtitle={t('install.install.linuxVersion')}
  isDetected={device?.type === 'linux'}
>
  <DownloadButton
    href={downloadLinks.linux.primary}
    label={`v${displayVersion}`}
  />
  <div className="mt-2 text-[10px] text-muted-foreground">
    <p>{t('install.install.cliInstall')}</p>
    <code className="block mt-1 bg-muted rounded px-2 py-1 text-[9px] font-mono break-all">
      curl -fsSL https://kaitu.io/i/k2 | sudo bash
    </code>
  </div>
</PlatformCard>
```

Add a `linux` entry to `platformIcons`:

```typescript
linux: ({ className }) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48" fill="currentColor" className={className}>
    <path d="M24 2C17.4 2 12 7.4 12 14v6c-2.2 1.6-4 4.2-4 7.2 0 2.8 1.2 5 3.2 6.6C12.8 38 16 42 20 44h8c4-2 7.2-6 8.8-10.2C38.8 32.2 40 30 40 27.2c0-3-1.8-5.6-4-7.2v-6C36 7.4 30.6 2 24 2zm-4 14c0-2.2 1.8-4 4-4s4 1.8 4 4v2h-8v-2zm-4 12a2 2 0 110-4 2 2 0 010 4zm16 0a2 2 0 110-4 2 2 0 010 4zm-8 8c-2.2 0-4-1.8-4-4h8c0 2.2-1.8 4-4 4z"/>
  </svg>
),
```

- [ ] **Step 9: Add CLI command to macOS card**

After the macOS download button, add:

```tsx
<div className="mt-2 text-[10px] text-muted-foreground">
  <p>{t('install.install.cliInstall')}</p>
  <code className="block mt-1 bg-muted rounded px-2 py-1 text-[9px] font-mono break-all">
    curl -fsSL https://kaitu.io/i/k2 | sudo bash
  </code>
</div>
```

- [ ] **Step 10: Update grid responsive classes**

Change grid from 4 columns to 5:

```tsx
<div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4 mb-8 items-stretch">
```

- [ ] **Step 11: Add stable version fallback links**

After the platform grid, add stable version links when displaying beta:

```tsx
{/* Stable version alternative */}
{isBeta && stableDownloadLinks && (
  <p className="text-xs text-muted-foreground text-center mt-2">
    {t('install.install.alsoAvailableStable', { version: effectiveStable })}
    {': '}
    <a href={stableDownloadLinks.windows.primary} target="_blank" rel="noopener noreferrer"
       className="hover:text-foreground hover:underline">Windows</a>
    {' · '}
    <a href={stableDownloadLinks.macos.primary} target="_blank" rel="noopener noreferrer"
       className="hover:text-foreground hover:underline">macOS</a>
    {' · '}
    <a href={stableDownloadLinks.linux.primary} target="_blank" rel="noopener noreferrer"
       className="hover:text-foreground hover:underline">Linux</a>
  </p>
)}
```

- [ ] **Step 12: Update backup download section**

Update the backup download section at bottom to include Linux and use new variable names:

```tsx
<p className="text-xs text-muted-foreground">
  {t('install.install.backupDownload')}
  {': '}
  <a href={downloadLinks.windows.backup} target="_blank" rel="noopener noreferrer"
     className="hover:text-foreground hover:underline">Windows</a>
  {' · '}
  <a href={downloadLinks.macos.backup} target="_blank" rel="noopener noreferrer"
     className="hover:text-foreground hover:underline">macOS</a>
  {' · '}
  <a href={downloadLinks.linux.backup} target="_blank" rel="noopener noreferrer"
     className="hover:text-foreground hover:underline">Linux</a>
</p>
```

Remove the old `showBetaAndStable && stableLinks` conditional block.

- [ ] **Step 13: Build and verify**

```bash
cd web && yarn build
```

Expected: Build succeeds with no TypeScript errors.

- [ ] **Step 14: Commit**

```bash
git add web/src/app/*/install/InstallClient.tsx web/src/app/*/install/page.tsx
git commit -m "feat(install): beta-first version logic, Linux platform, CLI command

- Beta version shown as primary when available, with badge
- Stable version as subtle fallback links
- Linux platform card with AppImage download + CLI command
- Linux hero shows CLI command instead of auto-download countdown
- macOS card also shows CLI install command
- 5-column responsive grid
- Remove showBetaAndStable hack and dead code"
```

---

## Task 6: Final Verification

- [ ] **Step 1: Verify dl.kaitu.io serves all expected paths**

```bash
curl -sI "https://dl.kaitu.io/kaitu/desktop/cloudfront.latest.json" | head -3
curl -sI "https://dl.kaitu.io/kaitu/desktop/beta/cloudfront.latest.json" | head -3
curl -sI "https://dl.kaitu.io/kaitu/k2/k2s-cloudfront.latest.json" | head -3
curl -sI "https://dl.kaitu.io/kaitu/guides/mac-guide.pdf" | head -3
```

Expected: All return HTTP 200.

- [ ] **Step 2: Verify web build passes**

```bash
cd web && yarn build && yarn lint
```

- [ ] **Step 3: Run existing tests**

```bash
cd web && yarn test
```

- [ ] **Step 4: Verify install script syntax**

```bash
bash -n web/public/i/k2
shellcheck web/public/i/k2 2>/dev/null || true
```

- [ ] **Step 5: Final commit — update spec status**

No file changes if all passed. If any fixes were needed, commit them.
