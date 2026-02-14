# Testing Strategies

Patterns and lessons from test implementation.

---

## VpnClient Dependency Injection for Testing (2026-02-14, k2app-rewrite)

**Pattern**: Factory function with optional override parameter enables test injection without mocking modules.

**Implementation**:
```typescript
let instance: VpnClient | null = null;

export function createVpnClient(override?: VpnClient): VpnClient {
  if (override) { instance = override; return override; }
  if (!instance) instance = new HttpVpnClient();
  return instance;
}
```

**Test usage**:
```typescript
const mock = new MockVpnClient();
createVpnClient(mock);  // Inject test double
// Test code using getVpnClient() now gets the mock
```

**Benefits**:
- No module mocking required (avoids jest.mock complexity)
- Type-safe injection (override must implement VpnClient)
- Works with any test framework (vitest, jest, etc.)
- Production code has no test-specific logic (override is optional)

**Validating tests**:
- `webapp/src/vpn-client/__tests__/index.test.ts` — factory injection tests
- All store tests use this pattern to inject MockVpnClient

---

## State Deduplication Testing (2026-02-14, k2app-rewrite)

**Challenge**: Verify that polling loop does NOT emit events for unchanged state.

**Test approach**:
```typescript
test('subscribe deduplicates consecutive identical states', async () => {
  const events: VpnEvent[] = [];
  const unsubscribe = client.subscribe((e) => events.push(e));

  // Mock returns same state twice
  mockStatus({ state: 'connected' });
  await tick(2100);  // First poll

  mockStatus({ state: 'connected' });
  await tick(2100);  // Second poll, same state

  expect(events).toHaveLength(1);  // Only one event emitted
  unsubscribe();
});
```

**Key insight**:
- Use timer mocking (`vi.useFakeTimers()`) to control poll intervals
- Verify event array length instead of checking "what didn't happen"
- Test both positive (state change → event) and negative (no change → no event) cases

**Validating tests**:
- `webapp/src/vpn-client/__tests__/http-client.test.ts` — deduplication tests

---

## Integration Test Script Pattern (2026-02-14, k2app-rewrite)

**Pattern**: Bash scripts for end-to-end verification of build artifacts and runtime behavior.

**Examples**:
- `scripts/test_version_propagation.sh` — Verify version flows from package.json to all outputs
- `scripts/test_build.sh` (implied) — Verify binaries exist, signing valid, structure correct

**Structure**:
```bash
#!/usr/bin/env bash
set -euo pipefail

# Extract expected value
VERSION=$(node -p "require('./package.json').version")

# Trigger build step
make pre-build

# Verify output
ACTUAL=$(jq -r .version webapp/public/version.json)
test "$ACTUAL" = "$VERSION" || exit 1

echo "✓ Version propagation verified"
```

**Why scripts over unit tests**:
- Build system verification requires real file I/O and process execution
- Cross-platform checks (macOS codesign, Windows signtool) need native tools
- Scripts can be run in CI exactly as they run locally
- Fast feedback: exit on first failure (`set -e`)

**Best practices**:
- Use `set -euo pipefail` for strict error handling
- Print verification steps for CI logs
- Return non-zero exit code on failure (test command pattern)
- Verify artifacts exist before checking content

**Validating tests**:
- `scripts/test_version_propagation.sh` — runs in CI and locally

---

## Mock Client Observable State Pattern (2026-02-14, k2app-rewrite)

**Pattern**: Test double with setter methods to control returned values and track calls.

**Implementation**:
```typescript
export class MockVpnClient implements VpnClient {
  public connectCalls: string[] = [];
  private statusToReturn: VpnStatus = { state: 'stopped' };
  private listeners: Set<(event: VpnEvent) => void> = new Set();

  async connect(wireUrl: string): Promise<void> {
    this.connectCalls.push(wireUrl);
  }

  setStatus(status: VpnStatus): void {
    this.statusToReturn = status;
  }

  async getStatus(): Promise<VpnStatus> {
    return this.statusToReturn;
  }

  emitEvent(event: VpnEvent): void {
    for (const listener of this.listeners) {
      listener(event);
    }
  }
}
```

**Test usage**:
```typescript
const mock = new MockVpnClient();
mock.setStatus({ state: 'connected' });
store.connect('k2v5://example');

expect(mock.connectCalls).toEqual(['k2v5://example']);
```

**Benefits**:
- No mocking library needed (plain TypeScript class)
- State observable (public call tracking arrays)
- Controllable (setters for return values, manual event emission)
- Type-safe (implements VpnClient interface)

**Validating tests**:
- `webapp/src/vpn-client/__tests__/mock-client.test.ts` — MockVpnClient behavior tests
- All Zustand store tests use MockVpnClient

---

## Rust Service Module Testing (2026-02-14, k2app-rewrite)

**Pattern**: Unit tests embedded in service module verify version matching logic without requiring running daemon.

**Example**:
```rust
#[test]
fn test_versions_match_with_build_metadata() {
    assert!(versions_match("0.4.0", "0.4.0+abc123"));
    assert!(versions_match("0.4.0+x", "0.4.0+y"));
}
```

**Why**:
- Version matching is pure function (no I/O, no HTTP)
- Tests run fast (no daemon startup needed)
- Tests run in CI without daemon dependency
- Edge cases covered (empty string, build metadata, mismatches)

**Validating tests**:
- `desktop/src-tauri/src/service.rs` — `mod tests` block with 4 tests
- All tests pass in `cargo test`

---

## NativeVpnClient Constructor Injection Testing (2026-02-14, mobile-rewrite)

**Pattern**: NativeVpnClient takes K2Plugin as constructor parameter. Tests create a mock plugin object with `vi.fn()` methods — no module mocking needed.

**Mock factory**:
```typescript
function createMockPlugin() {
  return {
    checkReady: vi.fn(),
    getUDID: vi.fn(),
    getVersion: vi.fn(),
    getStatus: vi.fn(),
    getConfig: vi.fn(),
    connect: vi.fn(),
    disconnect: vi.fn(),
    addListener: vi.fn(),
  };
}
const plugin = createMockPlugin();
const client = new NativeVpnClient(plugin);
```

**Why constructor injection over vi.mock**:
- `vi.mock('k2-plugin')` doesn't work because `k2-plugin` is a Capacitor native module — not a normal npm package
- Constructor injection is simpler, type-safe, and portable across test frameworks
- Each test gets fresh mock (no shared module-level state to reset)

**Testing plugin events (subscribe)**:
```typescript
const stateChangeCall = plugin.addListener.mock.calls.find(
  (call) => call[0] === 'vpnStateChange'
);
const handler = stateChangeCall![1];
handler({ state: 'disconnected' });
expect(events).toEqual([{ type: 'state_change', state: 'stopped' }]);
```

**Key insight**: Plugin listener setup is async (returns Promise with handle), but the handler callback is sync. Tests can trigger handlers immediately after subscribe().

**Validating tests**:
- `webapp/src/vpn-client/__tests__/native-client.test.ts` — 18 tests using this pattern

---
