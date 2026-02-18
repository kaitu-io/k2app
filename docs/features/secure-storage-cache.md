# Feature: Secure Storage & Cache

## Meta

| Field | Value |
|-------|-------|
| Feature | secure-storage-cache |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

k2app 的客户端数据持久化系统，由两个独立层组成：

1. **Secure Storage** — AES-256-GCM 加密的 localStorage 封装，用于存储敏感数据（auth tokens、设备标识等）。通过 `ISecureStorage` 接口定义，跨平台统一。
2. **Cache Store** — 双层（内存 + localStorage）通用缓存，用于 API 响应缓存（plans、user info、tunnels 等）。TTL 过期机制 + 命名空间隔离。

设计原则：

- **零用户交互** — 加密/解密全自动，不弹 keychain/密码/指纹授权
- **跨平台统一接口** — `ISecureStorage` 接口在 Tauri/Capacitor/Web 三端通用，当前全部使用 `webSecureStorage` 实现
- **分层职责** — Secure Storage 管敏感数据加密，Cache Store 管 API 数据缓存，互不耦合
- **前缀隔离** — 两套系统使用不同 localStorage 前缀（`_k2_secure_` vs `kaitu_cache:`），不会互相干扰

## Product Requirements

### Secure Storage 需求

| 需求 | 说明 |
|------|------|
| 敏感数据加密存储 | Auth tokens 不能以明文形式存在于 localStorage |
| 设备绑定密钥 | 加密密钥从设备指纹派生，不硬编码 |
| TTL 过期支持 | 存储条目可设置过期时间，过期自动清除 |
| 命名空间隔离 | 不同模块可创建独立的 storage 实例，前缀隔离 |
| 解密失败容错 | 数据损坏时自动清除该键，不阻塞应用 |

### Cache Store 需求

| 需求 | 说明 |
|------|------|
| API 响应缓存 | 减少重复网络请求，提升页面加载速度 |
| TTL 过期 | 不同数据类型使用不同过期时间（tunnels 10s、plans 300s、user 3600s） |
| 应用重启恢复 | localStorage 持久化，冷启动时从磁盘恢复缓存 |
| 过期 fallback | 网络不可用时可返回过期数据作为降级 |
| 登录/登出清空 | 切换账号时清空全部缓存，避免数据串台 |

### 存储的数据清单

#### Secure Storage 存储内容

| Key | 数据类型 | 来源 | 说明 |
|-----|----------|------|------|
| `k2.auth.token` | `string` | authService | Access token，每次 API 请求携带 |
| `k2.auth.refresh` | `string` | authService | Refresh token，用于 401 自动刷新 |

#### Cache Store 缓存内容

| Key | 数据类型 | TTL | 来源 |
|-----|----------|-----|------|
| `api:tunnels` | `TunnelListResponse` | 10s | CloudTunnelList — 线路列表 |
| `api:plans` | `{ items: Plan[] }` | 300s (5min) | Purchase — 套餐列表 |
| `api:app_config` | `AppConfig` | 600s-3600s | Purchase / useAppConfig — 应用配置 |
| `api:user_info` | `DataUser` | 3600s (1h) | useUser — 用户信息 |
| `api:members:{key}` | `ListResult<DataUser>` | 180s (3min) | MemberSelection — 成员列表 |

## Technical Decisions

### 1. AES-256-GCM + Web Crypto API

**决策**: 使用浏览器原生 Web Crypto API 的 AES-256-GCM 模式加密 localStorage 数据。

**原因**:
- Web Crypto API 是 W3C 标准，所有现代浏览器（含 WebView）均支持
- AES-256-GCM 提供认证加密（authenticated encryption），同时保证机密性和完整性
- 每次加密使用随机 12-byte IV，相同明文产生不同密文

**实现细节**:
- 密文格式：`base64(IV[12] + ciphertext)`，IV 和密文拼接后统一 base64 编码
- base64 编解码使用逐字节 `String.fromCharCode` / `charCodeAt` 遍历，避免 spread operator 在大数据上的兼容问题

### 2. 设备指纹派生密钥（非 PBKDF2）

**决策**: 使用设备浏览器特征拼接后 SHA-256 哈希作为 AES 密钥，不使用 PBKDF2。

**密钥派生流程**:
```
fingerprint = userAgent | language | screenW | screenH | colorDepth
            | timezoneOffset | hardwareConcurrency | "kaitu-secure-storage-v1"
           ↓ SHA-256
hashBuffer (32 bytes)
           ↓ importKey('raw', 'AES-GCM')
CryptoKey (不可导出)
```

**指纹组件** (7个特征 + 1个固定盐值):
- `navigator.userAgent` — 浏览器/WebView 标识
- `navigator.language` — 用户语言
- `screen.width` / `screen.height` — 屏幕分辨率
- `screen.colorDepth` — 色深
- `Date().getTimezoneOffset()` — 时区偏移
- `navigator.hardwareConcurrency` — CPU 核心数（fallback "unknown"）
- `"kaitu-secure-storage-v1"` — 固定应用盐值

**安全性权衡**:
- 不是"绝对安全" — 指纹可被逆向。但比明文 localStorage 安全数量级提升
- 攻击者需同时满足：物理访问设备 + 理解加密方案 + 逆向密钥派生
- 定位：保护敏感配置级别数据，不适用于高价值密钥（如私钥）
- 密钥缓存在内存中（`cachedKey` 模块变量），避免每次操作重复派生

**为什么不用 PBKDF2**:
- 设备指纹是确定性输入（非密码），不需要 KDF 抗暴力破解
- SHA-256 直接产出 32 bytes = AES-256 密钥长度，无需拉伸
- 性能更好，首次派生更快

### 3. webSecureStorage 全平台共用

**决策**: Tauri、Capacitor、Web standalone 三端 `_platform.storage` 均指向同一个 `webSecureStorage` 实例。

**原因**:
- ISecureStorage 接口设计了 `tauri-plugin-store`（Tauri）、`EncryptedSharedPreferences`（Android）、Swift 文件加密（iOS）等原生实现方案
- 实际上 WebView 的 localStorage 在三端都可用且够用
- 统一实现减少了原生桥接复杂度和跨端 bug 风险
- 未来可按平台替换为原生实现，接口不变

### 4. Cache Store 双层架构

**决策**: 内存 Map + localStorage 双层缓存，读优先内存、写同步两层。

**原因**:
- 内存层：零延迟读取，SPA 生命周期内高频数据（如 status polling）不触发 JSON 序列化
- localStorage 层：应用重启后冷启动恢复，避免所有 API 数据重新请求
- 读回退：内存未命中 → 尝试 localStorage → 反填内存缓存（promote）
- 写同步：`set()` 同时写两层，保证一致性

### 5. TTL 单位差异

**设计注意**:
- `CacheStore.ttl`: 单位为**秒** — `set('key', data, { ttl: 300 })` = 300 秒
- `ISecureStorage StorageOptions.ttl`: 单位为**毫秒** — `set('key', data, { ttl: 60000 })` = 60 秒
- 两个系统独立设计，使用方需注意单位

### 6. 过期数据 fallback 机制

**决策**: `CacheStore.get(key, allowExpired=true)` 可返回过期数据。

**场景**: 网络请求失败时，用过期的缓存数据渲染页面，优于显示空白/loading。典型用法见 `useUser.ts` — API 调用失败时尝试 `cacheStore.get('api:user_info', true)` 获取最后一次成功的数据。

### 7. 前缀隔离策略

| 系统 | 前缀 | 示例 localStorage key |
|------|------|----------------------|
| Secure Storage（默认） | `_k2_secure_` | `_k2_secure_k2.auth.token` |
| Secure Storage（自定义） | `_k2_{customPrefix}_` | `_k2_mymodule_somekey` |
| Cache Store | `kaitu_cache:` | `kaitu_cache:api:tunnels` |

`clear()` 操作只清除自己前缀的键，不影响其他系统的数据。

### 8. 登录/登出缓存清理

**决策**: 登录成功、登出、切换账号时调用 `cacheStore.clear()` 清空所有 API 缓存。

**触发点**:
- `LoginDialog` — 登录成功后
- `EmailLoginForm` — 登录成功后
- `cloudApi` — 检测到 auth 路径（login/register/logout）且成功时

**原因**: 不同账号的 user info、plans、tunnels 不同，必须清空避免数据串台。

### 9. StorageEntry 结构

Secure Storage 内部存储格式（加密前的 JSON）:

```typescript
interface StorageEntry<T> {
  value: T;         // 实际数据
  expiry?: number;  // 过期时间戳 (ms since epoch)，undefined = 永不过期
  createdAt: number; // 创建时间戳
}
```

Cache Store 内部存储格式（直接 JSON 写入 localStorage）:

```typescript
interface CacheEntry<T> {
  data: T;           // 实际数据
  expireAt?: number; // 过期时间戳 (ms since epoch)，undefined = 永不过期
}
```

### 10. `createSecureStorage()` 工厂函数

**决策**: 提供 `createSecureStorage(customPrefix)` 工厂函数，创建带自定义前缀的 ISecureStorage 实例。

**用途**: 模块级隔离 — 不同功能模块可拥有独立的加密存储空间，`clear()` 不会误删其他模块数据。当前代码中尚未被调用，但接口已就绪。

## Key Files

| 文件 | 职责 |
|------|------|
| `webapp/src/services/secure-storage.ts` | AES-256-GCM 加密存储实现。导出 `webSecureStorage`（默认实例）和 `createSecureStorage()`（工厂函数） |
| `webapp/src/services/cache-store.ts` | 双层缓存实现。导出 `CacheStore` class + `cacheStore` 单例 |
| `webapp/src/types/kaitu-core.ts` | 接口定义：`ISecureStorage`、`StorageOptions`、`IPlatform`（含 `storage` 字段） |
| `webapp/src/services/web-platform.ts` | Web standalone 平台实现，`storage` 字段指向 `webSecureStorage` |
| `webapp/src/services/auth-service.ts` | 使用 `_platform.storage` 管理 auth tokens（`k2.auth.token`、`k2.auth.refresh`） |
| `webapp/src/services/tauri-k2.ts` | Tauri 桥接，`_platform.storage` = `webSecureStorage` |
| `webapp/src/services/capacitor-k2.ts` | Capacitor 桥接，`_platform.storage` = `webSecureStorage` |
| `webapp/src/services/standalone-k2.ts` | Standalone 桥接，`_platform.storage` = `webSecureStorage` |
| `webapp/src/services/cloud-api.ts` | 登出时调用 `cacheStore.clear()` |
| `webapp/src/hooks/useUser.ts` | 缓存 user info，fallback 用过期数据 |
| `webapp/src/hooks/useAppConfig.ts` | 缓存 app config |
| `webapp/src/pages/Purchase.tsx` | 缓存 plans 和 app config |
| `webapp/src/components/CloudTunnelList.tsx` | 缓存 tunnel 列表（TTL 10s） |
| `webapp/src/components/MemberSelection.tsx` | 缓存成员列表 |
| `webapp/src/components/LoginDialog.tsx` | 登录成功后 `cacheStore.clear()` |
| `webapp/src/components/EmailLoginForm.tsx` | 登录成功后 `cacheStore.clear()` |
| `webapp/src/services/__tests__/cache-store.test.ts` | CacheStore 完整单元测试 |

## Acceptance Criteria

### Secure Storage

- [x] `webSecureStorage` 实现 `ISecureStorage` 全部 6 个方法（get/set/remove/has/clear/keys）
- [x] `set()` 后 localStorage 中只有 base64 密文，无明文 JSON
- [x] 同一 key 多次 `set()` 产生不同密文（随机 IV）
- [x] `get()` 能正确解密并返回原始数据，泛型类型安全
- [x] 设置 `ttl` 后，过期数据 `get()` 返回 `null` 并自动清除
- [x] 解密失败（数据损坏/密钥变化）不抛异常，返回 `null` 并清除该键
- [x] `clear()` 只删除 `_k2_secure_` 前缀的键，不影响 cache 数据
- [x] `keys()` 返回未过期的键列表（不含前缀）
- [x] `createSecureStorage(prefix)` 创建的实例使用独立前缀 `_k2_{prefix}_`
- [x] 加密密钥从设备指纹派生，不硬编码，`CryptoKey` 设置为不可导出
- [x] 密钥在模块内存中缓存（`cachedKey`），不重复派生

### Cache Store

- [x] `set()/get()` 类型安全，泛型参数正确推断
- [x] 设置 TTL 后，过期数据默认 `get()` 返回 `null`
- [x] `get(key, true)` 可返回过期数据用于 fallback
- [x] `persist: false` 时只写内存不写 localStorage
- [x] 冷启动（new CacheStore）能从 localStorage 恢复缓存
- [x] localStorage 恢复时自动 promote 到内存缓存
- [x] `clear()` 同时清空内存和 localStorage（仅 `kaitu_cache:` 前缀）
- [x] `clearExpired()` 清除所有过期条目（内存 + localStorage）
- [x] localStorage 损坏（JSON 解析失败）时返回 null 并清除坏数据
- [x] localStorage 写入失败（QuotaExceededError）时不影响内存缓存
- [x] 导出 `cacheStore` 单例，全应用共享
- [x] `isExpired()` / `has()` / `getEntry()` 辅助方法正常工作

### 集成

- [x] `authService` 通过 `_platform.storage`（= `webSecureStorage`）读写 tokens
- [x] Tauri / Capacitor / Standalone 三端 `_platform.storage` 均指向 `webSecureStorage`
- [x] 登录成功后 `cacheStore.clear()` 清空旧缓存
- [x] 登出后 `cacheStore.clear()` 清空用户数据缓存
- [x] API hooks（useUser、useAppConfig）优先读缓存，miss 时请求 API 并回填
- [x] 网络失败时 `useUser` 用 `allowExpired=true` 获取降级数据
- [x] CacheStore 单元测试覆盖：基本读写、TTL 过期、localStorage fallback、异常处理
