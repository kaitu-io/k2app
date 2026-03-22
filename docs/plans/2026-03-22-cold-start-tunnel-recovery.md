# Cold Start Tunnel Identity Recovery

## Problem

iOS/Android/Desktop 上，VPN 后端进程（NE/VpnService/daemon）独立于 app 进程运行。当 app 进程被系统回收后冷启动，VPN 保持连接但 webapp 丢失 tunnel 身份信息。

**表现：**
1. 连接按钮显示 connected 但无国旗
2. Cloud tunnel list 中没有选中正在使用的 tunnel

**Root cause：** `connection.store.ts` 的 `connectedTunnel`（ActiveTunnel，含 domain/name/country）仅存于 Zustand 内存，无持久化。冷启动后 store 重置为 null，但 config store 已持久化了 server URL（含 tunnel domain），VPN machine 也能通过 IPC 恢复连接状态。两个恢复源都在，但 connection store 没有利用它们。

**附带问题（Android）：** Always-on VPN 系统重启时，`K2VpnService.onStartCommand()` 收到 null Intent 直接 `stopSelf()`，没有从 SharedPreferences 读取已保存的 configJSON，导致 VPN 无法恢复连接。

## Data Sources

冷启动时各平台已有的持久化数据：

| 数据 | 存储位置 | 加载时机 |
|------|---------|---------|
| VPN 连接状态 | NE/daemon/VpnService 进程内 | `initializeVPNMachine()` → `_k2.run('status')` |
| Server URL | `_platform.storage` key `k2.vpn.config` | `config.loadConfig()` → `config.server` |
| Self-hosted tunnel | `_platform.storage` key `k2.self_hosted.tunnel` | `selfHosted.loadTunnel()` → `{uri, name, country}` |
| Cloud tunnel list | API `/api/tunnels/k2v4` + cacheStore | CloudTunnelList 组件 mount 后 |
| Android configJSON | SharedPreferences `k2vpn.configJSON` | K2Plugin.connect() 时写入 |

**关键：** server URL 格式为 `k2v5://udid:token@{domain}:port?...`，其 host 就是 `tunnel.domain`（数据库唯一索引）。可从 URL 反推 domain，再从 tunnel list 匹配完整信息。

## Solution

### Part 1: Webapp Tunnel Identity Recovery（全平台）

**改动文件：**
- `webapp/src/stores/connection.store.ts`
- `webapp/src/pages/Dashboard.tsx`

#### 1.1 Domain 解析 helper

`connection.store.ts` 新增 module-level function：

```typescript
/** 从 k2v5://udid:token@host:port?... 提取 host（即 tunnel domain） */
function extractDomainFromServerUrl(serverUrl: string): string | null {
  try {
    const url = new URL(serverUrl.replace(/^k2v\d+:\/\//, 'https://'));
    return url.hostname.toLowerCase() || null;
  } catch {
    return null;
  }
}
```

#### 1.2 冷启动恢复 function

`connection.store.ts` 新增 module-level function：

```typescript
/**
 * 冷启动恢复：VPN 活跃但 connectedTunnel 丢失时，
 * 从持久化的 config.server URL + self-hosted store 恢复。
 *
 * Guard 条件：configLoaded && selfHostedLoaded && vpnActive && !connectedTunnel
 * 正常连接流程中 connectedTunnel 在 connect() 里先设置，guard 不会命中。
 *
 * 三个异步数据源（config/selfHosted/vpnState）加载顺序不确定，
 * 由三个独立订阅触发，guard 确保全部就绪后才执行且仅执行一次。
 */
function tryRestoreConnectedTunnel(): boolean {
  const vpnState = useVPNMachineStore.getState().state;
  const { connectedTunnel } = useConnectionStore.getState();
  const { config, loaded: configLoaded } = useConfigStore.getState();
  const { loaded: selfHostedLoaded } = useSelfHostedStore.getState();

  if (!configLoaded) return false;
  if (!selfHostedLoaded) return false;
  if (connectedTunnel) return false;
  if (vpnState !== 'connected' && vpnState !== 'connecting' && vpnState !== 'reconnecting') return false;

  const serverUrl = config.server;
  if (!serverUrl) return false;

  const domain = extractDomainFromServerUrl(serverUrl);
  if (!domain) return false;

  // 优先检查 self-hosted（完整信息已持久化）
  const selfHosted = useSelfHostedStore.getState().tunnel;
  if (selfHosted) {
    const selfHostedDomain = extractDomainFromServerUrl(selfHosted.uri);
    if (selfHostedDomain === domain) {
      const activeTunnel = computeSelfHostedActiveTunnel();
      console.info('[Connection] Cold start restore: self-hosted domain=' + domain);
      useConnectionStore.setState({
        selectedSource: 'self_hosted',
        connectedTunnel: activeTunnel,
        activeTunnel,
      });
      return true;
    }
  }

  // Cloud: domain-only 部分恢复，等 tunnel list 加载后补全 name/country
  console.info('[Connection] Cold start restore: cloud domain=' + domain + ' (pending enrichment)');
  useConnectionStore.setState({
    selectedSource: 'cloud',
    connectedTunnel: {
      source: 'cloud',
      domain,
      name: domain,    // 占位，tunnel list 加载后替换
      country: '',     // 占位，tunnel list 加载后替换
      serverUrl,
    },
  });
  return true;
}
```

#### 1.3 Tunnel list 补全 action

`connection.store.ts` store 新增 action：

```typescript
enrichFromTunnelList: (tunnels: Tunnel[]) => {
  const { connectedTunnel } = get();
  if (!connectedTunnel || connectedTunnel.source !== 'cloud') return;
  if (connectedTunnel.country) return; // 已补全，幂等

  const match = tunnels.find(t => t.domain.toLowerCase() === connectedTunnel.domain);
  if (!match) return;

  const enriched = computeCloudActiveTunnel(match);
  console.info('[Connection] Enriched from tunnel list: domain=' + match.domain
    + ', name=' + (match.name || match.domain) + ', country=' + (match.node?.country || ''));
  set({
    connectedTunnel: enriched,
    selectedCloudTunnel: match,
    activeTunnel: enriched,
  });
},
```

#### 1.4 initializeConnectionStore 添加三个订阅

修改 `initializeConnectionStore()`。三个异步数据源（config / selfHosted / vpnState）加载顺序不确定，各自完成时触发 `tryRestoreConnectedTunnel()`，内部 guard 确保全部就绪后才执行，且仅执行一次（connectedTunnel 非 null 后 guard 拦截后续调用）。

```typescript
export function initializeConnectionStore(): () => void {
  // 触发点 1: VPN 状态变化（vpn-machine.store 使用 subscribeWithSelector middleware）
  const unsubVPN = useVPNMachineStore.subscribe(
    (s) => s.state,
    (state) => {
      // 现有：VPN idle 时清除 connectedTunnel
      if (state === 'idle') {
        const { connectedTunnel } = useConnectionStore.getState();
        if (connectedTunnel) {
          console.info('[Connection] VPN idle — clearing stale connectedTunnel');
          useConnectionStore.setState({ connectedTunnel: null });
        }
      }
      // 新增：冷启动恢复
      if (state === 'connected' || state === 'connecting' || state === 'reconnecting') {
        if (!useConnectionStore.getState().connectedTunnel) {
          tryRestoreConnectedTunnel();
        }
      }
    },
  );

  // 触发点 2: config 加载完成
  // config store 未使用 subscribeWithSelector middleware，用 Zustand v5 基础 subscribe(listener)
  // 签名: (state, prevState) => void
  const unsubConfig = useConfigStore.subscribe((state, prevState) => {
    if (state.loaded && !prevState.loaded) {
      tryRestoreConnectedTunnel();
    }
  });

  // 触发点 3: selfHosted 加载完成
  // selfHosted store 同样未使用 subscribeWithSelector，用基础 subscribe
  const unsubSelfHosted = useSelfHostedStore.subscribe((state, prevState) => {
    if (state.loaded && !prevState.loaded) {
      tryRestoreConnectedTunnel();
    }
  });

  return () => {
    unsubVPN();
    unsubConfig();
    unsubSelfHosted();
  };
}
```

#### 1.5 Dashboard 传递 onTunnelsLoaded

`Dashboard.tsx` 修改：

```typescript
// 新增从 connection store 获取 enrichFromTunnelList
const { enrichFromTunnelList } = useConnectionStore();

// CloudTunnelList 添加 onTunnelsLoaded
<CloudTunnelList
  selectedDomain={displayTunnel?.domain || ''}
  onSelect={handleCloudTunnelSelect}
  disabled={isInteractive}
  onTunnelsLoaded={enrichFromTunnelList}
/>
```

`onTunnelsLoaded` 已被 CloudTunnelList 支持（prop 定义在 line 34），每次 tunnel list 加载（初始 + cache SWR + 5min 刷新）都会回调。`enrichFromTunnelList` 内部幂等（`country` 非空则跳过）。

### Part 2: Android Always-on VPN Recovery

**改动文件：**
- `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt`

#### 2.1 null Intent 时从 SharedPreferences 恢复

修改 `onStartCommand()`：

```kotlin
override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    Log.d(TAG, "onStartCommand: action=${intent?.action} flags=$flags startId=$startId")

    if (intent == null) {
        // System-initiated restart (always-on VPN) — try to recover from saved config
        val savedConfig = applicationContext
            .getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
            .getString("configJSON", null)
        if (savedConfig != null) {
            Log.i(TAG, "onStartCommand: null intent — recovering from saved config (always-on VPN restart)")
            startVpn(savedConfig)
            return START_NOT_STICKY
        }
        Log.w(TAG, "onStartCommand: null intent, no saved config — stopping self")
        stopSelf()
        return START_NOT_STICKY
    }

    when (intent.action) {
        // ... 现有逻辑不变
    }
    return START_NOT_STICKY
}
```

**行为：** 当 Android 系统因 always-on VPN 重启 service（null Intent），从 K2Plugin 已保存的 SharedPreferences 读取 configJSON 恢复连接。如果 SharedPreferences 无 config（从未连接过），仍然 stopSelf()。

## Timing Analysis

### 三个异步事件

`initializeAllStores()` 中三个 fire-and-forget 异步操作完成顺序不确定：

| 事件 | 来源 | 预估耗时 | 完成效果 |
|------|------|---------|---------|
| **A** config.loadConfig() | `_platform.storage` 读取 | ~10ms | `configLoaded=true` |
| **B** selfHosted.loadTunnel() | `_platform.storage` 读取 | ~10ms | `selfHostedLoaded=true` |
| **C** _k2.run('status') | NE/daemon IPC | ~50ms | vpnState 更新 |

### 全排列验证（VPN connected + self-hosted 连接）

| 顺序 | 第 1 个完成 | tryRestore | 第 2 个完成 | tryRestore | 第 3 个完成 | tryRestore |
|------|-----------|-----------|-----------|-----------|-----------|-----------|
| A→B→C | A: configLoaded ✓ selfHostedLoaded ✗ | fail | B: vpnState=idle | fail | C: 全部就绪 | **成功** |
| A→C→B | A: vpnState=idle | fail | C: selfHostedLoaded ✗ | fail | B: 全部就绪 | **成功** |
| B→A→C | B: configLoaded ✗ | fail | A: vpnState=idle | fail | C: 全部就绪 | **成功** |
| B→C→A | B: configLoaded ✗ | fail | C: configLoaded ✗ | fail | A: 全部就绪 | **成功** |
| C→A→B | C: configLoaded ✗ | fail | A: selfHostedLoaded ✗ | fail | B: 全部就绪 | **成功** |
| C→B→A | C: configLoaded ✗ | fail | B: configLoaded ✗ | fail | A: 全部就绪 | **成功** |

**结论：无论哪种顺序，最后一个事件完成时 restore 成功。** 前面的尝试因 guard 安全失败。

### tryRestore vs enrichFromTunnelList 时序

CloudTunnelList 的 `onTunnelsLoaded` 在 React `useEffect` 中触发（commit 后、paint 后执行），而 store 订阅在 Promise 微任务中触发（paint 前）：

```
JS 微任务队列:  A/B resolve → 订阅触发（guard fail）
                C resolve → 订阅触发 → tryRestore 成功 → connectedTunnel 已设
React paint
useEffect:      CloudTunnelList refresh() → cache hit/API → onTunnelsLoaded
                → enrichFromTunnelList → domain 匹配 → 补全 name/country
```

**tryRestore 总是先于 enrichment 执行。** 如果极端情况下 enrichment 先于 tryRestore（不应发生），enrichment 检查 connectedTunnel=null → 跳过，5 分钟后自动刷新时重新触发。

### 幂等保证

| 多次触发场景 | 保护机制 |
|-------------|---------|
| 3 个订阅连续触发 tryRestore | 第一次成功设 connectedTunnel → 后续 `if (connectedTunnel) return false` |
| onTunnelsLoaded 被多次回调（cache + API + 5min 刷新） | `if (connectedTunnel.country) return` → 已补全则跳过 |
| config store 多次状态变更 | `if (state.loaded && !prevState.loaded)` → 仅 false→true 转换触发一次 |

## Edge Cases

| 场景 | 行为 | 结果 |
|------|------|------|
| 正常连接（非冷启动） | `connect()` 先设 connectedTunnel → guard 跳过 restore | 无影响 |
| 冷启动 + VPN connected + cloud | domain 部分恢复 → tunnel list 补全 | 国旗 + 选中 |
| 冷启动 + VPN connected + self-hosted | self-hosted store 完整恢复 | 国旗 + 选中 |
| 冷启动 + VPN disconnected | vpnState=idle → guard 跳过 | 无动作 |
| 冷启动 + VPN error 状态 | vpnState=error → guard 跳过（不在 connected/connecting/reconnecting 中） | 无动作 |
| 冷启动 + VPN reconnecting | vpnState=reconnecting → guard pass → restore | 显示重连中的 tunnel |
| config 未加载时 VPN 状态先到 | guard: configLoaded=false → fail → config 订阅后重试 | 延迟恢复 |
| selfHosted 未加载时 VPN 状态先到 | guard: selfHostedLoaded=false → fail → selfHosted 订阅后重试 | 延迟恢复 |
| Tunnel list 加载失败 | domain-only connectedTunnel → list 高亮 ✓, name=domain, 无国旗 | 可接受降级 |
| 用户断开后重连 | disconnect 清除 connectedTunnel → 正常 connect 流程 | 正常 |
| enrichFromTunnelList 多次调用 | country 非空 → 幂等跳过 | 安全 |
| Desktop app 重启（daemon 保持连接） | 同一逻辑，config store 也持久化了 server URL | 跨平台通用 |
| Standalone web（无 VPN 后端） | vpnState 始终 idle → guard 跳过 | 无影响 |
| Android always-on + SharedPreferences 无 config | stopSelf()，等用户手动打开 app 连接 | 安全降级 |
| Android always-on + 用户已切换 tunnel | SharedPreferences 存最新 config（connect 时覆盖写入） | 恢复最新 tunnel |
| 未登录用户冷启动 | CloudTunnelList 不渲染 → 无 enrichment → domain-only 显示 | 登录后补全 |

## Files Changed

| 文件 | 改动 | 大小 |
|------|------|------|
| `webapp/src/stores/connection.store.ts` | 新增 `extractDomainFromServerUrl()`, `tryRestoreConnectedTunnel()`, `enrichFromTunnelList` action, 修改 `initializeConnectionStore()` 增加三个订阅 | ~70 行新增 |
| `webapp/src/pages/Dashboard.tsx` | 传递 `onTunnelsLoaded={enrichFromTunnelList}` | ~3 行 |
| `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt` | `onStartCommand()` null Intent 从 SharedPreferences 恢复 | ~8 行 |

## Not In Scope

- iOS NE 层改动（不需要，config 已持久化在 App Group）
- Desktop daemon 层改动（不需要，state.json 已持久化）
- 新增持久化 key（不需要，复用 config store 已有的 `k2.vpn.config`）
- VPN status response 添加 tunnel 信息（不需要，domain 从 URL 反推）
