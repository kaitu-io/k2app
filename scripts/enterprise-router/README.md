# Enterprise Router Firmware Image

Prebuilt OpenWrt image for the 8-slot enterprise multi-SSID router product.
Reference target: MT7981 (Mediatek Filogic), OpenWrt 23.05+. Machine model
selection is an open item — `build-image.sh` is parameterized by `PROFILE`
so a different device only needs a new profile name + `TARGET`, not a
script rewrite.

## Factory topology (baked by `files/etc/uci-defaults/99-enterprise-slots`)

- 8 independent bridges `br-line1..8`, each on `10.81.N.1/24` (N = slot
  number), each with its own DHCP pool.
- 8 Wi-Fi ifaces, UCI section names `wireless.slot1..slot8` — this exact
  naming is k2r's contract (`gateway/uci_linux.go`); k2r only ever touches
  `ssid`/`key`/`disabled` on these sections, nothing else.
- All 8 disabled at factory (`disabled=1`), placeholder SSID/key. k2r
  enables + names them once it converges the customer's slot manifest.
- Firewall: every `lineN` zone has `forward=REJECT` to `wan` — fail-closed
  at the kernel level. If k2r dies, every slot goes dark; the router's WAN
  IP can never leak traffic on its own. No inter-slot forwarding either
  (account isolation between customers sharing one physical router).
- Wired LAN ports stay on `br-lan`, management-only: reachable for the
  anchor/admin path, no `wan` forwarding (no internet).

## `files/etc/init.d/k2r`

Static rendering of k2's `cmd/k2r/service_linux.go` `generateOpenWRTInitScript`
output for exe path `/usr/bin/k2r` (where `build-image.sh` installs the
binary). **Keep in sync manually** — if the generator function's respawn
policy or exe path handling changes upstream, re-copy its output here.
`files/etc/rc.d/S99k2r` symlinks to it so the service is enabled at boot
(ImageBuilder's `FILES=` copies symlinks verbatim via `cp -a`).

## Delivery flow

1. Build the image: `PROFILE=<device-profile> K2R_BIN=<path-to-k2r-binary> ./build-image.sh`
   (`K2R_BIN` is produced by the repo's `scripts/build-openwrt.sh`, e.g.
   `release/k2r/<version>/k2r-linux-arm64`).
2. Flash the image to the device.
3. On first boot, SSH in and run `k2r setup <k2subs:// URL>` — this is the
   **enterprise-issue** URL minted for this specific gateway account (see
   `api/CLAUDE.md` "Dedicated Line" / Plan 2 Task 2), not a consumer one.
4. Record the router's `udid` (k2r prints it on setup) and hand it to
   ops.
5. Ops uses the `kaitu-center` MCP `bind_enterprise_slot` tool (or the
   `/manager/enterprise` admin page) to bind the customer's lines to this
   device's slots. Bindings converge to the router within one subs refresh
   cycle (~30 min) or immediately after the next `k2r` restart.

## Parameters

| Variable | Default | Meaning |
|----------|---------|---------|
| `OPENWRT_VERSION` | `23.05.5` | OpenWrt release the ImageBuilder targets |
| `TARGET` | `mediatek/filogic` | OpenWrt target/subtarget (MT7981 reference) |
| `PROFILE` | *(required)* | OpenWrt board profile name for the specific device |
| `K2R_BIN` | *(required)* | Path to the cross-compiled `k2r` binary to embed |
| `WORKDIR` | `./.ib-work` | ImageBuilder download + build scratch directory |

## Verification

```bash
shellcheck build-image.sh files/etc/uci-defaults/99-enterprise-slots
```

Real-machine flashing, first-boot uci-defaults execution, and end-to-end
slot convergence are a **release gate**, not part of this script's own
verification — see
`docs/superpowers/specs/2026-07-22-enterprise-router-multi-ssid-design.md`
§9 for the real-hardware smoke checklist.
