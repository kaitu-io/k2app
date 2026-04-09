# k2r Development Principles

Lessons from sing-box router implementation, applied to k2r gateway development.

## 1. nftables: Use Go Library, Not Shell Exec

**Principle:** All nftables operations via `github.com/google/nftables` (or sagernet fork), never shell out to `nft` command.

**Why:** sing-box uses programmatic nftables (`Conn.Flush()` atomic commits). Benefits: type-safe rule construction, atomic setup/teardown, no shell injection risks, no output parsing. Their cleanup is simply "delete the entire table" — impossible to leave stale rules.

**Apply to k2r:**
- `intercept_nft.go` should use Go nftables library
- All rule changes via `Conn.AddSet()`, `Conn.AddRule()`, `Conn.Flush()`
- MAC allowlist updates via nftables set element add/delete (atomic, no full-table rebuild)
- Cleanup: `Conn.DelTable()` removes everything

## 2. Independent nftables Table

**Principle:** k2r owns `table inet k2r` exclusively. Never modify system firewall tables (fw4, firewalld, etc.).

**Why:** sing-box's `inet sing-box` table coexists peacefully with fw4. Modifying fw4 rules directly would break on fw4 reload. Independent table survives system firewall changes.

**Apply to k2r:**
- Already using `table inet k2r` — correct
- For fw4 compatibility: detect fw4 → write ACCEPT rules for TUN/TPROXY interface to `/etc/nftables.d/0-k2r.nft` → `fw4 reload`
- On cleanup: delete table + remove nftables.d file

## 3. MAC Filtering in Prerouting Only

**Principle:** `ether saddr` matching works only in prerouting chain (LAN forwarded traffic). Not in output chain (locally generated traffic has no L2 header).

**Why:** sing-box confirms this works. Their implementation: check `iiftype == ARPHRD_ETHER` first (skip non-Ethernet), then match `ether saddr` against nftables set.

**Apply to k2r:**
- RouterDevice allowlist filtering in prerouting chain only
- Check `iiftype` before MAC match (safety for non-Ethernet interfaces like VPN tunnels)
- Use anonymous nftables set for multiple MACs (efficient kernel-side matching)

## 4. Conntrack Mark for Loop Prevention

**Principle:** Use conntrack marks to distinguish redirected traffic from k2r's own outbound traffic, preventing redirect loops.

**Why:** sing-box uses 3 marks (input/output/reset) saved to conntrack. Subsequent packets in the same connection are handled at kernel level without userspace overhead.

**Apply to k2r:**
- Define k2r-specific mark values (avoid collision with sing-box defaults)
- Mark outbound traffic from k2r process → bypass in prerouting
- Save marks to conntrack for connection-level bypass

## 5. DNS Redirect Must Be Configurable

**Principle:** DNS interception (port 53 DNAT) must be a user toggle, not hardcoded.

**Why:** sing-box's hardcoded DNS hijack (#3705) breaks common setups where dnsmasq on port 53 forwards to sing-box on 5353. Closed as NOT_PLANNED — users are frustrated.

**Apply to k2r:**
- `dns_redirect` is already a config toggle in k2r — correct
- When enabled: DNAT port 53 to k2r's DNS resolver
- When disabled: no DNS interception, user manages DNS externally
- Default: enabled (most users want it), but clearly documented how to disable

## 6. Performance: Mind the Router Constraints

**Principle:** Router CPUs are weak (ARM Cortex-A53 @ 1.3GHz typical). Every abstraction layer costs throughput.

**Why:** sing-box gVisor stack caps at ~200Mbps on MT7986 (a relatively powerful router SoC). GSO helped reach 700Mbps. TPROXY avoids the TUN→userspace→TUN round-trip.

**Apply to k2r:**
- k2r uses TPROXY (not TUN) — avoids the TUN overhead that hurt sing-box
- TPROXY keeps traffic in kernel until the proxy connection is established
- Profile on actual router hardware (aarch64 + armv7), not just x86 dev machines
- Avoid unnecessary memory allocations in the hot path (per-packet processing)

## 7. OpenWrt fw4 Integration Pattern

**Principle:** Detect fw4 at runtime. Write compatibility rules to `/etc/nftables.d/`. Reload fw4. Clean up on exit.

**Why:** fw4 manages zones/forwarding. k2r's independent table handles interception, but fw4 needs ACCEPT rules for k2r's interfaces/ports. Writing to nftables.d ensures rules survive fw4 reloads.

**Apply to k2r:**
- On startup: detect fw4 (`nft list table inet fw4`)
- If present: write `/etc/nftables.d/0-k2r.nft` with ACCEPT rules for TPROXY port + DNS redirect port
- Call `fw4 reload`
- On shutdown: delete the nftables.d file + `fw4 reload`

## 8. Clean Teardown is Non-Negotiable

**Principle:** Every rule k2r creates must be removed on exit. Stale rules break networking.

**Why:** sing-box's cleanup = delete entire table. If k2r crashes, stale TPROXY rules can blackhole LAN traffic. Recovery.go's stale rule cleanup on startup is critical.

**Apply to k2r:**
- Already has `recovery.go` for stale rule cleanup — correct
- Startup: always attempt cleanup before creating new rules
- Use signal handlers (SIGTERM, SIGINT) for graceful teardown
- procd/systemd KillSignal = SIGTERM (not SIGKILL) to allow cleanup

## 9. No Platform Detection Needed (sing-box lesson)

**Principle:** Don't try to detect "router vs desktop" at runtime. The configuration determines behavior.

**Why:** sing-box runs the same binary everywhere. Router mode is just a config with `auto_redirect: true` + LAN subnet rules. k2r is already a dedicated gateway binary — this is even cleaner than sing-box's approach.

**Apply to k2r:**
- k2r IS the router binary. No detection needed.
- Platform differences (OpenWrt vs standard Linux) handled by: ubus detection for device discovery, fw4 detection for firewall integration, init system detection for service install.

## 10. Webapp Embedding is Our Advantage

**Principle:** sing-box has no built-in management UI. Community LuCI projects fill the gap poorly. k2r's embedded webapp is a significant differentiator.

**Why:** HomeProxy (LuCI) requires ImmortalWrt, is tightly coupled to LuCI framework, and doesn't work on standard Linux. k2r's embedded React webapp works everywhere — OpenWrt, standard Linux, NAS, VM — via any browser.

**Apply to k2r:**
- This is already the architecture. Maintain it.
- The webapp provides: device management, purchase, updater, settings — things no sing-box wrapper offers
- Keep the webapp light (bundle size matters for router flash storage)
