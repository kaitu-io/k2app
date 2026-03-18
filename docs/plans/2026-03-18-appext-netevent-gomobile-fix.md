# appext NetEvent Gomobile Fix Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Define `NetEvent` in the `appext` package so gomobile bind `./appext/` exports it correctly to Android (`appext.NetEvent`) and iOS (`AppextNetEvent`), eliminating the cross-package `engine.NetEvent` reference that causes compile failures.

**Architecture:** Mirror `engine.NetEvent`'s 8 fields into a new `appext.NetEvent` struct (identical to the `EngineConfig` pattern). `NotifyNetEvent` converts from `*appext.NetEvent` to `*engine.NetEvent` before passing to the engine. Native code (Kotlin/Swift) updates their type references.

**Tech Stack:** Go (gomobile), Kotlin (Android), Swift (iOS)

---

## File Map

| Action | File | Change |
|--------|------|--------|
| Create | `k2/appext/net_event.go` | New `NetEvent` struct + `NewNetEvent()` constructor |
| Modify | `k2/appext/appext.go:276` | `NotifyNetEvent` parameter type + field conversion |
| Modify | `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt:26` | `import appext.NetEvent` |
| Modify | `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift:461,480` | `AppextNetEvent()` |

---

### Task 0: Pre-flight — find all `engine.NetEvent` references

**Files:** read-only scan

- [ ] **Step 1: Grep for all engine.NetEvent references**

```bash
grep -rn "engine\.NetEvent\|EngineNetEvent" k2/ mobile/ --include="*.go" --include="*.kt" --include="*.swift"
```

Expected references BEFORE the fix:
- `k2/engine/netmon.go` — struct definition (stays unchanged)
- `k2/engine/engine.go` — `OnNetEvent(*NetEvent)` internal usage (stays unchanged)
- `k2/appext/appext.go:276` — `NotifyNetEvent(event *engine.NetEvent)` (to be fixed in Task 2)
- `mobile/android/app/.../K2VpnService.kt:26` — `import engine.NetEvent` (to be fixed in Task 3)
- `mobile/ios/.../PacketTunnelProvider.swift:461` — `EngineNetEvent()` (to be fixed in Task 4)

If any other files appear, fix them too before proceeding.

---

### Task 1: Create `appext/net_event.go`

**Files:**
- Create: `k2/appext/net_event.go`

- [ ] **Step 1: Create the file**

```go
package appext

// NetEvent carries a platform network state change to the engine.
// Defined here (not in engine/) so gomobile bind ./appext/ exports it.
// gomobile generates: AppextNetEvent (iOS/ObjC), appext.NetEvent (Android/Java).
// Fields mirror engine.NetEvent exactly — keep in sync if engine.NetEvent changes.
type NetEvent struct {
	Signal         string // "available", "unavailable", "changed"
	InterfaceName  string // e.g. "en0", "pdp_ip0"
	InterfaceIndex int
	IsWifi         bool
	IsCellular     bool
	HasIPv4        bool
	HasIPv6        bool
	Source         string // e.g. "NWPathMonitor", "ConnectivityManager", "sing-tun"
}

// NewNetEvent creates a NetEvent with zero values.
// gomobile requires a constructor for Java/ObjC instantiation.
func NewNetEvent() *NetEvent {
	return &NetEvent{}
}
```

- [ ] **Step 2: Verify compilation**

Run from `k2/`:
```bash
go build ./appext/
```
Expected: no output (success).

---

### Task 2: Update `NotifyNetEvent` in `appext.go`

**Files:**
- Modify: `k2/appext/appext.go` around line 276

- [ ] **Step 1: Replace `NotifyNetEvent` signature and body**

Old:
```go
func (e *Engine) NotifyNetEvent(event *engine.NetEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in NotifyNetEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	e.inner.OnNetEvent(event)
}
```

New:
```go
func (e *Engine) NotifyNetEvent(event *NetEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("appext: panic in NotifyNetEvent", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	if event == nil {
		slog.Warn("appext: NotifyNetEvent called with nil event")
		return
	}
	e.inner.OnNetEvent(&engine.NetEvent{
		Signal:         event.Signal,
		InterfaceName:  event.InterfaceName,
		InterfaceIndex: event.InterfaceIndex,
		IsWifi:         event.IsWifi,
		IsCellular:     event.IsCellular,
		HasIPv4:        event.HasIPv4,
		HasIPv6:        event.HasIPv6,
		Source:         event.Source,
	})
}
```

Also update the comment above it — change `See engine.NetEvent for field documentation.` to `See NetEvent for field documentation.`

- [ ] **Step 2: Run go vet and tests**

Run from `k2/`:
```bash
go vet ./appext/
go test -short -race ./appext/
```
Expected: all pass, no errors.

- [ ] **Step 3: Commit Go changes**

```bash
cd k2
git add appext/net_event.go appext/appext.go
git commit -m "fix(appext): define NetEvent in appext package for gomobile export

gomobile bind ./appext/ does not export types from non-bound packages.
engine.NetEvent was invisible to the generated AAR/xcframework.
Mirror the struct in appext with a field conversion in NotifyNetEvent."
```

---

### Task 3: Update Android Kotlin

**Files:**
- Modify: `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt`

- [ ] **Step 1: Fix the import on line 26**

Old:
```kotlin
import engine.NetEvent
```

New:
```kotlin
import appext.NetEvent
```

No other changes needed — `NetEvent().apply { signal = ... }` etc. all stay identical because field names are the same.

- [ ] **Step 2: Verify Kotlin compiles locally (optional but recommended)**

If Android SDK is available:
```bash
cd mobile/android
./gradlew compileReleaseKotlin
```
Expected: `BUILD SUCCESSFUL`

If Android SDK not available locally, skip — CI will verify.

- [ ] **Step 3: Commit**

```bash
git add mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt
git commit -m "fix(android): import appext.NetEvent instead of engine.NetEvent

engine.NetEvent is not exported by gomobile bind ./appext/.
appext.NetEvent is the gomobile-compatible type defined in the bound package."
```

---

### Task 4: Update iOS Swift

**Files:**
- Modify: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`

- [ ] **Step 1: Replace `EngineNetEvent` with `AppextNetEvent`**

gomobile iOS naming: package `appext` + type `NetEvent` → `AppextNetEvent`.

`AppextNetEvent` is automatically available via `import K2Mobile` (the xcframework generated by `gomobile bind ./appext/`) — no additional import needed.

Two sites to change:

**Site 1** (around line 461, inside `startPathMonitor()`):
```swift
// Old:
let event = EngineNetEvent()
// New:
let event = AppextNetEvent()
```

**Site 2** — the method call on line 480 stays unchanged:
```swift
self.engine?.notifyNetEvent(event)
```
(Method name is generated from `NotifyNetEvent` → `notifyNetEvent`, unchanged.)

- [ ] **Step 2: Commit**

```bash
git add mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift
git commit -m "fix(ios): use AppextNetEvent() — gomobile name for appext.NetEvent

EngineNetEvent referenced engine.NetEvent which is not in the xcframework.
AppextNetEvent is generated by gomobile bind ./appext/ for the appext.NetEvent type."
```

---

### Task 5: Verify AAR contains `appext.NetEvent`

Goal: confirm gomobile actually generates the class.

- [ ] **Step 1: Run gomobile bind for Android locally** (requires Android NDK)

```bash
cd k2
make appext-android
```

- [ ] **Step 2: Inspect the generated AAR**

```bash
cd /tmp && mkdir -p aar_verify
cp k2/build/k2mobile.aar /tmp/aar_verify/
cd /tmp/aar_verify
unzip -o k2mobile.aar -d extracted > /dev/null
jar tf extracted/classes.jar | grep -i netevent
```

Expected output:
```
appext/NetEvent.class
```

Also verify `Engine.class` has `notifyNetEvent`:
```bash
jar xf extracted/classes.jar appext/Engine.class
javap appext/Engine.class | grep -i notify
```
Expected:
```
public native void notifyNetEvent(appext.NetEvent);
```

- [ ] **Step 3: Copy updated AAR into Android project**

```bash
cp k2/build/k2mobile.aar mobile/android/app/libs/k2mobile.aar
git add mobile/android/app/libs/k2mobile.aar
git commit -m "chore(android): update k2mobile.aar with appext.NetEvent"
```

> **Note:** If gomobile bind is not available locally, skip Steps 1-3. CI `build-mobile` workflow runs `make appext-android` before Gradle, so the fresh AAR is always used at build time. The committed AAR in `libs/` is only a fallback for local dev without gomobile installed.

---

### Task 6: Final verification

- [ ] **Step 1: Full Go test suite**

```bash
cd k2
go test -short -race ./...
```
Expected: all pass.

- [ ] **Step 2: Push k2 submodule and update k2app ref**

```bash
cd k2
git push origin master

cd ..
git add k2
git commit -m "chore: update k2 submodule — appext.NetEvent gomobile fix"
git push
```

- [ ] **Step 3: Monitor CI**

After push, watch GitHub Actions `Build Mobile` workflow:
- Android: `compileReleaseKotlin` must pass (no more "Unresolved reference: NetEvent")
- iOS: Certificate issue is separate — focus on build step, not signing

---

## Reference

**gomobile naming rules:**
- iOS/ObjC: `<PascalPackageName><TypeName>` → `appext.NetEvent` → `AppextNetEvent`
- Android/Java: `<package>.<TypeName>` → `appext.NetEvent` → `appext.NetEvent`

**Pattern reference:** `appext/config.go` — `EngineConfig` is the identical pattern (defined in appext, used by both iOS as `AppextEngineConfig` and Android as `appext.EngineConfig`).

**engine.NetEvent location:** `k2/engine/netmon.go` — if fields ever change there, update `appext/net_event.go` to match.
