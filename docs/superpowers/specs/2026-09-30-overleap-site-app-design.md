# Overleap 站点独立成 app — 设计 spec

日期：2026-09-30 · 基线：`main` @ c7507acb · 分支：`feat/overleap-site-app`
前序：`2026-09-04-overleap-site-decoupling-and-uk-positioning-design.md`（同一 app 内按 `pageExtensions` 分页面树）

## 0. 决策（2026-09-29/30 与 David）

1. **overleap.io 从 `web/` 拆出，成为 monorepo 内独立 Next 应用 `sites/overleap/`**。不拆仓库：Center API、`contracts/api-contract.json`、Stripe 建价脚本都在本仓库，拆仓库这些门就失效。
2. **复制精简，不建共享包。** 理由（以 AI 为主要维护者）：重复对 AI 几乎零成本（grep 即可找到兄弟副本）；耦合对 AI 代价高——改共享代码要读懂并回归所有消费方，而 agent 常只跑眼前一侧（本仓库品牌泄漏系列即此形态，守卫 `brand-guard` / `brand-leak-ssr` / `brand-page-tree` / `site-config-keys` 都是在给耦合打补丁）。
   - **必须一致的**（API 路径 / 错误码 / 品牌 hosts / Stripe 价格）→ 由生成的契约与测试保证，共享发生在协议层（JSON），不在代码层。
   - **只是长得像的**（Header、定价卡、购买流、文案）→ 各自演化。
   - 唯一跨站提示：认证 / token 处理源自 `web/src/lib/auth.ts` + `api.ts`，安全修复需两边都查（写进 `sites/overleap/CLAUDE.md`）。
3. **全新搭建、一次切换。** 旧 overleap.io（`web/` + `NEXT_PUBLIC_BRAND=overleap`）在新站完成前保持在线；新站走预览域名；完成后 Amplify overleap app 的 `appRoot` 从 `web` 切到 `sites/overleap`。首页 / 定价 / 购买 / 安装 / 帮助不先原样搬迁，由子项目 2（定价）与 3（信息架构 + 视觉）直接在新 app 里重做。

## 1. 分期

| 阶段 | 内容 | 本 spec |
|---|---|---|
| ① 骨架 | app 脚手架、i18n / 中间件 / API 代理、设计 token、Header / Footer、登录、账户（订阅状态 + 安全）、删号、法务、k2 文档、sitemap / robots、契约测试、CI | **是** |
| ② 定价 | 价格点、跨币种一致、档位、退款 / 试用、App Store / Play 同步 | 另起 spec |
| ③ 营销页 | 首页、`/pricing`（唯一比价页）、结账（`/purchase` 降级为结账一步）、`/install`、`/support`、视觉系统 | 另起 spec |
| ④ 切换 | Amplify appRoot 切换、预览域名验证、回滚方案 | 随 ③ |
| ⑤ 清理 | 从 `web/` 删除全部 overleap 页面 / 文案 / en+ja 内容 / 品牌守卫；开途站回到单品牌 | 另起 spec |

## 2. 阶段 ① 设计

### 2.1 目录与栈

`sites/overleap/`，独立 `package.json` + `yarn.lock`（与 `web/` 相同：不进根 workspace）。栈同 `web/`：Next 15 App Router、React 19、TypeScript、Tailwind 4、shadcn/ui（只带用到的原语）、next-intl、Velite、Vitest、Sentry。

```
sites/overleap/
  CLAUDE.md
  amplify.yml            # appRoot: sites/overleap（切换时由 Amplify console 指向）
  next.config.ts         # /api、/app 以外无 rewrite；/app/* 不代理（admin 只属开途）
  velite.config.ts
  content/{en-GB,en-US,en-AU,ja}/k2/*.md    # 从 web/content 复制
  messages/{en-GB,en-US,en-AU,ja}/*.json     # 只含本站 namespace
  public/                 # overleap 图标 / og / legal/*.md
  src/
    middleware.ts         # locale 协商 + X-K2-Brand: overleap 注入 + / 不缓存
    i18n/{routing,request}.ts
    lib/site.ts           # 品牌常量（名称、域名、联系邮箱、法务署名、CDN、导航、页脚、sitemap 路由）
    lib/api.ts            # 精简 API 客户端（本站实际调用的方法）
    lib/auth.ts
    contexts/AuthContext.tsx
    components/{Header,Footer,EmailLogin,ui/*}
    app/[locale]/{layout,page,not-found,login,account,account/security,delete-account,privacy,terms,k2/[[...path]]}
    app/{layout,page,sitemap,robots,not-found}.ts(x)
  tests/                  # vitest
```

- **没有品牌注册表、没有 `siteBrand()`**：本 app 只有一个品牌，品牌展示词直接写在 `lib/site.ts` 与文案里。开途词禁入由一条守卫扫全 app 保证（`tests/no-kaitu.test.ts`：`开途` / `Kaitu`（大小写敏感的品牌词）/ 支付宝 / 微信支付 / 银联 / `zh-` locale 目录）。
- 语言：`en-GB`（默认，英式母版）/ `en-US` / `en-AU` / `ja`。缺文件回落 en-GB。
- `X-K2-Brand: overleap` 由 `lib/api.ts` 与中间件两处注入（同 `web/`）。

### 2.2 阶段 ① 的页面

| 路由 | 来源 | 说明 |
|---|---|---|
| `/` | 新 | 307 → 协商 locale（`Cache-Control: private, no-store`） |
| `/[locale]` | 新 | **占位首页**：品牌 + 一句定位 + 下载 / 登录入口；③ 替换 |
| `/login` | 迁移 | 邮箱验证码 + 密码登录；`?next=` 回跳 |
| `/account` | 迁移 | 订阅状态、Stripe 客户门户、登出（≙ `OverleapAccountClient`） |
| `/account/security` | 迁移 | 改密码 |
| `/delete-account` | 迁移 | 删号说明页（应用商店要求） |
| `/privacy` `/terms` | 迁移 | 渲染 `public/legal/*.md`；本站自有副本，后续按 UK GDPR 口径改写 |
| `/k2/[[...path]]` | 迁移 | Velite k2 协议文档 + 侧栏；只迁可公开的文档（见 §3.2） |

结账（`/purchase`）不在 ① ——它的信息架构在 ③ 定；① 期间旧站继续承担购买。

### 2.3 设计 token

`globals.css` 只有一套主题（沿用线上 overleap 的深色调色板作为起点），token 命名采用语义名（`--background` / `--foreground` / `--primary` / `--muted` …，shadcn 约定）。③ 的视觉系统在此基础上改值，不改名。

### 2.4 测试与门

- `tests/cross-layer-contract.test.ts`：读 `contracts/api-contract.json`，断言 `host(lib/site.ts baseUrl) ∈ contract.brands.overleap.hosts`（宿主归属，非字符串相等），且 `api.ts` 引用的错误码都在契约 `errorCodes` 内。
- `tests/no-kaitu.test.ts`：见 2.1。
- `tests/messages-parity.test.ts`：四个 locale × 全部 namespace，key 集合与 en-GB 相等。
- `tests/middleware.test.ts`：`/` no-store + 307、`Accept-Language: en` → en-GB、`zh-*` 路径 → 301 en-GB、`/api/*` 注入 `X-K2-Brand`、`/app/*` 404。
- 页面 SSR 测试：真实文案渲染（不 mock next-intl），零原始 key、零未填 `{占位}`。
- CI：`.github/workflows/ci.yml` 增一个 job：`sites/overleap` 下 `yarn install --frozen-lockfile && yarn test && yarn build`。
- 本地验证：`yarn dev` 起站，用浏览器逐页实测（en-GB / ja 各一遍，含登录→账户闭环，打本地或 dev Center）。

### 2.5 不变量

- `web/` 在阶段 ①–④ **零改动**（kaitu.io 与旧 overleap.io 行为不变）；唯一例外是根 `CLAUDE.md` 项目结构表加一行。
- 新 app 不 import `web/` 任何文件（守卫：`tests/no-cross-import.test.ts` 扫 `../../web`、`@/…` 以外的相对跨目录引用）。
- Stripe key、Center 凭据永不入 git。

## 3. 实现记录（阶段 ①，2026-09-30）

### 3.1 与原设计的偏差

- `/403` 未迁移：本站无按角色拦截的页面；未知路由由 `[locale]/not-found.tsx` 承接。
- 登录态以用户档案为准（`GET /api/user/info` 成功 ⇔ 已登录）。`web/` 的 AuthContext 读取该接口并不返回的 `id` / `email`（Go `DataUser` 无此二字段），在新站被去掉；邮箱取 `loginIdentifies`。
- 守卫落在 `tests/source-guards.test.ts`（他品牌词 / 中国支付渠道 / 跨 `web/` import / locale 目录），替代 2.4 里分开写的 `no-kaitu` 与 `no-cross-import`。
- 消息 key 由 next-intl `AppConfig` 绑定 en-GB 文件做**编译期**校验（2.4 未列，新增）。
- Sentry 只报错误：关闭 Replay、`sendDefaultPii: false`、`tracesSampleRate: 0`；不设 Cookie 同意横幅（仅严格必要 Cookie，PECR 豁免）。
- 所有守卫做过变异验证；`negotiateLocale` 的 `ja-JP` 分支原被用例遮蔽，已补用例。

### 3.2 迁移中发现、留给后续阶段的问题

| # | 问题 | 现状 | 归属 |
|---|---|---|---|
| 1 | 隐私政策 1.3 写“记录连接时间戳和所用服务器位置”，与首页 “No logs” 字面冲突 | 新站原文照搬，未改 | ②/③ + 法务 |
| 2 | `content/en-GB/k2/{quickstart,server,client}.md` 标 `brand: kaitu`，线上 overleap.io 的 `/k2/quickstart` 404，而页脚 “Run your own server” 与首页 “Built in the open” 都指向自部署 | 新站删掉三篇与该页脚链接 | ③（写 overleap 自有的自部署文档，或改文案） |
| 3 | k2 文档侧栏含 “Congestion Control Under Censorship”（`vs-bbr.md`），违反 Overleap 面不出现审查叙事的规则 | 原样迁移 | ③ 内容 |
| 4 | 线上 Cookie 横幅文案提“邀请码”（开途概念），`/purchase` 标题 “Purchase Pro Plan”、权益列表为开途卖点；`web/messages/en-*/account.json` 含中文代付文案 | 新站不存在这些 | ⑤ 随 `web/` 清理消失 |
| 5 | 线上 Sentry 对 overleap.io 访客 100% 会话录屏 + PII | 新站已关 | ④ 切换后自然消失 |
| 6 | `web/` 账户守卫跳 `/login?redirect=`，登录页读 `?next=` → 登录后不回原页 | 新站统一 `?next=` + `safeNext` 防开放重定向 | ⑤（或在 `web/` 单独小修，影响开途） |
