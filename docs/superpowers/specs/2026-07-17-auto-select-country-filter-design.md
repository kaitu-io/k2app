# 自动选择国家排除过滤 — 设计规格

**日期**: 2026-07-17
**状态**: 已批准
**范围**: 仅 App 端（桌面/移动/Web 的 webapp manual 模式）。k2r 网关 k2sub 路径、Center API、k2 submodule 均不改动。

## 背景

用户反馈：在香港网络环境下 app 连不上，自动选择会选到香港节点导致连接失败。需要让用户能把某些国家从自动选择中排除。

「自动选择」有两条独立路径：

1. **App 端（本次范围）**：webapp 前端从 `/api/tunnels` 列表自选（`webapp/src/utils/auto-tunnel-pick.ts` 的 `pickAutoTunnel`，recommendScore top-5 加权随机），节点自带 `node.country`（ISO 3166-1 alpha-2），纯前端即可过滤。
2. **k2r 网关（不在范围）**：`k2subs://` 订阅由 daemon `Subscription.Pick` 选择，tunnel 不带国家字段；已有 `?country=` 单国过滤可用。

## 产品决策

- **排除语义**：用户多选要排除的国家，自动选择跳过它们。默认全不选 = 现状不变；新上线国家自动纳入候选池。
- **只影响自动选择**：手动列表仍显示全部国家，被排除的国家仍可手动选择（逃生通道）。
- **排除后无可用节点时报错，不静默回退**：用户排除某国正是因为它不可用，静默回退等于把坏节点塞回去。

## 设计

### 1. UI — `webapp/src/components/CloudTunnelList.tsx`

「自动选择」行（现 442-478 行）：

- 行尾 Radio 之前加 `FilterListIcon` 的 `IconButton`，`onClick` 需 `stopPropagation`（不触发选中 Auto）。
- 有排除国家时：图标 primary 色 + `Badge` 显示排除数量；行 secondary 文案追加「已排除 N 个国家」。
- 点击打开 **MUI Dialog**（禁用 window.confirm/alert/prompt — Capacitor WebView 会静默吞掉）：
  - 标题「排除国家」+ 说明「自动选择将跳过勾选的国家」。
  - 国家列表从当前 tunnels 的 `node.country` 去重派生 — 把 `K2subConfig.tsx` 的 `buildCountryList` 抽成共享 util（如 `webapp/src/utils/country-list.ts`），两处复用。
  - 每行：国旗（`getFlagIcon`）+ 国家名（`getCountryName`）+ `Checkbox`，勾选 = 排除。
  - 底部操作：「清除」（全不选）/「完成」（关闭）。勾选即时生效并持久化，无需确认按钮提交语义。

### 2. 状态与持久化 — `webapp/src/stores/connection.store.ts`

完全复刻现有 `subsCountry` 模式（76-79、385-408 行先例）：

- 新增 `excludedCountries: string[]`（小写 ISO alpha-2，去重）。
- 新增 `setExcludedCountries(countries: string[])`：`set()` + `_platform.storage.set(EXCLUDED_COUNTRIES_STORAGE_KEY, JSON.stringify(countries))`。
- 持久化 key：`k2.connection.excludedCountries`。
- 启动加载：与 `subsCountry` 同处的 load 逻辑，JSON 解析失败回退 `[]`。
- 持久化中残留的已下线国家代码无害：过滤时匹配不到即自然跳过；Dialog 只显示当前列表派生的国家。

### 3. 选择逻辑 — `webapp/src/utils/auto-tunnel-pick.ts`

- 签名扩展：`pickAutoTunnel(tunnels, excludedCountries?: string[], rng?)`。
- pool filter（现有 `recommendScore > 0 && !!serverUrl`）追加 `!excluded.has(t.node.country.toLowerCase())`（比较双方均归一为小写；DB 侧存大写，store 存小写）。
- top-5 加权随机逻辑不动。
- 过滤后空池返回 `null` → caller（`connection.store.ts` `connect()` 现 427-442 行）走现有 `NO_TUNNEL_AVAILABLE_AUTO` 错误路径；当 `excludedCountries` 非空且原始池非空时，错误文案用增强版：「可用节点已全部被国家过滤排除，请调整过滤设置」。

### 4. i18n

`webapp/src/i18n/locales/{en-AU,en-GB,en-US,ja,zh-CN,zh-HK,zh-TW}/dashboard.json` 全部补齐：

- `auto.filterTitle`（排除国家）
- `auto.filterHint`（自动选择将跳过勾选的国家）
- `auto.excludedCount`（已排除 {{count}} 个国家）
- `auto.filterClear`（清除）
- `auto.filterDone`（完成）
- `auto.allExcluded`（可用节点已全部被国家过滤排除，请调整过滤设置）

### 5. 测试（vitest）

- `pickAutoTunnel` 过滤：排除命中、大小写归一（大写 country vs 小写偏好）、空池返 `null`、空排除列表 = 现状行为。
- store：`setExcludedCountries` 持久化写入与启动加载（含损坏 JSON 回退）。
- 注意 gotcha：`vi.clearAllMocks()` 会清 mock 实现，`beforeEach` 里必须重设 `mockResolvedValue`。

## 不改动的部分

- k2r 网关 k2sub 订阅路径（`Subscription.Pick`、`buildSubsUrl`、`subsCountry` 现有语义）。
- Center API（`/api/tunnels`、`/api/subs`）。
- k2 submodule。
- `recommendScore` 评分模型与列表排序（纯国家字母序）。

## 已否决的替代方案

- **包含语义**（只保留勾选国家）：新上线国家默认被排除，长期易过时。
- **过滤器同时隐藏列表行**：手动选择是逃生通道，「排除自动选择」≠「不想看到它」。
- **覆盖 k2r 网关**：需扩展 `/api/subs` exclude 参数 + daemon 透传，跨三层且 k2 是独立 repo；报障用户是 App 用户，网关已有单国过滤，收益不匹配成本。
- **空池静默回退到全部节点**：会把用户明确标记为坏的节点塞回去，行为不可预测。
