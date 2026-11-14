# Rule Layer (rule/)

Multi-ruleset routing engine. Turns `routes:` in `ClientConfig` into an ordered
`[]RouteEntry`, then answers two different questions against it:

| Caller | Entry point | Knows |
|--------|-------------|-------|
| DNS pipeline (`core/dns`) | `Match(host)` | a host string, nothing else |
| DNS pipeline, per-process view (`core/dns`) | `MatchDNSMeta(ConnMeta)` | the QUERYING process's identity, nothing else — only purely-process routes participate (no host criteria, and `{apps, port}` combos fail closed on the zeroed dims). Steers resolver-view only, never routing. See `dns_meta.go` + `core/dns/CLAUDE.md` |
| L4 tunnel (`core/tunnel.go`) | `MatchConn(host, ConnMeta)`, reached via `core.Router.RouteConn` | host + connection metadata |
| Sniffer, post-first-packet (`core/tunnel.go`) | `RouteConnProto(host, ConnMeta)` | the sniffed application protocol |

Route entries are built in `engine/engine.go buildRouteEntries`, not here — this
package owns the *matching*, the engine layer owns the *compilation*.

Matching is first-match-wins in route order; no match → `fallback`.

Long-form notes (not auto-loaded — read before touching the code they cover): **`docs/matching-notes.md`** (two-phase trap examples, protocol re-route and sniff gates, criteria-predicate failure modes, `match.region` cost, known debt), **`docs/pin-bundle-notes.md`** (tmp pin LRU, reload, bundle lifecycle detail).

## Two-Phase Matching (design invariant, not an accident)

- `Match(host)` runs at DNS time; `matchHostLocked` has **no `ConnMeta` at all** — it skips routes without host criteria (`hasHostCriteria()`) and checks *only* the host criteria of the rest. `MatchConn(host, meta)` (`matchConnLocked`) runs at dial time: `(no host criteria OR host matches) AND (no meta criteria OR meta matches)`.
- **`port` and `protocol` are connection-time dimensions, and on a *combined* route they are silently dropped at DNS time**: a `{domain_suffix, port}` route matches on host alone, and `core/dns/middleware.go` uses its target to pick the upstream (`queryUpstream`), to **NXDOMAIN when the target is reject**, and to `setTmpRule` **every resolved IP** to it. So `{domain_suffix:["x"], port:["443"], via: reject}` NXDOMAINs the whole domain, and a `{domain_suffix, port}` proxy route's port condition **never takes effect in production** (the `e.tmp` hit returns first). `engine.buildRouteEntries` WARNs at compile time (`dnsDroppedMetaFields`); `engine_combined_route_test.go` pins the semantics — changing them is a design decision with that test in the diff.
- The pre-tmp pass in `matchConnLocked` skips on `hasHostCriteria() || !hasMetaCriteria()` — only **purely meta** routes (port-only, process-only, …) get their say ahead of the pin cache. It is gated on a local `hasAnyMeta` (see Known debt).

## Match Dimensions

`MatchConfig` (config side, `target.go`) → `RouteEntry` (compiled side,
`engine.go`). Which phase a dimension can participate in follows directly from
which predicate sees it.

| `match:` key | `RouteEntry` field | Class | Phase |
|---|---|---|---|
| `preset` / `names` / `exclude` | `Sets` (via `ExpandMatch` + bundle index) | host | DNS + conn |
| `domain_suffix` / `ip_cidr` | `Sets` (inline, `krs.BuildSet`) | host | DNS + conn |
| `ip_is_private` | `IPIsPrivate` | host | DNS + conn |
| `region` | expands to one host entry + one meta entry | both | DNS + conn |
| `apps` | expands to a meta entry (process/package globs) | meta | conn |
| `app_paths` | `ProcessPathPrefixes` in the SAME entry as `apps` (OR'd with the names). Desktop only; `NormalizeProcessDir` (component match, Windows lowercased, roots/relative dropped) vs `ConnMeta.ProcessPath` (`provider.ProcessPathSearcher`) | meta (process) | conn |
| `process_name` / `process_name_prefix` | `ProcessNames` / `ProcessNamePrefixes` | meta (process) | conn |
| `package_name` / `package_name_prefix` / `installer_package` | `PackageNames` / `PackageNamePrefixes` / `InstallerPackages` | meta (process) | conn |
| `network` | `NetworkMatch` | meta | conn |
| `src_ip_cidr` | `SrcCIDRs` | meta | conn |
| `port` | `PortRanges` (`ParsePortRange`) | meta | conn |
| `protocol` | `Protocols` (lowercased, trimmed) | meta | conn, **post-sniff only** |
| `all` | — | catch-all → becomes `fallback` | — |

Within one dimension the list is **OR**; across dimensions it is **AND**.
Group definitions live in `matchMeta`'s doc comment.

Every meta dimension is **fail-closed** when the observed value is unknown:

- `Port == 0` never matches any range (`PortRange.Contains` rejects 0 outright —
  0 is `ConnMeta`'s "unknown", not a real port).
- `Protocol == ""` never matches a route carrying `Protocols`, even if the
  compiled list contains an empty string.
- invalid `SrcIP` never matches a route carrying `SrcCIDRs`.
- empty `ProcessName` never matches a route carrying process-name criteria.

`ProcessNamePrefixes` are pre-lowercased at compile time (`engine.lowerCopy`); `PackageNamePrefixes` are case-preserved. On Android `ConnMeta.PackageNames` is the **set** of packages sharing the owning UID (sharedUserId) and matches if ANY hits; `PackageName` remains the legacy fallback. Exact process-name match also accepts the 15-char kernel truncation (`commTruncLen`).

## Protocol Matching Participates Only After Sniffing

`RouteConnProto(host, ConnMeta)` — called from `core/tunnel.go` after sniffing — (1) considers **only** routes with `len(Protocols) > 0`, (2) does **not** consult the tmp pin cache, (3) returns `(Target, bool)`: **callers must check the bool** (`Target(0)` is `direct`). Full `matchMeta` applies; host criteria via `matchRouteHost` against the **best host known** (sniffed SNI, else the IP literal) — both the UDP handler (step 6) and the TCP sniff block.

### ⚠ The re-route crosses route priority

On a hit it overwrites the already-matched target without re-running the table, so a protocol rule **overrides any earlier route regardless of position** — including **App Bypass** (unlike the SNI re-routes, which call `Router.RouteConn` so a process bypass survives). Measured: `[{network:"udp", via:direct}, {protocol:["quic"], via:proxy}]` sends a real QUIC Initial to the **proxy**. Impact today is **zero** (no shipped `protocol:` rule). **Before shipping any default `protocol:` rule, decide whether this override semantics is intended** — a design decision with its own review, not a bug fix.

### A protocol rule can only fire if the protocol was sniffed

- **Outer gate**: peek only with a usable outbound (`t.outbound(action) != nil` — **reject** is never sniffed) and a sniff flag on — TCP `sniffBT || sniffSNI`, UDP `sniffBT || sniffSNI || blockQUIC`. `SniffBT`/`SniffSNI` = `!ruleEngine.DisableSniff()` (off in global mode → no TCP protocol rule can fire), but `BlockQUIC` comes from `client.BlockQUIC` — **global mode does not mean protocol rules are off** on UDP.
- **Inner gate**:

| Protocol | Transport | Additionally requires | So a rule cannot fire when |
|---|---|---|---|
| `bittorrent` | TCP + UDP | `sniffBT` **and** the connection was headed for the **proxy** (`action != directTarget`) | the connection was already routed direct |
| `stun`, `dtls` | UDP | same gate as above | the connection was already routed direct |
| `quic` | UDP | `meta.Protocol == ""` (not already classified STUN/DTLS/BT). **Not** subject to the proxy-bound precondition — but see the `blockQUIC` drop below | the first packet was classified STUN/DTLS/BT first, **or** `blockQUIC` is on and the connection was proxy-bound |

- So `{protocol:["stun"], via: <some proxy>}` on an already-direct connection never fires (`ConnMeta.Protocol` stays empty, fail-closed), and `{protocol:["quic"], via: direct}` **never fires on a proxy-bound connection while `blockQUIC` is on** (the QUIC detector's `if t.blockQUIC && action != t.directTarget { return }` drop runs first). Do not "fix" the inner gate: it keeps WebRTC on the tunnel for #3051 and sniffing off already-direct traffic. TCP only ever produces `bittorrent`.

### Post-sniff defaults when no protocol rule exists

- **BitTorrent** → forced **direct**. **STUN / DTLS** → detected, `ConnMeta.Protocol` set, **kept on the tunnel** — **ticket #3051** (forcing WebRTC direct broke Telegram / KakaoTalk calls behind the GFW). `core/voip_stun_regression_test.go` locks this; do not "fix" STUN back to direct.

## The criteria Predicate Family

Seven hand-written field enumerations; adding a match dimension means touching the ones that apply — **every miss is silent**.

| Predicate | Location | Question it answers |
|---|---|---|
| `MatchConfig.HasCriteria()` | `rule/target.go` | does this `match:` block constrain anything at all? |
| `MatchConfig.HasHostCriteria()` | `rule/target.go` | does it constrain host (domain/IP)? |
| `RouteEntry.hasHostCriteria()` | `rule/engine.go` | compiled-side host question — gates DNS-time eligibility |
| `RouteEntry.hasMetaCriteria()` | `rule/engine.go` | does it have any **connection-time** criteria? |
| `RouteEntry.hasProcessCriteria()` | `rule/engine.go` | does it need **process attribution**? |
| `hasAnyCriteria` (local) | `engine/engine.go buildRouteEntries` | is this compiled entry worth appending to the table? |
| `hasAnyMeta` (local) | `rule/engine.go matchConnLocked` | is there any meta on this connection worth a pre-tmp pass? |

- missed in `hasAnyCriteria` → a route with only the new dimension vanishes from the table; in `HasCriteria` → a **last** such route is promoted to `fallback`; in `hasMetaCriteria` → `matchConnLocked` matches connections it should reject.
- **`hasMetaCriteria` vs `hasProcessCriteria` — do not conflate.** `hasProcessCriteria` (strict subset) backs `Engine.HasProcessCriteria` → `engine/engine.go gateProcessSearcher`; counting Port / Protocol / SrcCIDRs / NetworkMatch there charges **every user on a port- or protocol-only ruleset** a per-connection process lookup for nothing.
- **`match.region` opens the ProcessSearcher gate**: `engine.buildRouteEntries` expands it into a host entry over `bundle.Sets` *and* a meta entry over `bundle.Apps[GOOS]`. Smart-bypass pays per-connection attribution; plain global (`match.All`, zero entries) does not. `preset: "{cc}-access"` instead of `region: "{cc}"` keeps coverage but drops the app half — a bisection tool for `DIAG: process-lookup-slow`, not a free swap. macOS cost: `provider/CLAUDE.md` "Process Attribution".
- **Known debt — `hasAnyMeta` is narrower than the criteria it guards**: it counts `ProcessName` / `ProcessPath` / `PackageName` / `PackageNames` / `Network`, not `SrcIP`, `Port`, `Protocol`. Harmless today (it only gates the pre-tmp pass, and every production `MatchConn` via `Router.RouteConn` / `routeConn` sets `Network`); would bite a caller leaving `Network` empty. Left deliberately — widening it changes match ordering and needs its own review.
- **The structural guard** `rule/criteria_coverage_test.go` (reflection over `RouteEntry` / `MatchConfig`, written-reason exemption map) proves "some predicate sees this field", **not** "the right one". Misclassification is pinned by `TestGateProcessSearcher_PortProtocolOnly_DropsProcessSearcher` / `TestGateProcessSearcher_ProcessRule_KeepsProcessSearcher` (`engine/engine_test.go`); the local predicates `hasAnyCriteria` / `hasAnyMeta` only by example (`TestBuildRouteEntries_PortOnly` / `_ProtocolOnly`).

## tmp Pin Cache (DNS-learned per-IP pins)

`SetTmpRule(ip, target)` is the **only** bridge from `Match(domain)` to `Match(ip)` when exception routes exist; an optimization, never a source of truth.

- Gated on `len(e.routes) != 0`, **not** on `DisableSniff()` (zero exception routes = a pin can never change a decision).
- **Capacity `tmpRuleCap = 4096`; a full cache evicts exactly the least-recently-**pinned** entry.** `MatchConn` read hits do **not** refresh (that would need a write lock on the read path); the DNS pipeline re-pins on every upstream resolve and prefresh (`core/dns/middleware.go`); a cache hit does not pin. **The comment above `tmpRuleCap` in `rule/engine.go` is the single number of record** for memory cost (~1.07 MB at cap, ~2.1% of the iOS NE 50 MB budget) — size any cap change off it, do not restate a different figure.
- **`e.tmp`, `e.tmpOrder` (`*list.List`, Front = oldest), `e.tmpIndex` move together** — mutating one alone makes eviction a no-op `delete` and `e.tmp` grows past the cap silently. Write sites: `NewEngine` (constructs), `SetTmpRule` (`tmpLRUTouch`, `tmpLRUEvictOldest` at cap), `Close` (`tmpLRUReset()`), the last two under `e.mu`. Helpers in `tmp_lru.go` take no lock (`Engine.mu` is standalone).
- **`reload` (`Reload` / `ReloadWithClosers` / `ReloadNamed`) touches none of the three — pins survive a hot reload** (#3889): a DNS **cache hit returns without pinning** and apps hold answers for the TTL (min 10 min), so clearing routed resolved domains by IP alone for minutes to hours.

### DIAG

`rule-tmp-pin` (carries `evictedIP` when an add displaced an entry; duplicates suppressed — cache hits never pin), `rule-tmp-lru-desync`, `rule-tmp-reload` (`pinsKept`) — level, fields and the removed `rule-tmp-evict` are in `k2/docs/diag-events.md`, the lint-enforced registry. Don't re-list event fields here.

Eviction deliberately has no line of its own: once the cache is warm, LRU
eviction fires once per new pin, so a standalone event would be a 1:1 flood —
exactly what `rule-tmp-pin`'s duplicate suppression exists to avoid.

## Bundle Lifecycle (rule data, not matching)

- `ensure.go` — `EnsureBundles`: TTL-gated (`bundlesShortTTL` / `bundlesFreshTTL`), `flock_*.go` + singleflight. **Never on the connect path** (#3889): a singleflight joiner waits on the leader's ctx and held `engine.Start` up to 30s. Callers: background `PrefetchRules` and the updater only.
- `manifest.go` — k2-rules `dist/manifest.json` (schema v2); newest-wins by parsed RFC3339 time, never string compare.
- `downloader.go` — Happy Eyeballs race across `DefaultSources` (GitHub + jsdelivr/ghproxy).
- `embed.go` + `embed/krs.tar.gz` — embedded cold-start baseline (~66 KB placeholder; root `pre-build` / `fetch-rules-embed` overwrites it — see k2app-root `CLAUDE.md`, `EMBED_REQUIRE_FULL=1`). **Never commit the fetched ~2 MB file** (labtest `k2` suite + `scripts/check-embed-size.sh` fail above 300 KB); `k2/Makefile` targets do not fetch it.
- `updater.go` — post-connect check, then every `defaultCheckInterval` (1h): `refreshAndReload` → `ensureBundles` → `ReloadNamed` **iff** `BundleSignature` (`signature.go`) differs from what the engine serves (seeded by `WithLoadedSignature` in `engine.Start`) — "disk differs from engine", not "this call downloaded". An empty signature never replaces a working set.
- `loadnamed.go` (heap `*krs.Bundle`) vs `opennamed.go` (mmap `NamedDiskBundle`): **iOS NE must use `OpenNamed`** (`LoadNamed` puts ~19 MB on the dirty heap). `NewEngineWithClosers` / `ReloadWithClosers` munmap closers always run outside the state lock.
- `globs.go` — `SplitGlobs`: single-`*` patterns → exact/prefix; leading/middle `*` dropped with a WARN.
- `classify.go` — `ClassifyInstalled` uses the **same** `krs.MatchInstalled` codepath as connect-time routing, so UI badges and routing cannot disagree.
- `miss_reporter.go` — `SetMissReporter` (atomic pointer); `Report` **must not block** (DNS / L4 dial hot path).
