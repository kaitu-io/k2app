#!/bin/bash
# K2 Test Control Script - run from Git Bash (no admin needed)
# Usage: ./scripts/test-k2-ctl.sh <command>
#   up      - Connect TUN tunnel
#   down    - Disconnect
#   status  - Show connection status
#   debug   - Set log level to debug
#   info    - Set log level to info
#   logs    - Tail the debug log file
#   test    - Run connectivity tests

K2_API="http://127.0.0.1:1778"
K2_LOG="C:/Users/david/k2-debug.log"
K2_SERVER="k2v5://test:tset@www.hunan.people.cn:443?ech=AEX-DQBB-gAgACBJydO-Rii7jcSaLNZq82ECJ4zFz1HHvrvYIHSlTeKYHwAEAAEAAQASY2xvdWRmbGFyZS1lY2guY29tAAA=&pin=sha256:ikIeFWzOscT9Tc6FTlk5vx4oCy6zJ1lHAbhY75GkHzo=,sha256:rFOuBHqNUZSo2FVLYa15-oOLYlOfvaRqoRzpW1ezAHg=&hop=40000-40019&ip=8.218.55.0"

case "${1:-status}" in
  up)
    echo "Connecting TUN mode..."
    curl -s -X POST "$K2_API/api/core" \
      -H "Content-Type: application/json" \
      -d "{
        \"action\": \"up\",
        \"params\": {
          \"config\": {
            \"server\": \"$K2_SERVER\",
            \"mode\": \"tun\",
            \"rule\": {\"global\": true},
            \"log\": {\"level\": \"debug\"}
          }
        }
      }"
    echo ""
    echo "Waiting 5s for connection..."
    sleep 5
    $0 status
    ;;
  down)
    curl -s -X POST "$K2_API/api/core" \
      -H "Content-Type: application/json" \
      -d '{"action":"down"}'
    echo ""
    ;;
  status)
    curl -s -X POST "$K2_API/api/core" \
      -H "Content-Type: application/json" \
      -d '{"action":"status"}'
    echo ""
    ;;
  debug)
    curl -s -X POST "$K2_API/api/log-level" \
      -H "Content-Type: application/json" \
      -d '{"level":"debug"}'
    echo ""
    ;;
  info)
    curl -s -X POST "$K2_API/api/log-level" \
      -H "Content-Type: application/json" \
      -d '{"level":"info"}'
    echo ""
    ;;
  logs)
    echo "Tailing $K2_LOG (Ctrl+C to stop)..."
    tail -f "$K2_LOG"
    ;;
  test)
    echo "=== Public IP ==="
    curl -s https://api.ipify.org 2>&1; echo ""
    echo "=== Google ==="
    curl -s -o /dev/null -w "HTTP %{http_code} | %{time_total}s\n" https://www.google.com 2>&1
    echo "=== YouTube ==="
    curl -s -o /dev/null -w "HTTP %{http_code} | %{time_total}s\n" https://www.youtube.com 2>&1
    echo "=== Speed (1MB) ==="
    curl -s -o /dev/null -w "HTTP %{http_code} | %{time_total}s | %{speed_download} B/s\n" https://speed.cloudflare.com/__down?bytes=1048576 2>&1
    echo "=== Metrics ==="
    curl -s "$K2_API/metrics" 2>&1; echo ""
    ;;
  *)
    echo "Usage: $0 {up|down|status|debug|info|logs|test}"
    ;;
esac
