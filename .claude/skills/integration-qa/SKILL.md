---
name: integration-qa
description: Use when performing integration testing, regression testing, or release verification across platforms (Tauri desktop, Capacitor mobile, standalone web). Triggers on post-refactor validation, feature verification, error display audits, or when user says "测试", "回归", "QA", "regression", "verify features"
---

# Integration QA

Structured integration/regression testing across all k2app platforms.

**Core principle:** Discover ALL issues first, then fix systematically.
Finding a bug is not the end — document it, then KEEP TESTING.

**This skill orchestrates. It does NOT debug or fix.**
Fix delegation goes to existing skills:
- `superpowers:systematic-debugging` — root cause (Phase 1-4)
- `superpowers:test-driven-development` — fix with regression test
- `superpowers:verification-before-completion` — evidence before claims

## Test Matrix

Maintain `docs/test-matrix.md` as the single source of truth.

```markdown
| ID | Pri | Category | Test Case | Platform | Expected | Status | Notes |
|----|-----|----------|-----------|----------|----------|--------|-------|
```

**Priority:**
- **P0** — Release blocker. Connect, disconnect, error display.
- **P1** — Core. Lifecycle, recovery, specific error codes.
- **P2** — Edge. Network transitions, concurrent ops.

**Status values:** `PASS`, `FAIL:{description}`, `SKIP:{reason}`, `BLOCKED:{by}`

**Session start:** Read `git diff`, recent commits, CLAUDE.md changes → generate or update matrix.

## Platform Observation

### Tauri Desktop (MCP)

```
driver_session(action='start')           # Connect to running app
webview_screenshot()                     # Visual state
webview_dom_snapshot(type='accessibility')# UI tree
read_logs(source='console')              # JS console
webview_execute_js(script='...')         # Query app state
```

Daemon direct:
```bash
curl http://127.0.0.1:1777/ping
curl -X POST http://127.0.0.1:1777/api/core -d '{"action":"status"}'
sudo cat ~/Library/Logs/kaitu/k2.log     # macOS
type %LOCALAPPDATA%\kaitu\k2.log         # Windows
```

### Capacitor iOS

```bash
# Xcode console (build + run)
xcrun simctl spawn booted log stream --predicate 'subsystem == "io.kaitu"' --level debug
# App Group (NE error propagation)
xcrun simctl get_app_container booted io.kaitu.mobile data
```

### Capacitor Android

```bash
adb logcat -s K2Plugin:* K2Mobile:* capacitor:*
```

### Standalone Web

Open browser devtools → Console + Network tabs. VPN ops are mocked in `standalone-k2.ts`.

## Execution Flow

```
SCAN → FIX → RESCAN → (repeat until done)
```

### Phase 1: SCAN

Run ALL test cases at current priority level. For each case:

1. Set up precondition (e.g., app running, daemon up)
2. Execute action (via MCP tool, curl, or UI interaction)
3. Observe result (screenshot, logs, DOM state)
4. Compare against Expected
5. Record PASS or FAIL with evidence (screenshot path, log snippet)
6. **Move to next case — do NOT stop to fix**

After scanning all cases, produce a summary:
```
SCAN COMPLETE: 8 PASS, 3 FAIL, 1 SKIP
FAIL: T03 — error state shows raw message instead of i18n
FAIL: T05 — daemon kill not detected by SSE stream
FAIL: T09 — reconnecting state never reaches UI
```

### Phase 2: FIX

For each FAIL, in priority order:

1. **Invoke `superpowers:systematic-debugging`** with collected evidence
2. Find root cause (10/10 confidence before touching code)
3. **Invoke `superpowers:test-driven-development`** for the fix
4. **Invoke `superpowers:verification-before-completion`** to confirm

**Iteration cap:** Max 5 fix attempts per issue. If exceeded → mark `BLOCKED` and move on.

### Phase 3: RESCAN

Re-run ALL previously failed cases + spot-check 2-3 passed cases (regression check).
If new failures appear → add to FAIL list → return to Phase 2.

### Exit Condition

- All P0 cases PASS → proceed to P1 (if user wants)
- All target-priority cases PASS → session complete
- User decides to stop → update matrix with current state

## Session Protocol

**Start:**
1. Check if `docs/test-matrix.md` exists → load or generate
2. Read git diff for recent changes → identify affected areas
3. Determine target platform (ask user or detect from context)
4. Confirm priority scope (default: P0 first)

**End:**
1. Update `docs/test-matrix.md` with all statuses
2. Print summary: PASS/FAIL/SKIP/BLOCKED counts
3. List remaining FAIL/BLOCKED items for next session

## Red Flags

- Stopping at first failure to debug → **NO. Record and continue.**
- Fixing without root cause confidence → **Use systematic-debugging.**
- Claiming PASS without evidence → **Use verification-before-completion.**
- Fixing > 5 iterations on same bug → **Mark BLOCKED, move on.**
- Skipping rescan after fixes → **Regressions hide here.**
