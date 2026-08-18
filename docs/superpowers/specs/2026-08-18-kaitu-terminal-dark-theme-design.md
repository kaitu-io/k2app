# 开途 webapp Terminal Dark 配色迁移

**日期**: 2026-08-18
**状态**: 设计已批准，待实现
**范围**: `webapp/` — 仅开途（kaitu）品牌，Overleap 视觉零变化

---

## 1. 目标

把开途 webapp 的配色迁移到官网（`web/`）的 Terminal Dark 体系，**同时不丢失 VPN app 特有的状态语义**。

官网是内容站，没有「连接状态」这个维度；app 有。因此**照搬的是调性（深黑底 + 高饱和霓虹），不是色彩分配**。原样照搬官网的 `--primary: #00ff88` 到 MUI `palette.primary` 会造成可举证的表达丢失（见 §2）。

### 非目标

- **不动 Overleap 的配色**（已达成并由测试锁定，见 §7）
- 不引入 light mode（`ThemeContext` 硬编码 dark，维持现状）
- 不改动官网 `web/`

### 范围外溢：Overleap 会获得 §5 / §6 的 UX 改进

实现后确认：**配色零变化**（侧边栏紫、CTA 紫、背景 `#0F0F13` 均与基线一致，
`theme.brand.test.ts` 锁死），但 Overleap 也会得到三处**行为**变化：主按钮
dormant 熄灭态、遮罩层的锁 + 价值说明、CTA 辉光。

这是有意的，理由三条：

1. 视觉层级倒置是**两个品牌都有**的真实缺陷，不是开途专属问题。
2. 要让它只在开途生效，只能在共享组件里按品牌 id 分叉 —— `webapp/CLAUDE.md`
   明令禁止（唯一 resolver 是 `brandConfig`）。
3. 走 `BrandFeatures` 门控在技术上可行，但为一个纯视觉改进新增 feature gate
   过重，且会让「哪个品牌长什么样」散进 config。

若产品要求 Overleap 保持原样，正确做法是加 `BrandFeatures.dormantConnectionButton`
门控，而不是在组件里判断品牌 id。

---

## 2. 为什么不能直接把 primary 设成 `#00ff88`

原方案（primary = 官网主色霓虹绿）在真实界面上验证后否决。实拍证据：绿色同屏出现在 **六处**——主连接按钮、节点 Radio 选中点、侧边栏当前页高亮、Tab 下划线、Badge 计数、每行 RecommendBar。用户无法区分「已连接」/「我选了这个节点」/「我在这一页」。

冲突点在代码中的确切位置：

| 位置 | 元素 | 冲突 |
|---|---|---|
| `components/CloudTunnelList.tsx:514` | Auto 行 `Radio color="primary"` | 选中态 = 已连接色 |
| `components/CloudTunnelList.tsx:583` | 节点行 `Radio color="primary"` | 同上 |
| `components/SelfHostedTunnelItem.tsx:124` | 自建节点 `Radio color="primary"` | 同上 |
| `pages/Dashboard.tsx:563` | 幻影列表 `Radio color="primary"` | 同上 |
| `components/CloudTunnelList.tsx:506` | `Badge color="primary"` | 计数 = 状态色 |
| `components/RecommendBar.tsx:40` | `bgcolor: success.main` | 绿已承担「节点健康」 |

Dashboard 同屏渲染 `CollapsibleConnectionSection`（连接按钮）与 `CloudTunnelList`（节点列表），上述冲突全部实际发生。

**佐证**：官网自己就没有合并——`web/src/app/globals.css` 中 `--primary: #00ff88` 与 `--success: hsl(142,60%,55%)` 是两个独立 token。

---

## 3. 色彩语义契约

**每个颜色只有一个 job。** 这是本设计的核心约束，后续任何改动都必须遵守。

| 色 | 唯一 job | 落点 |
|---|---|---|
| 霓虹绿 `#00ff88` | **通了 / 受保护** | 已连接主按钮（唯一大色块）、品牌 logo |
| 青 `#00d4ff` | **可操作 / 你在哪** | MUI `palette.primary`：导航高亮、Tab、Radio、Checkbox、Switch、Badge、按钮、`CircularProgress`、`TextField` focus |
| 柔绿 `#66BB6A` | **数据可视化：健康度** | MUI `palette.success`：`RecommendBar`、`Chip color="success"` |
| 橙 `#FFB74D` | 连接中 / 警告 | 不变 |
| 红 `#EF5350` | 故障 / 断开操作 | 不变 |
| 中性灰 | 承重 | 边框 `rgba(255,255,255,.12)`、文本、分隔线 |

### 三条推论（实现时必须遵守）

1. **`palette.success` 保持 `#66BB6A`，不改成品牌绿。** RecommendBar 每行都有，用品牌 accent 会抢戏。柔绿与霓虹绿在尺寸量级上天然分离（4×24px 细条 vs 220px 大圆），反而强化「绿 = 通/好」的统一语义。
2. **边框用中性灰，不跟官网的 `rgba(0,255,136,.15)`。** 这是与官网的一处**有意偏离**：边框带绿会让绿变成背景噪音，削弱「已连接」的信号强度。
3. **绿实心大色块全 app 同屏唯一。** Dashboard 的绿席位恒属于连接按钮。

---

## 4. 品牌隔离机制

`webapp/src/theme/colors.ts` 的 `APP_COLORS` 目前**品牌共享**（12 个文件消费），`BrandThemeTokens` 只有 primary/secondary 两组。要让 Terminal Dark 只落到开途，必须扩展品牌 token 契约。

### 4.1 契约扩展（`brands/types.ts`）

```ts
/** 连接状态色 —— 驱动 ConnectionButton / CompactConnectionButton */
export interface BrandStatusTokens {
  /** 已连接 / 受保护 —— 该品牌的高光色 */
  connected: { main: string; gradient: string; glow: string; glowStrong: string };
  /** 已登录未连接（待命，可点击） */
  idle:      { main: string; gradient: string; glow: string; glowStrong: string };
  /** 未登录且无可用节点（熄灭，见 §5） */
  dormant:   { border: string; icon: string };
}

export interface BrandSurfaceTokens {
  background: string;  // palette.background.default
  paper: string;       // palette.background.paper
  border: string;      // 卡片 / 列表边框
}

export interface BrandThemeTokens {
  light: { primary: PaletteTriple; secondary: PaletteTriple };
  dark:  { primary: PaletteTriple; secondary: PaletteTriple };
  status: BrandStatusTokens;    // 必填
  surface: BrandSurfaceTokens;  // 必填
}
```

**两个品牌都必须显式提供 `status` / `surface`**，不设 optional 兜底。Overleap 填入当前生效的实际值（等价拷贝，视觉零变化）。理由：optional fallback 是隐式行为，未来改共享默认值会静默改变 Overleap 外观。

### 4.2 开途取值

```ts
// brands/kaitu/theme.ts
dark: {
  primary:   { main: '#00d4ff', light: '#5ce3ff', dark: '#00a8cc' },  // 青 = 交互
  secondary: { main: '#00ff88', light: '#5affb3', dark: '#00cc6a' },  // 绿 = 品牌 accent
},
surface: { background: '#0a0a0f', paper: '#111118', border: 'rgba(255,255,255,0.12)' },
status: {
  connected: {
    main: '#00ff88',
    gradient: 'linear-gradient(135deg, #00ff88 0%, #00cc6a 100%)',
    glow: 'rgba(0,255,136,0.35)', glowStrong: 'rgba(0,255,136,0.5)',
  },
  idle: {
    main: '#00d4ff',
    gradient: 'linear-gradient(135deg, #00d4ff 0%, #00a8cc 100%)',
    glow: 'rgba(0,212,255,0.3)', glowStrong: 'rgba(0,212,255,0.5)',
  },
  dormant: { border: 'rgba(255,255,255,0.10)', icon: 'rgba(255,255,255,0.30)' },
},
```

`background`/`paper` 与官网 `--background`/`--card` 精确一致。

**关于 `secondary`**：`color="secondary"` 在当前 webapp 中**零使用**（全量 grep 确认），所以把品牌绿放进 `palette.secondary` 不会渲染任何现有元素。它的作用是让 `secondary.main` 成为品牌绿的**语义入口**——未来需要品牌绿点缀时从这里取，而不是散落硬编码。连接状态的绿走 `status.connected`，两者取值相同但职责不同，不可互相替代。

### 4.3 唯一供给者

`colors.ts` 的 `getStatusGradient()` / `getStatusShadow()` / `getStatusColor()` 三个函数成为**连接状态色的唯一供给者**，改为从 `brandConfig.theme.status` 读取。

`APP_COLORS` 中的 `success*` / `info*` 键继续服务于**非连接状态**语境（Alert、Chip 等），保持共享。

**守卫**：组件禁止直接读 `APP_COLORS.successGradient` / `infoGradient` 表达连接状态——必须走三个函数。加 grep 守卫进 CI。

---

## 5. ConnectionButton：新增 `dormant` 视觉态

### 5.1 动机

未登录态实拍暴露的问题**比颜色更严重：视觉层级倒置**。220px 蓝色大圆是整屏最强焦点，但此刻它是死的（无节点可连）；真正该点的「登入解鎖全球節點」CTA 只是个小按钮。

`dormant` 态让主按钮"没通电"，把绿完整保留给真正连上的那一刻，并让 CTA 成为该屏唯一亮点。

### 5.2 触发判据

`hasTunnelSelected` **不可复用**——未登录时它为 `true`（存在"自動選擇"行），按钮当前是可点的蓝色实心圆，并非 disabled。

新增 prop：

```ts
/** 无任何可连节点（未登录且无自建节点）→ 熄灭态 */
dormant?: boolean;
```

Dashboard 传入 `!isAuthenticated && !selfHostedTunnel`。未登录但已配置自建节点的用户，按钮保持可用（`idle` 态），因为他确实能连。

### 5.3 视觉规格

| 态 | 背景 | 边框 | 阴影 | 动画 | 图标色 |
|---|---|---|---|---|---|
| `dormant` | `transparent` | `1px solid` `status.dormant.border` | 无 | 无 | `status.dormant.icon` |
| `disconnected`(idle) | `status.idle.gradient` | 无 | `idle.glow` | breathe 3s | `#0a0a0f` |
| `connected` | `status.connected.gradient` | 无 | `connected.glow` | 无 | `#0a0a0f` |
| `transitioning` | warning 渐变 | 无 | warning glow | pulse 2s | `#ffffff` |
| `stop` | error 渐变 | 无 | error glow | 无 | `#ffffff` |

### 5.4 读取点审计（新增状态必须全覆盖）

`VisualStatus` 增加 `'dormant'` 后，以下**全部**读取点必须更新，遗漏任何一处都是静默 bug（TS 的联合类型穷尽检查只在有 `switch` 返回值时报错，三元链不报）：

- [ ] `theme/colors.ts` `getStatusGradient()` — 加 `dormant` 分支
- [ ] `theme/colors.ts` `getStatusShadow()` — 加 `dormant` 分支（返回 `'none'`）
- [ ] `theme/colors.ts` `getStatusColor()` — 加 `dormant` 分支（`CompactConnectionButton.tsx:76` 消费）
- [ ] `ConnectionButton.tsx:58-62` `animation` 三元链
- [ ] `ConnectionButton.tsx:74-86` hover `boxShadow` 四元链 —— dormant 不应放大/发光
- [ ] `ConnectionButton.tsx:90-94` `&:disabled` 分支与 dormant 的优先级
- [ ] `ConnectionButton.tsx:161-171` `visualStatus` useMemo — dormant 优先级高于 disconnected，低于 stop/transitioning
- [ ] `CompactConnectionButton.tsx` — 折叠态是否需要 dormant 呈现

**验证方式**：把 `VisualStatus` 的消费改为 `switch` + `default: assertNever(status)`，让编译器代替人工审计。

### 5.5 必修缺陷：硬编码白色图标

`ConnectionButton.tsx:205 / 209 / 213 / 216` 四处硬编码 `color: 'white'`（`Stop` / `CircularProgress` / `Check` / `PlayArrow`）。

已连接底色变 `#00ff88` 后，**白色图标对比度 1.34:1**——几乎不可见。已连接与待命态必须改为深色 `#0a0a0f`（对比度：绿底 14.8:1、青底 11.2:1，均远超 WCAG AA 4.5:1）。橙底/红底保留白色仍可读。

图标色改为跟随 `visualStatus`，与 §5.3 表格一致。

---

## 6. Dashboard 未登录遮罩改造

`pages/Dashboard.tsx:514-600`（`!isAuthenticated` 分支）。

| 项 | 现状 | 改为 | 理由 |
|---|---|---|---|
| 幻影列表 | `opacity .5` + `blur(4px)` | `opacity .75` + `blur(5px)` | 现在又暗又糊，两个手段叠加过头，"有一批节点可解锁"的营销意图没达成。改为更亮更糊：看得见是一列带国旗的节点，读不出是哪些 |
| 遮罩底 | `background.default + 99` | 不变 | — |
| CTA | 默认尺寸 `Button` | `padding: 12px 28px`、`fontSize: .95rem`、`boxShadow: 0 8px 32px rgba(0,212,255,.35)` | 升为该屏唯一亮点 |
| 锁标识 | 无 | MUI `LockOutlined`，22px，`opacity .5`，CTA 上方 | 说明"为什么看不到" |
| 价值说明 | 无 | 一行 12px `rgba(255,255,255,.55)` 副文案，锁与 CTA 之间 | 说明"解锁得到什么" |
| 主按钮 | 蓝色实心大圆 | `dormant` 态（§5） | 修正视觉层级倒置 |

### 副文案数字：已决定不写

原型用了「登入後可用 15 個國家/地區的節點」，但**幻影列表的 15 条假数据只覆盖 11 个国家/地区**（JP/SG/US/HK/TW/KR/DE/GB/AU/CA/FR）。未登录状态拿不到真实节点数。

**已实现**：`dashboard.unlockCloudNodesHint`，中文「全球多国家/地区节点，登录后即可选用」，7 个 locale 全部补齐，不含任何数字。若产品坚持要数字，须由 Center API 提供未授权可读的公开计数，不得硬编码。

### i18n

新增 2 个 key（锁副文案、可能的 CTA 辅助文案）到 `dashboard` 命名空间，7 个 locale 全部补齐，zh-CN 先行。中文用户面禁用「Kaitu」裸词，用「开途」。

---

## 7. 验证

| 层 | 方式 |
|---|---|
| 单测 | `cd webapp && npx vitest run`；`K2_BRAND=overleap npx vitest run` 也必须绿 |
| 类型 | `npx tsc --noEmit` |
| Overleap 零回归 | 逐项比对 `brands/overleap/theme.ts` 新增的 `status`/`surface` 与改动前实际生效值 |
| 品牌纯净度 | `scripts/check-brand-purity.sh kaitu dist` + overleap |
| 视觉 | Chrome DevTools MCP 实拍以下屏：Dashboard 未登录 / 已登录未连接 / 已连接、Purchase、Account、SubmitTicket、Tunnels |
| 对比度 | 所有前景/背景对 ≥ 4.5:1；重点核 17 处硬编码白字（`grep -rn "color: *['\"]#fff\|white" src --include=*.tsx`）逐个验底色 |

---

## 8. 已知风险

| 风险 | 严重度 | 处置 |
|---|---|---|
| 17 处硬编码白字中，除 ConnectionButton 4 处外的其余 13 处底色未逐个核对 | 中 | 实现时逐个验；抽查显示均不与 `primary.main` 同处 |
| `getSemanticColors()`（`theme.ts:199-270`）在 tsx 中**零消费**，是死代码 | 低 | 本次不清理，但不要为它适配新色板；标记待删 |
| `CircularProgress` 43 处默认 primary → 全部变青 | 低 | 符合语义（进行中 = 交互层），无需逐个改 |
| 新增 `dormant` 遗漏读取点 | 高 | §5.4 用 `assertNever` 让编译器兜底 |
| Overleap `status`/`surface` 填错值造成静默视觉回归 | 中 | 改动前先截 Overleap 基线图，改动后逐屏比对 |

---

## 9. 置信度

- **诊断（原方案有表达丢失）**：10/10 — 真实界面实拍举证，六处绿同屏。
- **色彩语义方案**：9.5/10 — 已在 dev server 真实渲染验证，对比度实测达标。差 0.5：Purchase/Account/SubmitTicket 等页尚未逐页核对。
- **`dormant` 态 + 遮罩改造**：9/10 — 视觉已验证；副文案数据源待定，锁图标待换 MUI icon，`CompactConnectionButton` 的 dormant 呈现待设计。
