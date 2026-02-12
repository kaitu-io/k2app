# Task Splitting Patterns

Patterns for breaking features into mergeable incremental tasks.

## Foundation Tasks First (l4-architecture, 2026-02-12)

**Pattern**: Clean up blocking issues before building new functionality.

**Example**: F1 task deleted dead L3 code (tunnel.go, packet.go, direct.go) before implementing new L4 code.

**Reason**: Broken code prevents compilation. Cannot test incrementally if codebase doesn't build.

**Validation**: After F1, `go build ./...` succeeds. Tests pass. All following tasks build on clean slate.

**Anti-pattern**: Try to implement new L4 tunnel while old L3 tunnel exists → import conflicts, confusion about which is current.

---

## Parallel Independence (l4-architecture, 2026-02-12)

**Pattern**: Group tasks that have no inter-dependencies, execute in parallel.

**Example** (Group A after F1):
- T1: QUIC Dialer + Listener
- T2: TCP-WS Dialer + Listener
- T3: Provider interface redesign

All three can be implemented simultaneously. No task reads outputs from another.

**Validation**: Separate branches merged independently. No merge conflicts.

**Benefits**: Reduces wall-clock time, parallelizes work across team members.

---

## Interface-First Implementation (l4-architecture, 2026-02-12)

**Pattern**: Define and compile-check interfaces before implementing logic.

**Example**: T1/T2 started with:
```go
var _ wire.Dialer = (*QUICClient)(nil)
var _ wire.Listener = (*QUICServer)(nil)
```

Then implemented methods to satisfy interface.

**Validation**: Compile errors if interface contract broken during implementation.

**Benefits**:
- Early feedback (compile vs runtime)
- Clear contract (interface is self-documenting)
- Easy mocking (interface = test seam)

---

## Integration Last (l4-architecture, 2026-02-12)

**Pattern**: Implement components independently with mocks, integrate at end with E2E tests.

**Example**:
- T1-T5: Implement components (wire, provider, tunnel, DNS) with unit tests
- T6: Wire daemon to components (integration point)
- T8: E2E tests with full stack

**Validation**: Unit tests pass throughout. E2E tests validate integration.

**Benefits**:
- Catch bugs early (unit tests faster than E2E)
- Clear integration seam (T6 is the "glue")
- Debuggable (E2E failures isolated to integration, not component logic)

---

## Vertical Slice per Transport (l4-architecture, 2026-02-12)

**Pattern**: Implement full Dialer+Listener for one transport before starting another.

**Example**:
- T1: QUIC full implementation (DialTCP, DialUDP, AcceptTCP, ServeUDP)
- T2: TCP-WS full implementation (same methods)

Not: "T1a: all DialTCP implementations, T1b: all DialUDP implementations"

**Reason**: Transport is the integration boundary. QUIC Dialer+Listener must work together. Splitting across method types breaks feedback loop.

**Validation**: T1 includes integration_test.go with QUIC client ↔ server. T2 same for TCP-WS.

---

## Dependency Graph Visualization (l4-architecture, 2026-02-12)

**Pattern**: Draw task dependency graph before starting implementation. Use it to determine merge order.

**Example** (from plan.md):
```
F1 → (T1, T2, T3) → T4 → T6 → T8
                  ↘ T5 ↗   ↘
                    T7 ──────┘
```

Tasks on same row = parallel. Arrows = dependency.

**Usage**:
- Bottom row (F1): start immediately
- Second row (T1/T2/T3): start after F1 merges
- Third row (T4/T5/T7): start after respective dependencies
- Top row (T6/T8): start after all dependencies

**Validation**: No merge blocked waiting for unrelated task.

---

## Test-First for Interfaces, Test-During for Logic (l4-architecture, 2026-02-12)

**Pattern**: Write interface tests before implementation. Write logic tests during implementation.

**Example**:
- T1 RED phase: Write `TestQUICClient_DialTCP` (expects interface satisfied)
- T1 GREEN phase: Implement `QUICClient.DialTCP` until test passes
- T1 REFACTOR phase: Extract helpers, improve code

**Validation**: All tests pass at end of task. No untested code paths.

**Benefits**: TDD rhythm maintains quality. Interface tests = acceptance criteria.

---

## Incremental Merges, Not Long-Lived Branches (l4-architecture, 2026-02-12)

**Pattern**: Merge each task to master as soon as tests pass. Avoid accumulating 10+ commits on a branch.

**Example**: l4-architecture had 10 tasks → 10 merges (F1, T1-T8, k2rule upgrade).

Not: one giant PR with 50 files changed.

**Validation**: Each merge is <500 lines changed, reviewable in <30 min.

**Benefits**:
- Small diffs = easier review
- Fast feedback = catch errors early
- No rebase hell (branch diverges from master)

---

## Shared Test Fixtures (l4-architecture, 2026-02-12)

**Pattern**: Extract common test setup to `helpers_test.go` after 2-3 tests duplicate setup code.

**Example**: After T1/T2, noticed repeated wire.Config setup. Extracted `newTestWireConfig()` to `test/helpers_test.go`.

**Threshold**: Don't extract on first use. Wait for duplication (Rule of Three).

**Validation**: Tests remain readable. No "magic" hidden in helper.

**Anti-pattern**: Extract too early → abstraction obscures test intent.
