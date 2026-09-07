#!/bin/bash
# install-launchd.sh — register two per-user launchd jobs on THIS Mac (no sudo):
#   io.kaitu.k2cc-obs-collect        every 6 h (+ once at load)  → collect.sh
#   io.kaitu.k2cc-obs-weekly-review  Sep 14 09:07 local           → weekly-review.sh
# Logs: $K2CC_OBS_DATA/launchd-*.log. Re-running replaces the jobs. `--uninstall` removes them.
# launchd jobs see no ssh-agent: the SSH key for the nodes must be passphrase-free or the agent
# socket must be reachable (macOS exports SSH_AUTH_SOCK to user agents; install-launchd checks).
set -uo pipefail
DIR=$(cd "$(dirname "$0")" && pwd)
DATA=${K2CC_OBS_DATA:-$HOME/k2cc-obs-data}; mkdir -p "$DATA"
LA="$HOME/Library/LaunchAgents"; mkdir -p "$LA"
UID_=$(id -u)
C=io.kaitu.k2cc-obs-collect; R=io.kaitu.k2cc-obs-weekly-review
if [ "${1:-}" = "--uninstall" ]; then
  for l in $C $R; do launchctl bootout "gui/$UID_/$l" 2>/dev/null; rm -f "$LA/$l.plist"; done; echo "removed"; exit 0
fi
PATHS="/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
mk() { # label script extra-xml
cat > "$LA/$1.plist" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$1</string>
  <key>ProgramArguments</key><array><string>/bin/bash</string><string>$DIR/$2</string></array>
  <key>EnvironmentVariables</key><dict><key>PATH</key><string>$PATHS</string><key>K2CC_OBS_DATA</key><string>$DATA</string><key>HOME</key><string>$HOME</string></dict>
  <key>StandardOutPath</key><string>$DATA/launchd-$2.log</string>
  <key>StandardErrorPath</key><string>$DATA/launchd-$2.log</string>
  $3
</dict></plist>
PL
}
mk $C collect.sh '<key>StartInterval</key><integer>21600</integer><key>RunAtLoad</key><true/>'
mk $R weekly-review.sh '<key>StartCalendarInterval</key><dict><key>Month</key><integer>9</integer><key>Day</key><integer>14</integer><key>Hour</key><integer>9</integer><key>Minute</key><integer>7</integer></dict>'
for l in $C $R; do launchctl bootout "gui/$UID_/$l" 2>/dev/null; launchctl bootstrap "gui/$UID_" "$LA/$l.plist" && echo "loaded $l"; done
launchctl list | grep -E "$C|$R"
