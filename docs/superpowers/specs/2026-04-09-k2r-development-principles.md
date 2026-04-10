# k2r Development Principles

Lessons from sing-box router implementation, applied to k2r gateway development.

## 1. nftables: Use Go Library (Direct Netlink)

**Principle:** Rewrite `intercept_nft.go` to use `google/nftables` Go library, speaking netlink directly to kernel. Keep iptables as fallback for old kernels.

**Why:**
- 不依赖 `nft` 二进制 — 非 OpenWrt 的 Linux（NAS、Alpine、嵌入式）可能没装 `nft` 用户态工具，但内核支持 nftables (3.13+)
- iptables 正在被淘汰 — 长期不能依赖它作为主要方案
- 原子事务 — netlink batch 提交，多条规则要么全成功要么全失败
- 结构化错误 — netlink 返回具体错误码，不用解析 stderr 文本
- MAC set 动态操作更干净 — 直接操作 set elements，不用拼字符串

**Apply to k2r:**
- 重写 `intercept_nft.go`：用 `google/nftables` 库替代 `exec.Command("nft", ...)`
- 保留 `intercept_ipt.go` 作为 fallback（旧内核、不支持 nftables 的系统）
- `NewInterceptor` 自动检测：优先 Go nftables (netlink)，回退 iptables (shell exec)

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

## 7. OpenWrt fw4 Integration — Verify During Testing

**Principle:** k2r's `table inet k2r` (priority mangle/-150) processes before fw4's chains (priority dstnat/-100). They should coexist without interference.

**Why:** Two independent nftables tables don't interact. k2r's TPROXY marks packets and ip-rule routes them to loopback before fw4 sees them. In theory, no fw4 integration needed.

**Apply to k2r:**
- Test on real OpenWrt 22+ hardware with fw4 active
- If fw4 blocks TPROXY traffic: add `/etc/nftables.d/0-k2r.nft` with ACCEPT rules + `fw4 reload`
- If fw4 doesn't interfere: no action needed
- This is a test-phase verification, not a design-phase decision

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
