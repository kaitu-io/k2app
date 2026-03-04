# Onboarding Guide Design

用户首次登录后的交互式使用指引，帮助新用户了解核心功能。

## 触发条件

- **首次登录自动弹出**：通过 `_platform.storage` 存储 `onboarding_completed` 标记
- 未标记完成的用户，登录后进入 Dashboard 自动启动引导
- 用户可随时跳过（skip），跳过也标记完成

## 技术方案

- **库**: `react-joyride`（controlled mode）
- **状态管理**: Zustand store `onboarding.store.ts`
- **持久化**: `_platform.storage`（跨平台加密存储）
- **路由感知**: 监听 `useLocation()` + phase 决定当前 steps
- **元素定位**: 目标元素加 `data-tour` 属性
- **iOS 适配**: `window._platform.os === 'ios'` 时跳过购买步骤 + 隐藏购买导航项

## 引导流程

### Phase 1: 展开/折叠连接区域

- **页面**: Dashboard (`/`)
- **目标元素**: CollapseToggle（展开/折叠按钮）, `data-tour="collapse-toggle"`
- **行为**: spotlight 高亮，tooltip 说明
- **推进条件**: 用户点击 "下一步" 或 点击了 toggle

### Phase 2: 连接/断开 VPN

- **页面**: Dashboard (`/`)
- **目标元素**: ConnectionButton 或 CompactConnectionButton, `data-tour="connect-button"`
- **行为**: spotlight 高亮连接按钮区域
- **推进条件**: 用户点击 "下一步"

### Phase 3: 反馈按钮

- **页面**: Dashboard (`/`)
- **目标元素**: FeedbackButton (FAB), `data-tour="feedback-button"`
- **行为**: spotlight 高亮浮动按钮
- **推进条件**: 用户点击 FAB（自然触发导航到 /submit-ticket）

### Phase 4: 反馈页面

- **页面**: `/submit-ticket`
- **目标元素**: 页面整体 or BackButton, `data-tour="submit-ticket-page"`
- **行为**: 居中 tooltip（无 spotlight target），展示真诚文案
- **推进条件**: 用户点击返回按钮，回到 Dashboard

### Phase 5: 邀请功能

- **页面**: Dashboard (`/`)
- **目标元素**: 邀请导航项（SideNavigation / BottomNavigation）, `data-tour="nav-invite"`
- **行为**: spotlight 高亮邀请导航
- **推进条件**: 用户点击邀请导航项（自然触发导航到 /invite）

### Phase 6: 邀请页面

- **页面**: `/invite`
- **目标元素**: 页面整体, `data-tour="invite-page"`
- **行为**: 居中 tooltip，展示真诚文案
- **推进条件**: 用户点击 "知道了" 或导航离开

### Phase 7: 购买功能（非 iOS）

- **页面**: Dashboard (`/`) 或 `/invite`
- **目标元素**: 购买导航项, `data-tour="nav-purchase"`
- **行为**: spotlight 高亮购买导航
- **推进条件**: 用户点击购买导航 → 引导完成 ✅

### iOS 流程

Phase 1 → 2 → 3 → 4 → 5 → 6 → ✅ 完成（跳过 Phase 7）

## 文案设计

原则：**利他、真诚、客观、积极**

### Phase 1 — 折叠/展开

```
zh-CN: 点击这里可以展开或折叠连接面板，让界面更简洁
en-US: Tap here to expand or collapse the connection panel
```

### Phase 2 — 连接按钮

```
zh-CN: 点击按钮即可一键连接，再次点击断开。就这么简单！
en-US: Tap the button to connect instantly. Tap again to disconnect. That's it!
```

### Phase 3 — 反馈按钮

```
zh-CN: 遇到任何问题，点击这个按钮告诉我们。您也可以把它拖到顺手的位置。
en-US: Having any issues? Tap this button to let us know. You can also drag it to a spot you prefer.
```

### Phase 4 — 反馈页面

```
zh-CN: 我们非常重视您的使用体验。
       无论是连接不稳定、速度不理想，还是任何不顺畅的地方，都请在这里告诉我们。
       每一条反馈我们都会认真对待，尽快改进。
en-US: Your experience matters to us.
       Whether it's connection issues, speed concerns, or anything that doesn't feel right — please let us know here.
       We read every piece of feedback and work to improve quickly.
```

### Phase 5 — 邀请导航

```
zh-CN: 点击这里查看邀请奖励，邀请好友一起用
en-US: Tap here to see invite rewards and share with friends
```

### Phase 6 — 邀请页面

```
zh-CN: 真诚地邀请您把 Kaitu 推荐给需要的朋友。
       作为一个专注品质的小团队，我们在各平台的推广渠道非常有限，主要靠用户间的口碑传播。
       您每邀请一位好友注册，都能获得免费使用天数作为感谢。
en-US: We'd genuinely appreciate it if you could recommend Kaitu to friends who need it.
       As a small team focused on quality, our marketing channels are very limited — we rely on word-of-mouth from people like you.
       For every friend you invite, you'll earn free days as our thanks.
```

### Phase 7 — 购买导航（非 iOS）

```
zh-CN: 点击这里选择适合您的套餐，开始使用完整服务
en-US: Tap here to choose a plan that works for you and get started
```

### 通用按钮文案

```
zh-CN:
  next: 下一步
  skip: 跳过引导
  back: 上一步
  close: 知道了
  last: 完成

en-US:
  next: Next
  skip: Skip tour
  back: Back
  close: Got it
  last: Done
```

## 代码结构

```
webapp/src/
├── stores/onboarding.store.ts        # Zustand store: phase, active, skip, complete
├── components/OnboardingGuide.tsx     # react-joyride wrapper, phase→steps 映射
├── i18n/locales/zh-CN/onboarding.json # 中文文案
├── i18n/locales/en-US/onboarding.json # 英文文案
└── (其他 locale 文件)
```

## 改动清单

1. **新增** `react-joyride` 依赖
2. **新增** `onboarding.store.ts` — Zustand store
3. **新增** `OnboardingGuide.tsx` — 引导组件（在 Layout 层渲染）
4. **新增** `onboarding.json` — 7 个 locale 的 i18n 文件
5. **修改** `CollapsibleConnectionSection.tsx` — CollapseToggle 加 `data-tour="collapse-toggle"`
6. **修改** `ConnectionButton.tsx` — 加 `data-tour="connect-button"`
7. **修改** `FeedbackButton.tsx` — 加 `data-tour="feedback-button"`
8. **修改** `SideNavigation.tsx` — 邀请/购买导航项加 `data-tour`，iOS 隐藏购买
9. **修改** `BottomNavigation.tsx` — 同上
10. **修改** `SubmitTicket.tsx` — `BackButton` 去掉 `to="/faq"`，改为 `navigate(-1)`
11. **修改** `Layout.tsx` — 渲染 `<OnboardingGuide />`

## iOS 购买隐藏

在 `SideNavigation.tsx` 和 `BottomNavigation.tsx` 中，当 `window._platform.os === 'ios'` 时过滤掉 purchase 导航项：

```typescript
const primaryNavItems = useMemo(() => {
  const items = [ /* ... */ ];
  // iOS: hide purchase entry (App Store policy)
  if (window._platform.os === 'ios') {
    return items.filter(item => item.path !== '/purchase');
  }
  return items;
}, [/* ... */]);
```

## react-joyride 集成要点

- **Controlled mode**: `run={active}`, `stepIndex={currentStepIndex}`, `callback={handleJoyrideCallback}`
- **暗色主题**: 自定义 `styles` 配合 MUI dark theme
- **Spotlight**: `disableOverlayClose: false`, `spotlightClicks: true`（允许点击高亮区域内的元素）
- **跨页 step**: 每个 phase 对应一组 steps，切换页面时 joyride 暂停/恢复
- **Tooltip 自定义**: 使用 `tooltipComponent` 自定义 React 组件匹配 MUI 风格
