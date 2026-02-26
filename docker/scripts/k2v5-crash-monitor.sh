#!/bin/bash
# k2v5 crash monitor — real-time detection via docker events
#
# Watches for k2v5 container "die" events. On abnormal exit:
#   - Logs crash context (exit code, signal, memory state)
#   - Sends Slack alert
#
# Skips exit 0 (clean stop) and 143 (SIGTERM from compose down).
# Zero CPU when idle — docker events is a blocking stream.
#
# Deployed as: /etc/systemd/system/k2v5-crash-monitor.service
# Logs to:     journalctl -u k2v5-crash-monitor

set -euo pipefail

COMPOSE_DIR="/apps/kaitu-slave"
SLACK_WEBHOOK="https://hooks.slack.com/services/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"

NODE_NAME=$(grep -oP '^K2_NODE_NAME=\K.*' "${COMPOSE_DIR}/.env" 2>/dev/null || hostname)

slack_notify() {
    local emoji="$1" title="$2" msg="$3"
    [ -z "$SLACK_WEBHOOK" ] && return
    local payload
    payload=$(printf '{"text":"%s *%s*\\n`%s` — %s"}' "$emoji" "$title" "$NODE_NAME" "$msg")
    curl -sf -X POST -H 'Content-type: application/json' \
        -d "$payload" "$SLACK_WEBHOOK" >/dev/null 2>&1 || true
}

interpret_exit_code() {
    case "$1" in
        137) echo "SIGKILL (9) — likely OOM killer" ;;
        139) echo "SIGSEGV (11) — segmentation fault" ;;
        134) echo "SIGABRT (6) — abort/panic" ;;
        1)   echo "exit(1) — application error" ;;
        2)   echo "exit(2) — misuse or SIGINT" ;;
        *)   echo "exit($1)" ;;
    esac
}

handle_crash() {
    local exit_code="$1"
    local signal_hint
    signal_hint=$(interpret_exit_code "$exit_code")
    local timestamp
    timestamp=$(date -u '+%Y-%m-%d %H:%M:%S UTC')

    echo "[$timestamp] k2v5 CRASHED: ${signal_hint}"

    # Log extra context to journald (this script's own journal)
    echo "--- container state ---"
    docker inspect --format='ExitCode={{.State.ExitCode}} OOMKilled={{.State.OOMKilled}} RestartCount={{.RestartCount}} StartedAt={{.State.StartedAt}} FinishedAt={{.State.FinishedAt}}' k2v5 2>/dev/null || echo "(unavailable)"

    echo "--- dmesg OOM ---"
    dmesg -T 2>/dev/null | grep -i -E 'oom|out of memory|killed process' | tail -10 || echo "(none)"

    echo "--- memory ---"
    free -h 2>/dev/null || true

    # Slack alert
    slack_notify ":rotating_light:" "k2v5 CRASHED" "${signal_hint}. Docker will auto-restart."
}

echo "[$(date -u '+%Y-%m-%d %H:%M:%S UTC')] k2v5-crash-monitor started on ${NODE_NAME}"

# Main event loop
docker events \
    --filter "container=k2v5" \
    --filter "event=die" \
    --format '{{.Actor.Attributes.exitCode}}' \
    | while read -r exit_code; do

    # Skip graceful stops
    if [ "$exit_code" = "0" ] || [ "$exit_code" = "143" ]; then
        echo "[$(date -u '+%Y-%m-%d %H:%M:%S UTC')] k2v5 stopped gracefully (exit=${exit_code})"
        continue
    fi

    handle_crash "$exit_code"
done
