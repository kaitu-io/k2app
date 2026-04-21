# `/purchase` CRO 审查

*Date: 2026-04-21 · Skill: `marketing-skills:paywall-upgrade-cro` · Context source: `.agents/product-marketing-context.md`*

---

## Summary scorecard

| Dimension | State |
|---|---|
| Step structure (3-step layout) | ✅ Solid |
| Plan comparison (price + monthly equivalent + strikethrough) | ✅ OK but anchor-reversed |
| Trust signals | ⚠️ Weak — 3 micro-icons below CTA |
| **Objection handling** | ❌ **Critical gap** — refund / trial / payment privacy invisible |
| Payment method transparency | ❌ Zero methods shown before clicking |
| Brand voice (context's 禁用词 / AI 指纹) | ⚠️ Subtitle is AI-fingerprinted |
| Multi-tier readiness (Spec B) | ⚠️ Flat list won't scale to 2D tier × duration grid |

**结论**：3 步骨架合理，但 context 里列的 4 大 top 异议（退款 / 试用 / 设备数 / 付款隐私），**在付费决策页上有 3 个看不到**。

---

## P0 — Do this week（最高 ROI）

### P0-1. 把 7 天无理由退款显示出来 ❗
- **Problem**: i18n 键 `moneyBackGuarantee` + `moneyBackGuaranteeDetail` 存在但**根本没渲染**。这是死代码，同时也是最强转化/信任杠杆之一。
- **Fix**: 在 CTA 旁边加显著徽章："✓ 7 天无理由退款 · 用量 <1GB 全额退到钱包"
- **Where**: `PurchaseStep3.tsx` 里，支付按钮上方 + `MembershipBenefits` 下方小 callout。
- **Expected impact**: 直接回应 "付完用不了怎么办"（context 里 top-4 异议）。初次购买页加上这类徽章通常拉 CVR 5-15%。

### P0-2. 点击前展示可用支付方式 ❗
- **Problem**: 大红色 "立即支付" 按钮，用户完全不知道是否支持支付宝/微信/信用卡/crypto，直到跳转到外部支付页。对隐私敏感的买家是阻塞点。
- **Fix**: 支付按钮上方放一行灰度 logo：支付宝 · 微信支付 · 银联 · Visa/Master · USDT · USDC · BTC · ETH。
- **战略角度**：加密货币支付是 context 里的定位资产 —— 单凭图标就能回应 "付款会被追踪" 这个异议。

### P0-3. 为未决策访客加一个试用路径
- **Problem**: "新用户注册送试用额度" 是 context 里的核心承诺，但在 `/purchase` 页**零可见性**。当前页只给两个选项："bind email to pay" 或 "pay now"。未决策访客直接跳出。
- **Fix**: 未登录用户的 Step 1 里加次级 CTA："先免费试用 → `/install`"，与邮箱绑定输入同级。文案："不确定？先试用再决定。"
- **Expected impact**: 降低 Step 1 跳出；试用→付费转化当前未埋点（context 已标记 TODO），修完同时解锁测量。

### P0-4. 修 AI 指纹文案 + 死码
- **Problem**: 副标题 `"通过三个简单步骤完成您的购买"` 读起来像 AI 生成（与 context 的禁用词模式匹配 —— 结构化列举 + 中立企业腔）。
- **Fix**: 换人声："选套餐 → 支付 → 马上开通" 或 "一分钟开通，5 台设备全家用"（把一个强证据点拉进副标题）。
- **Also**: 死 i18n keys —— `mostPopular`、`moneyBackGuarantee*`、`dollarSign` 要么接上要么删。
- **Also**: `proAuthorization` 文案说 "专业版" 但 tier-rename spec 说大家都是 `basic` —— 待 tier-rename 上线后同步修。

### P0-5. 修价格锚定方向
- **Problem**: `plans.sort((a,b) => a.month - b.month)` 显示 1 年档在前。用户**第一眼看到的月付等价是最贵的**（~$6.50/mo for 1yr）。这是反锚定 —— 5 年档（最低 /mo）才是应该设锚的选项。
- **Fix（最简）**: 在 admin `plans/page.tsx` 把 `highlight=true` 设到 3 年或 5 年档上。"⭐ 最超值" 徽章自然把长档做成视觉锚，不用动代码。
- **Fix（更好）**: 按月数**降序**排，5yr/3yr 视觉堆叠在前。
- **Test**: A/B 升序 vs 降序，测套餐分布 + 整体 CVR。

---

## P1 — Next sprint

### P1-6. 加支付隐私安抚文案
支付方式图标下方一行："商户交易描述为中性摘要，银行看不到具体购买内容。加密货币支付完全匿名。" 直接拆 "付款会被追踪" 异议。

### P1-7. `/purchase` 页内联 FAQ 块（可折叠）
首页已有 FAQ；在 `/purchase` 页面 footer 上方放一个紧凑 4 问版：可以退款吗 / 有试用吗 / 5 台设备够吗 / 付款会被追踪吗。每问答 ≤2 句。不外链 —— 在决策时刻内联回答。

### P1-8. 把授权码路径提到页面顶部
- **Problem**: `"已有授权码？点此兑换"` 在页面最底部。持码用户要滚过整个付费流。
- **Fix**: 顶部卡片（Step 1 上方）："有授权码？[兑换 →]"。持码用户的 happy path 变 2 秒。

### P1-9. 把信任信号行挪到 CTA 上方
`securePayment / instantActivation / support24x7` 目前在支付按钮**下方**。移动端这一行在关键时刻在 fold 下面。挪上去。

### P1-10. 用 webhook 验证替代 `PayResultDialog` 自报
- **Problem**: 支付跳转后弹框问用户 "你付成功了吗？"（成功/失败按钮）。这是信任异味 —— 显得不专业，用户关 tab 会丢订单。
- **Fix**: `payUrl` 返回后服务端轮询订单状态（webhook → 服务端状态）。自动展示结果。30s 仍失败才降级到手动 fallback。
- **Note**: 需后端改动。归类 "伤 CVR 的技术债"。

---

## P2 — Polish

11. **缓和 expired-user 横幅** —— 橙色 `AlertTriangleIcon` + "授权已过期" 把续费框架成警报，不是机会。换蓝色/中性 + "欢迎回来，选套餐续费即刻恢复"。
12. **在 plan 列表上方加 "所有套餐均包含：5 设备 / 全球节点 / 无流量限制"** —— 澄清套餐差异只在时长，非功能分档。
13. **Post-purchase 预览** —— CTA 下方："付款后自动开通，返回此页即可下载客户端。" 降低 "付完什么都没发生" 焦虑。
14. **为 tier × duration 二维网格做准备**（Spec B 阻塞） —— 当前扁平 RadioGroup over `filteredPlans` 在 lite/family/business 上线时会炸。重构成两级结构：顶层 tier 选择（卡片网格）+ 行内时长切换（pill 组）。Spec B 上线前做。
15. **删掉无用的 i18n keys** —— proxy 清理合并后删 `mostPopular`、`moneyBackGuaranteeDetail`、`dollarSign`、`priceSlash`（in `purchaseStep3`）、`hotPlan`、`proxyPurchaseDeprecated`。

---

## Brand voice / context-doc 合规扫描

扫 `purchase.json` 对照 `.agents/product-marketing-context.md` 里的禁用词：

| Issue | Location | Suggested fix |
|---|---|---|
| "通过三个简单步骤完成您的购买"（AI 指纹：结构化列举 + 企业中立腔） | `purchase.subtitle` | "选套餐 → 支付 → 马上开通" |
| "推荐给大多数用户"（空泛无证据） | `recommendedForMostUsers` | "多数用户选这档" 或移除 |
| "最受欢迎" + "最超值" 同时存在（两个互相矛盾的权威标签） | `mostPopular` + `bestValue` | 只用一个（建议 `bestValue`） |
| "Pro" / "专业版" 术语（与 tier-rename 后的 basic 不一致） | `proAuthorization*` | 等 Spec B tier-name 决策；临时改为 "会员" / "订阅" |

---

## A/B 测试排队（按顺序）

1. **退款徽章位置** —— CTA 上方 vs 按钮区 vs 每 plan 卡上。主指标：支付按钮 CTR。
2. **Plan 排序** —— 升序 vs 降序 vs 5yr highlight。主指标：plan 分布（是否向长档偏移）+ 整体 CVR。
3. **副标题文案** —— 当前企业腔 vs 人声重写。主指标：跳出率 + 滚动深度。
4. **未登录用户的试用 CTA vs 支付 CTA** —— 当前（仅 bind-to-pay）vs 加试用路径。主指标：Step 1 完成率。
5. **支付隐私安抚行** —— 有 vs 无。主指标：Step 3 → 支付点击率。

每测试每变体需 ≥500 购买 / 2 周窗口达到显著 —— 若月订单 <2000，改串行。

---

## 一周冲刺建议

1. **Day 1–2**: 同时 ship P0-1（退款徽章）+ P0-2（支付图标）+ P0-4（副标题 + 死键）+ P0-5（5yr `highlight=true`）。全是 i18n + 1 个组件改动。
2. **Day 3**: Ship P0-3（试用替代 CTA）—— 需要决定放哪。
3. **Day 4–5**: 搭测量（Step 1 / Step 2 / Step 3 漏斗事件 + 试用路径点击），为后续 A/B 建基线。
4. **Day 6–7**: 启动 A/B #1（退款位置）。

**ROI 偏差**：Day 1-2 那批 4 项是零风险文案/布局改动，对每个后续买家有复利。**不要等测量结果才上线 —— 先 ship，再测叠加提升**。

---

## Follow-ups

- tier-rename Spec B 上线时，P0-4 的 "Pro / 专业版" 文案要同步收拾
- PayResultDialog 自报改 webhook 验证 → 开技术债 ticket
- i18n 死键清理
- 首页 FAQ 能否抽成组件供 `/purchase` 复用

---

## 后续（建议）

1. 跑 `marketing-skills:ab-test-setup` 详细设计 P0 那批 A/B 测试
2. 跑 `marketing-skills:analytics-tracking` 补齐试用 / 漏斗埋点
3. 修完 P0 后本文件加 "Post-fix metrics" 段落记录基线 → 修后对比
