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

## Two-Phase Matching (design invariant, not an accident)

`Match(host)` runs at DNS time. Its whole scope — `matchHostLocked` — has **no
`ConnMeta` in it at all**: there is no connection yet, so there is no
destination port, no transport, no source IP, no process, no sniffed protocol.
It therefore skips every route that has no host criteria (`hasHostCriteria()`),
and for routes that do have them it checks *only* those.

`MatchConn(host, meta)` runs when the connection is dialed. `matchConnLocked`
checks host and meta criteria with **AND** semantics: a route matches when
`(no host criteria OR host matches) AND (no meta criteria OR meta matches)`.

Consequence worth internalizing: **`port` and `protocol` are connection-time
dimensions and cannot participate in a DNS-time decision.** A rule like
But **"cannot participate" is not "has no effect at DNS time"** — and for a
*combined* route the difference is dangerous.

At DNS time `matchHostLocked` does not skip a `{domain_suffix, port}` route; it
**discards the port condition and matches on host alone**, returning that
route's target. `core/dns/middleware.go` then uses that target for three
things: picking the upstream resolver (`queryUpstream`), short-circuiting to
**NXDOMAIN when the target is reject**, and calling `setTmpRule` to pin **every
resolved IP** to it. So:

- `{domain_suffix:["x"], port:["443"], via: reject}` NXDOMAINs **the entire
  domain**, not just port 443. The port narrowing you wrote is silently gone.
- `{domain_suffix:["x"], port:["27000-27100"], via: proxy}` pins **all** of that
  domain's IPs to the proxy at DNS time. At connection time the `e.tmp` hit
  returns before the route is ever reached, so the port condition **never takes
  effect in production** — the rule is overridden by a pin it planted itself.

(Since 2026-08 `engine.buildRouteEntries` emits a compile-time WARN for such
combined routes — `dnsDroppedMetaFields` — and `engine_combined_route_test.go`
pins these semantics. The matching behavior itself is unchanged.)

Mind the ordering too — it is the opposite of what "meta beats the pin cache"
suggests for *combined* routes. The pre-tmp pass in `matchConnLocked`
skips on `hasHostCriteria() || !hasMetaCriteria()`, i.e. it considers **purely
meta** routes only. A `{domain_suffix, port}` route compiles to a non-empty
`Sets`, so it has host criteria and is skipped there; if the destination IP was
already pinned at DNS time, the `e.tmp` hit returns before the main loop and
that route is never evaluated at all. Only a route with **no host criteria
whatsoever** (port-only, process-only, …) is guaranteed its say ahead of the
pin cache — that is the case the pre-tmp pass exists for, so per-connection
identity can supersede IP-level caching.

That pre-tmp pass is itself gated on a local `hasAnyMeta`, which is narrower
than the criteria it guards — see "Known debt" under the predicate family
below.

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
| `app_paths` | `ProcessPathPrefixes` in the SAME entry as `apps` (OR'd with the names — one "these applications" criterion). Desktop only: a process matches when its executable lives under the directory (macOS `.app` bundle, Windows install dir). Entries go through `NormalizeProcessDir` — trailing separator (component match, not string prefix), Windows lowercased, roots/relative dirs dropped. Compared against `ConnMeta.ProcessPath` (`provider.ProcessPathSearcher`) | meta (process) | conn |
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

`ProcessNamePrefixes` are pre-lowercased at compile time (`engine.lowerCopy`) so
the hot path lowercases the observed name once. `PackageNamePrefixes` are
case-preserved (Android package ids are case-sensitive). On Android the
observed side is a **set**: `ConnMeta.PackageNames` carries every package
sharing the connection's owning UID (Play Store + GMS share one via
sharedUserId — attribution is UID-granular), and the package group matches
when ANY of them hits; the single `PackageName` (mirroring `PackageNames[0]`)
remains the fallback for the legacy /proc path and desktop. Exact process-name
matching also accepts the 15-char kernel truncation of a longer rule name
(`commTruncLen`, Linux `/proc/PID/comm` and macOS `lsof`).

## Protocol Matching Participates Only After Sniffing

`Protocols` is the one dimension whose observed value does not exist at dial
time. `RouteConnProto` is its entry point, called from `core/tunnel.go` after
the first packet/bytes have been sniffed. It differs from `MatchConn` in three
ways, all deliberate:

1. It considers **only** routes with `len(Protocols) > 0`. Routes that never
   mention a protocol are untouched by the re-route.
2. It does **not** consult the tmp pin cache. Protocol is per-connection and
   post-sniff; an IP-keyed DNS pin has nothing to say about it.
3. It returns `(Target, bool)`. **Callers must check the bool** —
   `Target(0)` is `direct`, a perfectly legitimate result, so the zero value is
   not distinguishable from "no match" on its own.

### ⚠ The re-route crosses route priority

`RouteConnProto` scans **only** protocol-carrying routes and, on a hit,
overwrites the target that normal matching already produced. It does **not**
re-run the routing table. So a protocol rule **overrides any earlier route that
already matched, regardless of position** — first-match-wins does not hold
across this step.

Measured: with `[{network:"udp", via:direct}, {protocol:["quic"], via:proxy}]`,
a real QUIC Initial ends up on the **proxy** — the earlier, higher-priority
`direct` route loses to the later protocol rule.

This includes **App Bypass**: a process-based direct route that matched at dial
time can be undone by a protocol rule. Contrast the two SNI re-routes
(`core/tunnel.go` TCP and UDP), which call `Router.RouteConn` — the full table —
specifically so a process bypass survives; their comments say as much ("a
Chrome-bypass QUIC flow stays direct"). The protocol step deliberately does not
do that, and therefore does not inherit that protection.

Reachability today is narrow: STUN/DTLS/BT are only sniffed on proxy-bound
connections, so an already-direct route can only be overridden via **QUIC**,
whose detection sits outside that gate. Current real-world impact is **zero** —
no shipped config carries a `protocol:` rule.

**Before shipping any default `protocol:` rule, decide whether this override
semantics is intended.** Changing it is a design decision about what
`{protocol: ...}` means — whether it is a filter over the matched route or a
free-standing override — not a bug fix, and it needs its own review cycle.

### Other matching details

Full meta criteria still apply inside `RouteConnProto` (`matchMeta`), and host
criteria are applied when present via `matchRouteHost`. The call site passes the
**best host it knows**: the sniffed SNI domain when there is one, otherwise the
destination IP literal. Passing the raw IP unconditionally would make any
`{protocol, domain_suffix}` combination compare an IP against a domain matcher —
never a hit, and no log line to explain it.

Both transports do this: the UDP handler as step 6, the TCP handler as the last
step of its single-peek sniff block.

### A protocol rule can only fire if the protocol was sniffed

This is the real semantic boundary of the dimension, and it is easy to write a
rule that can never match. Sniffing is gated twice.

**Outer gate** — the first packet/bytes are peeked only when the connection has
a usable outbound (`t.outbound(action) != nil`, so a **reject**-routed
connection is never sniffed) **and** at least one sniff flag is on. The flag
set differs by transport:

- TCP: `sniffBT || sniffSNI`
- UDP: `sniffBT || sniffSNI || blockQUIC`

`SniffBT` and `SniffSNI` are both wired to `!ruleEngine.DisableSniff()`
(`engine/engine.go`), so in bulk-proxy / global mode both are false and **no
TCP protocol rule can fire**. UDP is **not** symmetric: `BlockQUIC` comes
straight from `client.BlockQUIC` and has nothing to do with `DisableSniff()`.
A global-mode config with `blockQUIC` on still peeks UDP, so a QUIC packet on a
connection that a preceding direct exception route sent direct still reaches
the re-route. Do not read "global mode" as "protocol rules are off".

**Inner gate (per protocol)**:

| Protocol | Transport | Additionally requires | So a rule cannot fire when |
|---|---|---|---|
| `bittorrent` | TCP + UDP | `sniffBT` **and** the connection was headed for the **proxy** (`action != directTarget`) | the connection was already routed direct |
| `stun`, `dtls` | UDP | same gate as above | the connection was already routed direct |
| `quic` | UDP | `meta.Protocol == ""` (not already classified STUN/DTLS/BT). **Not** subject to the proxy-bound precondition — but see the `blockQUIC` drop below | the first packet was classified STUN/DTLS/BT first, **or** `blockQUIC` is on and the connection was proxy-bound |

Concretely, for the three protocols behind the proxy-bound precondition:

- `{protocol:["stun"], via: direct}` **works** — the connection was proxy-bound,
  got sniffed, and the rule flips it to direct.
- `{protocol:["stun"], via: <some proxy>}` applied to a connection that was
  **already direct never fires** — that connection was never sniffed, so
  `ConnMeta.Protocol` stays empty and every route carrying `Protocols` is
  fail-closed against it.

And the QUIC-specific trap, which bites the most natural rule someone writes:

- `{protocol:["quic"], via: direct}` — "don't block it, just send it direct" —
  **never fires on a proxy-bound connection while `blockQUIC` is on**. The
  fast-path drop inside the QUIC detector (`if t.blockQUIC && action !=
  t.directTarget { return }`, and the second identical check after the SNI
  step) returns from the handler *before* the re-route runs. `blockQUIC`
  pre-empts exactly the rule meant to override it.

Do not "fix" that inner gate: it is what keeps WebRTC on the tunnel for #3051
and what keeps sniffing off the hot path for already-direct traffic.

TCP only ever produces `bittorrent` — it has no STUN/DTLS/QUIC detector.

### Post-sniff defaults when no protocol rule exists

Unchanged by the protocol dimension — a config with no `protocol:` rule behaves
exactly as before:

- **BitTorrent** (TCP peer handshake; UDP uTP / tracker) → forced **direct**.
- **STUN / DTLS** (UDP) → detected, `ConnMeta.Protocol` set, and **kept on the
  tunnel**.

The STUN/DTLS default is a support-ticket-backed decision, not an oversight.
See **ticket #3051**: forcing WebRTC direct sent Telegram / KakaoTalk call setup
(STUN/DTLS ICE) out the local interface, whose egress to a foreign reflector is
blocked behind the GFW — calls never connected while text (TCP, proxied) worked.
`core/voip_stun_regression_test.go` locks this down; do not "fix" STUN back to
direct.

## The criteria Predicate Family

Seven hand-written field enumerations answer four different questions. Adding a
match dimension means touching the ones that apply — and **every miss is
silent**.

| Predicate | Location | Question it answers |
|---|---|---|
| `MatchConfig.HasCriteria()` | `rule/target.go` | does this `match:` block constrain anything at all? |
| `MatchConfig.HasHostCriteria()` | `rule/target.go` | does it constrain host (domain/IP)? |
| `RouteEntry.hasHostCriteria()` | `rule/engine.go` | compiled-side host question — gates DNS-time eligibility |
| `RouteEntry.hasMetaCriteria()` | `rule/engine.go` | does it have any **connection-time** criteria? |
| `RouteEntry.hasProcessCriteria()` | `rule/engine.go` | does it need **process attribution**? |
| `hasAnyCriteria` (local) | `engine/engine.go buildRouteEntries` | is this compiled entry worth appending to the table? |
| `hasAnyMeta` (local) | `rule/engine.go matchConnLocked` | is there any meta on this connection worth a pre-tmp pass? |

Failure modes, all without an error or a log line:

- missed in `hasAnyCriteria` → a route whose only condition is the new dimension
  compiles fine, then the append gate drops it. It vanishes from the routing
  table.
- missed in `HasCriteria` → a **last** route carrying only that dimension is
  read as criteria-less and promoted to `fallback`, sending every unmatched
  connection out that route's outbound.
- missed in `hasMetaCriteria` → `matchConnLocked` treats the route as having
  nothing to check and matches connections it should have rejected.

### `hasMetaCriteria` vs `hasProcessCriteria` — do not conflate

They are not the same question and the cost of merging them is paid by every
user:

- `hasMetaCriteria` serves **matching**. Port / Protocol / SrcCIDRs /
  NetworkMatch belong here — they are connection-time criteria.
- `hasProcessCriteria` backs `Engine.HasProcessCriteria`, which
  `engine/engine.go gateProcessSearcher` uses to decide whether the tunnel gets
  a `ProcessSearcher` at all. Port, protocol, network and src-IP come from the
  packet/socket alone and never need process lookup. Counting them here keeps
  the gate open and charges **every user on a port- or protocol-only ruleset**
  (e.g. the default games/STUN rules) a per-connection lsof-equivalent for
  nothing.

`hasProcessCriteria` is a strict subset of `hasMetaCriteria`.

#### `match.region` opens the ProcessSearcher gate — this is not obvious

Read the table above literally and `region` looks like a host dimension. It is
not only that: `engine.buildRouteEntries` expands one `match.region` route into
**two** entries — a host entry over `bundle.Sets` *and* a meta entry over
`bundle.Apps[GOOS]` (the "domestic apps go direct" half). That second entry
carries process criteria, so **any config with a `match.region` route turns the
ProcessSearcher on for every connection**, exactly like a hand-written
`match.apps` rule would.

Consequences worth knowing before you debug a slow smart-bypass session:

- Smart-bypass (`{region: cn} → direct` + catch-all → proxy) pays
  per-connection process attribution; plain global (`{all: true}` only) does
  not — `match.All` breaks out of the loop with zero entries, so the gate stays
  shut. A user toggling between the two changes attribution cost, not just
  routing.
- Substituting `preset: "{cc}-access"` for `region: "{cc}"` keeps the same
  domain/IP coverage but drops the app half — and with it the searcher. That
  difference is a useful bisection tool (measured: identical routing, zero
  `DIAG: process-lookup-slow`), but it is **not** a free swap: you lose
  automatic direct-routing for domestic apps.
- On macOS the per-connection cost is a full `lsof` sweep unless the snapshot
  index absorbs it — see `provider/CLAUDE.md` "Process Attribution" for why the
  cache cannot help and what keeps it bounded.

### Known debt: `hasAnyMeta` is narrower than the criteria it guards

`matchConnLocked`'s local `hasAnyMeta` counts only `ProcessName` /
`PackageName` / `Network`. It does **not** count `SrcIP`, `Port`, or
`Protocol`, even though all three are meta criteria that `matchMeta` checks.

**Why it is harmless today**: `hasAnyMeta` only gates the *pre-tmp* pass —
being false costs a route its chance to be evaluated ahead of the DNS-learned
pin cache, it never causes a wrong match. And it is never false on a real
connection: every production `MatchConn` call goes through `Router.RouteConn`,
and all three `core/tunnel.go` call sites (initial `routeConn`, plus the TCP
and UDP SNI re-routes) pass a `ConnMeta` that `routeConn` built with `Network`
set to "tcp" or "udp". The protocol dimension is unaffected either way —
`RouteConnProto` does not go through `matchConnLocked` at all.

**When it would start to bite**: a `MatchConn` caller that leaves `Network`
empty while setting only `SrcIP`/`Port`. Then a meta-only route carrying just
that dimension would be skipped in the pre-tmp pass and a stale DNS pin could
win over it.

Left as-is deliberately — widening it is a semantic change to match ordering
and deserves its own review, not a drive-by edit. It is also the one member of
the family the reflection guard structurally cannot reach (a local variable,
and one that enumerates `ConnMeta` observations rather than `RouteEntry`
fields).

### The structural guard, and what it cannot see

`rule/criteria_coverage_test.go` walks `RouteEntry` and `MatchConfig` by
reflection and asserts every field is visible to the relevant predicate, with an
explicit exemption map (each entry carrying a written reason) for fields that
are not match dimensions.

It proves "no predicate sees this field". It **cannot** prove "this field is in
the *right* predicate" — a misclassification leaves both predicates seeing the
field and the guard green. Misclassification is covered instead by per-predicate
semantic tests: `engine/engine_test.go`'s
`TestGateProcessSearcher_PortProtocolOnly_DropsProcessSearcher` and
`TestGateProcessSearcher_ProcessRule_KeepsProcessSearcher` pin what
`hasProcessCriteria` is *for*. The two local-variable predicates
(`hasAnyCriteria`, `hasAnyMeta`) are not reachable by reflection at all; they are
pinned by example (`TestBuildRouteEntries_PortOnly` / `_ProtocolOnly`).

## tmp Pin Cache (DNS-learned per-IP pins)

`SetTmpRule(ip, target)` records the DNS-time decision for a resolved IP so the
later IP-routed connection reaches the same outbound. It is the **only** bridge
from `Match(domain)` to `Match(ip)` when exception routes exist.

- Gated on `len(e.routes) != 0`, **not** on `DisableSniff()` — those answer
  different questions. With zero exception routes every host takes the fallback
  and a pin can never change a decision, so pinning is skipped purely to bound
  the dirty heap.
- The cache is an optimization, never a source of truth: routes still match
  correctly without it.

**Capacity: `tmpRuleCap = 4096`, and a full cache evicts exactly the
least-recently-**pinned** entry** — not the whole table (the pre-LRU behavior).

Per-entry cost figures live in the comment above `tmpRuleCap` in
`rule/engine.go` (~274 B/entry across `tmp` + `tmpOrder` + `tmpIndex`, ~1.07 MB
resident at cap, vs ~349 KB for the old bare map — ~2.1% of the iOS NE 50 MB
jetsam budget). **That comment is the single number of record**; size any cap
change off it and do not restate a differently-measured figure elsewhere.

"Least recently **pinned**", not "least recently used": `MatchConn` read hits do
**not** refresh freshness, because refreshing on read would need a write lock on
the read path. The behaviors are equivalent in practice — the DNS pipeline
re-pins on every cache hit, so a live domain is continuously touched.

### The three structures move together

`e.tmp` (addr → Target), `e.tmpOrder` (`*list.List`, Front = oldest), and
`e.tmpIndex` (addr → `*list.Element`) are **one logical unit**. Clearing or
mutating one without the others leaves stale elements whose eviction becomes a
no-op `delete` — and `e.tmp` then grows past the cap silently. Four write
sites; the three mutating ones all run under `e.mu` held for writing, the
constructor runs before the engine is reachable by any other goroutine:

1. `NewEngine` — constructs all three (no lock: nothing else can see it yet).
2. `SetTmpRule` — `e.tmp[addr] = target` + `tmpLRUTouch`, plus
   `tmpLRUEvictOldest` at cap (which deletes from all three).
3. `Close` — `e.tmp = make(...)` + `tmpLRUReset()`.
4. `reload` — same pair (shared by `Reload` / `ReloadWithClosers` /
   `ReloadNamed`).

Helpers live in `tmp_lru.go`; none of them take a lock of their own, by design
(`Engine.mu` is standalone and never nested inside another lock).

### DIAG

- `DIAG: rule-tmp-pin` (INFO) — first pin of an IP, or target change on an
  already-pinned IP. Duplicates are suppressed (the DNS pipeline re-pins on
  every cache hit). Carries `evictedIP` when that add displaced an entry.
- `DIAG: rule-tmp-lru-desync` (WARN) — at cap but eviction found nothing to
  evict. Unreachable while the three structures stay in lockstep; it exists so
  the desync failure mode is not silent.
- `DIAG: rule-tmp-reload` (INFO) — reload cleared the cache.
- `DIAG: rule-tmp-evict` — **removed**. Eviction now rides along as
  `rule-tmp-pin`'s `evictedIP` field. Grepping production logs for the old name
  returns nothing.

Eviction deliberately has no line of its own: once the cache is warm, LRU
eviction fires once per new pin, so a standalone event would be a 1:1 flood —
exactly what `rule-tmp-pin`'s duplicate suppression exists to avoid.

## Bundle Lifecycle (rule data, not matching)

- `ensure.go` — `EnsureBundles`: TTL-gated refresh (`bundlesShortTTL` fast path,
  `bundlesFreshTTL` hard backstop), cross-process serialized by `flock_*.go`,
  in-process by singleflight.
- `manifest.go` — parses k2-rules `dist/manifest.json` (schema v2); newest-wins
  by parsed RFC3339 time, never by string compare.
- `downloader.go` — Happy Eyeballs race across `DefaultSources` (GitHub origin +
  jsdelivr/ghproxy mirrors for CN resilience).
- `embed.go` + `embed/krs.tar.gz` — build-time embedded baseline (`//go:embed`),
  extracted on true cold start (offline, censorship-proof). The committed
  archive is a small baseline placeholder; a release build is expected to
  replace it with the full production archive beforehand, but **no target in
  this repo's Makefile does that** — the refresh is out-of-band. (An earlier
  comment named a `make fetch-rules-embed` target; no such target exists.)
- `updater.go` — background re-check tick; hot-reloads via `ReloadNamed`.
- `loadnamed.go` (heap `*krs.Bundle`) vs `opennamed.go` (mmap `NamedDiskBundle`).
  The iOS NE path **must** use `OpenNamed`: `LoadNamed` expands the full corpus
  onto the dirty heap (~19 MB); mmap keeps resident dirty heap in the tens of KB.
  `NewEngineWithClosers` / `ReloadWithClosers` carry the munmap closers, always
  run outside the state lock.
- `globs.go` — `SplitGlobs` partitions single-`*` krs patterns into
  exact/prefix pairs; unsupported globs (leading/middle `*`) are dropped with a
  WARN.
- `classify.go` — `ClassifyInstalled` answers "which installed apps default to
  direct for region X" through the **same** `krs.MatchInstalled` codepath the
  engine uses at connect time, so UI badges and routing cannot disagree.
- `miss_reporter.go` — optional rule-miss telemetry sink. Installed via
  `SetMissReporter` (atomic pointer; hot path pays one nil-check when unset).
  `Report` **must not block** — it is called from the DNS / L4 dial hot path.
