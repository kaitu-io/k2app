# Android 品牌安装指南 — 设计文档

**日期**: 2026-03-20
**位置**: `web/src/app/[locale]/install/` — AndroidPanel 内

## 概述

在 /install 页面 Android 区域的下载按钮下方，新增品牌安装指南区域。通过 tab 切换展示不同品牌的图文安装步骤，帮助小白用户解决各品牌特有的安装拦截问题。

## 需求

1. 下载按钮下方常驻显示品牌 tab 栏，用户一眼可见所有品牌选项
2. 每个 tab 内显示该品牌的图文步骤（截图 + 文字说明）
3. 根据 User Agent 自动检测品牌并预选对应 tab
4. 图片由用户后续提供，先搭好框架和占位

## Tab 列表

| Tab ID | 显示名 | UA 匹配规则 | 内容类型 |
|--------|--------|-------------|----------|
| `xiaomi` | 小米 | `xiaomi\|redmi\|miui\|poco` | 图文步骤 |
| `huawei` | 华为 | `huawei\|honor\|hmscore` | 图文步骤 |
| `oppoVivo` | OPPO·vivo | `oppo\|realme\|oneplus\|vivo` | 图文步骤 |
| `desktopUsb` | 电脑辅助安装 | 桌面浏览器默认选中 | 图文步骤（复用现有 DesktopUsbInstallGuide） |
| `generic` | 通用安装 | Android UA 但未匹配品牌时默认 | 纯文字步骤 |

所有 ID 统一 camelCase 命名，与 i18n key 保持一致。

## 品牌自动检测与默认 Tab 选择

```typescript
function detectDefaultTab(ua: string): string {
  const lower = ua.toLowerCase();
  const isAndroid = /android/.test(lower);

  if (!isAndroid) {
    // 桌面浏览器或 iOS → 默认"电脑辅助安装"
    return "desktopUsb";
  }

  // Android 设备 → 匹配品牌
  if (/xiaomi|redmi|miui|poco/.test(lower)) return "xiaomi";
  if (/huawei|honor|hmscore/.test(lower)) return "huawei";
  if (/oppo|realme|oneplus/.test(lower)) return "oppoVivo";
  if (/vivo/.test(lower)) return "oppoVivo";

  // Android 但未匹配品牌 → 通用安装
  return "generic";
}
```

完整逻辑：
1. 非 Android UA（桌面 / iOS）→ `"desktopUsb"`
2. Android + 匹配品牌 → 对应品牌 tab
3. Android + 未匹配品牌 → `"generic"`

## 页面布局

```
AndroidPanel
├── Hero icon + 标题 + 版本号
├── 下载按钮（主 + 备用链接）
└── AndroidGuides（常驻可见）
    ├── 区域标题："安装疑难指南"
    ├── Tab 栏：[ 小米 | 华为 | OPPO·vivo | 电脑辅助安装 | 通用安装 ]
    └── Tab 内容区（当前选中品牌）
        ├── Step 1: 图片 + 标题 + 说明文字
        ├── Step 2: 图片 + 标题 + 说明文字
        └── ...
```

## 数据结构

```typescript
// 品牌指南的每一步
interface GuideStep {
  image?: string;        // "/images/install/xiaomi/step1.png" — 可选
  titleKey: string;      // i18n key，如 "androidGuides.xiaomi.step1Title"
  descriptionKey: string; // i18n key，如 "androidGuides.xiaomi.step1Desc"
}

// 一个品牌的完整指南
interface BrandGuide {
  id: string;            // "xiaomi" | "huawei" | "oppoVivo" | "desktopUsb" | "generic"
  labelKey: string;      // i18n key，如 "androidGuides.xiaomiLabel"
  steps: GuideStep[];
}
```

步骤数据定义在 `android-guides-data.ts` 中，i18n key 和图片路径在数据文件中硬编码，组件用 `t(step.titleKey)` 渲染。

## 文件结构

### 新增文件

```
web/src/app/[locale]/install/
  ├── android-guides.tsx        # AndroidGuides 组件：tab 栏 + 步骤渲染 + 品牌检测
  └── android-guides-data.ts    # 品牌数据：步骤数组、图片路径、i18n keys

web/public/images/install/
  ├── xiaomi/                   # step1.png, step2.png, ...
  ├── huawei/
  ├── oppo-vivo/
  └── desktop-usb/
```

### 修改文件

```
web/src/app/[locale]/install/platform-panels.tsx
  — AndroidPanel 中引入 AndroidGuides 组件，替换现有的单个 DesktopUsbInstallGuide 卡片

web/messages/zh-CN/install.json
  — 新增 androidGuides.* keys（标题、各品牌 tab 名、步骤文案）

web/messages/{其他 locale}/install.json
  — 同步新增 androidGuides keys（初始可复制 zh-CN 或留英文占位）
```

## i18n 结构

在 `install.json` 中新增扁平化 key 结构（不使用数组索引）：

```json
{
  "androidGuides": {
    "title": "安装疑难指南",
    "xiaomiLabel": "小米",
    "xiaomiStep1Title": "步骤1标题",
    "xiaomiStep1Desc": "步骤1说明",
    "xiaomiStep2Title": "步骤2标题",
    "xiaomiStep2Desc": "步骤2说明",
    "huaweiLabel": "华为",
    "huaweiStep1Title": "...",
    "huaweiStep1Desc": "...",
    "oppoVivoLabel": "OPPO·vivo",
    "oppoVivoStep1Title": "...",
    "oppoVivoStep1Desc": "...",
    "desktopUsbLabel": "电脑辅助安装",
    "genericLabel": "通用安装",
    "genericStep1Title": "下载 APK",
    "genericStep1Desc": "点击上方下载按钮获取安装包",
    "genericStep2Title": "允许安装未知来源",
    "genericStep2Desc": "系统提示时，点击「设置」→ 开启「允许此来源」",
    "genericStep3Title": "安装 APK",
    "genericStep3Desc": "打开下载的文件，点击「安装」",
    "genericStep4Title": "打开应用",
    "genericStep4Desc": "安装完成后点击「打开」，允许 VPN 权限"
  }
}
```

数据文件中的 i18n key 引用示例：
```typescript
{
  titleKey: "androidGuides.xiaomiStep1Title",
  descriptionKey: "androidGuides.xiaomiStep1Desc",
}
```

组件中使用：`t(step.titleKey)`

## "电脑辅助安装" Tab

复用现有的 `DesktopUsbInstallGuide` 组件（install-guides.tsx:152-257），不重新制作。AndroidGuides 组件在 `desktopUsb` tab 被选中时直接渲染 `<DesktopUsbInstallGuide />`，其他 tab 渲染步骤列表。

## 步骤渲染

### 桌面端（≥md）
每个步骤水平排列：左侧图片（固定宽度 ~300px）+ 右侧文字（标题 + 说明）

### 移动端（<md）
每个步骤垂直排列：上方图片（全宽）+ 下方文字

### 图片占位
图片文件不存在或 `image` 字段为空时，渲染灰色占位框（`bg-muted rounded-lg`）+ 居中步骤序号。确保布局不因缺图而错乱。Next.js `<Image>` 组件加 `onError` fallback 到占位框。

### 无图步骤（generic tab）
不渲染图片区域，纯文字列表：序号 + 标题 + 说明，紧凑排列。

## 样式

- Tab 栏使用自定义按钮样式，与页面顶部平台 tab 风格一致（`border-primary`/`bg-primary/10` active 态）
- 移动端 tab 栏：`flex overflow-x-auto` 水平滚动，`whitespace-nowrap` 防止换行
- 桌面端 tab 栏：`flex gap-2` 自然排列（5 个 tab 桌面端放得下）
- 步骤之间 `space-y-6` 间距
- 深色主题，与页面整体一致

## 交互

- Tab 切换即时显示对应品牌步骤，无动画延迟
- 用户手动切换 tab 后，不再自动跳回检测结果
- Tab 选择仅 session 内有效，不持久化到 localStorage
- 图片支持点击放大查看（可选，后续迭代）

## 不做的事

- 不做品牌 icon（tab 纯文字即可）
- 不做步骤的展开/折叠（步骤数量不多，全部展示）
- 不做步骤进度追踪
- 不改现有 FAQ 区域（Android FAQ 保持不变）
- 不做 tab 选择持久化
