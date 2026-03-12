# Plan: iOS App Store 提交（代码改动 + 截图 + 元数据 + 提审）

## 背景

Kaitu VPN iOS 版首次提交 App Store 审核。本 plan 覆盖从代码验证到提审的完整流程。

**分支**: `fix/windows-build-on-macos`（worktree: `.claude/worktrees/fix-mobile-share`）
**App 版本**: `0.4.0-beta.2` / MARKETING_VERSION `0.4.0-beta.2` / build `400`

---

## Phase 0: 前置准备

### 0.1 环境变量配置

asc-mcp（App Store Connect MCP）已添加到 `.mcp.json`，需要配置环境变量：

```bash
# 在 shell profile 中添加（~/.zshrc 或 ~/.bashrc）
export APP_STORE_CONNECT_KEY_ID="<你的 Key ID>"      # 例如 D2JN583UVK 或 964C28484J
export APP_STORE_CONNECT_ISSUER_ID="<你的 Issuer ID>" # 在 App Store Connect > Users and Access > Integrations > Keys 页面
export APP_STORE_CONNECT_P8_PATH="/Users/david/Downloads/AuthKey_<KEY_ID>.p8"
```

可用的 .p8 密钥文件：
- `/Users/david/Downloads/AuthKey_D2JN583UVK.p8`
- `/Users/david/Downloads/AuthKey_964C28484J.p8`

选择一个有 Admin 或 App Manager 权限的 key。

### 0.2 验证 asc-mcp 连通

在新 session 中确认 asc MCP 工具可用：
- 调用 `asc.list_apps` 获取 app 列表
- 确认 Kaitu app (bundle ID `io.kaitu.app`) 存在
- 记下 app ID 用于后续操作

### 0.3 模拟器确认

已验证可用：
- `iPhone 16 Pro Max` (01F2566B) → 截图尺寸 1320x2868（满足 6.9" 要求）
- `iPad Pro 13-inch (M4)` (5CA87202) → 截图尺寸 2064x2752（满足 13" 要求）

---

## Phase 1: 代码改动验证

> 以下改动已在本 session 完成，需在执行时验证。

### 1.1 变更文件清单

| 文件 | 改动 | 状态 |
|------|------|------|
| `webapp/src/pages/Account.tsx` | 注销账号按钮+Dialog、iOS 续费按钮隐藏、Slogan 7天延迟 | ✅ 已改 |
| `webapp/src/App.tsx` | iOS `/purchase` 路由 guard | ✅ 已改 |
| `webapp/src/components/SideNavigation.tsx` | Sidebar 添加 logo 图标 | ✅ 已改 |
| `webapp/src/i18n/locales/*/account.json` (×7) | 注销账号 5 个 i18n key | ✅ 已改 |
| `mobile/ios/App/App/PrivacyInfo.xcprivacy` | iOS Privacy Manifest | ✅ 新建 |
| `webapp/src/pages/__tests__/Account.test.tsx` | 15 个测试用例 | ✅ 新建 |
| `.mcp.json` | 添加 asc-mcp 配置 | ✅ 已改 |

### 1.2 验证命令

```bash
cd /Users/david/projects/kaitu-io/k2app/.claude/worktrees/fix-mobile-share

# TypeScript 编译检查
cd webapp && npx tsc --noEmit

# 全量测试
cd webapp && npx vitest run

# 预期结果：33 test files, 475 tests passed
```

### 1.3 提交代码

确认无误后提交（不含 `k2` submodule 和 `scripts/ci/` 无关改动）：

```bash
git add webapp/src/pages/Account.tsx \
        webapp/src/App.tsx \
        webapp/src/components/SideNavigation.tsx \
        webapp/src/i18n/locales/*/account.json \
        webapp/src/pages/__tests__/Account.test.tsx \
        mobile/ios/App/App/PrivacyInfo.xcprivacy \
        .mcp.json

git commit -m "feat(ios): App Store compliance - delete account, iOS purchase guard, slogan delay, privacy manifest"
```

---

## Phase 2: iOS 构建

### 2.1 构建 iOS App

```bash
cd /Users/david/projects/kaitu-io/k2app/.claude/worktrees/fix-mobile-share

# 完整构建流程：gomobile bind → cap sync → xcodebuild
make build-mobile-ios
```

如果 CI 构建，确认 `build-mobile.yml` workflow 已触发并成功。

### 2.2 上传 Build 到 App Store Connect

构建产物通过 Xcode Organizer 或 `xcrun altool` 上传：

```bash
# 或者直接用 xcodebuild export + altool
xcrun altool --upload-app -f <path-to-ipa> -t ios \
  --apiKey $APP_STORE_CONNECT_KEY_ID \
  --apiIssuer $APP_STORE_CONNECT_ISSUER_ID
```

也可以在 Xcode → Window → Organizer → Distribute App 手动上传。

---

## Phase 3: 截图采集

### 3.1 安装 App 到模拟器

```bash
# 启动 iPhone 模拟器
xcrun simctl boot "iPhone 16 Pro Max"
# 安装 App（使用 .app 路径，非 .ipa）
xcrun simctl install booted <path-to-.app>
xcrun simctl launch booted io.kaitu.app

# 同理 iPad
xcrun simctl boot "iPad Pro 13-inch (M4)"
xcrun simctl install booted <path-to-.app>
xcrun simctl launch booted io.kaitu.app
```

### 3.2 截图内容（每设备 5-8 张）

| # | 页面 | 操作 | 展示重点 |
|---|------|------|----------|
| 1 | Dashboard（未连接） | 打开 App | 主界面、节点列表 |
| 2 | Dashboard（已连接） | 点连接 | 连接成功状态、延迟显示 |
| 3 | Account 页 | 切到 Account tab | 会员状态、设置项、注销按钮 |
| 4 | 隧道选择 | 点击节点区域 | 多地区节点列表 |
| 5 | FAQ 页 | 进入 FAQ | 帮助文档 |

**注意**：iPad 横屏会走 sidebar 布局（≥768px），需要横屏截图展示 sidebar + logo。

### 3.3 截图命令

```bash
mkdir -p ~/screenshots/iphone ~/screenshots/ipad

# iPhone 截图（竖屏）
xcrun simctl io "iPhone 16 Pro Max" screenshot ~/screenshots/iphone/01-dashboard.png
xcrun simctl io "iPhone 16 Pro Max" screenshot ~/screenshots/iphone/02-connected.png
# ... 依次操作并截图

# iPad 截图（横屏 — 需先旋转模拟器到横屏）
xcrun simctl io "iPad Pro 13-inch (M4)" screenshot ~/screenshots/ipad/01-dashboard.png
```

### 3.4 尺寸校验

```bash
sips -g pixelWidth -g pixelHeight ~/screenshots/iphone/*.png
# 预期：1320 x 2868 (iPhone 16 Pro Max @3x)

sips -g pixelWidth -g pixelHeight ~/screenshots/ipad/*.png
# 预期：2752 x 2064 (iPad Pro 13" 横屏) 或 2064 x 2752 (竖屏)
```

Apple 要求：JPEG/PNG，RGB，无透明，≤10MB，1-10 张/locale。

---

## Phase 4: 通过 asc-mcp 上传截图 + 元数据

### 4.1 获取 App 和 Version 信息

通过 MCP 工具：
1. `list_apps` → 找到 `io.kaitu.app` 的 app ID
2. `list_app_store_versions` → 找到待审核 version 的 ID
3. `list_app_store_version_localizations` → 获取各语言 localization ID

### 4.2 上传截图

对每个 locale（zh-Hans、en-US 等）和每个 display type：
1. `list_app_screenshot_sets` → 获取/创建 screenshot set
2. `create_app_screenshot` 或对应上传工具 → 上传截图文件
3. 确认截图顺序正确

**Display Types**:
- `APP_IPHONE_67` (iPhone 6.9")
- `APP_IPAD_PRO_3GEN_129` (iPad Pro 12.9") 或对应 13" type

### 4.3 更新元数据

每个 locale 需要：
- **App 名称**: zh-Hans="开途 VPN", en-US="Kaitu VPN"
- **副标题**: zh-Hans="安全快速的网络加速", en-US="Fast & Secure VPN"
- **描述**: 产品功能介绍
- **关键词**: VPN 相关关键词
- **What's New**: 首版可写 "首次发布"
- **Support URL**: https://www.kaitu.io
- **Privacy Policy URL**: https://www.kaitu.io/privacy

---

## Phase 5: 提交审核

### 5.1 审前检查清单

- [ ] Build 已上传到 App Store Connect 且处理完毕
- [ ] 截图已上传（iPhone + iPad，至少 zh-Hans + en-US）
- [ ] 元数据已填写（名称、描述、关键词、隐私政策 URL）
- [ ] App Review Information 已填写（联系方式、demo 账号）
- [ ] Content Rights / Age Rating 已确认
- [ ] `PrivacyInfo.xcprivacy` 包含在 build 中（已在 pbxproj 引用）

### 5.2 Demo 账号

审核员需要一个可登录的测试账号：
- 邮箱: （需要提供）
- 密码: （需要提供）
- 账号需有有效会员状态以展示完整功能

### 5.3 审核备注

建议在 Review Notes 中说明：
> This is a VPN app using Network Extension (Packet Tunnel Provider). The VPN service requires an active subscription. A demo account is provided for testing.

### 5.4 提交

通过 asc-mcp 的 `submit_app_for_review` 或在 App Store Connect 网页手动提交。

---

## 风险与注意事项

| 风险 | 应对 |
|------|------|
| asc-mcp 截图上传不支持 | 回退到 Xcode Organizer 手动上传 |
| 模拟器截图尺寸不对 | 用 `sips` 校验，不对则用 Simulator.app 菜单截图 |
| 构建失败 | 检查 gomobile bind 是否成功、signing identity 是否有效 |
| 审核被拒 | 常见原因：缺少注销功能（已加）、购买入口（已屏蔽）、隐私清单（已加）|
| Slogan 7天内被审核员看到 | 正常，此时不显示 slogan，安全 |

---

## 改动文件列表（供 code review）

```
.mcp.json                                    # 添加 asc-mcp
mobile/ios/App/App/PrivacyInfo.xcprivacy      # 新建：iOS Privacy Manifest
webapp/src/App.tsx                            # iOS /purchase 路由 guard
webapp/src/components/SideNavigation.tsx       # Sidebar logo 图标
webapp/src/i18n/locales/en-AU/account.json    # i18n: delete account keys
webapp/src/i18n/locales/en-GB/account.json    # i18n: delete account keys
webapp/src/i18n/locales/en-US/account.json    # i18n: delete account keys
webapp/src/i18n/locales/ja/account.json       # i18n: delete account keys
webapp/src/i18n/locales/zh-CN/account.json    # i18n: delete account keys
webapp/src/i18n/locales/zh-HK/account.json    # i18n: delete account keys
webapp/src/i18n/locales/zh-TW/account.json    # i18n: delete account keys
webapp/src/pages/Account.tsx                  # 注销账号、iOS购买隐藏、Slogan延迟
webapp/src/pages/__tests__/Account.test.tsx   # 15 个测试用例
```
