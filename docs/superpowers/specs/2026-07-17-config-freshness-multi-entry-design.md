# Config.js 新鲜度标记 + 多 entry Failover + 文件名迁移 ui.js

**Date**: 2026-07-17
**Status**: Approved
**Context**: 承接 `2026-07-17-disable-relay-restore-direct-design.md`。relay 已关停,config.js 直连成为唯一传输。现有直连路径有两个正确性缺陷:(1) 10 个 CDN 镜像并行加载时 `promiseAny` 取**首个响应者**,而第三方镜像"快但陈旧",系统性偏向旧内容,旧 entry 覆盖新 entry;(2) 只用 `entries[0]`,entry[0] 被封时无回退。

## 目标

1. **内容新鲜度标记**:config 明文加入 `ts`(发布时刻 epoch 秒,在 AES-GCM 认证载荷内),消费端跨镜像取 `ts` 最大者,并以 `ts` 门控 localStorage 写入 —— 陈旧镜像永远盖不掉更新的缓存。
2. **多 entry failover**:`entries` 可含多个,API 具备逐个尝试能力。按 HTTP 方法区分:幂等(GET/HEAD)并发竞速,非幂等(POST/PUT/DELETE/PATCH)顺序 failover(仅连接级失败时换下一个,绝不重放)。
3. **文件名迁移** `config.js` → `ui.js`:给新 app 全新缓存键,一次性绕开被投毒的第三方镜像旧缓存。`config.js` 双发布保留给已发布旧 app。

## 非目标

- 不改加密算法、密钥、`__k2ac` 全局名、`{v,data}` 外层信封格式(`v` 仍为格式版本,恒 1)。
- 不动 relay 代码(kill-switch 保持 `RELAY_ENABLED=false`)。
- 不改 `buildSubsUrl`(auth-service.ts):它取单个最佳 entry 塞进 `k2subs://`,多 entry failover 由 daemon(Go)负责,非本次范围。
- 不加"老键→新键"代码兼容桥(遵循 no-defensive-migration-bridges)。`config.js` 双发布是**静态资源**对无法更新的已发布客户端的兼容,非代码桥。

## 数据格式变更

config 明文:`{ entries: string[] }` → `{ entries: string[], ts: number }`

- `ts` = 发布时刻 `Math.floor(Date.now()/1000)`,整数 epoch 秒。
- 向后兼容:消费端把**缺失 `ts` 视为 `ts=0`**(最旧)。任何带 `ts` 的新 config 都能盖过旧格式,但旧格式仍能正常出 entries。
- IV 每次加密随机 → `data` 每次发布都变(现状即如此),`ts` 不改变 churn 特性。

## 生成端(`scripts/antiblock-encrypt.js`)

- `encrypt()` 保持不变(纯函数,加密任意 config 对象)。
- Main 分支:构造 config 时注入 `ts`。**同时写 `config.js` 与 `ui.js`,内容一致**(都是 `{entries, ts}`,`__k2ac` 全局)。
- 版本化 seed `v/<N>.js` 载荷也加 `ts`(格式统一;seed 已被 kill-switch 关停,但保持一致)。
- 自测:新增断言 —— 解密后的载荷含整数 `ts` 字段。

## 发布工作流(`.github/workflows/publish-antiblock.yml`)

- generate 步骤后,`cp` **两个**文件到 dist:`config.js` 和 `ui.js`。
- `git add config.js ui.js v/`。
- Purge 增加 `https://purge.jsdelivr.net/gh/kaitu-io/ui-theme@dist/ui.js`。

## 消费端(`webapp/src/services/antiblock.ts`)

### CDN_SOURCES

10 个源全部 `config.js` → `ui.js`(路径其余不变)。

### 关联:`antiblock-seed.ts` `seedUrls` 正则

`seedUrls(n)` 用 `src.replace(/\/config\.js$/, '/')` 从 CDN_SOURCES 推导 `v/<n>.js`。改名后正则须同步为 `/\/ui\.js$/`,否则不匹配 → 拼出错误 URL。seed 通道已被 kill-switch 关停(`bootstrapAntiblockSeed` 提前返回,不调 `seedUrls`),此改动仅为 relay 若重启时的正确性,不影响当前行为。

### localStorage 存储模型:单一原子记录

entries 与 ts **必须绑在一条记录里**,作为所有更新决策的唯一基础。用**一个**键:

| 键 | 值 | 用途 |
|----|----|------|
| `k2_entry_cfg`(新) | `JSON({ entries: string[], ts: number })` | 唯一事实源:一次 `setItem` 原子写、一次 `getItem`+parse 原子读,entries 与 ts 永不错位 |

- **不再拆分多键**(`k2_entry_list` / `k2_entry_ts` 方案已废弃):拆分会引入"list 已更新但 ts 未更新"的中间态,以及跨读者看到 entries 与 ts 不一致。单记录消除该问题。
- `k2_entry_url`(legacy):**新代码不再读写它做解析**。它仅剩 `antiblock-seed.ts` 的 poisoned 清理(删除)与已被 kill-switch 关停的 relay 写入分支。见"时序问题分析 #3"。

### decode

`decryptConfig` → `decodeConfig`,返回 `{ entries: string[]; ts: number } | null`:
- 校验 `config.v === 1 && typeof config.data === 'string'`。
- 解密 → `JSON.parse` → 校验 `entries` 为非空数组;`ts` 取 `typeof parsed.ts === 'number' ? parsed.ts : 0`。
- 返回 `{ entries, ts }`。

### 读写记录

```ts
const RECORD_KEY = 'k2_entry_cfg';
interface EntryRecord { entries: string[]; ts: number; }

function readRecord(): EntryRecord | null {
  try {
    const raw = localStorage.getItem(RECORD_KEY);
    if (!raw) return null;
    const p = JSON.parse(raw);
    if (Array.isArray(p.entries) && p.entries.length > 0 && typeof p.ts === 'number') {
      return { entries: p.entries, ts: p.ts };
    }
  } catch { /* corrupt → treat as no cache */ }
  return null;
}
```

`readRecord` **不从 legacy `k2_entry_url` 迁移**(见"时序问题分析 #3")。解析失败/形状不符 → 返回 null → 视作冷启动重拉。

### ts 门控写入(全同步,原子)

```ts
function commitIfFresher(cfg: EntryRecord): void {
  try {
    if (cfg.entries.length === 0) return;
    const stored = readRecord();
    if (!stored || cfg.ts >= stored.ts) {
      localStorage.setItem(RECORD_KEY, JSON.stringify({ entries: cfg.entries, ts: cfg.ts }));
    }
  } catch { /* best-effort */ }
}
```

- **全同步、内部无 `await`** —— 这是 load-bearing 不变量(见"时序问题分析 #2")。单线程 JS 下每次调用从"读 stored"到"写记录"原子执行到底,多个镜像的 `.then` 微任务串行、不交错,消除读-改-写竞态。**实现时严禁在 `commitIfFresher` 内引入任何 await/异步。**
- `!stored || cfg.ts >= stored.ts`:无记录 → 任何 config(含 legacy `ts=0`)都提交;有记录 → `>=` 允许同 ts 幂等重写,拒绝更低 ts。

### 首个响应先返回 + 后台升级

`fetchEntryFromCDN(): Promise<string[] | null>`:
- 并行发起所有镜像 `loadJsonp → decodeConfig`。
- 每个成功解码的镜像**都**调 `commitIfFresher`(ts 门控,原子)。
- **第一个**成功解码者一到达就 `resolve(readRecord()?.entries ?? null)` 返回给调用方(冷启动快;返回值取记录中"已提交里最新的",可能已比首个到达者更新);其余镜像的 `.then` 仍在跑,持续原子升级记录。
- 全部失败 → `resolve(null)`。

### 公共 API

```ts
export async function resolveEntries(): Promise<string[]> {
  const cached = readRecord();
  if (cached) {
    refreshEntryInBackground();
    return cached.entries;
  }
  const entries = await fetchEntryFromCDN();
  return entries && entries.length > 0 ? entries : [DEFAULT_ENTRY];
}

export async function resolveEntry(): Promise<string> {
  return (await resolveEntries())[0] ?? DEFAULT_ENTRY;
}
```

`resolveEntry` 保持单串签名不变(`buildSubsUrl` 与既有导出零改动)。

## 消费端 failover(`webapp/src/services/resolve-and-fetch.ts`)

`tryDirect` 改为多 entry,按方法区分:

```ts
async function tryDirect(req: RelayReq): Promise<TransportResult | null> {
  const entries = await resolveEntries();
  const idempotent = req.method === 'GET' || req.method === 'HEAD';
  if (idempotent && entries.length > 1) return raceEntries(entries, req);
  return sequentialEntries(entries, req);
}
```

### raceEntries(幂等)

- 对所有 entry 并发 `fetch(entry + path)`,各自 `AbortController` 超时 = `DIRECT_PROBE_TIMEOUT_MS`(14s)。
- **第一个拿到 HTTP 响应**(任何 status)的胜出 → 返回 `{transport:'ok', status, json}`,abort 其余。
- 全部连接级失败 → `null`。
- 所有 entry 指向同一源站,故 race 本质是"哪个前端最快可达",4xx/5xx 与单 entry 语义一致。

### sequentialEntries(非幂等 + 单 entry)

- 共享截止 `deadline = now + DIRECT_PROBE_TIMEOUT_MS`;每 entry 超时 = `max(4000, floor(TOTAL / N))`,但不超过剩余预算。
- 逐个:`fetch` 拿到 **任何 HTTP 响应** → 返回(请求已执行,含 4xx/5xx,不 failover)。
- **连接级失败**(throw/abort,证明请求未到后端)→ 试下一个 entry。对非幂等请求安全:绝不重放。
- 全部失败或预算耗尽 → `null`。

## 时序问题分析

存储以单记录 `k2_entry_cfg = {entries, ts}` 为唯一基础,逐条覆盖并行/异步下的竞态:

1. **多键非原子写** —— 已消除。单 JSON 记录,一次 `setItem` 原子写、一次 `getItem` 原子读。绝不会出现"entries 已更新但 ts 未更新"的中间态。
2. **并发 commit 的读-改-写竞态** —— `commitIfFresher` **全同步、内部无 await**。单线程 JS 下,每个镜像的 `.then` 是独立微任务,串行执行且不可抢占;一次 commit 从"读 stored ts"到"写记录"原子跑完,下一个 commit 才开始。因此两个镜像不会同时读到旧 ts 再各自覆盖。**约束:实现绝不可在 commitIfFresher 内加 await。**
3. **legacy `k2_entry_url` poisoned 迁移竞态** —— 彻底规避。新解析只读 `k2_entry_cfg`,**不从 `k2_entry_url` 迁移**。升级用户首启无 `k2_entry_cfg` → 冷拉取干净 entries → 写新记录;历史遗留的 poisoned `k2_entry_url`(relay 时代 seed 写入的 GFW 被封 CloudFront)成为无人解析的死键,`antiblock-seed.ts` 的关停路径仍会顺手 `removeItem` 它。由此消除了原方案里"迁移读 vs 清理删"谁先执行的时序依赖。
4. **后台刷新命中陈旧镜像** —— ts 门控 `!stored || cfg.ts >= stored.ts` 丢弃低 ts,缓存永不降级。
5. **fetch 首返 vs 后台升级** —— 首个成功镜像触发 `resolve`,但返回值取 `readRecord()?.entries`(此刻记录已是"已提交里最新",可能已被更早到达的更高 ts 镜像升级);后续更高 ts 镜像继续原子升级记录,下次读即得。用户已选此"速度优先"取舍。

## 行为效果

- 冷启动:首个镜像秒回,后台收敛到最新 config;之后走缓存。
- 被封用户:GET 竞速下备用 entry 秒赢;POST 顺序下 entry[0] 连接失败后立即切 entry[1](未执行,安全)。
- 陈旧镜像后台刷新回来的旧 config 因 ts 门控被丢弃,不污染缓存。
- 401 刷新原子性不变(仍在 cloud-api,传输层不碰 401)。

## 测试

**`scripts/antiblock-encrypt.js` 自测**:载荷解密后含整数 `ts`;`config.js` 与 `ui.js` 内容一致性(如可行)。

**`webapp/src/services/__tests__/antiblock.test.ts`**:
- `decodeConfig` 解出 `{entries, ts}`;缺 `ts` → `ts=0` 且 entries 仍有效。
- 跨镜像:高 ts 与低 ts 混合响应 → `k2_entry_cfg` 记录收敛到高 ts 的 entries(无论镜像响应顺序)。
- ts 门控:已缓存高 ts 记录,后台来低 ts → **不覆盖**。
- 存储原子性:`k2_entry_cfg` 为单 JSON 记录,`readRecord` 解出的 entries 与 ts 始终来自同一条记录。
- **不迁移 legacy**:localStorage 仅有 poisoned `k2_entry_url`、无 `k2_entry_cfg` → `resolveEntries` **不**返回该 legacy 值,而是冷拉取 / 兜底 `[DEFAULT_ENTRY]`。
- `resolveEntries` 返回完整列表;`resolveEntry` 返回 `[0]`。
- 缓存命中:`k2_entry_cfg` 存在 → 直接返回 `record.entries` + 后台刷新。
- CDN_SOURCES 全部匹配 `/ui\.js$/`;既有 mirror 断言更新。
- 全 CDN 失败 → `[DEFAULT_ENTRY]`。

**`webapp/src/services/__tests__/resolve-and-fetch.test.ts`**(如存在,否则新增):
- GET + 多 entry:entry[0] 连接失败 → entry[1] HTTP 响应胜出。
- POST + 多 entry:entry[0] 连接失败 → 顺序切 entry[1] 返回;entry[0] 返回 500 → 直接返回 500,**不** failover(证明已执行)。
- 单 entry:退化为顺序单次,行为不变。

验证命令:`cd webapp && npx vitest run && npx tsc --noEmit && yarn build`;`node scripts/antiblock-encrypt.js --test`。

## 部署清单

1. 合并本分支后,`ui-theme` 发布工作流下次 cron(或手动 `workflow_dispatch`)将同时产出 `config.js` + `ui.js`,两者含 `ts`。
2. 新版 app(含本次 webapp 改动)发版后,CDN_SOURCES 指向 `ui.js`,享受全新缓存键。
3. 旧版 app 继续读 `config.js`(双发布,内容一致),不受影响。
4. 待旧版 app 淘汰(数个发布周期后),从生成端与工作流移除 `config.js` 双发布。

## 风险与取舍

- **首个响应先返回** = 冷启动那一次可能先用到略陈旧但可用的 entry,下次即被后台升级修正。用户已明确选此取舍(速度优先)。可接受:陈旧 entry 仍指向同源站,且 ts 门控保证不产生持久污染。
- **race GET** 对所有前端各发一次 GET(幂等,无副作用);**非幂等走顺序**,规避多前端重复执行。
- `ui.js` 缓存键随时间同样会累积镜像陈旧 —— 但 `ts` 标记 + max-ts 选取才是长期新鲜度保证,文件名迁移只是一次性缓存重置。

## 恢复路径

回退文件名:CDN_SOURCES 改回 `config.js`;生成端/工作流本就仍在双发布 `config.js`。回退 ts/多 entry:`decodeConfig` 忽略 `ts`、`tryDirect` 用 `entries[0]` 单串即可,但无必要(向后兼容已内建)。
