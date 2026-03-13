# Beta Channel Subscription — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable server-side beta channel subscription so admin can send targeted email notifications (especially iOS TestFlight invites) to beta users, and Android can switch update channels in-app.

**Architecture:** Add `beta_opted_in` field to User model, expose a PUT endpoint for toggle sync, extend EDM filtering. Android K2Plugin gets channel persistence + dynamic manifest endpoints. Webapp BetaChannelToggle becomes visible on all platforms with API sync. iOS gets TestFlight-specific copy.

**Tech Stack:** Go/Gin/GORM (API), Kotlin/JUnit (Android K2Plugin), TypeScript/React/Vitest (Webapp), Next.js (Web Admin), Swift (iOS — version comparison fix only)

**Design doc:** `docs/plans/2026-03-13-beta-channel-subscription.md`

**Dependency graph:**
```
Task 1 (API) ──────────┬──→ Task 3 (Webapp + Bridge)
Task 2 (Android K2Plugin) ─┘
Task 1 (API) ──────────────→ Task 4 (EDM Admin)
Task 5 (iOS version fix) — independent
```
Tasks 1, 2, 5 can run in parallel. Task 3 depends on 1+2. Task 4 depends on 1.

---

## Task 1: API Layer — Beta Channel Endpoint + EDM Filter

**Files:**
- Modify: `api/model.go:79` (User struct — add fields after PasswordLockedUntil)
- Modify: `api/type.go:84` (DataUser — add field)
- Modify: `api/type.go:625` (UserFilter — add field)
- Modify: `api/api_user.go:472-476` (buildDataUserWithDevice — add mapping)
- Modify: `api/api_user.go` (new handler function)
- Modify: `api/route.go:138` (add route after language route)
- Modify: `api/logic_email_task.go:128` (add beta filter before execute query)
- Test: `api/beta_channel_test.go` (new)

### Step 1: Write failing tests for beta channel endpoint

Create `api/beta_channel_test.go`:

```go
package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBetaChannel_OptIn(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	router, userID := setupAuthenticatedRouter(t)

	body, _ := json.Marshal(map[string]bool{"opted_in": true})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/user/beta-channel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, userID)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["code"])

	// Verify DB state
	var user User
	require.NoError(t, db.Get().Where(&User{ID: userID}).First(&user).Error)
	assert.True(t, user.BetaOptedIn != nil && *user.BetaOptedIn)
	assert.Greater(t, user.BetaOptedAt, int64(0))
}

func TestBetaChannel_OptOut_PreservesTimestamp(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	router, userID := setupAuthenticatedRouter(t)

	// First opt in
	body, _ := json.Marshal(map[string]bool{"opted_in": true})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/user/beta-channel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, userID)
	router.ServeHTTP(w, req)

	// Get the opted_at timestamp
	var user User
	db.Get().Where(&User{ID: userID}).First(&user)
	originalOptedAt := user.BetaOptedAt

	// Wait a moment then opt out
	time.Sleep(10 * time.Millisecond)
	body, _ = json.Marshal(map[string]bool{"opted_in": false})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/user/beta-channel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, userID)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	// Verify: opted_in false, but opted_at preserved
	db.Get().Where(&User{ID: userID}).First(&user)
	assert.False(t, user.BetaOptedIn != nil && *user.BetaOptedIn)
	assert.Equal(t, originalOptedAt, user.BetaOptedAt) // Timestamp preserved
}

func TestBetaChannel_UserInfo_IncludesBetaOptedIn(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	router, userID := setupAuthenticatedRouter(t)

	// Opt in first
	body, _ := json.Marshal(map[string]bool{"opted_in": true})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/user/beta-channel", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	addAuthHeader(req, userID)
	router.ServeHTTP(w, req)

	// Get user info
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/user/info", nil)
	addAuthHeader(req, userID)
	router.ServeHTTP(w, req)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]any)
	assert.Equal(t, true, data["betaOptedIn"])
}
```

> **Note**: The test helpers `setupAuthenticatedRouter` and `addAuthHeader` may not exist yet. If the existing test pattern uses a different approach (e.g., mock DB with `SetupMockDB(t)`), adapt to match. Check `api/login_flow_e2e_test.go` for the integration test pattern. The key assertions are what matters — the setup wrapper can be adjusted.

### Step 2: Run tests to verify they fail

```bash
cd api && go test -run TestBetaChannel -v ./...
```

Expected: FAIL — `User` has no field `BetaOptedIn`, handler doesn't exist, route not registered.

### Step 3: Add User model fields

Modify `api/model.go` — add after line 79 (after PasswordLockedUntil):

```go
	// Beta channel subscription
	BetaOptedIn *bool `gorm:"default:false"`          // 是否订阅 beta 更新通知
	BetaOptedAt int64 `gorm:"not null;default:0;index"` // 订阅时间戳（关闭时保留历史）
```

### Step 4: Add DataUser field

Modify `api/type.go` — add after line 83 (after Roles):

```go
	BetaOptedIn bool `json:"betaOptedIn"` // 是否订阅 beta
```

### Step 5: Add UserFilter field

Modify `api/type.go` — add after line 625 (after RetailerLevels, before closing `}`):

```go

	// Beta 订阅筛选
	BetaOptedIn *bool `json:"betaOptedIn,omitempty"` // true=仅beta用户, nil=不筛选
```

### Step 6: Update buildDataUserWithDevice

Modify `api/api_user.go` — add to the return struct in `buildDataUserWithDevice` (after line 474, Roles):

```go
		BetaOptedIn: user.BetaOptedIn != nil && *user.BetaOptedIn,
```

### Step 7: Add request type

Modify `api/type.go` — add after `UpdateLanguageRequest` (after line 634):

```go

// UpdateBetaChannelRequest 更新用户 beta channel 订阅状态
type UpdateBetaChannelRequest struct {
	OptedIn bool `json:"opted_in"`
}
```

### Step 8: Add handler function

Add to `api/api_user.go` (after `api_update_user_language` function):

```go
// api_update_user_beta_channel 更新用户 beta channel 订阅状态
func api_update_user_beta_channel(c *gin.Context) {
	userID := ReqUserID(c)

	var req UpdateBetaChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	updates := map[string]any{
		"beta_opted_in": req.OptedIn,
	}
	if req.OptedIn {
		updates["beta_opted_at"] = time.Now().Unix()
	}
	// Note: when opting out, beta_opted_at is NOT updated (preserves history)

	if err := db.Get().Model(&User{}).Where(&User{ID: userID}).Updates(updates).Error; err != nil {
		log.Errorf(c, "failed to update beta channel for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "failed to update beta channel")
		return
	}

	log.Infof(c, "user %d updated beta channel: opted_in=%v", userID, req.OptedIn)
	SuccessEmpty(c)
}
```

### Step 9: Register route

Modify `api/route.go` — add after line 138 (`user.PUT("/language", ...)`):

```go
			// 更新 beta channel 订阅状态
			user.PUT("/beta-channel", AuthRequired(), api_update_user_beta_channel)
```

### Step 10: Add EDM beta filter

Modify `api/logic_email_task.go` — add after line 128 (after retailer level filtering, before `// ==================== 执行查询 ====================`):

```go

	// ==================== Beta 订阅筛选（SQL层） ====================
	if filters.BetaOptedIn != nil && *filters.BetaOptedIn {
		query = query.Where("beta_opted_in = ?", true)
		log.Infof(ctx, "[EDM] Filtering by beta_opted_in = true")
	}
```

### Step 11: Run DB migration

```bash
cd api/cmd && go build -o kaitu-center . && ./kaitu-center migrate -c ../config.yml
```

GORM AutoMigrate will add the new columns automatically.

### Step 12: Run tests to verify they pass

```bash
cd api && go test -run TestBetaChannel -v ./...
```

Expected: PASS — all 3 tests green.

### Step 13: Run full test gate

```bash
cd api && go test ./...
```

Expected: All existing tests still pass.

### Step 14: Commit

```bash
git add api/model.go api/type.go api/api_user.go api/route.go api/logic_email_task.go api/beta_channel_test.go
git commit -m "feat(api): add beta channel subscription endpoint + EDM filter

- User model: add BetaOptedIn, BetaOptedAt fields
- PUT /api/user/beta-channel: toggle beta subscription
- DataUser: include betaOptedIn in /api/user/info response
- EDM: support betaOptedIn filter for targeted email campaigns
- Tests: integration tests for opt-in, opt-out, user info"
```

---

## Task 2: Android K2Plugin — Version Comparison Fix + Channel Support

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2PluginUtils.kt`
- Modify: `mobile/plugins/k2-plugin/android/src/test/java/io/kaitu/k2plugin/K2PluginUtilsTest.kt`
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`
- Modify: `mobile/plugins/k2-plugin/src/definitions.ts`

### Step 1: Write failing tests for version comparison with beta suffixes

Add to `mobile/plugins/k2-plugin/android/src/test/java/io/kaitu/k2plugin/K2PluginUtilsTest.kt` (after line 48, the `isNewerVersion_non_numeric_treated_as_zero` test):

```kotlin
    // ==================== isNewerVersion beta suffix ====================

    @Test
    fun isNewerVersion_stable_greater_than_same_beta() {
        // 0.5.0 (stable) > 0.5.0-beta.1
        assertTrue(K2PluginUtils.isNewerVersion("0.5.0", "0.5.0-beta.1"))
    }

    @Test
    fun isNewerVersion_beta_less_than_same_stable() {
        // 0.5.0-beta.1 < 0.5.0 (stable)
        assertFalse(K2PluginUtils.isNewerVersion("0.5.0-beta.1", "0.5.0"))
    }

    @Test
    fun isNewerVersion_beta_increment() {
        // 0.5.0-beta.2 > 0.5.0-beta.1
        assertTrue(K2PluginUtils.isNewerVersion("0.5.0-beta.2", "0.5.0-beta.1"))
    }

    @Test
    fun isNewerVersion_same_beta_equal() {
        assertFalse(K2PluginUtils.isNewerVersion("0.5.0-beta.1", "0.5.0-beta.1"))
    }

    @Test
    fun isNewerVersion_cross_version_beta_greater() {
        // 0.5.0-beta.1 > 0.4.0 (different major.minor)
        assertTrue(K2PluginUtils.isNewerVersion("0.5.0-beta.1", "0.4.0"))
    }

    @Test
    fun isNewerVersion_new_stable_greater_than_old_beta() {
        // 0.6.0 > 0.5.0-beta.1
        assertTrue(K2PluginUtils.isNewerVersion("0.6.0", "0.5.0-beta.1"))
    }
```

### Step 2: Run tests to verify beta suffix tests fail

```bash
cd mobile/plugins/k2-plugin && ./android/gradlew -p android test --tests "io.kaitu.k2plugin.K2PluginUtilsTest" 2>&1 | tail -20
```

If gradle wrapper is not available:
```bash
cd mobile/plugins/k2-plugin/android && ../../../android/gradlew test --tests "io.kaitu.k2plugin.K2PluginUtilsTest" 2>&1 | tail -20
```

Expected: FAIL — `isNewerVersion_stable_greater_than_same_beta` fails (returns false, expected true).

### Step 3: Fix isNewerVersion in K2PluginUtils.kt

Replace the `isNewerVersion` function in `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2PluginUtils.kt` (lines 7-18):

```kotlin
    fun isNewerVersion(remote: String, local: String): Boolean {
        val (rBase, rPre) = splitVersion(remote)
        val (lBase, lPre) = splitVersion(local)
        val baseCmp = compareSegments(rBase, lBase)
        if (baseCmp != 0) return baseCmp > 0
        // Same base version: stable (no pre-release) > beta (has pre-release)
        if (rPre == null && lPre != null) return true   // 0.5.0 > 0.5.0-beta.1
        if (rPre != null && lPre == null) return false   // 0.5.0-beta.1 < 0.5.0
        if (rPre == null && lPre == null) return false    // equal
        // Both have pre-release: compare numeric segments
        return compareSegments(
            rPre!!.split(".").map { it.toIntOrNull() ?: 0 },
            lPre!!.split(".").map { it.toIntOrNull() ?: 0 }
        ) > 0
    }

    internal fun splitVersion(v: String): Pair<List<Int>, String?> {
        val parts = v.split("-", limit = 2)
        val base = parts[0].split(".").map { it.toIntOrNull() ?: 0 }
        val pre = if (parts.size > 1) parts[1] else null
        return Pair(base, pre)
    }

    internal fun compareSegments(a: List<Int>, b: List<Int>): Int {
        val maxLen = maxOf(a.size, b.size)
        for (i in 0 until maxLen) {
            val av = a.getOrElse(i) { 0 }
            val bv = b.getOrElse(i) { 0 }
            if (av != bv) return av.compareTo(bv)
        }
        return 0
    }
```

### Step 4: Run tests to verify they pass

```bash
cd mobile/plugins/k2-plugin/android && ../../../android/gradlew test --tests "io.kaitu.k2plugin.K2PluginUtilsTest" 2>&1 | tail -20
```

Expected: PASS — all tests green including new beta suffix tests. Existing tests (`isNewerVersion_major_bump`, etc.) must still pass.

### Step 5: Write tests for manifest endpoint generation

Add to `K2PluginUtilsTest.kt`:

```kotlin
    // ==================== manifestEndpoints ====================

    @Test
    fun androidManifestEndpoints_stable() {
        val endpoints = K2PluginUtils.androidManifestEndpoints("stable")
        assertTrue(endpoints[0].endsWith("/android/latest.json"))
        assertTrue(endpoints[1].endsWith("/android/latest.json"))
        assertFalse(endpoints[0].contains("/beta/"))
    }

    @Test
    fun androidManifestEndpoints_beta() {
        val endpoints = K2PluginUtils.androidManifestEndpoints("beta")
        assertTrue(endpoints[0].endsWith("/android/beta/latest.json"))
        assertTrue(endpoints[1].endsWith("/android/beta/latest.json"))
    }

    @Test
    fun webManifestEndpoints_stable() {
        val endpoints = K2PluginUtils.webManifestEndpoints("stable")
        assertTrue(endpoints[0].endsWith("/web/latest.json"))
        assertFalse(endpoints[0].contains("/beta/"))
    }

    @Test
    fun webManifestEndpoints_beta() {
        val endpoints = K2PluginUtils.webManifestEndpoints("beta")
        assertTrue(endpoints[0].endsWith("/web/beta/latest.json"))
    }
```

### Step 6: Run tests to verify endpoint tests fail

Same gradle command. Expected: FAIL — `androidManifestEndpoints` method doesn't exist.

### Step 7: Add manifest endpoint functions to K2PluginUtils.kt

Add to `K2PluginUtils.kt` (after `compareSegments`):

```kotlin
    private const val CDN_PRIMARY = "https://d13jc1jqzlg4yt.cloudfront.net/kaitu"
    private const val CDN_FALLBACK = "https://d0.all7.cc/kaitu"

    fun androidManifestEndpoints(channel: String): List<String> {
        val prefix = if (channel == "beta") "beta/" else ""
        return listOf(
            "$CDN_PRIMARY/android/${prefix}latest.json",
            "$CDN_FALLBACK/android/${prefix}latest.json"
        )
    }

    fun webManifestEndpoints(channel: String): List<String> {
        val prefix = if (channel == "beta") "beta/" else ""
        return listOf(
            "$CDN_PRIMARY/web/${prefix}latest.json",
            "$CDN_FALLBACK/web/${prefix}latest.json"
        )
    }
```

### Step 8: Run tests to verify endpoint tests pass

Expected: PASS.

### Step 9: Add channel methods + update K2Plugin.kt

Modify `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`:

**9a.** Remove hardcoded manifest constants (lines 42-49) and replace references in `performAutoUpdateCheck()` with calls to `K2PluginUtils.androidManifestEndpoints(getChannel())` and `K2PluginUtils.webManifestEndpoints(getChannel())`.

**9b.** Add channel persistence methods:

```kotlin
    private fun getChannel(): String =
        context.getSharedPreferences("k2_prefs", Context.MODE_PRIVATE)
            .getString("update_channel", "stable") ?: "stable"

    private fun saveChannel(channel: String) =
        context.getSharedPreferences("k2_prefs", Context.MODE_PRIVATE)
            .edit().putString("update_channel", channel).apply()
```

**9c.** Add Capacitor plugin methods:

```kotlin
    @PluginMethod
    fun getUpdateChannel(call: PluginCall) {
        val ret = JSObject()
        ret.put("channel", getChannel())
        call.resolve(ret)
    }

    @PluginMethod
    fun setUpdateChannel(call: PluginCall) {
        val channel = call.getString("channel") ?: "stable"
        val oldChannel = getChannel()
        saveChannel(channel)

        val ret = JSObject()
        ret.put("channel", channel)
        call.resolve(ret)

        // beta→stable switch: trigger downgrade check with relaxed version comparison
        if (oldChannel == "beta" && channel == "stable") {
            performAutoUpdateCheck(forceDowngrade = true)
        } else {
            performAutoUpdateCheck()
        }
    }
```

**9d.** Update `performAutoUpdateCheck` signature to accept `forceDowngrade` parameter:

```kotlin
    private fun performAutoUpdateCheck(forceDowngrade: Boolean = false) {
        // ... existing code ...
        // Where it currently calls K2PluginUtils.isNewerVersion(remoteVersion, localVersion),
        // when forceDowngrade && localVersion.contains("-beta"):
        //   use (remoteVersion != localVersion) instead of isNewerVersion()
    }
```

### Step 10: Add TypeScript definitions

Modify `mobile/plugins/k2-plugin/src/definitions.ts` — add to `K2PluginInterface` (after `debugDump()` line):

```typescript
  getUpdateChannel(): Promise<{ channel: string }>;
  setUpdateChannel(options: { channel: string }): Promise<{ channel: string }>;
```

### Step 11: Rebuild K2Plugin dist

```bash
cd mobile/plugins/k2-plugin && npm run build
```

### Step 12: Run all K2Plugin tests

```bash
cd mobile/plugins/k2-plugin/android && ../../../android/gradlew test 2>&1 | tail -20
```

Expected: PASS.

### Step 13: Commit

```bash
git add mobile/plugins/k2-plugin/
git commit -m "feat(android): add beta channel support to K2Plugin

- Fix isNewerVersion to correctly handle -beta.N suffixes
- Add channel persistence via SharedPreferences
- Add getUpdateChannel/setUpdateChannel Capacitor methods
- Dynamic manifest endpoints based on channel (stable vs beta)
- Downgrade support: beta→stable triggers relaxed version check
- Tests: 6 new beta version comparison + 4 endpoint generation tests"
```

---

## Task 3: Webapp + Capacitor Bridge — BetaChannelToggle Refactor

**Files:**
- Modify: `webapp/src/components/BetaChannelToggle.tsx`
- Modify: `webapp/src/components/__tests__/BetaChannelToggle.test.tsx`
- Modify: `webapp/src/services/capacitor-k2.ts:167-187` (updater initialization)
- Modify: `webapp/src/services/api-types.ts` (DataUser interface)
- Modify: `webapp/src/i18n/locales/zh-CN/account.json`
- Modify: `webapp/src/i18n/locales/en-US/account.json`
- Modify: `webapp/src/i18n/locales/ja/account.json`
- Modify: `webapp/src/i18n/locales/zh-TW/account.json`
- Modify: `webapp/src/i18n/locales/zh-HK/account.json`
- Modify: `webapp/src/i18n/locales/en-AU/account.json`
- Modify: `webapp/src/i18n/locales/en-GB/account.json`

### Step 1: Write failing tests for new BetaChannelToggle behavior

Replace `webapp/src/components/__tests__/BetaChannelToggle.test.tsx` with updated tests. Keep existing mocks at top (lines 1-24), replace the describe block:

```typescript
// Add cloudApi mock at top level (after existing mocks)
const mockCloudApiRequest = vi.fn().mockResolvedValue({ code: 0 });
vi.mock('../../services/cloud-api', () => ({
  default: {
    request: (...args: any[]) => mockCloudApiRequest(...args),
  },
}));

// Mock useUser hook
const mockUseUser = vi.fn().mockReturnValue({ user: null, loading: false });
vi.mock('../../hooks/useUser', () => ({
  useUser: () => mockUseUser(),
}));

describe('BetaChannelToggle', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
    mockCloudApiRequest.mockResolvedValue({ code: 0 });
    mockUseUser.mockReturnValue({ user: { betaOptedIn: false }, loading: false });
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
    vi.clearAllMocks();
  });

  it('renders nothing when user is not logged in', () => {
    mockUseUser.mockReturnValue({ user: null, loading: false });
    (window as any)._platform = { os: 'macos' };

    const { container } = render(<BetaChannelToggle />);
    expect(container.innerHTML).toBe('');
  });

  it('renders toggle on desktop with setChannel', () => {
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('renders toggle on iOS without setChannel', () => {
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        // No setChannel
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('renders toggle on Android with setChannel', () => {
    (window as any)._platform = {
      os: 'android',
      updater: {
        channel: 'beta', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('shows iOS-specific description on iOS', () => {
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    // iOS should use descriptionIos key
    expect(screen.getByText('betaProgram.descriptionIos')).toBeDefined();
  });

  it('shows standard description on non-iOS', () => {
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByText('betaProgram.description')).toBeDefined();
  });

  it('calls setChannel AND API on desktop enable', async () => {
    const mockSetChannel = vi.fn().mockResolvedValue('beta');
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        setChannel: mockSetChannel,
      },
    };

    render(<BetaChannelToggle />);
    fireEvent.click(screen.getByRole('checkbox'));
    const confirmButton = screen.getAllByRole('button').find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('beta');
      expect(mockCloudApiRequest).toHaveBeenCalledWith('PUT', '/api/user/beta-channel', { opted_in: true });
    });
  });

  it('calls only API (no setChannel) on iOS enable', async () => {
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        // No setChannel
      },
    };

    render(<BetaChannelToggle />);
    fireEvent.click(screen.getByRole('checkbox'));
    const confirmButton = screen.getAllByRole('button').find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockCloudApiRequest).toHaveBeenCalledWith('PUT', '/api/user/beta-channel', { opted_in: true });
    });
  });

  it('API failure does not block local channel switch', async () => {
    mockCloudApiRequest.mockRejectedValue(new Error('network error'));
    const mockSetChannel = vi.fn().mockResolvedValue('beta');
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
        setChannel: mockSetChannel,
      },
    };

    render(<BetaChannelToggle />);
    fireEvent.click(screen.getByRole('checkbox'));
    const confirmButton = screen.getAllByRole('button').find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('beta');
    });
    // Toggle should still switch despite API failure
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });

  it('uses user.betaOptedIn for initial state on iOS (no updater.channel)', () => {
    mockUseUser.mockReturnValue({ user: { betaOptedIn: true }, loading: false });
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable', isUpdateReady: false, updateInfo: null,
        isChecking: false, error: null, applyUpdateNow: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });
});
```

### Step 2: Run tests to verify they fail

```bash
cd webapp && npx vitest run src/components/__tests__/BetaChannelToggle.test.tsx
```

Expected: FAIL — component still requires `setChannel` to render, no API sync, no iOS description.

### Step 3: Add DataUser.betaOptedIn

Modify `webapp/src/services/api-types.ts` — add to the `DataUser` interface (find the interface and add after existing fields):

```typescript
  betaOptedIn?: boolean;
```

### Step 4: Rewrite BetaChannelToggle.tsx

Replace `webapp/src/components/BetaChannelToggle.tsx`:

```typescript
import { useState, useCallback } from 'react';
import {
  ListItem,
  ListItemIcon,
  ListItemText,
  Typography,
  Switch,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  Button,
  CircularProgress,
  Divider,
} from '@mui/material';
import { Science as ScienceIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import cloudApi from '../services/cloud-api';
import { useUser } from '../hooks/useUser';

export default function BetaChannelToggle() {
  const { t } = useTranslation('account');
  const updater = window._platform?.updater;
  const platform = window._platform;
  const { user } = useUser();

  // Initial state: desktop/android use local updater.channel, iOS uses server-side betaOptedIn
  const getInitialBeta = () => {
    if (updater?.setChannel) return updater.channel === 'beta';
    return user?.betaOptedIn ?? false;
  };

  const [isBeta, setIsBeta] = useState(getInitialBeta);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [switching, setSwitching] = useState(false);

  // Don't render if user is not logged in
  if (!user) return null;

  const isIos = platform?.os === 'ios';
  const description = isIos ? t('betaProgram.descriptionIos') : t('betaProgram.description');

  const handleToggleClick = () => {
    setDialogOpen(true);
  };

  const handleConfirm = useCallback(async () => {
    const newBeta = !isBeta;
    const newChannel = newBeta ? 'beta' : 'stable';
    setSwitching(true);
    setDialogOpen(false);

    try {
      // Local channel switch (desktop + android only)
      if (updater?.setChannel) {
        await updater.setChannel(newChannel);
      }
      setIsBeta(newBeta);
    } catch (e) {
      console.error('[BetaToggle] Failed to switch channel:', e);
    } finally {
      setSwitching(false);
    }

    // API sync (all platforms, fire-and-forget)
    cloudApi.request('PUT', '/api/user/beta-channel', { opted_in: newBeta }).catch((e: any) => {
      console.warn('[BetaToggle] Failed to sync beta status to API:', e);
    });
  }, [isBeta, updater]);

  return (
    <>
      <Divider />
      <ListItem sx={{ py: 1.5 }}>
        <ListItemIcon>
          <ScienceIcon />
        </ListItemIcon>
        <ListItemText
          primary={
            <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
              {t('betaProgram.title')}
            </Typography>
          }
          secondary={
            <Typography variant="caption" color="text.secondary">
              {description}
            </Typography>
          }
        />
        {switching ? (
          <CircularProgress size={24} />
        ) : (
          <Switch
            checked={isBeta}
            onChange={handleToggleClick}
            color="warning"
          />
        )}
      </ListItem>

      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)}>
        <DialogTitle>
          {isBeta ? t('betaProgram.disableConfirm') : t('betaProgram.enableConfirm')}
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            {isBeta ? t('betaProgram.disableWarning') : t('betaProgram.enableWarning')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>
            {t('common:common.cancel', '取消')}
          </Button>
          <Button
            onClick={handleConfirm}
            color={isBeta ? 'primary' : 'warning'}
            variant="contained"
          >
            {isBeta ? t('betaProgram.disableConfirm') : t('betaProgram.enableConfirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
```

### Step 5: Add i18n keys (all 7 locales)

**zh-CN** (`webapp/src/i18n/locales/zh-CN/account.json`) — add after `"badge": "Beta"` line in betaProgram:

```json
    "descriptionIos": "提前体验新功能，帮助改进产品。开启后，新测试版本发布时你将收到邮件邀请加入 TestFlight 测试。"
```

**en-US** (`webapp/src/i18n/locales/en-US/account.json`):

```json
    "descriptionIos": "Get early access to new features and help improve the product. When enabled, you'll receive email invitations to join TestFlight testing when new beta versions are available."
```

**ja** (`webapp/src/i18n/locales/ja/account.json`):

```json
    "descriptionIos": "新機能をいち早く体験し、製品の改善にご協力ください。有効にすると、新しいベータ版がリリースされた際にTestFlightテストへの招待メールが届きます。"
```

**zh-TW** (`webapp/src/i18n/locales/zh-TW/account.json`):

```json
    "descriptionIos": "搶先體驗新功能，協助改進產品。開啟後，新測試版本發佈時你將收到郵件邀請加入 TestFlight 測試。"
```

**zh-HK** (`webapp/src/i18n/locales/zh-HK/account.json`):

```json
    "descriptionIos": "搶先體驗新功能，幫助改進產品。開啟後，新測試版本發佈時你將收到郵件邀請加入 TestFlight 測試。"
```

**en-AU** (`webapp/src/i18n/locales/en-AU/account.json`):

```json
    "descriptionIos": "Get early access to new features and help improve the product. When enabled, you'll receive email invitations to join TestFlight testing when new beta versions are available."
```

**en-GB** (`webapp/src/i18n/locales/en-GB/account.json`):

```json
    "descriptionIos": "Get early access to new features and help improve the product. When enabled, you'll receive email invitations to join TestFlight testing when new beta versions are available."
```

### Step 6: Update capacitor-k2.ts for Android channel support

Modify `webapp/src/services/capacitor-k2.ts` — replace the updater initialization block (lines 167-187) with:

```typescript
  const updater: IUpdater = {
    isUpdateReady: false,
    updateInfo: null,
    isChecking: false,
    error: null,
    channel: 'stable',
    applyUpdateNow: async () => {
      const currentPlatform = Capacitor.getPlatform();
      if (currentPlatform === 'android' && storedPath) {
        await K2Plugin.installNativeUpdate({ path: storedPath });
      } else if (currentPlatform === 'ios' && storedAppStoreUrl) {
        await Browser.open({ url: storedAppStoreUrl });
      }
    },
    onUpdateReady: (callback: (info: UpdateInfo) => void) => {
      updateReadyCallbacks.push(callback);
      return () => {
        updateReadyCallbacks = updateReadyCallbacks.filter(cb => cb !== callback);
      };
    },
  };

  // Android: initialize channel from native + provide setChannel
  if (Capacitor.getPlatform() === 'android') {
    try {
      const channelResult = await K2Plugin.getUpdateChannel();
      updater.channel = channelResult.channel as 'stable' | 'beta';
    } catch {
      // getUpdateChannel not available (old plugin version), default stable
    }
    updater.setChannel = async (channel: 'stable' | 'beta') => {
      await K2Plugin.setUpdateChannel({ channel });
      updater.channel = channel;
      return channel;
    };
  }
  // iOS: no setChannel — beta is API-only subscription
```

### Step 7: Run tests

```bash
cd webapp && npx vitest run src/components/__tests__/BetaChannelToggle.test.tsx
```

Expected: PASS.

### Step 8: Run full test gate

```bash
cd webapp && npx vitest run && npx tsc --noEmit
```

Expected: All tests pass, no type errors.

### Step 9: Commit

```bash
git add webapp/src/components/BetaChannelToggle.tsx webapp/src/components/__tests__/BetaChannelToggle.test.tsx webapp/src/services/capacitor-k2.ts webapp/src/services/api-types.ts webapp/src/i18n/locales/
git commit -m "feat(webapp): beta channel visible on all platforms with API sync

- BetaChannelToggle: visible when logged in (not just desktop)
- Desktop/Android: local setChannel + API sync
- iOS: API-only sync, no local channel switch
- iOS-specific description mentioning TestFlight email invite
- API failure does not block local channel switch
- Initial state from updater.channel (desktop/android) or user.betaOptedIn (iOS)
- capacitor-k2: Android setChannel via K2Plugin
- i18n: descriptionIos key added to all 7 locales"
```

---

## Task 4: EDM Admin — Beta User Filter

**Files:**
- Modify: `web/src/app/(manager)/manager/edm/create-task/page.tsx`

### Step 1: Write failing test for Beta filter checkbox

> **Note**: Check if `web/` has an existing test for the create-task page. If not, this may need a new test file. The key behavior to test: the filter form includes a Beta checkbox, and selecting it includes `betaOptedIn: true` in the preview request.

If tests exist for EDM pages, add a test. If not, create a minimal test:

```typescript
// web/src/app/(manager)/manager/edm/create-task/__tests__/page.test.tsx
import { describe, it, expect } from 'vitest';

describe('EDM Create Task - Beta Filter', () => {
  it('UserFilter type includes betaOptedIn field', () => {
    // Type-level test: verify the filter interface includes beta
    const filter = { betaOptedIn: true };
    expect(filter.betaOptedIn).toBe(true);
  });
});
```

> **Pragmatic note**: The EDM admin page is internal tooling. A type-level test + manual verification may be sufficient. The primary risk is the API contract, which is covered by Task 1 tests.

### Step 2: Add Beta checkbox to create-task filter form

Modify `web/src/app/(manager)/manager/edm/create-task/page.tsx` — add after the retailer levels section (after line ~776, before the closing of the filter form):

```tsx
{/* Beta 订阅筛选 */}
<div className="space-y-2">
  <h4 className="text-sm font-medium">Beta 测试用户</h4>
  <label className="flex items-center gap-2">
    <input
      type="checkbox"
      checked={formData.userFilters.betaOptedIn === true}
      onChange={(e) => {
        setFormData(prev => ({
          ...prev,
          userFilters: {
            ...prev.userFilters,
            betaOptedIn: e.target.checked ? true : undefined,
          },
        }));
      }}
    />
    <span className="text-sm">仅 Beta 订阅用户</span>
  </label>
</div>
```

### Step 3: Update UserFilter type (if defined locally in web/)

Check if `web/` has its own `UserFilter` type definition. If so, add `betaOptedIn?: boolean` to it. The API layer (Task 1) already accepts this field.

### Step 4: Run type check

```bash
cd web && npx tsc --noEmit
```

Expected: PASS.

### Step 5: Run web tests

```bash
cd web && yarn test
```

Expected: PASS.

### Step 6: Commit

```bash
git add web/src/app/\(manager\)/manager/edm/
git commit -m "feat(admin): add Beta user filter to EDM create task

- Checkbox to filter by beta_opted_in users
- Passes betaOptedIn in userFilters to preview + send APIs"
```

---

## Task 5: iOS Version Comparison Fix

**Files:**
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/K2Helpers.swift`

> **Note**: iOS has no JUnit-style unit test framework for K2Helpers. The fix is a direct port of the Kotlin fix. Verify correctness by matching the Android test cases mentally.

### Step 1: Fix isNewerVersion in K2Helpers.swift

Replace the `isNewerVersion` function in `mobile/plugins/k2-plugin/ios/Plugin/K2Helpers.swift` (lines 31-41):

```swift
/// Semantic version comparison: true if remote > local.
/// Handles -beta.N pre-release suffixes correctly.
func isNewerVersion(_ remote: String, than local: String) -> Bool {
    let (rBase, rPre) = splitVersion(remote)
    let (lBase, lPre) = splitVersion(local)
    let baseCmp = compareSegments(rBase, lBase)
    if baseCmp != 0 { return baseCmp > 0 }
    // Same base: stable (no pre-release) > beta (has pre-release)
    if rPre == nil && lPre != nil { return true }
    if rPre != nil && lPre == nil { return false }
    if rPre == nil && lPre == nil { return false }
    // Both have pre-release: compare segments
    let rPreSegs = rPre!.split(separator: ".").map { Int($0) ?? 0 }
    let lPreSegs = lPre!.split(separator: ".").map { Int($0) ?? 0 }
    return compareSegments(rPreSegs, lPreSegs) > 0
}

private func splitVersion(_ v: String) -> (base: [Int], pre: String?) {
    let parts = v.split(separator: "-", maxSplits: 1)
    let base = parts[0].split(separator: ".").map { Int($0) ?? 0 }
    let pre = parts.count > 1 ? String(parts[1]) : nil
    return (base, pre)
}

private func compareSegments(_ a: [Int], _ b: [Int]) -> Int {
    let maxLen = max(a.count, b.count)
    for i in 0..<maxLen {
        let av = i < a.count ? a[i] : 0
        let bv = i < b.count ? b[i] : 0
        if av != bv { return av < bv ? -1 : 1 }
    }
    return 0
}
```

### Step 2: Build to verify compilation

```bash
cd mobile/plugins/k2-plugin && npm run build
```

### Step 3: Commit

```bash
git add mobile/plugins/k2-plugin/ios/Plugin/K2Helpers.swift
git commit -m "fix(ios): handle -beta.N suffix in version comparison

- Port same fix from Android K2PluginUtils
- stable 0.5.0 correctly > beta 0.5.0-beta.1
- Prevents missed updates when comparing beta versions"
```

---

## Final Gate

After all tasks are complete, run the full gate:

```bash
cd api && go test ./...                                    # API tests
cd webapp && npx vitest run && npx tsc --noEmit           # Webapp tests + types
cd web && yarn test && npx tsc --noEmit                   # Web admin tests + types
cd mobile/plugins/k2-plugin/android && ../../../android/gradlew test  # Android plugin tests
```

All must pass before merging.
