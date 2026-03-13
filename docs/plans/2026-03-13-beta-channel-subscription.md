# Beta Channel 订阅 + 邮件通知

## 背景

当前 beta channel 是纯客户端机制——桌面端通过本地文件 (`update-channel`) 持久化，后端完全不知道哪些用户开了 beta。移动端没有 channel 切换能力。

**目标**：将 beta 从客户端行为提升为服务端可感知的用户属性，支持：
1. 手动定向邮件推送 beta 版本通知（尤其 iOS TestFlight 邀请）
2. Android 端 beta channel 切换 + app 内自动更新
3. iOS 端 beta 订阅 + 邮件通知（不做 app 内 beta 更新检测）

## 设计决策

| 决策 | 选项 | 理由 |
|------|------|------|
| 订阅粒度 | 统一（不分平台） | 简单，beta 用户本身就是少数 |
| 邮件触发 | 手动（admin dashboard） | 发布频率低，半自动增加复杂度无收益 |
| iOS beta 更新检测 | 不做，仅邮件 | 避免审核误解为绕过 App Store |
| Android beta 更新 | app 内检测 + 下载 APK | 和桌面端一致，Android 可直接安装 |
| 同步时机 | toggle 时一次性写入 | 明确的用户意图动作，不需间接推断 |

---

## 一、API 层

### 1.1 User model 新增字段

```go
// api/model.go — User struct
BetaOptedIn *bool  `json:"beta_opted_in" gorm:"default:false"`
BetaOptedAt int64  `json:"beta_opted_at" gorm:"not null;default:0;index"`
```

风格与现有 `IsActivated *bool` / `ActivatedAt int64` 一致。

### 1.2 新增端点

```
PUT /api/user/beta-channel
Body: { "opted_in": true }
Response: { "code": 0, "message": "ok" }
```

- 鉴权：需登录（Bearer token）
- `opted_in = true` → `beta_opted_in = true, beta_opted_at = now()`
- `opted_in = false` → `beta_opted_in = false`（`beta_opted_at` 保留历史）

### 1.3 EDM 筛选扩展

`userFilters` 新增 `betaOptedIn: boolean` 条件：
- 后端查询：`WHERE beta_opted_in = true`
- 与现有 `userStatus`、`expireDays`、`retailerLevels` 并列，可组合使用

### 1.4 DB migration

```sql
ALTER TABLE users ADD COLUMN beta_opted_in TINYINT(1) DEFAULT 0;
ALTER TABLE users ADD COLUMN beta_opted_at BIGINT NOT NULL DEFAULT 0;
CREATE INDEX idx_users_beta_opted_in ON users(beta_opted_in);
```

---

## 二、Webapp 层

### 2.1 BetaChannelToggle 改动

**可见性条件变更**：

```
旧：if (!updater?.setChannel) return null  // 只在桌面端显示
新：已登录即显示（所有平台）
```

**行为按平台分支**：

| 平台 | 本地 channel 切换 | API 同步 |
|------|-------------------|----------|
| 桌面 (有 `setChannel`) | `updater.setChannel(channel)` | `cloudApi.put('/api/user/beta-channel', { opted_in })` |
| Android (有 `setChannel`) | `updater.setChannel(channel)` | 同上 |
| iOS (无 `setChannel`) | 无 | 同上 |

API 调用失败不阻断本地 channel 切换（fire-and-forget + console.warn）。

**初始状态来源**：
- 桌面端：`updater.channel`（本地文件，立即可用）
- 移动端：从 API 获取用户的 `beta_opted_in` 状态作为初始值
- 方案：`GET /api/user/info`（webapp 已在 `useUser` hook 中调用）返回的 `DataUser` 追加 `beta_opted_in` 字段。BetaChannelToggle 从 `useUser()` 读取初始状态，无需额外请求。

### 2.2 i18n 文案

**修改 key**（account namespace）：

```jsonc
// 现有（不变）
"betaProgram.description": "提前体验新功能，帮助改进产品"

// 新增：iOS 专用描述
"betaProgram.descriptionIos": "提前体验新功能，帮助改进产品。开启后，新测试版本发布时你将收到邮件邀请加入 TestFlight 测试。"
```

组件内通过 `window._platform.os === 'ios'` 选择描述文案。

**所有 7 个 locale 都需要翻译 `betaProgram.descriptionIos`。**

---

## 三、Android K2Plugin

### 3.1 Channel 持久化

```kotlin
// SharedPreferences key: "update_channel", values: "stable" | "beta"
private fun getChannel(): String =
    context.getSharedPreferences("k2_prefs", Context.MODE_PRIVATE)
        .getString("update_channel", "stable") ?: "stable"

private fun saveChannel(channel: String) =
    context.getSharedPreferences("k2_prefs", Context.MODE_PRIVATE)
        .edit().putString("update_channel", channel).apply()
```

### 3.2 Manifest 端点动态化

```kotlin
// 旧：硬编码
private val ANDROID_MANIFEST_ENDPOINTS = listOf(
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/latest.json",
    "https://d0.all7.cc/kaitu/android/latest.json"
)

// 新：根据 channel 动态拼接
private fun androidManifestEndpoints(): List<String> {
    val prefix = if (getChannel() == "beta") "beta/" else ""
    return listOf(
        "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/${prefix}latest.json",
        "https://d0.all7.cc/kaitu/android/${prefix}latest.json"
    )
}

// Web OTA 同理
private fun webManifestEndpoints(): List<String> {
    val prefix = if (getChannel() == "beta") "beta/" else ""
    return listOf(
        "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/${prefix}latest.json",
        "https://d0.all7.cc/kaitu/web/${prefix}latest.json"
    )
}
```

### 3.3 新增 Capacitor 方法

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
    saveChannel(channel)
    val ret = JSObject()
    ret.put("channel", channel)
    call.resolve(ret)
    // 立即触发一次更新检查
    performAutoUpdateCheck()
}
```

### 3.4 版本比较修复（已确认需要）

**现有实现有 bug**：`K2PluginUtils.isNewerVersion()` 和 iOS `K2Helpers.isNewerVersion()` 均按 `.` 分割后 `toIntOrNull()`，无法正确处理 `-beta.N` 后缀。

**实测问题**：
```
isNewerVersion("0.5.0-beta.1", "0.4.0")
  → split: ["0", "5", "0-beta", "1"] → "0-beta".toIntOrNull() = null → 0
  → 比较 [0,5,0,1] vs [0,4,0] → true ✓（碰巧正确，因为 major.minor 已分出胜负）

isNewerVersion("0.5.0", "0.5.0-beta.1")
  → [0,5,0] vs [0,5,0,1] → 第4段 0 < 1 → false ✗
  → 应返回 true（stable 0.5.0 > beta 0.5.0-beta.1）
```

**修复方案**：先用 `-` 分割出 base version 和 pre-release，再比较：

```kotlin
// K2PluginUtils.kt
fun isNewerVersion(remote: String, local: String): Boolean {
    val (rBase, rPre) = splitVersion(remote)
    val (lBase, lPre) = splitVersion(local)
    val baseCmp = compareSegments(rBase, lBase)
    if (baseCmp != 0) return baseCmp > 0
    // 同 base version: stable (no pre-release) > beta (has pre-release)
    if (rPre == null && lPre != null) return true   // 0.5.0 > 0.5.0-beta.1
    if (rPre != null && lPre == null) return false   // 0.5.0-beta.1 < 0.5.0
    if (rPre == null && lPre == null) return false    // equal
    // 两者都有 pre-release: 按数字段比较
    return compareSegments(
        rPre!!.split(".").map { it.toIntOrNull() ?: 0 },
        lPre!!.split(".").map { it.toIntOrNull() ?: 0 }
    ) > 0
}

private fun splitVersion(v: String): Pair<List<Int>, String?> {
    val parts = v.split("-", limit = 2)
    val base = parts[0].split(".").map { it.toIntOrNull() ?: 0 }
    val pre = if (parts.size > 1) parts[1] else null
    return Pair(base, pre)
}

private fun compareSegments(a: List<Int>, b: List<Int>): Int {
    val maxLen = maxOf(a.size, b.size)
    for (i in 0 until maxLen) {
        val av = a.getOrElse(i) { 0 }
        val bv = b.getOrElse(i) { 0 }
        if (av != bv) return av.compareTo(bv)
    }
    return 0
}
```

**iOS 同步修复**（`K2Helpers.swift`）：相同逻辑的 Swift 版本。

**测试用例**（补充到 `K2PluginUtilsTest.kt`）：

| remote | local | 期望 | 场景 |
|--------|-------|------|------|
| `0.5.0` | `0.5.0-beta.1` | `true` | stable > 同版本 beta |
| `0.5.0-beta.1` | `0.5.0` | `false` | beta < 同版本 stable |
| `0.5.0-beta.2` | `0.5.0-beta.1` | `true` | beta 递增 |
| `0.5.0-beta.1` | `0.5.0-beta.1` | `false` | 相同 |
| `0.5.0-beta.1` | `0.4.0` | `true` | 跨版本 beta > 旧 stable |
| `0.6.0` | `0.5.0-beta.1` | `true` | 新 stable > 旧 beta |

### 3.5 降级处理（与桌面端一致）

Android 关闭 beta 后的降级逻辑与桌面端 `updater.rs` 保持一致：

- `setUpdateChannel("stable")` 被调用时，如果当前版本包含 `-beta`，立即触发一次更新检查
- 更新检查使用放宽的版本比较：`remote != current`（而非 `remote > current`），允许「降级」到 stable
- 用户看到更新提示 → 点击下载安装 stable 版本

```kotlin
@PluginMethod
fun setUpdateChannel(call: PluginCall) {
    val channel = call.getString("channel") ?: "stable"
    val oldChannel = getChannel()
    saveChannel(channel)

    val ret = JSObject()
    ret.put("channel", channel)
    call.resolve(ret)

    // beta→stable 切换：用放宽比较触发降级检查
    if (oldChannel == "beta" && channel == "stable") {
        performAutoUpdateCheck(forceDowngrade = true)
    } else {
        performAutoUpdateCheck()
    }
}
```

`performAutoUpdateCheck(forceDowngrade)` 中：当 `forceDowngrade = true` 且当前版本含 `-beta` 时，用 `remote != current` 替代 `isNewerVersion()`。

---

## 四、iOS K2Plugin

**不改动**。iOS manifest 端点始终查 stable（`ios/latest.json`）。不新增 `setChannel` / `getChannel` 方法。

---

## 五、Capacitor Bridge (capacitor-k2.ts)

### 5.1 Android 平台实现 setChannel

```typescript
// Android: 实现 setChannel
if (Capacitor.getPlatform() === 'android') {
    const channelResult = await K2Plugin.getUpdateChannel();
    updater.channel = channelResult.channel as 'stable' | 'beta';
    updater.setChannel = async (channel: 'stable' | 'beta') => {
        await K2Plugin.setUpdateChannel({ channel });
        updater.channel = channel;
        return channel;
    };
}
// iOS: 不实现 setChannel（updater.setChannel 保持 undefined）
```

### 5.2 K2Plugin definitions.ts 新增

```typescript
getUpdateChannel(): Promise<{ channel: string }>;
setUpdateChannel(options: { channel: string }): Promise<{ channel: string }>;
```

---

## 六、EDM Admin (web)

### 6.1 创建任务页面

`create-task/page.tsx` 的 `userFilters` 区域新增：

```
☐ Beta 测试用户
```

Checkbox，选中时 `userFilters.betaOptedIn = true`。与其他筛选条件（userStatus、expireDays 等）为 AND 关系。

Preview API 同步支持此筛选条件，实时显示匹配用户数。

---

## 七、改动范围汇总

| 层 | 文件 | 改动类型 |
|----|------|----------|
| API | `api/model.go` | User struct 加 `BetaOptedIn`, `BetaOptedAt` |
| API | 新文件或现有 handler | `PUT /api/user/beta-channel` 端点 |
| API | EDM 筛选查询 | `WHERE beta_opted_in = true` 条件 |
| DB | migration | `ALTER TABLE users` 加两列 + 索引 |
| Android | `K2Plugin.kt` | channel 持久化 + 动态 manifest + `setUpdateChannel`/`getUpdateChannel` |
| Mobile | `definitions.ts` | 新增方法签名 |
| Webapp | `capacitor-k2.ts` | Android 实现 `setChannel`，初始化读 channel |
| Webapp | `BetaChannelToggle.tsx` | 移动端可见 + API 同步 + iOS 文案 + 初始状态从 profile 读取 |
| Webapp | i18n `account.json` (×7 locales) | 新增 `betaProgram.descriptionIos` |
| Web Admin | `create-task/page.tsx` | 筛选条件加「Beta 测试用户」 |
| Web Admin | preview/send API | 透传 `betaOptedIn` 筛选 |

---

## 八、TDD 与 Test Gate

测试资源有限，必须用测试门控保证工程健壮性。每个模块改动必须先写测试再实现。

### 8.1 原则

- **Red-Green-Refactor**：先写失败测试，再写最小实现使其通过，最后重构
- **Test Gate**：PR 合并前所有测试必须通过，不允许跳过失败测试
- **每个改动点至少一个测试**：没有测试覆盖的代码不允许合并

### 8.2 各层测试计划

#### API 层 (`cd api && go test ./...`)

| 测试 | 类型 | 覆盖 |
|------|------|------|
| `PUT /api/user/beta-channel` opted_in=true | 集成 | 写入 DB + 返回 200 |
| `PUT /api/user/beta-channel` opted_in=false | 集成 | 关闭后 beta_opted_in=false，beta_opted_at 保留 |
| `PUT /api/user/beta-channel` 未登录 | 集成 | 返回 401 |
| EDM 筛选 betaOptedIn=true 查询 | 单元 | 只返回 beta 用户 |
| EDM 筛选 betaOptedIn + userStatus 组合 | 单元 | AND 逻辑正确 |

#### Webapp 层 (`cd webapp && yarn test`)

| 测试 | 类型 | 覆盖 |
|------|------|------|
| BetaChannelToggle 渲染：桌面端（有 setChannel） | 单元 | 显示 toggle + 标准描述 |
| BetaChannelToggle 渲染：iOS（无 setChannel） | 单元 | 显示 toggle + iOS 专用描述（TestFlight 文案） |
| BetaChannelToggle 渲染：Android（有 setChannel） | 单元 | 显示 toggle + 标准描述 |
| BetaChannelToggle 渲染：未登录 | 单元 | 不显示 |
| 开启 beta（桌面）：调 setChannel + 调 API | 单元 | 两个调用都执行 |
| 开启 beta（iOS）：只调 API | 单元 | 不调 setChannel，API 调用执行 |
| API 同步失败不阻断本地切换 | 单元 | setChannel 成功，API 失败，toggle 仍切换 |

#### Android K2Plugin

| 测试 | 类型 | 覆盖 |
|------|------|------|
| `getUpdateChannel` 默认返回 stable | 单元 | SharedPreferences 默认值 |
| `setUpdateChannel("beta")` 持久化 | 单元 | SharedPreferences 写入 |
| `androidManifestEndpoints()` stable 路径 | 单元 | 无 `beta/` 前缀 |
| `androidManifestEndpoints()` beta 路径 | 单元 | 有 `beta/` 前缀 |
| `webManifestEndpoints()` 跟随 channel | 单元 | Web OTA 端点也切换 |
| 版本比较：beta 后缀排序 | 单元 | `0.5.0 > 0.5.0-beta.1`, `beta.2 > beta.1` |

#### EDM Admin (`cd web && yarn test`)

| 测试 | 类型 | 覆盖 |
|------|------|------|
| 筛选表单渲染 Beta 选项 | 单元 | checkbox 存在且可点击 |
| 选中 Beta 后 preview 请求包含 betaOptedIn | 单元 | API 参数正确 |

### 8.3 Gate 规则

```
合并条件：
  ✅ cd api && go test ./...          通过
  ✅ cd webapp && yarn test           通过
  ✅ cd web && yarn test              通过
  ✅ webapp tsc --noEmit              通过
  ✅ web tsc --noEmit                 通过
  ✅ K2Plugin Android 单元测试         通过（如已有测试框架）
```

Android K2Plugin **已有 JUnit 测试框架**（`K2PluginUtilsTest.kt`，覆盖 `isNewerVersion`、`resolveDownloadURL`、`sha256`）。新增测试直接追加到现有测试文件即可。Channel 相关的纯逻辑（endpoint 拼接、版本比较）提取到 `K2PluginUtils.kt` 保持 JVM 可测。

### 8.4 实施节奏

每个 step 产出的代码必须附带对应测试，形成：

```
Step N:
  1. 写测试（红）
  2. 写实现（绿）
  3. 重构（保持绿）
  4. 运行 gate → 全部通过 → 提交
```

不允许「先全部实现，最后补测试」。

---

## 九、实施步骤

### Step 1: API 层（独立，无前端依赖）
- DB migration
- User model 字段
- `PUT /api/user/beta-channel` 端点
- EDM 筛选扩展
- **测试 gate**: `go test ./...`

### Step 2: Android K2Plugin（独立，无 webapp 依赖）
- Channel 持久化 (SharedPreferences)
- Manifest 端点动态化
- `getUpdateChannel` / `setUpdateChannel` 方法
- 版本比较修复（如需）
- `definitions.ts` 新增签名
- **测试 gate**: K2Plugin 单元测试

### Step 3: Webapp + Capacitor bridge（依赖 Step 1 + 2）
- `capacitor-k2.ts` Android 平台 `setChannel` 实现
- `BetaChannelToggle.tsx` 重构（移动端可见 + API 同步 + iOS 文案）
- i18n 文案（7 locales）
- **测试 gate**: `yarn test` + `tsc --noEmit`

### Step 4: EDM Admin（依赖 Step 1）
- `create-task/page.tsx` 加 Beta 筛选
- Preview API 透传
- **测试 gate**: `cd web && yarn test`

Step 1 和 Step 2 可并行。Step 3 依赖两者。Step 4 仅依赖 Step 1。
