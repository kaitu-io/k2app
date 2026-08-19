# 缓存资源 hook 统一设计（useCachedResource）

**状态**：设计草案，未实施。局部修复已落地（见「已做了什么」），重构待 0.4.8 发布后立项。
**起因**：2026-08-19 iOS IAP 真机测试暴露「购买成功后授权日期不刷新」，追查发现是架构问题而非 IAP 缺陷。

---

## 1. 问题

`webapp` 里「带缓存的远程资源」有一个反复出现的形状：

```ts
const [data, setData] = useState<T | null>(null);
useEffect(() => {
  const cached = cacheStore.get<T>(KEY);
  if (cached) { setData(cached); /* 后台 revalidate */ return; }
  const res = await cloudApi.get<T>(PATH);
  cacheStore.set(KEY, res.data, { ttl });
  setData(res.data);
}, [deps]);
```

`cacheStore` 是**全应用单例**，但每个 hook 实例把值**拷进自己的 useState**。
`cacheStore` 原本没有广播机制，于是：

> **任何一处刷新了数据，其余所有实例都不知道。**

这不是理论风险，已经造成线上可见故障：

- **iOS IAP 购买成功后授权日期不刷新**（用户报告）。`verifyAndGrant` 写入新用户数据，
  但只有发起购买的 `IosSubscribePanel` 那份 `useUser` 更新了；父级 `Purchase.tsx` 与
  `useSubscriptionAffordance` 内部各自的实例停在旧值 → `affordance.mode` 不翻转 →
  界面继续显示购买面板和旧日期。
- WordGate 支付路径**碰巧**没暴露：它成功后直接 `navigate` 跳走，从不依赖 affordance 翻转。

## 2. 现状清单（2026-08-19 实测）

| 缓存键 | 消费方 | 调用点 | 写入方 | 订阅 |
|---|---|---|---|---|
| `api:user_info` | `useUser` | **17** | `useUser`、`useIapPurchase.verifyAndGrant` | 已加 |
| `api:app_config` | `useAppConfig` | **7** | `useAppConfig`（ttl 3600）、~~`Purchase.tsx`（ttl 600）~~ | 已加 |
| `api:private_nodes` | `usePrivateNodes` | 2 | 自身 | **无** |
| `api:tunnels` | `connection.store`、`CloudTunnelList` | — | 两者互写 | **无** |
| `api:plans:*` | `Purchase.tsx` | 1 | 自身 | 不需要（单消费方） |

`cacheStore.clear()` 另有 4 个调用点（`EmailLoginForm` ×2、`LoginDialog` ×2、`cloud-api` 401），
清空后所有实例仍持有上一个账号的数据。

**自愈能力差异**（决定严重度）：

- `useUser` — effect 依赖 `[isAuthenticated]`，登录态变化能重拉
- `usePrivateNodes` — 依赖链带 `isAuthenticated`，能重拉
- `useAppConfig` — 依赖 **`[]`**，挂载一次永不重拉。这是最糟的一个

## 3. 已做了什么（局部修复，已合入 main）

1. `cacheStore` 增加 `subscribe(key, listener)`，`set`/`delete`/`clear`/`clearExpired`
   四条变更路径全部广播。listener 抛错被吞并记 error（缓存写入是主流程，通知是副作用）；
   `notify` 迭代快照，允许 listener 内部退订。
2. `useUser`、`useAppConfig` 订阅各自的键。
3. `Purchase.tsx` 删掉复制自 `useAppConfig` 的 40 行取数逻辑，改用 `useAppConfig()`——
   它带着一个 TTL 600 与 hook 的 3600 互相覆盖，且 `appConfigLoading` 是死状态
   （`const [, setAppConfigLoading]`，值被解构丢弃）。

**没做**：`usePrivateNodes` 与 `api:tunnels` 的订阅。它们都能自愈或有轮询兜底，
收益撑不起在发布验证期多担的风险。这是**已知债务，不是遗漏**。

## 4. 为什么要做通用 hook（而不是继续逐个加订阅）

逐个加订阅是把同一段代码抄第三遍。更实际的信号是**成本正在外溢到测试**：

每给一个 hook 加订阅，就要去补一批不完整的 `cacheStore` 测试替身——
`LoginDialog.test.tsx`（第一次）、`useAppLinks.brand.test.ts` 与
`Purchase.privateNode.test.tsx`（第二次）。全库共 9 个测试文件 mock 了 `cache-store`，
每个都手写了不同的部分接口。再加一个方法就要再扫一遍。

## 5. 设计约束（重构时必须满足）

### 5.1 唯一供给者

```ts
// services/cached-resource.ts
function useCachedResource<T>(key: string, fetcher: () => Promise<T>, opts): {
  data: T | null; loading: boolean; error: E | null; refresh: (force?: boolean) => Promise<void>;
}
```

内部固定四件事：读缓存 → SWR 后台 revalidate → 订阅广播 → 退订。
业务代码不再直接碰 `cacheStore`，配 grep 守卫进 CI。

**这个模式项目里已有先例**：`webapp/CLAUDE.md` 的 `capabilities.ts` 规矩
（唯一供给者 + grep 守卫），治的是同一类病（探测逻辑散落各处）。不是新范式。

### 5.2 opts 必须表达的差异（已核对三个 hook，非臆测）

| 需求 | 来自 |
|---|---|
| TTL | 三者不同（3600 / 3600 / 各自常量） |
| 取数成功后的副作用 | `useUser` 的 `syncDetectedProfile` |
| 认证门 | `usePrivateNodes` 的 `isAuthenticated` 短路 |
| 过期缓存兜底 | `usePrivateNodes` 的 `cacheStore.get(KEY, true)` |
| 401 特殊处理 | `useUser`（`setUser(null)`） |
| 依赖数组 | `[]` / `[isAuthenticated]` / `[refresh]` |

第一版会因此偏胖，这是这个方案的真实成本，不要假装没有。

### 5.3 「值没了」的策略必须逐资源显式决定

广播会在 `delete`/`clear` 时也触发。当前两处订阅都选择**忽略空值**：

- `useUser` — 登出由 `isAuthenticated` 驱动，跟着清会在正常缓存淘汰时闪空态
- `useAppConfig` — 配置为空会让消费方渲染降级分支

但这个默认**对 `api:tunnels` 未必对**（节点列表被清空可能确实该清 UI）。
通用 hook 要把它做成显式选项（`onEvicted: 'ignore' | 'clear' | 'refetch'`），
**不能有隐含默认**。

### 5.4 共享测试替身

提供 `src/test/utils/mock-cache-store.ts` 导出工厂，让 9 个测试文件不再各写一份部分接口。
新增 `cacheStore` 方法时只改工厂。

### 5.5 无循环

`cacheStore.get` 内存命中返回同一对象引用，`setState` 同引用时 React bail out。
订阅回调**只读缓存、不发请求**——写入方已放入权威数据，再打网络会在多实例下
放大成 N 个并发请求。这两条在当前实现里已成立，重构后必须保持。

## 6. 迁移顺序建议

1. 建 `useCachedResource` + 共享 mock 工厂，先不迁移任何调用点
2. 迁 `usePrivateNodes`（2 个调用点，风险最低，验证 opts 设计够不够用）
3. 迁 `useAppConfig`（7 个）
4. 迁 `useUser`（17 个，最后做）
5. `api:tunnels` 单独评估——它的消费方之一是 Zustand store 而非 hook，
   形状与其余三个不同，可能不适合套同一个壳

## 7. 未决问题

- `connection.store`（Zustand）与 `CloudTunnelList`（组件）互写 `api:tunnels`，
  谁该是所有者？在动它之前需要先答这个。
- 是否顺带收敛 `cacheStore.clear()` 的 4 个调用点？它们表达的其实是「会话结束」，
  也许该有一个语义化的 `clearSessionScoped()`。
