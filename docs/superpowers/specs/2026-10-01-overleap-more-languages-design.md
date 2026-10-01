# Overleap 多语言扩展：语言注册表、系统语言匹配、可搜索选择器

日期：2026-10-01 · 范围：`webapp/`（Overleap 构建）+ `sites/overleap/` · `web/` 不动

## 1. 目标

1. Overleap 在现有语言之外新增 16 种：`es` `pt-BR` `fr` `de` `it` `ru` `ko` `tr` `id` `vi` `ar` `fa` `my` `th` `km` `ms`。
2. 语言选择器可搜索。
3. 首次打开自动匹配系统语言；匹配不到落英文默认（webapp `en-US`，站点 `en-GB`，保持现状）。
4. 开途不变：仍是 7 种语言，默认 `zh-CN`。

非目标：`web/`（切换到独立站前线上 overleap.io 看不到新语言）；后端邮件模板（新语言仍发英文）；法务文书（保持英文单语）。

## 2. 品牌名

**只有一个品牌名 `Overleap`，任何语言都不翻译、不音译。** webapp 文案走 `{{brand}}` 插值，站点文案里的字面量 `Overleap` 逐值原样保留。`scripts/i18n-check-translation.mjs` 按值比对占位符、标签与品牌名出现次数，两端的测试都调用它。

## 3. 语言注册表

- **webapp**：`i18n/i18n.ts` 的 `languages` 是全量注册表，每项 `{ nativeName, englishName, dir }`。**品牌实际提供哪些语言由 `brandConfig.locales` 决定**（此前无人消费）：选择器、匹配、规范化都只在这个白名单内取值。Overleap 23 种，开途 7 种。
- **站点**：`lib/site.ts` 的 `LOCALES` 扩到 20 种（现有 4 种 + 16 种），同文件新增 `LOCALE_META`（本地名 / 英文名 / 方向）。

## 4. 系统语言匹配

两端各一个纯函数，规则相同（两个 app 不共享代码，各自实现各自测）：

```
matchLocale(preferred: string[], supported, fallback)
  对 preferred 按顺序逐个尝试：
    1. 精确匹配（大小写不敏感）
    2. 别名表（zh-Hant→zh-TW、zh-SG→zh-CN、en-NZ→en-AU …；只在目标属于 supported 时生效）
    3. 同主语言：取 supported 中第一个主语言相同的（pt-PT→pt-BR、es-MX→es、fr-CA→fr）
  全部不中 → fallback
```

- webapp：`preferred = navigator.languages`（此前只读 `navigator.language` 单值，首选不支持时不会看次选）。已保存的选择优先；保存值不在品牌白名单内则视为未选择。
- 站点：`preferred` 来自 `Accept-Language` 按 q 排序；`preferredLocale` cookie 优先。站点对未服务的英语变体保持落 `en-GB`（现有行为）。

## 5. 可搜索选择器

- webapp：Account 页的 `Select` 换成 `LanguageDialog`（MUI Dialog + 搜索框 + 列表）。
- 站点：`LanguageSwitcher` 下拉内加搜索输入框，列表限高滚动。
- 过滤对本地名、英文名、语言代码做不区分大小写的子串匹配；当前语言置顶；无结果显示提示文案。
- 列表项为「本地名 + 英文名」，不显示国旗（`ar` `es` 等没有合适的单一国旗）。开途的选择器外观随之变化。

## 6. 翻译

- 低成本模型翻译，无母语者审校。源：webapp `en-US`，站点 `en-GB`。
- 新语言与原有语言同等对待：**全部 namespace 都翻**，不设"部分翻译"层级（router / privateNode 等界面是数据驱动而非品牌门控，Overleap 用户也可能看到）。
- 守卫：`i18n/__tests__/locale-coverage.test.ts` 要求每种注册语言携带全部 namespace，且与 `en-US` 的 key、占位符、标签逐值一致；`static-keys` / `errorCatalog` 的语言清单改为读注册表。站点 `messages-parity` 由 `LOCALES` 派生，自动覆盖。

## 7. RTL（`ar` `fa`）

- webapp：`document.documentElement.dir` 与 MUI `theme.direction` 随语言切换；emotion cache 加 `stylis-plugin-rtl`（`@mui/stylis-plugin-rtl`）。
- 站点：`<html dir>` 由 `LOCALE_META` 决定；物理方向类（`ml-*` `text-left` 等）换成逻辑属性。
- 验收：阿拉伯语下逐页截图检查主要页面。

## 8. 周边

- 国家名：`utils/countries.ts` 对非 en/zh 语言用 `Intl.DisplayNames`，取不到回退英文。
- 桌面托盘菜单 `tray.rs` 三条文案补新语言（随下次桌面发版生效）。
- 后端无需改动：`users.language` 是自由 BCP 47 字段。

## 9. 发布

webapp 改动需推 `webapp/x.y.z-overleap` tag 才到客户端；站点目前只在预览域名。两步都需人工触发。
