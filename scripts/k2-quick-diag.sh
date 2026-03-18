#!/bin/bash
# k2-quick-diag.sh — Quick diagnostic analysis of k2 client logs
# Usage: ./scripts/k2-quick-diag.sh [logfile]
# Default: ~/Library/Logs/kaitu/k2.log (macOS)

set -euo pipefail

# Colors
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Determine log file
if [[ $# -ge 1 ]]; then
    LOGFILE="$1"
elif [[ -f "$HOME/Library/Logs/kaitu/k2.log" ]]; then
    LOGFILE="$HOME/Library/Logs/kaitu/k2.log"
elif [[ -f "$HOME/.local/share/kaitu/logs/k2.log" ]]; then
    LOGFILE="$HOME/.local/share/kaitu/logs/k2.log"
else
    echo "Usage: $0 [logfile]"
    echo "No default log file found."
    exit 1
fi

if [[ ! -f "$LOGFILE" ]]; then
    echo "Log file not found: $LOGFILE"
    exit 1
fi

echo -e "${BOLD}=== k2 Quick Diagnostic ===${NC}"
echo -e "Log: ${CYAN}$LOGFILE${NC}"
echo -e "Size: $(du -h "$LOGFILE" | cut -f1)"
echo ""

# --- Section 1: Last Session ---
echo -e "${BOLD}--- Session Info ---${NC}"
LAST_CONNECTED=$(grep "DIAG: connected" "$LOGFILE" | tail -1)
LAST_SESSION_END=$(grep "DIAG: session-end" "$LOGFILE" | tail -1)
if [[ -n "$LAST_CONNECTED" ]]; then
    echo -e "${GREEN}Last connected:${NC} $LAST_CONNECTED"
else
    echo -e "${YELLOW}No DIAG: connected found (pre-DIAG logs or never connected)${NC}"
fi
if [[ -n "$LAST_SESSION_END" ]]; then
    echo -e "${GREEN}Last session end:${NC} $LAST_SESSION_END"
fi
echo ""

# --- Section 2: Recent Heartbeats ---
echo -e "${BOLD}--- Recent Heartbeats (last 5) ---${NC}"
HEARTBEATS=$(grep "DIAG: heartbeat" "$LOGFILE" | tail -5)
if [[ -n "$HEARTBEATS" ]]; then
    echo "$HEARTBEATS"
    # Check for degraded/critical in heartbeats
    DEGRADED_COUNT=$(grep "DIAG: heartbeat" "$LOGFILE" | grep -c "health=degraded" || true)
    CRITICAL_COUNT=$(grep "DIAG: heartbeat" "$LOGFILE" | grep -c "health=critical" || true)
    FALLBACK_COUNT=$(grep "DIAG: heartbeat" "$LOGFILE" | grep -c "fallback=true" || true)
    if [[ "$CRITICAL_COUNT" -gt 0 ]]; then
        echo -e "${RED}!! $CRITICAL_COUNT heartbeats with health=critical${NC}"
    fi
    if [[ "$DEGRADED_COUNT" -gt 0 ]]; then
        echo -e "${YELLOW}! $DEGRADED_COUNT heartbeats with health=degraded${NC}"
    fi
    if [[ "$FALLBACK_COUNT" -gt 0 ]]; then
        echo -e "${YELLOW}! $FALLBACK_COUNT heartbeats with fallback=true (TCP-WS mode)${NC}"
    fi
else
    echo -e "${YELLOW}No heartbeats found (upgrade to DIAG-enabled build)${NC}"
fi
echo ""

# --- Section 3: Problems (DIAG events excluding heartbeat) ---
echo -e "${BOLD}--- Problem Events ---${NC}"
PROBLEMS=$(grep "DIAG:" "$LOGFILE" | grep -v "heartbeat\|connected\|session-end" | tail -20)
if [[ -n "$PROBLEMS" ]]; then
    echo "$PROBLEMS"
else
    echo -e "${GREEN}No problem events found${NC}"
fi
echo ""

# --- Section 4: Event Counts ---
echo -e "${BOLD}--- Event Summary ---${NC}"
for event in "dns-slow" "dns-fail" "proxy-dial-fail" "proxy-dial-slow" "quic-handshake-fail" "transport-switch" "wire-error"; do
    COUNT=$(grep -c "DIAG: $event" "$LOGFILE" 2>/dev/null || true)
    if [[ "$COUNT" -gt 0 ]]; then
        if [[ "$event" == *"fail"* || "$event" == *"error"* ]]; then
            echo -e "  ${RED}$event: $COUNT${NC}"
        else
            echo -e "  ${YELLOW}$event: $COUNT${NC}"
        fi
    fi
done
TOTAL_DIAG=$(grep -c "DIAG:" "$LOGFILE" 2>/dev/null || true)
HEARTBEAT_COUNT=$(grep -c "DIAG: heartbeat" "$LOGFILE" 2>/dev/null || true)
echo -e "  Total DIAG events: $TOTAL_DIAG (heartbeats: $HEARTBEAT_COUNT)"
echo ""

# --- Section 5: Health State Transitions (existing logs) ---
echo -e "${BOLD}--- Health Transitions (last 10) ---${NC}"
TRANSITIONS=$(grep -E "health: (degraded|critical|recovery)" "$LOGFILE" | tail -10)
if [[ -n "$TRANSITIONS" ]]; then
    echo "$TRANSITIONS"
else
    echo -e "${GREEN}No health transitions (stable)${NC}"
fi
echo ""

# --- Section 6: Panics / Errors ---
echo -e "${BOLD}--- Panics & Fatal Errors ---${NC}"
PANICS=$(grep -i "panic" "$LOGFILE" | tail -5)
if [[ -n "$PANICS" ]]; then
    echo -e "${RED}$PANICS${NC}"
else
    echo -e "${GREEN}No panics found${NC}"
fi
echo ""

# --- Verdict ---
echo -e "${BOLD}=== Verdict ===${NC}"
HAS_PROBLEMS=false

PANIC_COUNT=$(grep -ci "panic" "$LOGFILE" 2>/dev/null || true)
FAIL_COUNT=$(grep -c "DIAG:.*fail" "$LOGFILE" 2>/dev/null || true)
ERROR_COUNT=$(grep -c "DIAG: wire-error" "$LOGFILE" 2>/dev/null || true)

if [[ "$PANIC_COUNT" -gt 0 ]]; then
    echo -e "${RED}PANIC: $PANIC_COUNT panic(s) detected — check stack traces${NC}"
    HAS_PROBLEMS=true
fi
if [[ "${CRITICAL_COUNT:-0}" -gt 0 ]]; then
    echo -e "${RED}CRITICAL: $CRITICAL_COUNT critical health states — severe packet loss or UDP blocking${NC}"
    HAS_PROBLEMS=true
fi
if [[ "$FAIL_COUNT" -gt 10 ]]; then
    echo -e "${YELLOW}WARN: $FAIL_COUNT failure events — check DNS/proxy/QUIC sections${NC}"
    HAS_PROBLEMS=true
fi
if [[ "${FALLBACK_COUNT:-0}" -gt 0 ]]; then
    echo -e "${YELLOW}WARN: Running in TCP-WS fallback — QUIC may be blocked${NC}"
    HAS_PROBLEMS=true
fi

# Memory check (heapMB field in heartbeats, if present)
HIGH_HEAP_COUNT=$(grep "DIAG: heartbeat" "$LOGFILE" | grep -oP 'heapMB=\K[0-9.]+' | awk '$1 > 30 {count++} END {print count+0}' 2>/dev/null || true)
if [[ "$HIGH_HEAP_COUNT" -gt 0 ]]; then
    echo -e "${YELLOW}WARN: $HIGH_HEAP_COUNT heartbeats with heapMB > 30 — NE memory pressure risk${NC}"
    HAS_PROBLEMS=true
fi

if [[ "$HAS_PROBLEMS" == false ]]; then
    echo -e "${GREEN}OK: No significant issues detected${NC}"
fi
