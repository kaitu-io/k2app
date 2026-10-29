#!/usr/bin/env bash
# Predefined network degradation scenarios.
# Source this file, then call load_scenario <name>.
#
# Each scenario sets environment variables that run.sh uses
# to configure tc netem and iptables.

load_scenario() {
    case "$1" in
        cmcc-4g)
            # China Mobile 4G: high UDP loss + moderate latency
            LOSS=25 DELAY=100 JITTER=30 RATE=1000 PROTOCOL=udp
            SOAK=30 RECOVERY=30
            ;;
        cmcc-congested)
            # China Mobile peak hours: extreme UDP loss
            LOSS=50 DELAY=200 JITTER=80 RATE=500 PROTOCOL=udp
            SOAK=45 RECOVERY=30
            ;;
        satellite)
            # Satellite link: high latency, low loss
            LOSS=3 DELAY=600 JITTER=50 RATE=5000 PROTOCOL=all
            SOAK=30 RECOVERY=30
            ;;
        airport-wifi)
            # Airport WiFi: moderate loss + congestion
            LOSS=10 DELAY=50 JITTER=30 RATE=500 PROTOCOL=all
            SOAK=30 RECOVERY=20
            ;;
        mtu-blackhole)
            # PMTUD blackhole: large UDP packets dropped
            LOSS=0 DELAY=0 JITTER=0 RATE=0 MTU_LIMIT=1280 PROTOCOL=udp
            SOAK=30 RECOVERY=15
            ;;
        connection-reset)
            # NAT timeout / network switch: total UDP blackout then recovery
            LOSS=100 DELAY=0 JITTER=0 RATE=0 PROTOCOL=udp
            SOAK=15 RECOVERY=45
            ;;
        quic-blocked)
            # Firewall blocks all QUIC (UDP to port 443)
            LOSS=100 DELAY=0 JITTER=0 RATE=0 PROTOCOL=udp
            SOAK=30 RECOVERY=30
            ;;
        *)
            echo "Unknown scenario: $1"
            echo "Available: cmcc-4g cmcc-congested satellite airport-wifi mtu-blackhole connection-reset quic-blocked"
            return 1
            ;;
    esac
    return 0
}

# List all available scenario names (for iteration).
all_scenarios() {
    echo "cmcc-4g cmcc-congested satellite airport-wifi mtu-blackhole connection-reset quic-blocked"
}
