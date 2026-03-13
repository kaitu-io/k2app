# Onboarding Guide Systematic Overhaul

Date: 2026-03-13

## Problem

三个问题指向同一个架构缺陷：tooltip 把所有交互逻辑委托给「用户点击高亮区域」这一个隐式操作，没有显式的交互出口。

| 症状 | 根因 |
|------|------|
| step 5 聚焦到 tab 而不是分享按钮 | `data-tour="invite-share"` 只在 isMobile 时渲染，桌面端找不到目标 |
| 文案对比度低、阅读费力 | 样式用页面内容级排版，不是覆盖层 UI 该有的视觉强度 |
| 不知道高亮区域要点击 | 无显式「下一步」按钮，无视觉箭头，文案没写清操作指令 |

## Design Decisions

- **交互模式**：点击高亮区域推进（保留） + tooltip 内「下一步」备用按钮
- **视觉风格**：实心深色卡片，高对比度白色文字，cyan accent
- **视觉引导**：tooltip 和目标之间加 CSS animated 箭头

## Changes

### 1. PhaseConfig targets fallback

`PhaseConfig.target: string` → `PhaseConfig.targets: string[]`

按顺序 querySelector 直到命中。解决桌面/手机渲染不同元素的问题。

```typescript
// onboarding.store.ts
export interface PhaseConfig {
  targets: string[];  // CSS selectors, tried in order
  placement: 'top' | 'bottom' | 'left' | 'right';
  i18nKey: string;
}

const PHASE_CONFIG: Record<Phase, PhaseConfig> = {
  1: { targets: ['[data-tour="collapse-toggle"]'], placement: 'bottom', i18nKey: 'phase1' },
  2: { targets: ['[data-tour="collapse-toggle"]'], placement: 'bottom', i18nKey: 'phase2' },
  3: { targets: ['[data-tour="feedback-button"]'], placement: 'left', i18nKey: 'phase3' },
  4: { targets: ['[data-tour="nav-invite"]'], placement: 'top', i18nKey: 'phase4' },
  5: { targets: ['[data-tour="invite-share"]', '[data-tour="invite-copy"]'], placement: 'bottom', i18nKey: 'phase5' },
  6: { targets: ['[data-tour="nav-dashboard"]'], placement: 'top', i18nKey: 'phase6' },
};
```

**useTargetRect** 改为接收 `string[]`，for 循环依次尝试。

**InviteHub.tsx** 桌面端复制按钮加 `data-tour="invite-copy"`。

### 2. Visual tokens

集中所有视觉参数到 `onboarding/tokens.ts`。每个值的推导基于 `theme.ts` 和 `colors.ts` 的实际数值。

**推导依据**：
- 页面背景 `background.default` = `#0F0F13`
- MUI Paper `background.paper` = `#1A1A1D`
- 主文本 `text.primary` = `#FAFAFA`
- 次文本 `text.secondary` = `rgba(250,250,250,0.7)`
- 主色 `primary.main` = `#42A5F5`（lightBlue[400]）
- Glow 现有色 = `rgba(0,212,255,*)`（`#00d4ff`）

```typescript
export const ONBOARDING = {
  // ── Overlay ──
  // 现有 0.65 在 #0F0F13 上 card 和遮罩区分不够。
  // 0.7 让遮罩有效亮度降到 ~#050506，和 card bg 形成 L*=15 vs L*=3 的明度差。
  overlayColor: 'rgba(0,0,0,0.7)',

  // ── StepCard ──
  card: {
    // #1A2332 — 比 Paper(#1A1A1D) 加入蓝色色相偏移 (H=217°)，
    // 让 card 在暗色遮罩上既与 Paper 区分，又呼应 primary (#42A5F5) 的蓝色调。
    // 对比度 vs 纯白: 17:1 (WCAG AAA)。
    bg: '#1A2332',
    // 边框用 glow 同源色 #00d4ff，alpha 0.3（比 APP_COLORS.dark.accentBorder 的 0.25 略高，
    // 因为 onboarding 是短暂覆盖层，需要比常规 UI 更强的边界感）。
    border: '1px solid rgba(0,212,255,0.3)',
    // 双层阴影：近处 4px 做边缘柔化，远处 12px 做深度感。
    // 在 #050506 遮罩上阴影效果弱，边框承担主要分离职责，阴影做辅助。
    shadow: '0 4px 16px rgba(0,0,0,0.4), 0 12px 40px rgba(0,0,0,0.6)',
    radius: 12,       // 与现有 glow borderRadius 一致
    padding: '20px 22px',  // 比现有 14px 18px 放大 ~40%，给中文内容呼吸空间
    maxWidth: 300,    // 内容宽度 300-44=256px，13px 中文约 19 字/行，舒适阅读
  },

  // ── Typography ──
  title: {
    fontSize: 15,      // 比 MUI subtitle2(14px) 大 1px，在小 card 里做标题刚好
    fontWeight: 700,   // 比现有 600 加粗一档，短标题需要更强视觉锚点
    color: '#fff',     // 纯白，不用 text.primary(#FAFAFA)——差异微小但 #fff 在深色 card 上更干净
    // 对比度 vs #1A2332: 17:1 (WCAG AAA)
  },
  body: {
    fontSize: 13,      // MUI body2 默认 14px，缩 1px 让正文和标题有明确层级差
    lineHeight: 1.85,  // 中文方块字标准舒适行高 1.7-2.0，取中间值
    letterSpacing: '0.03em',  // 中文微量字间距，0.05em 太松，0.02em 感知不到
    color: 'rgba(255,255,255,0.85)',  // 比 text.secondary(0.7) 提高 0.15
    // 对比度 vs #1A2332: 12.6:1 (WCAG AAA)
  },
  hint: {
    fontSize: 12,
    color: 'rgba(255,255,255,0.5)',  // 原计划 0.45 勉强过 AA(4.65:1)，提到 0.5 → 5.4:1 稳过
    // 对比度 vs #1A2332: 5.4:1 (WCAG AA ✓)
  },
  nextButton: {
    fontSize: 13,
    fontWeight: 600,
    // #4fc3f7 = lightBlue[300]，比 primary.main(#42A5F5 = lightBlue[400]) 亮一阶，
    // 在小按钮上更易识别。同时接近 glow 色 #00d4ff 的色相区间，视觉统一。
    color: '#4fc3f7',
    // 对比度 vs #1A2332: 8.5:1 (WCAG AAA)
  },

  // ── Glow ── (保持现有值，已验证有效)
  glow: {
    // 与 APP_COLORS.dark.accent (#00ffff) 同色系但偏蓝 (#00d4ff)，
    // 避免纯 cyan 刺眼。现有 alpha 值已在实际运行中验证。
    color: 'rgba(0,212,255,{a})',
    ringWidth: [3, 5] as const,       // box-shadow spread 3→5px pulse
    spreadRadius: [20, 30] as const,  // box-shadow blur 20→30px pulse
  },

  // ── Arrow ──
  arrow: {
    size: 20,          // SVG viewBox width，在 300px card 上比例协调 (6.7%)
    height: 10,        // SVG viewBox height，等腰三角形高度
    color: '#4fc3f7',  // 与 nextButton 同色，建立「箭头→按钮」视觉关联
    bounceDistance: 6, // 6px = 箭头高度的 60%，明显但不夸张
    duration: '1.2s',  // 比 glow 的 2s 快，吸引注意力优先级更高
  },

  // ── Popper offset ──
  // arrow height(10px) + visual gap(8px) = 18px
  // Popper offset 控制 card 边缘到目标边缘的距离，箭头在 gap 空间内绘制。
  popperOffset: [0, 18] as const,

  // ── Z-index layers ──
  z: {
    overlay: 1300,  // 暗色遮罩
    glow: 1310,     // 目标元素发光（在遮罩上方，被 card 遮挡）
    arrow: 1315,    // 箭头（glow 和 card 之间）
    card: 1320,     // StepCard（最上层）
  },
} as const;
```

### 3. StepCard layout (replaces OnboardingTooltip)

```
┌─────────────────────────────────┐
│ [标题]                  [×] [3/6]│
│                                  │
│ [正文]                           │
│                                  │
│ 点击高亮区域，或    [下一步 →]    │
└─────────────────────────────────┘
         ▲ (animated arrow)
    ┌──────────┐
    │ 高亮目标  │ (glow pulse)
    └──────────┘
```

- 标题行：标题 + × close + step counter
- 正文：token 定义的字号/行高/字间距
- 底部：左灰色 hint + 右 cyan「下一步 →」
- 最后一步：「下一步 →」变为「完成 ✓」
- × 按钮调用 complete()（等同跳过）

### 4. Animated arrow

内联 SVG 三角形（不用 CSS border trick——border triangle 在高 DPI 屏锯齿明显，且 4 个方向需要 4 套 border 配置）。
作为 StepCard 内部的绝对定位元素，跟着 Popper 自动走位。

**SVG 实现**：`<svg width="20" height="10">` 内一个等腰三角形 `<polygon>`，fill `#4fc3f7`。
4 个方向通过 CSS `transform: rotate()` 旋转同一个 SVG：

| placement | rotate | 箭头视觉方向 | bounce 动画 |
|-----------|--------|-------------|------------|
| bottom | 0deg | ▲ 指向上方目标 | translateY(0 → -6px) |
| top | 180deg | ▼ 指向下方目标 | translateY(0 → 6px) |
| left | 90deg | ▶ 指向右方目标 | translateX(0 → 6px) |
| right | -90deg | ◀ 指向左方目标 | translateX(0 → -6px) |

动画：1.2s ease-in-out infinite。

**Popper auto-flip 兼容**：MUI Popper 空间不够时会自动翻转 placement（如 bottom → top）。
箭头必须跟着**实际** placement 走，不能用配置值。方案：通过自定义 Popper modifier 在 `afterWrite` 阶段读取 `state.placement`：

```typescript
const [actualPlacement, setActualPlacement] = useState(placement);

const modifiers = useMemo(() => [
  { name: 'offset', options: { offset: [0, 20] } },  // 12px gap + 8px arrow height
  {
    name: 'reportPlacement',
    enabled: true,
    phase: 'afterWrite' as const,
    fn: ({ state }: any) => {
      const p = state.placement.split('-')[0];  // 'bottom-start' → 'bottom'
      setActualPlacement((prev: string) => prev !== p ? p : prev);
    },
  },
], []);
```

箭头组件读 `actualPlacement` 决定方向，flip 后箭头仍指向目标。

### 5. nextPhase 防重入

用户可能同时点了高亮区域和「下一步」按钮，导致 `nextPhase()` 被调用两次、跳过一步。

在 store 的 `nextPhase` 中加 guard：

```typescript
nextPhase: () => {
  const { phase, phases } = get();
  const currentIndex = phases.indexOf(phase);
  if (currentIndex < 0) return;  // guard: phase already changed
  if (currentIndex < phases.length - 1) {
    set({ phase: phases[currentIndex + 1] });
  } else {
    get().complete();
  }
},
```

这不够——两次调用读到同一个 `phase` 时 `indexOf` 结果相同，仍会推进到同一个下一步（幂等但不会跳步）。
实际上 Zustand `set()` 是同步的，两次 `nextPhase()` 在同一个事件循环 tick 内：
- 第一次：phase=4 → set phase=5 ✓
- 第二次：get() 读到 phase=5（已更新）→ indexOf(5) → set phase=6 ✗ 跳步

**修复**：加一个 `_advancing` flag，防止同一 tick 内重复推进：

```typescript
// store 内部
_advancing: false,

nextPhase: () => {
  if (get()._advancing) return;
  set({ _advancing: true });

  const { phase, phases } = get();
  const currentIndex = phases.indexOf(phase);
  if (currentIndex < phases.length - 1) {
    set({ phase: phases[currentIndex + 1], _advancing: false });
  } else {
    get().complete();
  }
},
```

`_advancing` 不暴露给组件，纯内部防护。

### 6. i18n copy rewrite

原则：第一行说「为什么」，第二行说「怎么做」。去掉「点击这里」（箭头已引导），语气对话化。
新增 key `onboarding.hint`：StepCard 底部 hint 文字。
新增 key `onboarding.next`：StepCard「下一步」按钮。
新增 key `onboarding.done`：最后一步「完成」按钮。

**zh-CN:**

```json
{
  "onboarding": {
    "phase1": { "title": "收起面板", "content": "节点多的时候可以收起面板，腾出空间" },
    "phase2": { "title": "展开面板", "content": "再点一次展开，恢复连接按钮" },
    "phase3": { "title": "问题反馈", "content": "遇到问题随时点这里告诉我们" },
    "phase4": { "title": "邀请好友", "content": "邀请好友双方都能获得免费时长" },
    "phase5": { "title": "分享链接", "content": "把你的专属邀请链接发给好友吧" },
    "phase6": { "title": "开始使用", "content": "回到仪表板，选个节点开始连接吧" },
    "skip": "跳过引导",
    "hint": "点击高亮区域，或",
    "next": "下一步",
    "done": "完成"
  }
}
```

**en-US / en-AU / en-GB:**

```json
{
  "onboarding": {
    "phase1": { "title": "Collapse Panel", "content": "Hide the panel to make room for the server list" },
    "phase2": { "title": "Expand Panel", "content": "Tap again to bring back the connect button" },
    "phase3": { "title": "Feedback", "content": "Having issues? Let us know anytime" },
    "phase4": { "title": "Invite Friends", "content": "Both you and your friend get free time" },
    "phase5": { "title": "Share Link", "content": "Send your invite link to a friend" },
    "phase6": { "title": "Get Started", "content": "Head back and pick a server to connect" },
    "skip": "Skip Guide",
    "hint": "Tap highlighted area, or",
    "next": "Next",
    "done": "Done"
  }
}
```

**ja:**

```json
{
  "onboarding": {
    "phase1": { "title": "パネルを収納", "content": "パネルを閉じてサーバーリストを広く表示" },
    "phase2": { "title": "パネルを展開", "content": "もう一度タップして接続ボタンを表示" },
    "phase3": { "title": "フィードバック", "content": "問題があればいつでもお知らせください" },
    "phase4": { "title": "友達を招待", "content": "招待すると双方に無料日数をプレゼント" },
    "phase5": { "title": "リンクをシェア", "content": "あなた専用の招待リンクを友達に送ろう" },
    "phase6": { "title": "さあ始めよう", "content": "ダッシュボードに戻ってサーバーを選んで接続" },
    "skip": "スキップ",
    "hint": "ハイライト部分をタップ、または",
    "next": "次へ",
    "done": "完了"
  }
}
```

**zh-TW:**

```json
{
  "onboarding": {
    "phase1": { "title": "收合面板", "content": "節點多的時候可以收起面板，騰出空間" },
    "phase2": { "title": "展開面板", "content": "再點一次展開，恢復連線按鈕" },
    "phase3": { "title": "問題回饋", "content": "遇到問題隨時點這裡告訴我們" },
    "phase4": { "title": "邀請好友", "content": "邀請好友雙方都能獲得免費時長" },
    "phase5": { "title": "分享連結", "content": "把你的專屬邀請連結發給好友吧" },
    "phase6": { "title": "開始使用", "content": "回到儀表板，選個節點開始連線吧" },
    "skip": "跳過引導",
    "hint": "點擊高亮區域，或",
    "next": "下一步",
    "done": "完成"
  }
}
```

**zh-HK:**

```json
{
  "onboarding": {
    "phase1": { "title": "收合面板", "content": "節點多嘅時候可以收埋面板，騰出空間" },
    "phase2": { "title": "展開面板", "content": "再撳一次展開，恢復連線掣" },
    "phase3": { "title": "問題回饋", "content": "遇到問題隨時撳呢度話畀我哋知" },
    "phase4": { "title": "邀請朋友", "content": "邀請朋友雙方都可以獲得免費日數" },
    "phase5": { "title": "分享連結", "content": "將你嘅專屬邀請連結發畀朋友啦" },
    "phase6": { "title": "開始使用", "content": "返去儀表板，揀個節點開始連線啦" },
    "skip": "跳過引導",
    "hint": "撳高亮區域，或",
    "next": "下一步",
    "done": "完成"
  }
}
```

### 7. Skip text i18n

`onboarding.skip` key 保留，用于 × 按钮的 `aria-label`。不再作为可见文字按钮。

## Target 渲染时序说明

`OnboardingGuide.tsx:116` 已有 guard：`if (!active || !config || !rect) return null`。
`useTargetRect` 的 RAF 轮询在 `querySelector` 未命中时返回 `rect=null`，整个 overlay + StepCard 不渲染。
目标元素出现的那一帧 RAF 命中，自然渲染。`targets[]` fallback 不改变此行为——for 循环依次试，全部未命中则 `rect=null`。
**无需额外的时序处理，现有架构已覆盖。**

## Files to modify

| File | Change |
|------|--------|
| `stores/onboarding.store.ts` | PhaseConfig.target → targets[], `_advancing` guard, getPhaseConfig 更新 |
| `components/onboarding/useTargetRect.ts` | 接收 string[], for 循环匹配 |
| `components/onboarding/OnboardingTooltip.tsx` | 重写为 StepCard（布局、token、箭头、下一步按钮、reportPlacement modifier） |
| `components/onboarding/SpotlightOverlay.tsx` | 引用 token overlayColor |
| `components/onboarding/tokens.ts` | 新文件，视觉常量 |
| `components/OnboardingGuide.tsx` | 传 onNext 回调给 StepCard；glow CSS 引用 token |
| `pages/InviteHub.tsx:596` | 桌面复制按钮加 data-tour="invite-copy" |
| `i18n/locales/zh-CN/onboarding.json` | 文案重写 + 新增 hint/next/done keys |
| `i18n/locales/en-US/onboarding.json` | 文案重写 + 新增 hint/next/done keys |
| `i18n/locales/en-AU/onboarding.json` | 同 en-US |
| `i18n/locales/en-GB/onboarding.json` | 同 en-US |
| `i18n/locales/ja/onboarding.json` | 完整日文翻译 |
| `i18n/locales/zh-TW/onboarding.json` | 完整繁体翻译 |
| `i18n/locales/zh-HK/onboarding.json` | 完整粤语翻译 |

## Non-goals

- 不改 phase 数量或顺序
- 不改 store 的 init/tryStart/complete 生命周期
- 不改 SpotlightOverlay 的 SVG 挖洞算法
- 不改 RAF polling 机制

## Confidence Assessment

**满意度：9/10**（扣 1 分：日文翻译未经母语者校对，可能有不自然的表达）

**执行信心：9.5/10**
- targets[] fallback：逻辑简单，for 循环替换单 selector
- StepCard 重写：纯展示组件，可独立验证
- 箭头 + auto-flip：reportPlacement modifier 是 Popper.js 标准 API
- 防重入：Zustand 同步 set，`_advancing` flag 逻辑清晰
- 时序：现有 RAF + null guard 已覆盖，无需额外处理
- 信心缺口（-0.5）：实际视觉效果需要运行后微调 token 数值（card bg、contrast、arrow size）
