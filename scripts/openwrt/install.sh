#!/bin/sh
# k2 VPN — OpenWrt Installer

set -e

# Stop existing service
/etc/init.d/k2 stop 2>/dev/null || true

# Install binary
cp k2 /usr/bin/k2
chmod +x /usr/bin/k2

# Create config directory
mkdir -p /etc/k2

# Generate default config (preserve existing)
if [ ! -f /etc/k2/config.yaml ]; then
    cat > /etc/k2/config.yaml << 'CONF'
listen: "0.0.0.0:1777"
mode: tun
log:
  level: info
CONF
fi

# Install init.d script
cp k2.init /etc/init.d/k2
chmod +x /etc/init.d/k2

# Install LuCI integration (if LuCI present)
if [ -d /usr/lib/lua/luci ]; then
    mkdir -p /usr/lib/lua/luci/controller
    mkdir -p /usr/lib/lua/luci/view
    cp luci-app-k2/controller/k2.lua /usr/lib/lua/luci/controller/k2.lua
    cp luci-app-k2/view/k2.htm /usr/lib/lua/luci/view/k2.htm
    rm -rf /tmp/luci-* 2>/dev/null || true
    echo "LuCI integration installed"
fi

# Enable and start
/etc/init.d/k2 enable
/etc/init.d/k2 start

LAN_IP=$(uci get network.lan.ipaddr 2>/dev/null || echo "router-ip")
echo ""
echo "k2 installed successfully!"
echo "Web UI: http://${LAN_IP}:1777"
echo ""
echo "To configure: edit /etc/k2/config.yaml"
echo "To check status: /etc/init.d/k2 status"
