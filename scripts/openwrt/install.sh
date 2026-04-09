#!/bin/sh
# k2r VPN gateway — OpenWrt Installer
# For automated install, use: wget -qO- https://kaitu.io/i/k2r | sudo sh

set -e
cd "$(dirname "$0")"

# Stop existing service
/etc/init.d/k2r stop 2>/dev/null || true

# Install binary
cp k2r /usr/bin/k2r
chmod +x /usr/bin/k2r

# Create config directory
mkdir -p /etc/k2r

# Generate default config (preserve existing)
if [ ! -f /etc/k2r/config.yaml ]; then
    cat > /etc/k2r/config.yaml << 'CONF'
listen: "0.0.0.0:1779"
mode: tun
log:
  level: info
CONF
fi

# Install init.d script
cp k2r.init /etc/init.d/k2r
chmod +x /etc/init.d/k2r

# Install LuCI integration (if LuCI present)
if [ -d /usr/lib/lua/luci ]; then
    mkdir -p /usr/lib/lua/luci/controller
    mkdir -p /usr/lib/lua/luci/view
    cp luci-app-k2r/controller/k2r.lua /usr/lib/lua/luci/controller/k2r.lua
    cp luci-app-k2r/view/k2r.htm /usr/lib/lua/luci/view/k2r.htm
    rm -rf /tmp/luci-* 2>/dev/null || true
    echo "LuCI integration installed"
fi

# Enable and start
/etc/init.d/k2r enable
/etc/init.d/k2r start

LAN_IP=$(uci get network.lan.ipaddr 2>/dev/null || echo "router-ip")
echo ""
echo "k2r installed successfully!"
echo "Web UI: http://${LAN_IP}:1779"
echo ""
echo "To configure: edit /etc/k2r/config.yaml"
echo "To check status: /etc/init.d/k2r status"
