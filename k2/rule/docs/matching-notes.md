# Rule matching — full notes (two-phase, protocol re-route, criteria predicates)

Moved **verbatim** from `rule/CLAUDE.md` (2026-11-14) to keep that always-loaded file short. The invariants stay summarised in `rule/CLAUDE.md`; this file is the full rationale, measurements and the tests that pin each rule. **Read this before changing `matchHostLocked` / `matchConnLocked` / `RouteConnProto` / `matchMeta`, the criteria predicates, a match dimension, or the sniff gates in `core/tunnel.go`.**

**Corrections vs code (2026-11-14)** — `hasAnyMeta` (`rule/engine.go` `matchConnLocked`) now counts `ProcessName`, `ProcessPath`, `PackageName`, `PackageNames` and `Network` (the text below lists three); it still excludes `SrcIP` / `Port` / `Protocol`, so the "Known debt" conclusion holds.

Note: the HEAD text contained a truncated sentence ("A rule like" with no continuation) in the Two-Phase section; it is kept as-is below.

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
