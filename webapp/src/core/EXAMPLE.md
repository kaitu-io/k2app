# window._k2 使用示例

## 新架构概览

```typescript
window._k2 = {
  core: {
    exec(action, params) // VPN 控制
  },
  api: {
    exec(action, params) // API 调用
  },
  platform: {
    os, isDesktop, isMobile, version,
    openExternal, writeClipboard, ...
  }
}
```

## 使用示例

### 1. VPN 控制 (_k2.core)

```typescript
// 启动 VPN
await window._k2.core.exec('start');

// 停止 VPN
await window._k2.core.exec('stop');

// 获取状态
const status = await window._k2.core.exec('status');

// 获取配置
const config = await window._k2.core.exec('get_config');

// 设置配置
await window._k2.core.exec('set_config', { proxyRule: 'global' });

// 隧道管理
await window._k2.core.exec('add_self_hosted_tunnel', { name, url });
await window._k2.core.exec('update_self_hosted_tunnel', { id, name, url });
await window._k2.core.exec('remove_self_hosted_tunnel', { id });
const tunnels = await window._k2.core.exec('get_self_hosted_tunnels');
```

### 2. API 调用 (_k2.api)

```typescript
// 登录
await window._k2.api.exec('login', { email, code, remark });

// 登出
await window._k2.api.exec('logout');

// HTTP 请求
const user = await window._k2.api.exec('api_request', {
  method: 'GET',
  path: '/api/user/info'
});

const order = await window._k2.api.exec('api_request', {
  method: 'POST',
  path: '/api/user/orders',
  body: { planId: 'monthly' }
});
```

### 3. 平台能力 (_k2.platform)

```typescript
// 打开外部链接
await window._k2!.platform.openExternal?.('https://kaitu.io');

// 写入剪贴板
await window._k2!.platform.writeClipboard?.('text');

// 读取剪贴板
const text = await window._k2!.platform.readClipboard?.();

// 显示 Toast
await window._k2!.platform.showToast?.('消息', 'success');

// 同步语言
await window._k2!.platform.syncLocale?.('zh-CN');

// 获取平台信息
const os = window._k2!.platform.os; // 'windows' | 'macos' | ...
const isDesktop = window._k2!.platform.isDesktop;
const version = window._k2!.platform.version;
```

## 旧架构 vs 新架构

### 旧架构（已弃用）

```typescript
import { getKaituCore } from '../core';
import { usePlatform } from '../core';

// VPN 控制
const k2 = getKaituCore();
await k2.exec('start');

// 平台能力
const platform = usePlatform();
await platform.openExternal('https://kaitu.io');
```

### 新架构（当前）

```typescript
// 直接使用全局对象，无需 import

// VPN 控制
await window._k2.core.exec('start');

// API 调用
await window._k2.api.exec('api_request', { method: 'GET', path: '/api/user/info' });

// 平台能力
await window._k2!.platform.openExternal?.('https://kaitu.io');
```

## 优势

1. **更简单** - 无需 import，直接使用全局对象
2. **更清晰** - core / api / platform 分层明确
3. **更直接** - 减少封装层，降低复杂度
4. **更易调试** - 可以在浏览器控制台直接调用 `window._k2`

## 注意事项

- 使用 `window._k2!` (non-null assertion) 或 `window._k2?.` (optional chaining)
- platform 方法是可选的，使用 `?.` 调用
- 所有 exec 调用都返回 `Promise<SResponse<T>>`
