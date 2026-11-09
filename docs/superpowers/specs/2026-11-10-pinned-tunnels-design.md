# 固定节点 / 最近使用设计

日期：2026-11-10 · 状态：David 已认可方向，按此推进 · 范围：仅 webapp（web OTA 可发）

## 1. 为什么做

有用户在意**出口 IP 不变**（ChatGPT、TikTok、银行、游戏等会因 IP 变动触发风控）。他们想要「固定 / 收藏 / 最近用过」的节点，能稳定回到同一个节点。

现状里 IP 会变的来源（已核实）：

1. **手动选择重启后丢失**：`selectedCloudTunnel` 从不持久化（`connection.store.ts` 初始值 `null` = Auto）；`lastServerUrl` 只用于「VPN 还连着、webapp 进程被杀」的冷启动恢复。用户选了东京 03，重启 App 就回到 Auto，下次连接换 IP。**这很可能是主因**。
2. **Auto 每次连接重新挑**（`pickAutoCandidates`），并且 F2 会在故障时切到其他机器。
3. **选中节点从列表消失**时 `reconcileSelection` 静默回落 Auto。
4. **排空设计**（`2026-10-31-node-drain-redirect-design.md` §5）写明「手动选的节点同样会被切走」。
5. **节点本身换 IP**（运维 `change_ip_cloud_instance`、换机）。固定的是节点（domain），**不承诺 IP 永不变**——本设计不在 UI 上做此承诺。

## 2. 目标 / 非目标 / 反目标

**目标**
- G1：用户可固定**多个**节点；固定池就是用户自己圈的切换范围——故障时只在固定池内切换，不出池。
- G2：只固定 1 个时，故障不切换，原地重试（= 现在手动选择的行为）。
- G3：规则在**选择/固定的当下**就让用户看懂，不是出了问题才第一次知道。
- G4：「最近使用」自动记录最近连过的未固定节点，一键固定（「上次那个 IP 挺好」）。
- G5：手动选择（固定或不固定的节点）在 App 重启后保留。

**非目标**
- 跨设备同步固定列表（v1 本机存储；格式不挡以后上服务端）。
- 引擎 / API / 原生层改动。
- 检测并提示「固定节点的 IP 变了」。
- 拖拽排序固定节点（v1 按固定先后）。

**反目标（出现即失败）**
- 连着固定节点，被悄悄切到固定池以外的节点。
- 用户以为「固定 = IP 永不变」——文案不得这样写。
- 已选节点因重启或列表刷新被静默换成 Auto 而用户看不到。

## 3. 方案取舍

| 方案 | 取舍 | 结论 |
|---|---|---|
| **A. 只改 webapp**：本机存固定池；连接固定节点时把其余固定节点作为 `ClientConfig.fallbacks` 传给引擎（F2 已合并的机制） | 零引擎/API/原生改动，web OTA 即可发，不等 iOS 审核；单固定 = 空 fallbacks = 现有手动行为 | **采用** |
| B. 固定池存服务端、跨设备同步 | 要 API 字段 + 迁移 + contract；v1 没有需求 | 否（以后可加） |
| C. 引擎内「固定模式」 | 改 Go + 跨层契约 + 发二进制；fallbacks 已能表达「只在这几个之间切」 | 否 |

## 4. 已定设计

### 4.1 数据（本机，`window._platform.storage`）

- `k2.connection.pinnedTunnels`：`[{ domain, name, country, pinnedAt }]`，按 `pinnedAt` 升序 = 固定先后。domain 小写。存 name/country 快照，供节点暂时不在列表时显示。上限 10 个（我的决定，防止列表失控）。
- `k2.connection.recentTunnels`：`[{ domain, name, country, lastUsedAt }]`，最多 3 条，**不含已固定的**，最新在前。每次对**云节点**发起连接（含 Auto 解析出的节点）时记录；固定某节点时从 recent 移除。
- `k2.connection.selectedDomain`：手动选中的云节点 domain；选 Auto（`clearCloudSelection`）或切到 self_hosted 时清除。
- 读写模式与 `excludedCountries` 一致（JSON 字符串、坏 JSON → 默认空、带 `*Loaded` 标志）。

### 4.2 选择恢复（G5）

- 隧道列表加载后（Dashboard 现有 `onTunnelsLoaded` → `reconcileSelection` 路径），若 `selectedCloudTunnel === null` 且存有 `selectedDomain` 且列表里有该节点 → `selectCloudTunnel(该节点)`。
- 只在启动后首次列表加载时恢复一次，不与用户之后点 Auto 抢。

### 4.3 选中节点消失时（`reconcileSelection`）

- 选中的是**固定节点**且它不在列表里 → 改选固定池里第一个仍在列表中的节点。
- 固定池全部不在列表 → 回落 Auto（同现状），但固定区显示警示行「你固定的节点暂时都不可用」，用户连接前就能看到。
- 非固定节点 → 现状不变。

### 4.4 连接（`connect()`）

- 手动选中的节点**属于固定池**：`fallbackTunnels` = 固定池中其余节点，按固定先后，只取当前列表里存在且有 `serverUrl` 的，跳过与主节点同机器的（复用 `auto-tunnel-pick.ts` 的机器键），最多 3 个（F2 上限）。
- 只固定 1 个 → `fallbackTunnels = []`，与现有手动行为完全一致（原地重试；引擎已有的连续 10 分钟耗尽上限照旧）。
- 手动选中非固定节点、Auto、self_hosted → 不变。
- 国家排除（`excludedCountries`）只作用于 Auto，不影响固定池（用户显式选的）。

### 4.5 界面（`CloudTunnelList`）

```
★ 我的固定 (2)
  ● 🇯🇵 东京 03   住宅IP      [★]
  ○ 🇯🇵 大阪 01               [★]
  ⓘ 断线或节点维护时，只在这 2 个节点之间切换，不会换到其他节点
🕘 最近使用
  ○ 🇸🇬 新加坡 02             [☆]
─────────────
○ ⚡ 自动选择                 [漏斗]
○ 🇭🇰 香港 01 ...             [☆]     （原全部列表，每行加星标）
```

- 固定区 / 最近区只在非空时出现，位于 Auto 行之上。全量列表照常显示所有节点（含已固定的，星标实心）。
- 星标是独立 `IconButton`（`stopPropagation`，不触发选中），`aria-label` 走 i18n。固定满 10 个时空心星禁用并 Tooltip 说明。
- **说明行随数量变化（G3，规则在固定时就可见）**：
  - 1 个：「只固定了 1 个节点：它不可用时会等它恢复，不会换 IP。想断线时自动切换，可再固定一个同地区节点」
  - ≥2 个：「断线或节点维护时，只在这 {{count}} 个节点之间切换，不会换到其他节点」
- 固定节点暂不在列表（下线/维护）：灰显、不可选，副标题「暂不可用」，星标仍可取消。
- 固定池全不可用：说明行替换为警示色「你固定的节点暂时都不可用，当前为自动选择」。
- 402 / 空列表状态：固定区和最近区不显示（与现有空状态一致）。
- 早先讨论过的「首次固定弹一次提示」**不做**：常驻说明行已在固定当下讲清规则，再弹一次是重复（我的决定）。

### 4.6 i18n

新键放 `dashboard` 命名空间下新的 `pinned` 组（`title`、`recentTitle`、`pin`、`unpin`、`hintSingle`、`hintMulti`、`allUnavailable`、`unavailable`、`limitReached`）。先写 zh-CN，再补所有 23 个 locale。文案品牌中立，不出现品牌字面量。

## 5. 与其他设计的关系

- **F2（已合并）**：本设计只是给手动+固定的情况填 `fallbacks`，引擎行为不变。
- **排空（待审批、未实现）**：在该设计 §5 补一条约束——连接固定节点时，排空切换目标必须来自固定池（即 webapp 传入的 fallbacks），不得用 Center 随机下发的备用节点。实现排空时须遵守。

## 6. 测试

- `connection.store`：固定池 fallbacks 生成（顺序、过滤不在列表的、同机器去重、上限 3、单固定为空、非固定为空）；selectedDomain 持久化与首次恢复；reconcile 在固定池内回落。
- `CloudTunnelList`：固定区/最近区渲染与隐藏、星标不触发选中、两种说明文案、不可用灰显、上限禁用。
- 双品牌 vitest 全绿；i18n 覆盖测试全绿。

## 7. 留给实现者

- 存储 helper 放 store 内还是独立 `stores/pinned-tunnels.ts`——按体量定，建议独立文件以免 `connection.store.ts` 继续膨胀。
- 视觉细节（区块标题样式、星标颜色）沿用现有 `CloudTunnelList` 头部与 Chip 风格。
