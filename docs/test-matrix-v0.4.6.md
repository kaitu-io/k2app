# Test Matrix — v0.4.6 发布验证

对比基线 `v0.4.5` → `HEAD`（321 commits）。验证方式：各层自动化测试全量运行 + 变更→测试覆盖映射。
日期：2026-07-10。工具链：go 1.25 / node 22 / cargo 1.97（darwin/arm64）。

## 自动化测试结果（SCAN）

| 层 | 命令 | 结果 | 判定 |
|----|------|------|------|
| api | `go test ./...` | **993 PASS / 0 FAIL / 0 SKIP**（连真 DB） | ✅ 已验证 |
| webapp | `vitest run` | **1040 PASS / 0 FAIL**（84 文件） | ✅ 已验证 |
| mcp | `go test ./...` | 全绿 | ✅ 已验证 |
| desktop | `cargo test` | **91 PASS / 0 FAIL**（编译干净） | ✅ 已验证 |
| web | `vitest run` | 2797 PASS / 5 FAIL | ⚠️ 5 失败=预存，见下 |
| k2 (submodule `a70e32e`) | `go test ./...` | 核心逻辑全绿；3 包 FAIL=环境/算力限制 | ✅ 逻辑已验证 |
| sidecar | `go test ./...` | 修复时间炸弹后**全绿** | ✅ 已验证（已修） |

## 变更→测试覆盖映射（0.4.6 关键功能，均随上表绿灯验证）

- 专属线路/节点：`api/*private_node*` `dedicated_line_e2e` `worker_private_node*` `slave_api_*private*`（api 全绿）
- 节点操作队列：`api_admin_node_operation` `node_operation_e2e`（绿）
- Apple IAP/entitlement：`api_apple_webhook` `logic_apple_iap*` `entitlement_*`（api 绿）+ webapp `IosMembershipPanel`/`useSubscriptionAffordance`（绿）
- 用户封禁：`api_admin_user_block` `api_auth_block` `user_block_e2e`（绿）
- 抗封锁中继：webapp `resolve-and-fetch`/`entry-pool`/`node-descriptor`(9+8+6) + api `api_antiblock`（绿）
- 住宅IP/ipType/tunnels 版本化：`api_*iptype*` `api_tunnel_v20260717` `CloudTunnelList.residentialIp`（绿）
- 节点计量/掐断：sidecar `enforcer`/`traffic_meter`/`cutoff_state` + api `logic_node_usage`（绿）
- recommendScore 时间门控：`logic_tunnel_score_test.go`（随 api 绿）
- geo cn / 删号释放邮箱：`api_geo` `api_user_delete_account`（绿）
- web sentry-filters(55)/browser-detection(49)/OG metadata：全绿

## 需处理清单

| # | 项 | 类型 | 状态/处理 |
|---|----|------|----------|
| 1 | sidecar `traffic_meter_test.go` 4 失败 | 测试时间炸弹（`billingCycleEndAt=2026-07-01` 已过期→误触周期重置归零） | **已修**：引入 `futureCycleEnd=2100-01-01` 常量，复跑全绿。产品代码本无 bug |
| 2 | iOS Swift 单测（`K2Tests` target：`IapHelpersTests`/`NEHelpersTests`/`K2HelpersTests`） | target `TEST_HOST=App.app` 依赖 gomobile xcframework，走完整 App 构建代价高 | **已验证**：三个 helper 源（`IapHelpers`/`NEHelpers`/`K2Helpers`）均纯 Foundation，用 `swiftc` 直接把**真实源文件+真实测试**编成 XCTest runner 在 macOS 跑，**47/47 通过**（Iap 5 + NE 24 + K2 18），无需 host app / gomobile。IAP 边界整形（periodUnitString / productDict camelCase 契约）已覆盖 |
| 3 | k2 `rule` 集成测试 `overseas.krs`/`cn.krs` 404 | **陈旧测试**：k2-rules release 已改为发布 tarball（`krs.tar.gz`），不再单独发 `.krs` 文件；测试仍下单文件 | 引擎真实路径 `EnsureBundles`→`krs.tar.gz` **实测 github+jsdelivr 双源 HTTP 200 / 2.5MB** ✅；失败纯属 k2 submodule 陈旧测试，非产品/CDN/0.4.6 问题 |
| 4 | k2 `wire/k2cc` 收敛基准 far_loss30 | 重型基准本机跑不完 | k2 CI 长超时验证；非 0.4.6 改动区 |
| 5 | web `/k2/comparison` FAQPage JSON-LD 5 失败 | **陈旧测试**：`b6d758c3` 把 JSON-LD 从「单 script 装数组」改成「每实体独立 `<script>`」（更规范），测试未同步 | **已修**：新增 `extractAllJsonLd()` 收集全部 ld+json script，5 个测试改断言拆分契约（FAQPage 仍 4 Q&A + 品牌 `@id`）。纯测试改动，未动 `page.tsx`。复跑 web **2802/2802 全绿** ✅ |

## 移动端抗封锁中继 —— Phase 2b native 桥接已补齐（2026-07-10）

**更正**：早前"移动端中继显式 deferred / 根本没接线"表述不准确。中继逻辑本就是 Go 封装（`wire.RelayFetchJSON`），且 `k2/appext/relay.go` **已 gomobile 导出** `RelayFetch`（`go build ./appext/...` ✓，`go doc` 确认 `func RelayFetch(string) string`），编进移动端二进制里本就存在。缺口仅是 native 最后一公里 delegate 桥接，本次补齐：

| 层 | 改动 | 桌面对照 |
|---|---|---|
| webapp bridge | `capacitor-k2.ts` `relay-fetch` 由恒返 `code:-1` stub 改为 `K2Plugin.relayFetch({request})` → `JSON.parse(response)` 透传（信封与 daemon 完全一致） | `tauri-k2.ts` `daemon_exec` |
| plugin 契约 | `definitions.ts` + `web.ts` 新增 `relayFetch(options:{request}):Promise<{response}>` | — |
| iOS | `K2Plugin.swift` 新增 `@objc relayFetch` → `K2RelayBridge.handler`（App 进程静态 handler，`AppDelegate` `import K2Mobile` 注册 `AppextRelayFetch`，VPN 无关） | — |
| Android | `K2Plugin.kt` `@PluginMethod relayFetch` → `VpnServiceBridge.relayFetch` → `K2VpnService.Appext.relayFetch`（沿用 classifyApps 桥接，服务 `BIND_AUTO_CREATE` 于 load 绑定，VPN 无关） | — |

**已验证（desk）**：Go `appext` 编译 ✓；webapp `tsc` clean；webapp vitest **1042/1042**（新增 3 个 relay-fetch 桥接测试：成功信封透传 / 502 节点故障透传 / native 缺失降级 code:-1）。`isRelaySupported()` 门控因 native 不再恒返 -1 自动翻 true，webapp 侧零逻辑改动。

**仍需真机 smoke（本环境无法完成）**：① `make appext-android`（AAR 含 `Appext.relayFetch`）+ JDK21 gradle 构建；② `make appext-ios` + Xcode 26 构建（确认 App target `import K2Plugin` 解析 + `AppextRelayFetch` 符号）；③ 真机墙内/墙外各跑一次中继 round-trip。SourceKit 报错（No such module UIKit/Capacitor）为本机无 iOS SDK/pod 的编辑器噪音，非代码问题。

## 抗封锁冷启动登录 —— 真实 0.4.6 桌面装机验证 PASS（2026-07-10）

**结论先行**：桌面中继主路已真机验穿（下表）。移动端中继本次已补齐 native 桥接（见上节），逻辑与桌面同一 `wire.RelayFetchJSON`，剩真机 smoke。原桌面验证无代码改动：

| 环节 | 方法 | 结果 |
|---|---|---|
| 冷启动 seed 发现 | `gen-embedded-seed.js` 真拉 live CDN（GitHub dist→最高 cursor→AES-GCM 解密） | **cursor=54 / 2 entries / 5 节点** ✅（较 6 月的 cursor 已大幅推进，证明 publish-antiblock 持续轮换） |
| bootstrap 经中继 | 5 个 seed 节点各 relay-fetch `GET /api/app/config` → k2.52j.me → Center（daemon :1777） | **5/5 code:0 / HTTP 200 / 真 body（appLinks www.kaitu.io）** 807–963ms ✅ |
| 登录经中继 | seed 节点 relay `POST /api/auth/login`（一次性无效 body，login 只校验不发邮件=无副作用） | relay code:0 / HTTP 200 / Center 返结构化 **422**（鉴权处理器经中继可达并处理 POST body）✅ |
| 服务端佐证 | daemon `/private/var/log/kaitu/k2.log` | 新增 **6 条** `DIAG: relay-fetch ... status=200`，nodeIP 全吻合 ✅ |

代表性依据（设计定论，`resolve-and-fetch.ts` + `webapp/CLAUDE.md`）：**中继路径对被封/未封客户端完全相同**，故墙外执行即代表墙内行为。**唯一仍不可在此复现的**=真实 GFW 网络下 direct 兜底的 `markDirectBlocked` 触发（属兜底非主路，权重低，须墙内真机）。

## 纯手动（无自动化可覆盖，已最小化）

- Android 内存压力暂停假死修复（`K2VpnService.kt`）：已 **#3169 AU 真机验证** ✅
- desktop macOS 12 最低版本（`tauri.conf.json` plist）：配置项，低风险，建议一次 12.x 装机 smoke
- iOS 真实内购端到端：逻辑已单测覆盖（IapHelpers 47/47），端到端属发布前真机 smoke
- 抗封锁真实 GFW 墙内冷启动：中继主路已上方真机验穿；仅剩 direct 兜底触发需墙内环境
