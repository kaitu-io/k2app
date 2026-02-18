# i18n System

| Field | Value |
|-------|-------|
| Feature | Internationalization (i18n) System |
| Version | 0.4.0 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

---

## Overview

k2app 的国际化系统基于 i18next + react-i18next 构建，支持 7 种语言，采用 namespace 分片 + 动态加载架构。zh-CN 为唯一 source of truth，其他 locale 必须覆盖 zh-CN 的全部 key。整套系统服务于 webapp 层，跨 Desktop (Tauri)、Mobile (Capacitor)、Web (standalone) 三端共用同一份翻译资源。

核心设计决策：
- **zh-CN 为主语言**：新文本先写 zh-CN，再手动翻译到其他 locale
- **Namespace 分片**：13 个 namespace 按功能域拆分，支持按需加载
- **语言切换全量预加载**：切换语言时一次性加载目标 locale 的所有 namespace，避免闪烁
- **normalizeLanguageCode** 统一处理浏览器返回的各种语言变体

---

## Product Requirements

### 支持的语言 (7 Locales)

| Code | 语言 | 角色 | 国旗 |
|------|------|------|------|
| `zh-CN` | 简体中文 | Primary (source of truth) | CN |
| `en-US` | English (US) | Secondary | US |
| `en-GB` | English (UK) | Manual translation | GB |
| `en-AU` | English (AU) | Manual translation | AU |
| `ja` | 日本語 | Manual translation | JP |
| `zh-TW` | 繁體中文 | Manual translation | TW |
| `zh-HK` | 繁體中文 (香港) | Manual translation | HK |

### 用户需求

1. 自动检测浏览器/系统语言，匹配最接近的 supported locale
2. 用户可在 Account 页面手动切换语言，选择立即生效
3. 语言偏好持久化到 localStorage (`kaitu-language` key)
4. 不支持的语言自动 fallback 到 zh-CN
5. 所有 UI 文本通过 i18n key 引用，禁止 hardcoded 文本
6. 国家/地区名称随语言切换同步变化（基于 `i18n-iso-countries` 库）

---

## Technical Decisions

### 1. i18next 配置

初始化流程 (`webapp/src/i18n/i18n.ts`):

```
1. 读取 localStorage('kaitu-language') 或 navigator.language
2. normalizeLanguageCode() 标准化 → 得到 initialLang
3. preloadResources(initialLang) — 并发加载全部 13 个 namespace JSON
4. i18n.init() — LanguageDetector + initReactI18next 插件
5. 导出 i18nPromise — main.tsx 中 await 后再渲染 React
```

关键配置项：

```typescript
{
  lng: initialLang,             // 初始语言
  fallbackLng: 'zh-CN',        // 回退语言
  defaultNS: 'common',         // 默认 namespace
  ns: [...namespaces],         // 全部 13 个 namespace
  partialBundledLanguages: true, // 允许只 bundle 当前语言
  interpolation: { escapeValue: false }, // React 已做 XSS 防护
  detection: {
    order: ['localStorage', 'navigator'],
    caches: ['localStorage'],
    lookupLocalStorage: 'kaitu-language'
  }
}
```

### 2. Namespace 架构 (13 Namespaces)

资源文件按功能域拆分为 13 个 namespace，每个 namespace 对应一个 JSON 文件：

| Namespace | 对应功能域 | 包含的逻辑子域 (namespaceMapping) |
|-----------|-----------|--------------------------------|
| `common` | 全局通用 | common, status, errors, loadingAndEmpty, messages, brand, features |
| `nav` | 导航 & 顶栏 | navigation, appBar, appBarConnector, appBarMembership, layout |
| `auth` | 认证 & 登录 | auth, updateEmail, guard |
| `account` | 账户设置 | account, devices, proHistory, memberManagement, password |
| `dashboard` | 仪表板 & 连接 | dashboard, troubleshooting, versionComparison, tunnels |
| `purchase` | 购买 & 套餐 | purchase, plan, memberSelection, deviceInstall |
| `invite` | 邀请 & 邀请码 | invite, inviteCodeList |
| `retailer` | 分销商 | retailer, retailerStats, retailerRule |
| `wallet` | 钱包 & 提现 | wallet |
| `startup` | 启动 & 引导 | startup, serviceNotInstalled, upgradeRequired, forceUpgrade, serviceStatus, app |
| `theme` | 主题设置 | theme |
| `ticket` | 工单 & FAQ | ticket, faq, issues |
| `feedback` | 问题反馈 | feedback |

Namespace 定义文件 `webapp/src/i18n/locales/namespaces.ts` 由脚本自动生成（`scripts/i18n/split-namespaces.js`），包含：
- `namespaces` 常量数组
- `Namespace` 类型
- `defaultNamespace = 'common'`
- `namespaceMapping` — 将逻辑子域名映射回物理 namespace 文件

### 3. Key 引用约定

两种 t() 调用模式并存：

**模式 A — 带 namespace 前缀（跨 namespace 引用，最常见）：**

```typescript
const { t } = useTranslation();
t('dashboard:dashboard.rule.global')    // namespace:section.key
t('common:messages.logoutSuccess')
t('auth:updateEmail.title')
t('invite:invite.loadInviteCodeFailed')
t('purchase:purchase.hotPlan')
```

**模式 B — 指定默认 namespace（同 namespace 内引用）：**

```typescript
const { t } = useTranslation('dashboard');
t('tunnels.loginToSync')               // 省略 namespace 前缀
t('tunnels.cloudNodes.title')

const { t } = useTranslation('auth');
t('guard.defaultMessage')              // 直接用 section.key
```

**约定**：使用 `useTranslation('auth')` 时，调用 `t('title')` 而不是 `t('auth:title')`。Namespace scoping 由 useTranslation 参数决定。

### 4. 语言检测 & 标准化

`normalizeLanguageCode()` 将浏览器返回的各类语言代码映射到支持的 7 种 locale：

| 输入 | 输出 | 规则 |
|------|------|------|
| `zh` / `zh-SG` / `zh-MY` / `zh-Hans` | `zh-CN` | 简体中文变体 → zh-CN |
| `zh-Hant` | `zh-TW` | 繁体脚本标签 → zh-TW |
| `zh-MO` | `zh-HK` | 澳门 → 香港 |
| `en` / `en-CA` | `en-US` | 通用英语/加拿大 → 美式 |
| `en-NZ` | `en-AU` | 新西兰 → 澳洲 |
| `en-ZA` / `en-IE` | `en-GB` | 南非/爱尔兰 → 英式 |
| `ja-JP` | `ja` | 日本变体 → ja |
| 其他任何 | `zh-CN` | 不支持的语言 → 默认 zh-CN |

支持大小写不敏感匹配，有完整单元测试覆盖 (`webapp/src/i18n/__tests__/i18n.test.ts`)。

### 5. 语言切换

`changeLanguage()` 函数 (`webapp/src/i18n/i18n.ts`)：

```
1. normalizeLanguageCode(lang) 标准化
2. 检查目标语言是否已加载（hasResourceBundle for all namespaces）
3. 若未加载 → preloadResources() 并发加载全部 namespace
4. addResourceBundle() 注入到 i18next 实例
5. i18n.changeLanguage() 切换
6. localStorage.setItem('kaitu-language', normalizedLang) 持久化
```

### 6. 动态加载机制

资源文件通过 Vite 动态 import 加载：

```typescript
const module = await import(`./locales/${lang}/${ns}.json`);
```

- Vite 在构建时自动 code-split 每个 JSON 文件为独立 chunk
- 首次加载只 bundle 初始语言的全部 namespace
- 切换语言时按需加载目标语言的资源
- 加载失败时自动 fallback 到 zh-CN 对应 namespace

### 7. 国家名称本地化

`webapp/src/i18n/countries.ts` 基于 `i18n-iso-countries` 库提供国家名称翻译：

- 注册 zh、en、ja 三种语言的国家名称数据
- `getCountryName(alpha2, lang?)` — 根据当前 i18n 语言返回本地化国家名
- 用于节点列表、服务器位置等场景显示国家名称

### 8. 错误码 → i18n 映射

错误显示规则：`response.message` 仅用于 debug 日志，用户看到的是 `response.code` 映射的 i18n 文本。

```
Backend response.code → errorCode.ts 映射 → i18n key → t() 翻译 → 用户看到本地化文本
```

错误相关 key 集中在 `common` namespace 的 `errors` section 下。

### 9. 插值 (Interpolation)

i18next 标准插值语法，在翻译文本中用 `{{variable}}` 占位：

```json
{
  "retryAfter": "{{seconds}}s后重试",
  "codeSentTo": "验证码已发送至 {{email}}",
  "saveAmount": "立省${{amount}}",
  "proAuthorization": "Pro授权 {{months}} 个月"
}
```

组件中调用：

```typescript
t('auth:updateEmail.retryAfter', { seconds: countdown })
t('purchase:purchase.saveAmount', { amount: discount.toFixed(2) })
```

### 10. 双模式 Locale 文件结构

当前存在两种文件布局（历史迁移遗留）：

```
locales/
├── zh-CN.json          ← 旧版单体文件（monolithic，包含全部 key）
├── zh-CN/              ← 新版分片目录
│   ├── common.json
│   ├── auth.json
│   ├── dashboard.json
│   └── ... (13 files)
├── en-US.json          ← 旧版单体文件
├── en-US/              ← 新版分片目录
│   ├── common.json
│   └── ...
└── config.json         ← source/targets 配置
```

i18n 运行时使用分片目录 (`${lang}/${ns}.json`)。旧版单体文件为兼容遗留，新增 key 应只写入分片文件。

---

## Key Files

| 文件 | 职责 |
|------|------|
| `webapp/src/i18n/i18n.ts` | i18next 初始化、changeLanguage、normalizeLanguageCode、languages 定义 |
| `webapp/src/i18n/locales/namespaces.ts` | Namespace 列表、类型、namespaceMapping、动态加载函数（自动生成） |
| `webapp/src/i18n/locales/config.json` | source/targets 语言配置（check-i18n 使用） |
| `webapp/src/i18n/countries.ts` | 国家名称本地化（i18n-iso-countries 封装） |
| `webapp/src/i18n/__tests__/i18n.test.ts` | normalizeLanguageCode 单元测试 |
| `scripts/check-i18n.mjs` | i18n key 完整性检查脚本 |
| `webapp/src/i18n/locales/zh-CN/*.json` | 主语言资源（13 个 namespace 文件） |
| `webapp/src/i18n/locales/{locale}/*.json` | 各翻译语言资源 |
| `webapp/src/utils/errorHandler.ts` | 错误处理 + i18n 错误消息显示 |
| `webapp/src/utils/errorCode.ts` | 错误码 → i18n key 映射 |
| `webapp/src/main.tsx` | 入口：`await i18nPromise` 确保 i18n 就绪后再渲染 |

---

## check-i18n.mjs — 完整性检查工具

`scripts/check-i18n.mjs` 用于检测各 locale 翻译的完整性：

### 工作原理

1. 扫描 `zh-CN/` 目录下所有 namespace JSON 文件作为 baseline
2. 递归 flatten 每个 JSON 的嵌套 key 结构（e.g. `errors.network.timeout`）
3. 逐一对比其他 6 个 locale 目录：
   - **Missing keys**：zh-CN 有但目标 locale 缺失的 key
   - **Extra keys**：目标 locale 有但 zh-CN 没有的 key
   - **Missing namespaces**：zh-CN 有的 namespace 文件在目标 locale 完全缺失
   - **Extra namespaces**：目标 locale 多出的 namespace 文件

### 使用方式

```bash
# 完整详细报告：显示每个 locale 的 missing/extra key 列表
node scripts/check-i18n.mjs

# CI 模式：仅输出汇总表，有 missing key 时 exit 1
node scripts/check-i18n.mjs --ci
```

### 输出示例

```
Primary: zh-CN (13 namespaces, 650 keys)

Locale    Missing     Extra  Missing Namespaces
----------------------------------------------------------------------
en-AU          3         0  -
en-GB          3         0  -
en-US          5         2  -
ja             8         0  -
zh-HK          3         0  -
zh-TW          3         0  -
----------------------------------------------------------------------
Total: 25 missing, 2 extra across 6 locales (baseline: 650 keys)
```

---

## Acceptance Criteria

### 语言检测 & 切换
- [x] 首次访问自动检测浏览器语言并匹配最近 locale
- [x] 不支持的语言 fallback 到 zh-CN
- [x] 语言偏好持久化到 `localStorage('kaitu-language')`
- [x] 切换语言时全量加载目标 locale 资源后再切换，无闪烁
- [x] normalizeLanguageCode 覆盖 zh-SG/zh-Hans/zh-Hant/en-CA/en-NZ/en-ZA 等变体
- [x] normalizeLanguageCode 大小写不敏感

### Namespace 架构
- [x] 13 个 namespace 按功能域拆分
- [x] 默认 namespace 为 `common`
- [x] namespaceMapping 将逻辑子域映射回物理 namespace
- [x] 资源通过 Vite dynamic import 按需加载

### 翻译完整性
- [x] zh-CN 为 source of truth，包含全部 key
- [x] check-i18n.mjs 可检测 missing/extra keys
- [x] CI 模式 (`--ci`) 有 missing key 时 exit 1
- [x] 新文本先加到 zh-CN，再翻译到其他 locale

### 组件集成
- [x] 所有 UI 文本通过 `t()` 函数引用
- [x] 支持 `namespace:section.key` 跨 namespace 引用
- [x] 支持 `useTranslation('ns')` + `t('key')` 同 namespace 引用
- [x] 插值语法 `{{variable}}` 正常工作
- [x] 错误码通过 `response.code` → i18n key 映射显示本地化文本
- [x] 国家名称随语言切换同步变化

### 启动顺序
- [x] main.tsx 中 `await i18nPromise` 确保 i18n 初始化完成后再渲染 React
- [x] i18n 初始化在 Sentry 之后、平台注入之前

### 测试
- [x] normalizeLanguageCode 单元测试覆盖全部映射规则和边界情况
- [x] check-i18n.mjs 可作为 CI 卡点使用
