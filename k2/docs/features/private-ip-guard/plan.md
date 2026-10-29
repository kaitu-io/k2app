# Plan: Private IP Guard

## Meta

| Field | Value |
|-------|-------|
| Feature | private-ip-guard |
| Spec | docs/features/private-ip-guard/spec.md |
| Date | 2026-02-14 |
| Complexity | simple |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: TCP to private IP blocked | TestProxyHandler_HandleTCP_PrivateIP | T1 |
| AC2: UDP to private IP blocked | TestProxyHandler_HandleUDP_PrivateIP | T1 |
| AC3: Public IP works unchanged | TestProxyHandler_HandleTCP (existing) | T1 |
| AC4: Domain→private IP blocked | TestIsBlockedAddr_DomainToPrivate | T1 |

## Feature Tasks

### T1: Add private IP validation to ProxyHandler

**Scope**: Add `isReservedIP` and `checkAddr` functions to `handler.go`.
Gate `HandleTCP` and `HandleUDP` behind address validation before dialing.

**Files**:
- `server/handler.go` — add validation functions, gate HandleTCP/HandleUDP
- `server/handler_test.go` — add tests for blocked and allowed addresses

**Depends on**: none

**TDD**:

- RED: Write tests that verify:
  1. `TestIsReservedIP` — unit test for the IP checker:
     - `127.0.0.1` → blocked
     - `10.0.0.1` → blocked
     - `172.16.0.1` → blocked
     - `192.168.1.1` → blocked
     - `169.254.1.1` → blocked
     - `100.64.0.1` → blocked (CGN)
     - `::1` → blocked
     - `fe80::1` → blocked
     - `fc00::1` → blocked
     - `0.0.0.0` → blocked
     - `8.8.8.8` → allowed
     - `1.1.1.1` → allowed
     - `2001:4860:4860::8888` → allowed
  2. `TestProxyHandler_HandleTCP_PrivateIP` — HandleTCP with `addr="127.0.0.1:80"`
     should return error response via `wire.ReadStreamResponse`, never dial
  3. `TestProxyHandler_HandleUDP_PrivateIP` — HandleUDP with `addr="10.0.0.1:53"`
     should close conn without forwarding

- GREEN: Implement:
  1. `isReservedIP(ip net.IP) bool` — checks IsPrivate, IsLoopback,
     IsLinkLocalUnicast, IsUnspecified, plus manual CGN (100.64.0.0/10)
  2. `checkAddr(addr string) error` — SplitHostPort, ParseIP or LookupHost,
     check all resolved IPs with isReservedIP
  3. Add `checkAddr` call at top of `HandleTCP` (before `net.DialTimeout`)
  4. Add `checkAddr` call at top of `HandleUDP` (before `net.ResolveUDPAddr`)

- REFACTOR: Ensure error messages are clear for debugging. Keep functions
  minimal per project style.

**Acceptance**:
- All private IP ranges listed in spec are blocked (TCP + UDP)
- Public IP proxying works unchanged (existing tests still pass)
- Error response sent to client on block (TCP: via WriteStreamResponse, UDP: conn closed)

**Implementation notes**:
- Use stdlib `net.IP` methods where possible (IsPrivate, IsLoopback, etc.)
- CGN 100.64.0.0/10 not covered by stdlib — use `net.IPNet` with manual CIDR
- For domain resolution: `net.DefaultResolver.LookupHost` before dial
- TOCTOU between LookupHost and Dial is acceptable for this threat model
  (protecting against casual SSRF, not DNS rebinding attacks)
