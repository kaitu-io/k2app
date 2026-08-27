#!/usr/bin/env bash
# Shared config + helpers for the mainland-China Windows probe box.
#
# WHAT THIS BOX IS FOR
#   A Windows Server instance inside mainland China, used as a *GFW regression
#   probe* and as the real-Windows end-to-end smoke target that GitHub Actions
#   structurally cannot be (windows-latest is always elevated, so the
#   UAC-declined branch is unreachable there).
#
#   Read the validity boundary before trusting a result from it:
#     - Cloud egress and home-broadband egress cross the SAME GFW gateways, so
#       for "does the protocol survive the GFW" this box is a HIGHER
#       signal-to-noise probe than home broadband: it removes the carrier noise
#       (UDP QoS throttling, peak-hour congestion, PCDN policing) that would
#       otherwise mask the signal.
#     - It is therefore LOWER-BOUND evidence: fails here => fails on home
#       broadband too (strong). Passes here => says nothing about home
#       broadband (weak).
#     - It cannot answer performance complaints ("slow at night"). Those are
#       caused by exactly the carrier noise this box removes. Use real user
#       device logs for those.
#     - ECS only offers Windows *Server*. Server != the Windows 10/11 that users
#       run: different default firewall profile, different UAC defaults, no
#       SmartScreen consumer behaviour. Do not generalise install-UX findings.
#
# DO NOT enable Global Accelerator / CEN international bandwidth on this
# instance. Those route around the standard international gateway, which
# silently invalidates every GFW conclusion this box exists to produce.

set -euo pipefail

# The dedicated RAM user (AliyunECSFullAccess + AliyunVPCFullAccess only).
# Deliberately NOT the root AK, and deliberately NOT the `dcdn-cert` profile —
# that one deploys production certificates and must not share a blast radius
# with a box that tunnels traffic out of mainland China.
PROFILE="${K2_CN_PROFILE:-k2-cn}"
REGION="${K2_CN_REGION:-cn-hangzhou}"

INSTANCE_TYPE="${K2_CN_INSTANCE_TYPE:-ecs.e-c1m2.large}"   # 2 vCPU / 4 GiB
# Chinese-locale Server 2022. Chosen over en-US on purpose: CI's windows-latest
# is already en-US, so zh-CN *adds* coverage (locale detection, encoding, fonts)
# instead of duplicating it. assert-ui-rendered.ps1 finds its window by process
# id rather than by title, so it stays locale-safe.
IMAGE_ID="${K2_CN_IMAGE_ID:-win2022_21H2_x64_dtc_zh-cn_40G_uefi_alibase_20260812.vhd}"
DISK_CATEGORY="${K2_CN_DISK_CATEGORY:-cloud_essd}"
DISK_SIZE="${K2_CN_DISK_SIZE:-40}"                          # Windows images are 40G
BANDWIDTH_OUT="${K2_CN_BANDWIDTH_OUT:-100}"                 # Mbps cap; PayByTraffic bills per GB

NAME="k2-cn-probe"
TAG_KEY="purpose"
TAG_VALUE="$NAME"
PASSWORD_FILE="${K2_CN_PASSWORD_FILE:-$HOME/.aliyun/k2-cn-probe.password}"

ali() { aliyun --profile "$PROFILE" "$@"; }

die() { echo "ERROR: $*" >&2; exit 1; }
note() { echo "  $*"; }
step() { echo; echo "==> $*"; }

# Extract a single scalar from JSON on stdin via a python expression over `d`.
jget() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }

preflight() {
  command -v aliyun >/dev/null || die "aliyun CLI not found (brew install aliyun-cli)"
  command -v python3 >/dev/null || die "python3 not found"

  local arn
  arn=$(ali sts GetCallerIdentity 2>/dev/null | jget "d['Arn']") \
    || die "profile '$PROFILE' cannot call STS. Run: aliyun configure list"

  case "$arn" in
    *:user/*) : ;;
    *) die "profile '$PROFILE' resolves to '$arn', which is not a RAM user.
       Refusing to run: this script must not be driven by a root AccessKey." ;;
  esac
  note "identity: $arn"
}

# Our current public IP, used to scope the RDP rule. Never fall back to
# 0.0.0.0/0: a wide-open 3389 on a mainland box is found by scanners in minutes.
my_public_ip() {
  local ip
  ip=$(curl -fsS --max-time 10 https://checkip.amazonaws.com 2>/dev/null | tr -d '[:space:]') || true
  [ -n "$ip" ] || ip=$(curl -fsS --max-time 10 https://api.ipify.org 2>/dev/null | tr -d '[:space:]') || true
  case "$ip" in
    *.*.*.*) printf '%s' "$ip" ;;
    *) die "could not determine this machine's public IP; refusing to open 3389 to the world" ;;
  esac
}
