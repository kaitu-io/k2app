# Random UDID: 从硬件标识迁移到随机+存储

**Date:** 2026-03-22
**Status:** Approved
**Triggered by:** 用户 woshisikaozhe@gmail.com 反馈 Mac 设备反复被踢（ticket #56, #61）

## 问题

两台不同的 Mac 生成了相同的 UDID `f1d9bc2fba2ada911b1feba6aea64ad6`，导致设备在 user 5060 和 user 5063 之间反复转移。

**根因：** macOS `sysctl -n kern.uuid`（IOPlatformUUID）在某些场景下碰撞——VM 克隆、NVRAM 重置、Apple Silicon 去重等。所有平台的 UDID 均从硬件/OS API 实时读取，不做持久化，无法防御碰撞。

## 方案

**随机生成 + WebView localStorage 持久化。** 在 webapp 层统一生成 UUIDv4，通过 `_platform.storage`（AES-GCM 加密的 WebView localStorage）存储。不再依赖任何硬件 API。

### 云同步安全性

| 平台 | `_platform.storage` 底层 | 云同步？ |
|------|--------------------------|---------|
| macOS Tauri | `~/Library/WebKit/io.kaitu.app/` WebView 数据 | iCloud 不同步 |
| Windows Tauri | `%APPDATA%\io.kaitu.app\` WebView2 数据 | Microsoft 不同步 |
| Linux Tauri | `~/.local/share/io.kaitu.app/` | 无云同步 |
| iOS Capacitor | WKWebView 沙盒 localStorage | iCloud 不同步 |
| Android Capacitor | WebView 内部 localStorage | Google 不同步 |

Migration Assistant / Time Machine / iOS 备份恢复均不迁移 WebView 应用数据。**零云同步风险。**

### 碰撞概率

UUIDv4 = 122 bit 随机熵。即使 10 亿设备，碰撞概率 < 10^-19。

## 改动范围

### Layer 1 — webapp（核心）

#### 新增 `webapp/src/services/device-udid.ts`

```typescript
import type { ISecureStorage } from '../types/kaitu-core';

const STORAGE_KEY = 'device-udid';
let cachedUdid: string | null = null;

/**
 * Get or generate a persistent device UDID.
 *
 * First call: reads from _platform.storage.
 * If not found: generates crypto.randomUUID(), stores it, then returns SHA-256 hash.
 * Subsequent calls: returns cached value (no I/O).
 */
export async function getDeviceUdid(): Promise<string> {
  if (cachedUdid) return cachedUdid;

  const storage = window._platform?.storage;
  if (!storage) throw new Error('[DeviceUDID] Platform storage not available');

  let raw = await storage.get<string>(STORAGE_KEY);
  if (!raw) {
    raw = crypto.randomUUID();
    await storage.set(STORAGE_KEY, raw);
    // Migration guard: new UDID means old token is bound to old device_id.
    // Force re-login by clearing auth tokens.
    await clearStaleAuthTokens(storage);
  }

  cachedUdid = await hashToUdid(raw);
  return cachedUdid;
}

/** SHA-256 hash → first 16 bytes → 32 hex chars. Matches existing format. */
async function hashToUdid(raw: string): Promise<string> {
  const data = new TextEncoder().encode(raw);
  const hashBuffer = await crypto.subtle.digest('SHA-256', data);
  return Array.from(new Uint8Array(hashBuffer))
    .slice(0, 16)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

/**
 * Migration guard: clear auth tokens when UDID changes.
 * Old token has old device_id in JWT claims → wire auth would fail.
 * Better to show login screen than a cryptic VPN error.
 */
async function clearStaleAuthTokens(storage: ISecureStorage): Promise<void> {
  try {
    // Auth tokens stored by auth-service (see TOKEN_STORAGE_KEY, REFRESH_TOKEN_STORAGE_KEY)
    await storage.remove('k2.auth.token');
    await storage.remove('k2.auth.refresh');
    console.info('[DeviceUDID] New device UDID generated, cleared stale auth tokens');
  } catch {
    // Non-fatal: worst case user gets a VPN auth error and re-logs in manually
  }
}
```

#### 修改 `types/kaitu-core.ts`

从 `IPlatform` 接口移除 `getUdid()`:

```diff
  // ====== 核心能力 ======
  storage: ISecureStorage;
- getUdid(): Promise<string>;

  // ====== 跨平台能力 ======
```

#### 修改调用方（5 个文件）

所有 `window._platform.getUdid()` / `window._platform!.getUdid()` 改为：

```typescript
import { getDeviceUdid } from '../services/device-udid';
// ...
const udid = await getDeviceUdid();
```

| 文件 | 行 | 变更 |
|------|-----|------|
| `services/auth-service.ts` | 140-147 | `getUdid()` 方法改为调用 `getDeviceUdid()` |
| `components/LoginDialog.tsx` | 157 | `_platform.getUdid()` → `getDeviceUdid()` |
| `components/EmailLoginForm.tsx` | 171, 219 | 同上 |
| `pages/SubmitTicket.tsx` | 114 | 同上 |
| `services/beta-auto-upload.ts` | 23 | 同上 |
| `services/stats.ts` | 77 | 同上，同时清理 `getUdid failed` fallback 死代码（改用 `getDeviceUdid` 后不再需要 daemon 依赖的 fallback） |
| `debug.html` | 313 | `callPlatform('getUdid')` → 改为调用 `getDeviceUdid()` 或移除该按钮 |

#### 删除 bridge 层 getUdid

| 文件 | 变更 |
|------|------|
| `services/tauri-k2.ts` | 删除 `getUdid` 属性（~L226-231） |
| `services/capacitor-k2.ts` | 删除 `getUdid` 属性（~L225-228） |
| `services/standalone-k2.ts` | 删除 `getDaemonUdid()` 函数和 `getUdid` 属性 |

### Layer 2 — Rust log_upload（必须同步改）

**问题：** `log_upload.rs:667` 直接调用 `crate::service::get_hardware_uuid()` 构造 S3 路径。改了 webapp 后，S3 路径里的 UDID（硬件）≠ API 记录的 UDID（随机）→ 日志无法关联设备。

**修复：** `upload_service_log_command` 增加 `udid` 参数，从 webapp 传入。

```diff
// log_upload.rs
#[tauri::command]
pub async fn upload_service_log_command(
    params: UploadLogParams,
+   udid: String,
) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || {
-       let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());
        if params.reason == "beta-auto-upload" {
            upload_auto(&udid)
        } else {
            upload_service_log(params, udid)
        }
    })
    .await
    .map_err(|e| format!("Task failed: {}", e))
}
```

Tauri bridge 调用侧（`tauri-k2.ts` 的 `uploadLogs`）传入 UDID：

```typescript
import { getDeviceUdid } from '../services/device-udid';

uploadLogs: async (params) => {
  const udid = await getDeviceUdid();
  return invoke<UploadLogResult>('upload_service_log_command', { params, udid });
}
```

### Layer 3 — 原生代码清理（删除死代码）

以下代码不再被调用，安全删除：

| 文件 | 删除内容 |
|------|----------|
| `desktop/src-tauri/src/service.rs` | `hash_to_udid()`, `get_udid()`, `get_udid_native()`, `get_hardware_uuid()`, `get_raw_hardware_id()` 及相关测试 |
| `desktop/src-tauri/src/main.rs` | `service::get_udid` IPC 注册 |
| `desktop/src-tauri/src/ne.rs` | `get_udid_native()` 及其测试 |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | `getUDID()` 方法及 `hashToUdid()` |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.m` | `getUDID` ObjC 导出 |
| `mobile/plugins/k2-plugin/android/.../K2Plugin.kt` | `getUDID()` 方法及 `K2PluginUtils.hashToUdid()` |
| `mobile/plugins/k2-plugin/src/definitions.ts` | `getUDID` 接口定义 |
| `mobile/plugins/k2-plugin/src/web.ts` | `getUDID` web fallback |
| `mobile/plugins/k2-plugin/dist/` | 重新 build 生成 |

### 测试更新

所有 mock `_platform.getUdid` 的测试改为 mock `device-udid.ts` 模块：

```typescript
vi.mock('../services/device-udid', () => ({
  getDeviceUdid: vi.fn().mockResolvedValue('test-udid-abc123'),
}));
```

涉及文件：
- `services/__tests__/auth-service-v2.test.ts`
- `services/__tests__/stats.test.ts`
- `services/__tests__/tauri-k2.test.ts`
- `services/__tests__/standalone-k2-v2.test.ts`
- `services/__tests__/capacitor-k2.test.ts`
- `services/__tests__/web-platform-v2.test.ts`
- `stores/__tests__/config.store.test.ts`
- `stores/__tests__/self-hosted.store.test.ts`
- `stores/__tests__/onboarding.store.test.ts`
- `services/__tests__/consumer-migration.test.ts`
- `pages/__tests__/SubmitTicket.test.tsx`
- `types/__tests__/kaitu-core.test.ts`

## 不改的

| 组件 | 原因 |
|------|------|
| Web admin panel (`web/src/lib/udid.ts`) | 已经是 random + localStorage，保持不变 |
| Center API 服务端 | 不感知 UDID 来源，无需改动 |
| k2 Go 子模块 | Wire 协议从 JWT token 获取 device_id，不独立生成 UDID |
| Go daemon `/api/device/udid` | Standalone 模式不再调用此端点 |

## 发布策略

| 风险 | 处理 | 等级 |
|------|------|------|
| 全量用户重新登录 | 迁移守卫主动清 token → 用户看到登录页而非报错 | 可控 |
| 旧版本客户端兼容 | 旧版本继续用硬件 UDID，服务端不变 | 零影响 |
| 回滚 | 旧版本恢复硬件 UDID，新 random UDID 设备变孤儿（可手动清） | 可接受 |
| 灰度 | 不需要 — UDID 生成纯客户端逻辑，不影响服务端 | 直接全量 |
| S3 日志路径 | Rust 已改为接收 webapp UDID，路径一致 | 零影响 |

## 实施顺序

1. **webapp `device-udid.ts`** — 新模块，无破坏性
2. **Rust `log_upload.rs`** — 增加 udid 参数
3. **webapp 调用方迁移** — 5 个文件改 import
4. **`IPlatform` 接口** — 移除 `getUdid()`，bridge 删除实现
5. **测试全量更新** — mock 方式变更
6. **原生清理** — Rust/Swift/Kotlin 删除死代码
7. **Plugin rebuild** — `cd mobile/plugins/k2-plugin && npm run build`
8. **文档更新** — `webapp/CLAUDE.md` 移除 `getUdid()` 引用，更新架构图和 troubleshooting
9. **全量测试** — `cd webapp && yarn test` + `cd desktop/src-tauri && cargo test`

## 实施依赖

**Step 2（Rust log_upload）必须在 Step 6（原生清理）之前完成。** Step 6 删除 `get_hardware_uuid()`，而 Step 2 之前 `log_upload.rs` 仍在调用它。顺序错误会导致编译失败。
