#!/bin/sh
# install-k2r.sh — Install or upgrade k2r (gateway)
#
# Usage:
#   wget -qO- https://kaitu.io/i/k2r | sudo sh
#   wget -qO- https://kaitu.io/i/k2r | sudo sh -s <k2v5://URL>   # install + setup with server URL
#
# Installs k2r to /usr/bin, registers service (procd on OpenWrt, systemd on Linux),
# and optionally installs LuCI integration if /usr/lib/lua/luci is present.
set -e

INSTALL_DIR="/usr/bin"
BINARY="k2r"
GATEWAY_PORT="1779"

CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r"
CDN_FALLBACK="https://dl.kaitu.io/kaitu/k2r"

# --- helpers ---

log()  { printf '  %s\n' "$*"; }
die()  { printf 'Error: %s\n' "$*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "run with sudo: wget -qO- https://kaitu.io/i/k2r | sudo sh"
}

detect_platform() {
    # Explicit ranges, not [:upper:]/[:lower:]: OpenWrt's busybox tr is built
    # without character classes and turns "Linux" into "Linlx" (25.12 x86_64).
    # shellcheck disable=SC2018,SC2019
    OS="$(uname -s | tr 'A-Z' 'a-z')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux) ;;
        *) die "k2r is Linux only (got: $OS)" ;;
    esac

    case "$ARCH" in
        x86_64|amd64)   ARCH="amd64" ;;
        aarch64|arm64)  ARCH="arm64" ;;
        armv7l)         ARCH="armv7" ;;
        mips*)
            # uname -m can't tell MIPS endianness; read the ELF EI_DATA byte
            # (offset 5) of a known local binary. 0x01 = little-endian (mipsle),
            # 0x02 = big-endian (mips). Uses only head/tail (busybox lacks od on
            # minimal images like GL.iNet ath79).
            _elf="/bin/busybox"; [ -r "$_elf" ] || _elf="/bin/sh"
            if [ "$(head -c6 "$_elf" 2>/dev/null | tail -c1)" = "$(printf '\001')" ]; then
                ARCH="mipsle"
            else
                ARCH="mips"
            fi
            ;;
        *)              die "unsupported architecture: $ARCH" ;;
    esac
}

fetch() {
    url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- --timeout=10 "$url"
    else
        die "curl or wget required"
    fi
}

download() {
    url="$1"; dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --connect-timeout 10 -o "$dest" "$url"
    else
        wget -qO "$dest" --timeout=10 "$url"
    fi
}

# fetch_cdn <path> — try primary CDN, fallback on failure
fetch_cdn() {
    path="$1"
    fetch "${CDN_PRIMARY}${path}" 2>/dev/null || fetch "${CDN_FALLBACK}${path}"
}

# download_cdn <path> <dest> — try primary CDN, fallback on failure
download_cdn() {
    path="$1"; dest="$2"
    download "${CDN_PRIMARY}${path}" "$dest" 2>/dev/null || download "${CDN_FALLBACK}${path}" "$dest"
}

# sha256_check <file> <expected_hash>
sha256_check() {
    file="$1"; expected="$2"
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "$file" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "$file" | awk '{print $1}')"
    else
        log "WARNING: no sha256sum or shasum found, skipping checksum verification"
        return 0
    fi
    [ "$actual" = "$expected" ] || die "checksum mismatch: expected $expected, got $actual"
}

installed_version() {
    if command -v "$BINARY" >/dev/null 2>&1; then
        # "k2r VERSION (COMMIT)" — extract second word. k2r has no -v flag
        # (it exits 1 with "Unknown flag"), so an upgrade used to look like a
        # fresh install every time.
        "$BINARY" version 2>/dev/null | awk '{print $2}' || echo ""
    else
        echo ""
    fi
}

is_service_running() {
    if command -v procd >/dev/null 2>&1 || [ -f /etc/openwrt_release ]; then
        # OpenWrt procd
        /etc/init.d/k2r status >/dev/null 2>&1
    elif command -v systemctl >/dev/null 2>&1; then
        systemctl is-active k2r >/dev/null 2>&1
    else
        return 1
    fi
}

restart_service() {
    log "Restarting service..."
    if command -v procd >/dev/null 2>&1 || [ -f /etc/openwrt_release ]; then
        /etc/init.d/k2r restart 2>/dev/null || true
    elif command -v systemctl >/dev/null 2>&1; then
        systemctl restart k2r 2>/dev/null || true
    fi
}

# install_luci — add an "Admin > Services > K2 VPN" page that embeds the
# gateway panel. Two LuCI generations: the ucode/JS one (OpenWrt 21.02+,
# menu.d JSON + JS view + rpcd ACL) and the legacy Lua one (<= 19.07). The
# iframe targets the router's own hostname as seen by the browser — a
# hard-coded 127.0.0.1 would point at the visitor's machine, not the router.
install_luci() {
    if [ -d /usr/share/luci/menu.d ] && [ -d /www/luci-static/resources ]; then
        install_luci_js
    elif [ -d /usr/lib/lua/luci ]; then
        install_luci_lua
    else
        return 0
    fi
    # rpcd only learns new acl.d groups on reload — until then no session
    # holds "luci-app-k2r" and the page 403s (an ACL group with empty
    # read/write scopes is never granted at all). Then clear LuCI's caches.
    /etc/init.d/rpcd reload 2>/dev/null || true
    rm -rf /tmp/luci-*
    log "LuCI integration installed (Admin > Services > K2 VPN)"
}

install_luci_js() {
    log "Installing LuCI integration (JS)..."
    mkdir -p /usr/share/luci/menu.d /usr/share/rpcd/acl.d /www/luci-static/resources/view
    cat > /usr/share/luci/menu.d/luci-app-k2r.json <<'EOF'
{
	"admin/services/k2r": {
		"title": "K2 VPN",
		"order": 90,
		"action": {
			"type": "view",
			"path": "k2r"
		},
		"depends": {
			"acl": [ "luci-app-k2r" ]
		}
	}
}
EOF
    cat > /usr/share/rpcd/acl.d/luci-app-k2r.json <<'EOF'
{
	"luci-app-k2r": {
		"description": "Grant access to the K2 VPN gateway page",
		"read": {
			"file": {
				"/www/luci-static/resources/view/k2r.js": [ "read" ]
			}
		}
	}
}
EOF
    cat > /www/luci-static/resources/view/k2r.js <<'EOF'
'use strict';
'require view';

return view.extend({
	handleSave: null,
	handleSaveApply: null,
	handleReset: null,

	render: function() {
		return E('iframe', {
			'src': 'http://' + window.location.hostname + ':1779/',
			'style': 'width:100%;height:calc(100vh - 160px);border:none;'
		});
	}
});
EOF
}

install_luci_lua() {
    LUCI_DIR="/usr/lib/lua/luci"
    log "Installing LuCI integration (Lua)..."

    # Controller
    CTRL_DIR="${LUCI_DIR}/controller"
    mkdir -p "$CTRL_DIR"
    cat > "${CTRL_DIR}/k2r.lua" <<'EOF'
module("luci.controller.k2r", package.seeall)

function index()
    entry({"admin", "services", "k2r"}, template("k2r"), _("K2 VPN"), 90)
end
EOF

    # View (iframe to gateway HTTP port)
    VIEW_DIR="${LUCI_DIR}/view"
    mkdir -p "$VIEW_DIR"
    cat > "${VIEW_DIR}/k2r.htm" <<'EOF'
<%+header%>
<iframe
  id="k2r-panel"
  style="width:100%;height:calc(100vh - 120px);border:none;"
  allowfullscreen
></iframe>
<script type="text/javascript">
document.getElementById('k2r-panel').src = 'http://' + window.location.hostname + ':1779/';
</script>
<%+footer%>
EOF
}

get_lan_ip() {
    if [ -f /etc/openwrt_release ] || command -v uci >/dev/null 2>&1; then
        uci get network.lan.ipaddr 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}'
    else
        hostname -I 2>/dev/null | awk '{print $1}'
    fi
}

# detect_pkg_mgr — sets PKG_MGR to one of: openwrt-opkg, openwrt-apk, apt,
# apk, dnf, pacman, "". OpenWrt is checked first: since 25.x its package
# manager is apk, and a plain "apk" branch would install Alpine's iptables
# (wrong package, and no TPROXY kernel module) instead of kmod-nft-tproxy.
detect_pkg_mgr() {
    if [ -f /etc/openwrt_release ] && command -v opkg >/dev/null 2>&1; then PKG_MGR=openwrt-opkg
    elif [ -f /etc/openwrt_release ] && command -v apk >/dev/null 2>&1; then PKG_MGR=openwrt-apk
    elif command -v opkg >/dev/null 2>&1; then PKG_MGR=openwrt-opkg
    elif command -v apt-get >/dev/null 2>&1; then PKG_MGR=apt
    elif command -v apk >/dev/null 2>&1; then PKG_MGR=apk
    elif command -v dnf >/dev/null 2>&1; then PKG_MGR=dnf
    elif command -v pacman >/dev/null 2>&1; then PKG_MGR=pacman
    else PKG_MGR=""
    fi
}

# iptables_tproxy_works — true when iptables has working TPROXY support.
# Tries a real (then removed) rule against the mangle table. Avoids printing
# spurious failures to stderr.
iptables_tproxy_works() {
    command -v iptables >/dev/null 2>&1 || return 1
    iptables -t mangle -N __K2R_TPROXY_PROBE 2>/dev/null || return 1
    rc=0
    iptables -t mangle -A __K2R_TPROXY_PROBE \
        -p udp -j TPROXY --tproxy-mark 1/1 --on-port 12345 2>/dev/null || rc=1
    iptables -t mangle -F __K2R_TPROXY_PROBE 2>/dev/null
    iptables -t mangle -X __K2R_TPROXY_PROBE 2>/dev/null
    return $rc
}

# nft_tproxy_works — true when nft has tproxy expression support. Tests via
# a throwaway table that's deleted right after.
nft_tproxy_works() {
    command -v nft >/dev/null 2>&1 || return 1
    rc=0
    nft -c "add table inet __k2r_probe
add chain inet __k2r_probe pre { type filter hook prerouting priority -150 ; }
add rule inet __k2r_probe pre meta l4proto udp tproxy ip to :12345 meta mark set 1" \
        2>/dev/null || rc=1
    return $rc
}

# Install TPROXY packages for the detected package manager. Errors are
# tolerated — we re-test capability after this returns.
install_tproxy_pkg() {
    # OpenWrt: fw4 (22.03+) means the nftables backend — the kmod-ipt-tproxy
    # pair only helps on fw3 (iptables) releases and is absent from apk feeds.
    if command -v fw4 >/dev/null 2>&1; then
        OPENWRT_PKGS="kmod-nft-tproxy"
    else
        OPENWRT_PKGS="iptables-mod-tproxy kmod-ipt-tproxy"
    fi
    case "$PKG_MGR" in
        openwrt-opkg)
            log "  opkg: installing ${OPENWRT_PKGS}..."
            opkg update >/dev/null 2>&1 || true
            # shellcheck disable=SC2086
            opkg install $OPENWRT_PKGS >/dev/null 2>&1 || true
            ;;
        openwrt-apk)
            log "  apk: installing ${OPENWRT_PKGS}..."
            apk update >/dev/null 2>&1 || true
            # shellcheck disable=SC2086
            apk add $OPENWRT_PKGS >/dev/null 2>&1 || true
            ;;
        apt)
            log "  apt-get: installing iptables..."
            DEBIAN_FRONTEND=noninteractive apt-get install -y iptables >/dev/null 2>&1 || true
            ;;
        apk)
            log "  apk: installing iptables..."
            apk add --no-cache iptables >/dev/null 2>&1 || true
            ;;
        dnf)
            log "  dnf: installing iptables..."
            dnf install -y iptables >/dev/null 2>&1 || true
            ;;
        pacman)
            log "  pacman: installing iptables..."
            pacman -S --noconfirm iptables >/dev/null 2>&1 || true
            ;;
        *)
            log "  no supported package manager detected — install TPROXY support manually"
            ;;
    esac
    # Load the kernel module — name varies by backend and kernel version.
    modprobe nft_tproxy 2>/dev/null || true
    modprobe xt_TPROXY 2>/dev/null || modprobe ipt_TPROXY 2>/dev/null || true
}

# ensure_tproxy — guarantee TPROXY is usable before installing k2r, otherwise
# the daemon will start, fail Install(), and bounce in a respawn loop.
# Either iptables OR nftables support is sufficient — k2r auto-detects.
ensure_tproxy() {
    log "Checking TPROXY support..."

    if iptables_tproxy_works || nft_tproxy_works; then
        log "  TPROXY available"
        return 0
    fi

    detect_pkg_mgr
    install_tproxy_pkg

    if iptables_tproxy_works || nft_tproxy_works; then
        log "  TPROXY installed"
        return 0
    fi

    log ""
    log "ERROR: TPROXY support not available and could not be installed."
    log "k2r requires either iptables with xt_TPROXY or nftables with tproxy."
    log ""
    log "Install manually for your platform:"
    log "  OpenWrt 25.x+:  apk update && apk add kmod-nft-tproxy"
    log "  OpenWrt 22-24:  opkg update && opkg install kmod-nft-tproxy"
    log "  OpenWrt <=21:   opkg update && opkg install iptables-mod-tproxy kmod-ipt-tproxy"
    log "  Debian/Ubuntu:  apt-get install iptables  &&  modprobe xt_TPROXY"
    log "  Alpine:         apk add iptables          &&  modprobe xt_TPROXY"
    log "  Fedora/RHEL:    dnf install iptables      &&  modprobe xt_TPROXY"
    log ""
    die "TPROXY missing — see instructions above"
}

# --- main ---

main() {
    SERVER_URL="$1"

    require_root
    detect_platform
    ensure_tproxy

    log "Fetching latest version..."
    VERSION="$(fetch_cdn "/LATEST")" || die "failed to fetch latest version"
    VERSION="$(echo "$VERSION" | tr -d '[:space:]')"
    [ -n "$VERSION" ] || die "empty version from LATEST"

    CURRENT="$(installed_version)"
    FILENAME="${BINARY}-${OS}-${ARCH}"

    # Already installed?
    if [ -n "$CURRENT" ]; then
        if [ "$CURRENT" = "$VERSION" ]; then
            log "$BINARY is up to date ($VERSION)"
            exit 0
        fi
        log "Upgrading $BINARY: $CURRENT -> $VERSION"
    else
        log "Installing $BINARY $VERSION ($OS/$ARCH)"
    fi

    # Download + verify
    log "Downloading ${FILENAME}..."
    CHECKSUMS="$(fetch_cdn "/${VERSION}/checksums.txt")" || die "failed to fetch checksums"
    EXPECTED="$(echo "$CHECKSUMS" | grep "  ${FILENAME}\$" | awk '{print $1}')"
    [ -n "$EXPECTED" ] || die "no checksum found for ${FILENAME}"

    TMP="$(mktemp)"
    trap 'rm -f "$TMP"' EXIT
    download_cdn "/${VERSION}/${FILENAME}" "$TMP" || die "download failed"
    sha256_check "$TMP" "$EXPECTED"
    log "Checksum verified"

    # Install binary
    chmod +x "$TMP"
    mv "$TMP" "${INSTALL_DIR}/${BINARY}"
    trap - EXIT

    # Upgrade: restart service if running, then done
    if [ -n "$CURRENT" ]; then
        if is_service_running; then
            restart_service
        fi
        log "Upgraded to $VERSION"
        exit 0
    fi

    # Fresh install
    log "Installed $BINARY to ${INSTALL_DIR}/${BINARY}"
    echo ""

    # Register service (k2r auto-detects OpenWrt procd vs systemd)
    log "Installing service..."
    "$BINARY" service install

    # LuCI integration (OpenWrt only — no-op on plain Linux)
    install_luci

    # Run setup if a server URL was provided
    if [ -n "$SERVER_URL" ]; then
        echo ""
        log "Running setup with provided server URL..."
        "$BINARY" setup "$SERVER_URL"
    fi

    # Print access URL
    LAN_IP="$(get_lan_ip)"
    echo ""
    if [ -n "$LAN_IP" ]; then
        log "Gateway web UI: http://${LAN_IP}:${GATEWAY_PORT}"
    else
        log "Gateway web UI: http://<LAN-IP>:${GATEWAY_PORT}"
    fi
    echo ""
}

main "$@"
