# Feature Spec: 网站 K2 协议专区 + 首页改版 + Terminal Dark 主题 + SEO/GEO 优化

## Meta

| Field | Value |
|-------|-------|
| Status | implemented |
| Version | 1.2 |
| Created | 2026-02-21 |
| Updated | 2026-02-21 |

## 概述

对 `web/` Next.js 网站进行全面改版：

1. 新增 `/k2/` 协议文档专区（Velite Markdown + 侧边栏布局）
2. 首页重写，基于 k2v5 真实技术特点展示竞争优势
3. 全站强制 Terminal Dark 主题，移除亮色模式
4. 新增 `/k2/vs-hysteria2` 拥塞控制效果对比页
5. 全面 SEO/GEO 优化（Server Component 改造 + 结构化数据 + sitemap）

## 目标用户

- 技术小白用户：理解 k2 的技术领先性，学会部署 k2/k2s
- 开发者/自建用户：查阅协议细节，快速 bootstrap

## 技术方案

### 1. Velite Markdown + 侧边栏布局

**路由方案**：创建 `web/src/app/[locale]/k2/[[...path]]/page.tsx`

- Next.js 静态路由优先级高于 `[...slug]` catch-all，自动拦截 `/k2/*`
- 从 Velite 拉取 `slug` 以 `k2/` 开头的 markdown 内容
- Layout 层包含左侧固定侧边栏导航（React 组件）
- 主区域渲染 markdown 内容（`prose dark:prose-invert`）

**SEO/GEO 优势**：
- Velite 在 build time 生成静态 HTML → 完全可爬取
- 语义化 HTML 结构对搜索引擎和 AI 抓取友好
- 每个页面独立 URL → 搜索结果精准定位
- 内容更新不需要改代码，只改 markdown 文件

**内容文件位置**：`web/content/{locale}/k2/*.md`

**Velite schema 扩展**：在 frontmatter 中增加可选字段：
- `order: number` — 侧边栏排序权重
- `section: string` — 侧边栏分组（如 "入门"、"技术"、"对比"）

### 2. /k2/ 页面结构

```
/k2/                 → Overview 落地页（k2 是什么，核心特性一句话，链接到子页面）
├─ /k2/quickstart    → 1 分钟快速开始（k2s 安装 + k2 连接，完整 copy-paste 流程）
├─ /k2/server        → k2s 服务端详细部署（安装、运行、自动配置解释、Docker）
├─ /k2/client        → k2 客户端使用（CLI、proxy 模式、日常命令、排错）
├─ /k2/protocol      → 协议技术详解（三层身份模型、ECH、TLS 指纹、Wire 帧格式、传输管理器）
├─ /k2/stealth       → 隐身伪装技术（ECH 伪造、反向代理、TLS record padding、证书 pinning）
├─ /k2/vs-hysteria2  → vs Hysteria2 拥塞控制效果对比（仅展示效果，不透露算法细节）
└─ /k2/benchmark     → 预留：综合性能测评（暂不实现，等数据就绪）
```

**侧边栏导航结构**：
```
入门
  Overview
  快速开始
  服务端部署
  客户端使用
技术
  协议详解
  隐身伪装
对比
  vs Hysteria2
  (未来: vs WireGuard, vs VLESS+Reality, vs Shadowsocks)
```

### 3. 首页改版

**问题**：当前首页内容描述的是 "MPTCP 多路径聚合"、"CA 证书模拟"、"AES-256-GCM + ECDSA P-256" 等，与实际 k2v5 协议完全不符。

**新首页结构**：

#### Hero 区域
- 标题：突出 k2 是隐身网络隧道
- 副标题：一行命令部署，抗审查，不可检测
- CTA：快速开始 + 查看协议
- 终端动画：展示 `k2s run` → 生成证书 → 打印 connect URL 的过程

#### 核心技术特性区（替换当前 "Protocol Technology Showcase"）

基于 k2v5 真实技术重写，6 个特性卡片：

| 特性 | 说明 |
|------|------|
| ECH 隐身 | Encrypted Client Hello 隐藏真实 SNI，外观与 Cloudflare 流量一致 |
| 零配置部署 | 一行命令安装运行，自动生成证书、ECH 密钥、auth token |
| QUIC + TCP-WS 复合传输 | QUIC/H3 主传输，UDP 被封锁时自动降级 TCP-WebSocket |
| 自研拥塞控制 | 专为受限网络优化的自适应拥塞控制算法（细节将在开源日公开） |
| 反向代理伪装 | 非 ECH 连接透明转发到真实网站，主动探测看到正常 HTTPS 站点 |
| 自签名 + Pin | 无 CA 依赖，无 CT 日志暴露，证书指纹 pinning 验证 |

#### 技术对比区（替换当前星级评分对比）

对比表格：k2 vs 市面主流方案（WireGuard / VLESS+Reality / Hysteria2 / Shadowsocks）

| 维度 | k2 | WireGuard | VLESS+Reality | Hysteria2 | Shadowsocks |
|------|-----|-----------|---------------|-----------|-------------|
| ECH 隐身 | ✓ | ✗ | ✗ | ✗ | ✗ |
| TLS 指纹伪装 | ✓ (uTLS Chrome/Firefox/Safari) | N/A | ✓ (Reality) | ✗ | ✗ |
| 主动探测防御 | ✓ (反向代理) | ✗ | ✓ (steal cert) | ✗ | ✗ |
| QUIC 传输 | ✓ (主传输) | ✗ | 可选 | ✓ (仅QUIC) | ✗ |
| TCP 降级 | ✓ (自动) | ✗ | 需手动 | ✗ | ✓ |
| 拥塞控制优化 | ✓ (自研算法) | 无 | 无 | Brutal(固定速率) | 依赖系统 |
| 零配置部署 | ✓ | 需配置密钥对 | 需配置UUID/dest | 需配置密码 | 需配置密码/加密 |
| CT 日志零暴露 | ✓ (自签名+pin) | N/A | ✗ (偷取真证书) | ✗ (Let's Encrypt) | N/A |
| 端口复用 | ✓ (QUIC+TCP共享443) | ✗ (独占UDP端口) | ✗ | ✗ | ✗ |

#### 快速开始区（替换当前下载区域）

终端风格的步骤展示：
```
Server (1 min)               Client (30 sec)
$ curl ... | sudo sh -s k2s  $ curl ... | sudo sh -s k2
$ k2s run                    $ k2 up k2v5://...
```

#### 下载区域
保留当前 4 平台下载卡片，但改为 Terminal Dark 风格。

### 4. /k2/vs-hysteria2 拥塞控制对比

**核心原则**：只展示效果，不透露算法名称和实现细节。

页面内容：
- 标题：k2 vs Hysteria2 — 受限网络下的拥塞控制对比
- 背景：在高丢包、带宽受限、被审查的网络环境下，拥塞控制算法决定实际体验
- 对比维度：
  - 丢包率 vs 吞吐量恢复（Hysteria2 Brutal 固定速率 vs k2 自适应）
  - 延迟稳定性（高丢包环境下 RTT 变化）
  - 带宽利用率（低带宽和高带宽场景）
  - 与其他流量共存时的公平性
- 展示形式：效果对比图表（Chart.js 或静态 SVG）
- 结论：k2 在受限网络下自适应表现优于固定速率方案
- 尾注："k2 拥塞控制算法细节将在开源日公开"

**数据来源**：需要提供实际测试数据。如暂无数据，页面先以定性对比为主，预留数据填充位置。

### 5. Terminal Dark 主题

**全局变更**：
- 移除 `ThemeToggle` 组件及其在 Header 中的引用
- `EmbedThemeProvider` 中 `defaultTheme` 从 `"system"` 改为 `"dark"`
- 移除 `enableSystem` 属性
- `globals.css` 中 `:root` 变量改为暗色值（或直接使用 `.dark` 的值）

**Terminal Dark 色彩系统**：
```
--background: #0a0a0f (深黑)
--foreground: #e0e0e0 (浅灰文字)
--card: #111118 (卡片背景)
--card-foreground: #e0e0e0
--primary: #00ff88 (终端绿，主要 accent)
--secondary: #00d4ff (青色，次要 accent)
--muted: #1a1a22
--muted-foreground: #666
--border: rgba(0, 255, 136, 0.15) (绿色微光边框)
--accent: #00ff88
```

**字体**：
- 标题和正文：系统无衬线字体（Inter 保留）
- 代码块、终端示例、技术参数：JetBrains Mono 或 Fira Code（Google Fonts 加载）
- 导航栏品牌名可用等宽字体增加 hack 感

**组件风格调整**：
- 卡片：深色背景 + 细绿色边框，hover 时边框发光增强
- 按钮：主要按钮绿色背景黑色文字，次要按钮边框样式
- 代码块：终端窗口外观（标题栏 + 红绿黄按钮 + $ 提示符）
- 链接：绿色，hover 时下划线

**注意**：仅影响 `[locale]` layout 的公共页面。`(manager)` admin dashboard 不受影响。

### 6. 首页内容纠错

当前 `hero.json` 中的错误内容必须修正：

| 当前描述 | 实际技术 | 修正 |
|----------|----------|------|
| MPTCP 多路径聚合 | k2v5 无 MPTCP | 改为 QUIC + TCP-WS 复合传输 |
| CA 证书模拟/伪装 | ECH + 自签名 + pin | 改为 ECH 隐身 + 证书 pinning |
| AES-256-GCM + ECDSA P-256 | TLS 1.3（协议层加密） | 改为 TLS 1.3 加密 |
| PBKDF2 十万次密钥派生 | 无此机制 | 删除 |
| smux 流复用 | QUIC 原生多路复用 | 改为 QUIC 原生 streams |
| WebSocket/HTTP/2 传输 | QUIC/H3 主 + TCP-WS fallback | 修正传输描述 |
| 5000 并发连接 | 无此限制描述 | 删除或改为实际参数 |

### 7. SEO/GEO 全面优化

#### 7.1 致命问题修复：Server Component 改造

**现状**：所有 20 个公共页面都标记了 `"use client"`，初始 HTML 几乎为空。AI 爬虫（ChatGPT/Perplexity/Claude search）通常不执行 JS，看到空页面。

**修复**：首页 + /k2/ 全部页面改为 Server Component。

```tsx
// 之前 (CSR — SEO 有害)
"use client";
import { useTranslations } from 'next-intl';
export default function Home() {
  const t = useTranslations();
  // ...
}

// 之后 (SSR — SEO 友好)
import { getTranslations, setRequestLocale } from 'next-intl/server';
export default async function Home({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale);
  const t = await getTranslations();
  // ...
}
```

**交互部分处理**：Header（auth 状态检查、语言切换）保持为 client component 子组件，通过 composition pattern 嵌入 server-rendered 页面。

**本次改造范围**：
- 首页 `page.tsx` — 必须（配合改版）
- `/k2/[[...path]]/page.tsx` — 新建即为 Server Component
- `/k2/[[...path]]/layout.tsx` — 新建即为 Server Component
- 其他 18 个页面 — 记录为 TODO，后续批量改造

#### 7.2 metadata 纠错 + 各页面独立 metadata

**metadata.ts 内容修正**：

| 当前 | 修正 |
|------|------|
| "CA证书模拟技术" | "ECH 隐身隧道协议" |
| "网络代理服务" | "隐身网络隧道" |
| "网络加速" | "抗审查网络隧道" |

**各页面独立 generateMetadata**：

```tsx
// /k2/[[...path]]/page.tsx
export async function generateMetadata({ params }) {
  // 从 Velite markdown frontmatter 读取 title, summary
  return {
    title: `${post.title} | k2 Protocol`,
    description: post.summary,
    openGraph: { ... },
  };
}

// 首页 page.tsx
export async function generateMetadata({ params }) {
  const { locale } = await params;
  return {
    title: locale === 'zh-CN'
      ? 'k2 — 隐身网络隧道 | 一行命令部署，不可检测'
      : 'k2 — Stealth Network Tunnel | One Command Deploy, Undetectable',
    description: '...',
  };
}
```

#### 7.3 JSON-LD 结构化数据

为 AI 搜索引擎和 Google Rich Results 添加 schema.org 标记：

**首页**：`SoftwareApplication` + `Organization`
```json
{
  "@type": "SoftwareApplication",
  "name": "k2",
  "applicationCategory": "NetworkApplication",
  "operatingSystem": "macOS, Windows, Linux, iOS, Android",
  "description": "隐身网络隧道，ECH 加密，一行命令部署"
}
```

**/k2/ 文档页**：`TechArticle`
```json
{
  "@type": "TechArticle",
  "headline": "k2v5 协议技术详解",
  "about": { "@type": "Thing", "name": "k2v5 Protocol" },
  "proficiencyLevel": "Beginner/Advanced"
}
```

**/k2/vs-hysteria2**：`ComparisonArticle`（非标准，用 `Article` + 自定义属性）

#### 7.4 Sitemap 更新

`web/src/app/sitemap.ts` 修改：
- 添加 `/k2/` 及所有子页面到 staticPages 列表
- 或从 Velite posts 自动发现 `k2/` 前缀的内容
- 设置 `changeFrequency: 'weekly'`, `priority: 0.9`（技术文档高权重）

#### 7.5 其他 SEO 优化

- **heading 层级**：首页 h1 包含 "k2" + "隐身网络隧道"，h2 为各 section 标题
- **canonical URL**：每个 /k2/ 页面设置 canonical，避免 locale 重复
- **Open Graph images**：为首页和 /k2/ 生成 OG 图片（可用 Next.js OG image generation）
- **robots.txt**：确认 `/k2/` 路径未被 disallow

### 8. i18n 策略

- Velite markdown 内容：`content/zh-CN/k2/*.md` 为主，`content/en-US/k2/*.md` 为英文
- 首页文案：更新 `messages/{locale}/hero.json` 所有 7 个 locale
- 侧边栏导航文案：新增 `messages/{locale}/k2.json` namespace
- 优先完成 zh-CN 和 en-US，其余 locale 后续补充

## Acceptance Criteria

**执行顺序**：AC1 (SSR) → 验证通过 → AC2 (主题) → AC3 (Velite) → AC4 (/k2/ 文档) → AC5 (首页) → AC6 (vs-hysteria2) → AC7 (SEO 补全)

### AC1: Server Component 改造（前置，独立验证）

首页从 CSR 改为 SSR/SSG，在其他任何改动之前独立完成并验证。

**改造内容**：
- 首页 `page.tsx` 移除 `"use client"`
- `useTranslations()` → `getTranslations()` (from `next-intl/server`)
- 添加 `setRequestLocale(locale)` 调用
- 添加 `export const dynamic = 'force-static'` 确保 SSG（不走 Lambda）
- Header / Footer 保持为 client component 子组件（composition pattern，与 `[...slug]` 页面一致）
- 添加首页独立 `generateMetadata`

**验证标准**（全部通过才能继续后续 AC）：
1. `yarn build` 成功，build 输出中首页为 `.html` 静态文件（非 Lambda route）
2. `curl http://localhost:3000/zh-CN/ | grep -c '隐身\|k2\|隧道'` 返回 > 0（初始 HTML 含文字内容）
3. 部署到 Amplify preview branch 验证页面正常渲染
4. `(manager)` admin 页面不受影响
5. embed mode（iframe 嵌入）功能正常

**背景**：
- `"use client"` 从项目第一次提交就存在（`1f9a8e9`），不是从 SSR 改过来的，属于原始开发习惯
- Amplify 部署为 `WEB_COMPUTE` 平台 + `Next.js - SSR` 框架，完全支持 Server Component
- `[...slug]/page.tsx` 已是 Server Component 且在生产正常运行，为本次改造提供了验证先例
- 加 `force-static` 后首页为纯静态 HTML，由 CDN 直接分发，零 Lambda 开销

### AC2: Terminal Dark 主题
- 移除 ThemeToggle，全站强制暗色
- `EmbedThemeProvider` 中 `defaultTheme` 改为 `"dark"`，移除 `enableSystem`
- 新色彩系统应用到所有公共页面
- 代码块呈现终端窗口风格
- 等宽字体用于代码和技术内容
- `(manager)` admin 页面不受影响

### AC3: Velite 扩展 + /k2/ 路由
- Velite schema 添加 `order` 和 `section` 可选字段
- 创建 `web/src/app/[locale]/k2/[[...path]]/page.tsx`（Server Component）
- 创建 `/k2/` layout 含左侧固定侧边栏导航
- 侧边栏根据 section 分组、order 排序
- `[...slug]` catch-all 不受影响（/k2/ 路由优先拦截）
- 添加 `export const dynamic = 'force-static'`

### AC4: /k2/ 文档内容
- 创建 `web/content/{locale}/k2/*.md` 内容文件
- `/k2/` 落地页展示协议概述和导航
- `/k2/quickstart` 完整 bootstrap 流程（server + client）
- `/k2/server` 详细服务端部署指南
- `/k2/client` 客户端使用和排错
- `/k2/protocol` 协议技术详解（基于 k2v5-protocol-spec.md，适配面向用户的语言）
- `/k2/stealth` 隐身伪装技术独立页面
- 每个页面有独立 title 和 meta description（从 Velite frontmatter 生成）

### AC5: 首页内容改版
- Hero 区域反映 k2 隐身隧道定位
- 6 个技术特性卡片基于 k2v5 真实技术
- 技术对比表格对比 5 个方案
- 快速开始区域展示终端风格的部署步骤
- 移除所有 MPTCP、CA 证书模拟等错误内容
- 删除 MPTCPVisualization 组件
- hero.json 更新所有 7 个 locale
- metadata.ts 内容纠正，不含 "CA证书模拟" 等错误描述

### AC6: /k2/vs-hysteria2 拥塞控制对比
- 页面标题和介绍不透露算法名称（不出现 PCC/Vivace）
- 对比 k2 自适应 vs Hysteria2 Brutal 固定速率
- 效果维度：丢包恢复、延迟稳定性、带宽利用率、公平性
- 预留数据填充区域和"开源日公开细节"标语
- 页面在侧边栏"对比"分组下

### AC7: SEO/GEO 补全
- Open Graph tags 正确配置（title, description, url, type）
- JSON-LD 结构化数据：首页 SoftwareApplication，/k2/ 页面 TechArticle
- sitemap.xml 包含所有 /k2/ 页面 URL
- h1 包含 "k2" 关键词，heading 层级 h1 → h2 → h3 正确
- canonical URL 设置正确，避免 locale 重复索引
- robots.txt 确认 `/k2/` 路径未被 disallow

### AC8: 其他页面 SSR 改造（TODO）
- 记录为后续 TODO：将剩余 18 个 `"use client"` 公共页面改为 Server Component
- 不在本次 spec 范围内实施

## Non-Goals

- 不改动 `(manager)` admin dashboard 的主题和布局
- 不做 `/k2/benchmark` 综合测评页（等数据就绪后作为独立 feature）
- 不透露 PCC Vivace 算法名称和实现细节
- 不做 vs WireGuard / vs VLESS+Reality / vs Shadowsocks 页面（已记录为 TODO）
- 不做复杂的交互动画（打字机效果等留给 UX 工程阶段）
- 不改动 webapp/（VPN 客户端前端）的主题
- 不改造剩余 18 个公共页面的 SSR（记录为 TODO，后续批量处理）

## Technical Decisions

1. **Velite Markdown 而非纯 React 页面**：SEO/GEO 友好，内容更新不需改代码，build time 静态生成
2. **独立路由 `/k2/[[...path]]` 而非修改 `[...slug]`**：隔离关注点，/k2/ 有专属侧边栏布局
3. **Terminal Dark 强制暗色**：移除 next-themes 的 system/light 切换，`defaultTheme="dark"` 且不可切换
4. **拥塞控制不透露细节**：称为"自研自适应拥塞控制算法"，效果图不含公式或算法名称
5. **Server Component + force-static**：首页 + /k2/ 全部为 Server Component，加 `export const dynamic = 'force-static'` 确保 SSG。build time 生成静态 HTML 由 CDN 分发，不走 Lambda。证据：`"use client"` 从首次提交即存在（非从 SSR 改过来），Amplify `WEB_COMPUTE` 平台完全支持，`[...slug]` 已验证 Server Component 模式可行
6. **SSR 改造必须独立验证**：AC1 (SSR) 必须在其他 AC 之前独立完成。验证标准：build 输出为 `.html`、curl 验证初始 HTML 含文字、Amplify preview branch 部署正常。通过后才能进入主题和内容改版
7. **JSON-LD 结构化数据**：帮助 AI 搜索引擎理解页面类型（软件产品 vs 技术文档 vs 对比文章）

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.2 | 2026-02-21 | Scrum 辩论裁决：AC 拆分重排序，SSR 改造（AC1）独立验证，加 force-static + 构建验证标准 |
| 1.1 | 2026-02-21 | 新增 SEO/GEO 优化章节：Server Component 改造、metadata 纠错、JSON-LD、sitemap 更新 |
| 1.0 | 2026-02-21 | Initial spec from brainstorm |
