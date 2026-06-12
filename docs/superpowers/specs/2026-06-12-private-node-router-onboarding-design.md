# 专属线路路由器 onboarding（纯软件 BYO 单国家）Design

> 决策日期 2026-06-12。本文是 **Plan 5b（任务 #13）** 的设计记录：把"配额型专属线路"做成路由器（k2r）可用形态，让用户用**自带 OpenWrt 路由器**完成开通，全程软件、零 OpenWrt 系统注入。承接 Option D 计量断流（`2026-06-12-private-node-usage-reporting-in-k2s-design.md`）。

## 1. 产品框架（本次对齐结论）

路由器版重新定位为**定制线路版** —— 买了**独立 VPS 专属线路**的用户可用路由器全屋翻墙。围绕"会不会用超流量"的担心，逐项拍板如下：

| 决策点 | 结论 |
|---|---|
| 云账单归属 | **我们代购代管、我们付云账单** → 我们承担超额风险 → 必须硬断流 |
| 产品承诺 | **配额型「专属 X TB/月」**（非不限量）。X < VPS 真实额度，留安全边际 |
| 烧钱风险 | **0** —— Option D 进程内断流（Center 判 95%/100% → k2s `SetAccepting(false)` 拒新连接）已建，把流量焊死在配额内 |
| 撞墙体验 | 路由器是**全屋共享**，配额烧得快且无感；断流时**无共享池兜底**（能力矩阵：路由器只连专属节点）→ 全家海外访问一起停 |
| 预警 | **80%/95% 预警邮件 = P0**（本设计含），让全家提前知道要撞墙 |
| "充值"语义 | **作废** —— 配额绑物理 VPS 额度，卖"+GB"要么我们吃超额要么是假的。要更多流量只有 **① 升级实例档位 ② 加购第二条线路**（Phase 2，复用 provisioning 流水线）。撑不到月底的诚实答案 = **等月初 epoch 重置（免费）** |

**配额只数代理流量**：Option D 在 k2s 数 rx/tx，国内直连不经过节点 → 爱奇艺等国内流量不烧配额（卖点，写进文案）。

## 2. 范围

**本设计（#13）**：纯软件、BYO（用户自刷 OpenWrt + k2r）、**单国家**专属线路的 onboarding + 面板入口发现 + 95% 预警。

**显式不在本设计**：
- **多 SSID 多国家**（Plan 7）—— 软件可做（UCI/nftables/k2r 编排），但撞 BYO 硬件长尾（VLAN/DSA、多隧道算力、多播跨 VLAN）。归 Plan 7，并在那里加：**硬件能力探测门 + 认证路由器清单**（可选未来卖预配置路由器高端 SKU）。
- **升级/加购实例**（Phase 2）。
- **真机 smoke**（smoke-gated；在跑通前 #13 按发布框架封顶 6–7/10）。

## 3. 架构

### 3.1 关键事实（recon 坐实，地基不动）

- **路由器与 app 共用认证机制**：登录拿 JWT → `Basic(UDID:JWT)` 调 `/api/subs`，请求头 `X-K2-Client: kaitu-router/...` 令 `Device.IsGateway=true`（`middleware.go:48,93`）。Center 在 gateway 分支用 `ResolveGatewayPrivateTunnels`（`entitlement_resolver.go:14`）**只返回该用户的专属节点**（`api_subs.go:154`）。**无独立"路由器凭证类型"，不必新建匿名凭证签发面**。
- **专属节点 ↔ 用户绑定已建**（Plan 2）：claim 流水线落 `private_owner_user_id`（`model_private_node.go:35`、`slave_api_node.go:220` CAS 认领）。
- **k2r 是纯凭证消费者**：`k2r setup <url>`（`cmd/k2r/main.go:138`）消费预给的 `k2subs://`/`k2v5://`，持久化 `/etc/k2r/state.json`（`gateway/state.go`）。**无登录能力**、**不发 X-K2-Client**、嵌入式 webui 只读（`webui/embed.go:14`、`gateway/gateway.go:154`）。k2r 已在 LAN DNS 路径（`DNSRedirect`/`DNSPort`）。
- **配额数据 Center 已持有**：Option D 上报 `cumulative_bytes`，Center `/slave/usage` 算 `quota_used/quota_total` 判 verdict。

### 3.2 数据流

```
① 买配额型专属线路 → 节点开通并绑定 private_owner_user_id            ✅ 已建
② 用户自刷 OpenWrt+k2r，路由器跑起来                                用户侧
③ webapp(已登录)「添加路由器」→ Center 铸造 IsGateway=true 设备
   + 组装 k2subs://{UDID}:{JWT}@center.kaitu.io/api/subs              🆕 A
④ webapp 显示该 URL(复制按钮)+ 图文；并尝试公网-IP 发现弹
   「检测到你局域网的路由器 http://192.168.x.1:1777 【打开】」        🆕 B
⑤ 用户打开 k2r 面板(发现见 §3.4)→ /setup 页粘贴 URL → k2r
   持久化 state.json → k2r up                                        🆕 C
⑥ k2r 调 /api/subs(带 X-K2-Client: kaitu-router)→ gateway 分支
   ResolveGatewayPrivateTunnels 返其专属节点 → 连上                  ✅ 已建
⑦ k2s 侧 Option D 逐字节计量 + 80/95% 预警 + 100% 硬断流            Option D 已建;预警 🆕 D
```

### 3.3 组件

**A. 🆕 Center：铸造 gateway 凭证端点** `POST /api/gateway/credential`
- 鉴权：**现有 web 会话**（`AuthRequired`）—— 不是新匿名面，信任边界 = 已登录用户。
- 行为：校验该用户有 **active 专属线路**（`PrivateNodeSubscription` status=active）。无 → `402/403`。有 → 创建 **router-class Device**（复用 `createDeviceWithAppInfo` + `generateTokens`，`api_auth.go:350`，以 router 语义令 `IsGateway=true`），服务端生成 UDID，组装并返回 `k2subs://{UDID}:{JWT}@{centerHost}/api/subs`。
- **用 `k2subs://` 不用 `k2v5://`**：走 Center 已备 gateway 路径、跟随节点地址变更（弹性 IP/重开）、吃 Plan 4 token 滚动续期。
- 幂等：**一条线路一个 gateway 设备**；重铸 = 轮换 JWT（撤旧设备/旧 token）。
- 不变式：**铸造 gateway 设备不挤掉用户的 app 设备**（router-class 与 app-class 配额隔离，`EnforceDeviceClass` 已分类）。spec 实现时显式测此条。

**B. 🆕 webapp：「添加路由器」UI + 公网-IP 发现**
- 在已建的专属节点管理区加动作：调 A，展示 `k2subs://` URL（复制按钮）+ 图文（"打开路由器面板 → /setup → 粘贴"）。
- **公网-IP 发现**：页面加载时调 Center beacon 查询（§3.4 ①）；命中则显示「检测到你局域网内的路由器：`http://{lanIP}:1777` 【打开】」，点击直达 LAN 面板。多候选（CGNAT）→ 列出让用户挑。
- 凭证经 LAN HTTP 粘贴可接受（家庭同信任域）；webapp 端正常 HTTPS。

**C. 🆕 k2r：本地 /setup + 面板发现 + 凭证录入**（k2 子模块）
- **/setup 页**（嵌入式 webui 新增，现为只读）：粘贴框 + 「连接」。
- **set-credential 端点**（`POST /api/core action:set-credential` 或专用 `/api/setup`）：校验 URL 可解析 → 写 state.json → 触发 `k2r up`。**凭证绝不落 k2r 日志**（secret 卫生）。
- **发 `X-K2-Client: kaitu-router/<ver> (...)` 头**（recon 发现现在没发）：加固，使运行时类别与铸造时一致，并为 `/api/subs` 可能的 `EnforceDeviceClass` 守卫备好。
- **mDNS responder**：进程内广播 `k2.local`（§3.4 ②），零 OpenWrt 配置注入。
- **beacon 上报**：未配置（及配置后周期）向 Center 发 beacon（§3.4 ①）。

**D. 🆕 Center：80%/95% 预警 worker + beacon 关联端点**
- **预警 worker**：Asynq cron 扫 active 专属线路，读 `quota_used/quota_total`，跨 80%/95% 阈值发模板邮件。加 **per-epoch 去重列** `warn80_sent_epoch` / `warn95_sent_epoch`（epoch 变即重置）。复用 EDM + `send_templated_email`，新增 1 个模板（专属线路流量预警，中文用"开途/专属线路"，禁裸"Kaitu"）。
- **beacon 关联端点**（§3.4 ①）：`POST /pair/beacon`，按请求**公网源 IP** 记 `{lanIP, port, ttl}`（短 TTL，限速）。webapp 读侧按 web 请求公网 IP 匹配。**发现-only，绝不碰凭证**。

**E. 真机 smoke**（smoke-gated，§2）。

### 3.4 面板入口发现（三层，全程不依赖 unicast DNS）

**问题**：用户找不到 k2r 面板入口；OpenWrt 版本太多注入不进 dashboard；且用户 DNS 不一定走路由器，甚至 DoH/DoT（加密 DNS 绕过路由器）。故**不能把发现挂在 unicast DNS 上**。

| 层 | 机制 | 抗性 | 局限 |
|---|---|---|---|
| **① 公网-IP 关联发现（主力）** | k2r 发 outbound beacon（LAN IP）→ Center 按公网源 IP 关联 → webapp 弹「打开你的路由器」链接 | **DoH 无关**（非 DNS）；**Android 照样工作**（本质是 webapp 显示链接） | CGNAT 多户共享公网 IP 可能误配 → **仅提示+用户确认，绝不据此塞凭证**；浏览设备开 VPN 换出口；纯 IPv6 各设备地址不同 → 降级多候选 |
| **② mDNS `k2.local:1777`** | k2r 进程内 mDNS responder，链路本地多播 | **零 OpenWrt 注入**；**DoH/DoT/手设 8.8.8.8 全拦不住**（`.local` 与 unicast 解析器两套）；永不出 LAN | **Android 浏览器 `.local` 残缺**（软肋，中国 Android 盘痛点）；AP 多播隔离/部分 Linux 无 avahi |
| **③ 认证机型固定 IP（地板）** | 清单写死 `http://192.168.8.1:1777` | **零名字解析、零 DNS 依赖**，永远能用 | 用户改过网关 IP（认证机型默认不改） |
| ④（可选）captive-portal 探测拦截 | k2r 网关拦 OS captive 明文探测 → 弹面板 | DNS 无关 | OpenWrt 跨版本实现重，列可选增强 |

**端口**：统一 `:1777`（k2r 自有），不跟 LuCI 的 uhttpd 抢 :80。
**发现 ≠ 鉴权（硬边界）**：①②③ 只负责"把面板入口递到用户面前"；真正鉴权恒走"用户已登录铸造凭证 → 自己面板粘贴"。即便发现配错（CGNAT），最坏只是给错一个打开链接，泄不了凭证。
**`router.kaitu.io` unicast** 仅在设备恰好走路由器 DNS 时锦上添花，**不作依赖**；公网可挂一个 kaitu.io 帮助页，把"既没走路由器 DNS 又没 mDNS"的罕见设备引导到 `k2.local:1777` 或固定 IP。

## 4. 安全考量

- **A 铸造端点**：authed web 会话内为自己账号创建设备 —— 等同 app 登录建设备，trust 边界是现有会话。轻量安全复核即可，非重型新匿名签发面。
- **beacon 端点**：未配置 k2r 无凭证 → 端点近乎无鉴权，只存 `{publicIP→lanIP,port}`。攻击面：伪造 beacon 让受害者 webapp 显示错误 LAN 链接 → 最坏点开打不开/打开错设备，**无凭证暴露**（发现≠鉴权）。缓解：短 TTL + 限速 + 多候选呈现。
- **凭证卫生**：`k2subs://` 含 `UDID:JWT` bearer。k2r 端不记录；LAN HTTP 粘贴限家庭同信任域。
- **设备类别**：router-class 与 app-class 隔离，铸造不踢 app 设备（测试守卫）。

## 5. 测试策略（test = gate）

| 层 | 测试 | gate |
|---|---|---|
| A 铸造端点 | 有 active 线路→返合法 k2subs URL（解析出 UDID/JWT/host）；无线路→402/403；重铸→旧设备失效新 JWT 生效；**铸造不踢 app 设备**（建 router 设备后 app 设备仍在） | 凭证铸造正确 + 设备隔离不变式 |
| C k2r set-credential | 合法 URL→写 state.json→up；非法 URL→拒不改状态；**凭证不入日志**（捕获日志断言）；X-K2-Client 头出现在 /api/subs 请求 | 录入安全 + 头加固 |
| C mDNS | responder 应答 `k2.local`→网关 LAN IP（mock/loopback 验证） | 发现② |
| D 预警 worker | 跨 80%/95% 各发一次；同 epoch 不重发（去重列）；epoch 变后重置可再发；未跨阈值不发 | 预警正确 + 去重 |
| D beacon 关联 | 同公网 IP beacon→webapp 查询命中；不同公网 IP 不串；TTL 过期不返；多 beacon→多候选 | 发现① + CGNAT 隔离 |
| 回归 | /api/subs gateway 分支仍返专属节点；app 登录/设备流不回归 | 不回归 |

## 6. 部署

- **k2 子模块**（C：/setup + set-credential + mDNS + beacon + X-K2-Client）在 k2 仓内 commit，**parent submodule 指针保持 unstaged**。k2r 二进制随版本重编 + 路由器升级。
- **Center**（A 端点 + D worker + beacon 端点 + 去重列迁移 + EDM 模板）随 center 部署。**去重列 `warn80_sent_epoch`/`warn95_sent_epoch` 需手动迁移**（AutoMigrate 加列可行，确认非破坏性）。
- **webapp**（B：添加路由器 UI + 公网-IP 发现）随 webapp bundle 发版。
- **DNS/ops**：`k2.local` 为 mDNS 无需公网记录；可选公网 `router.kaitu.io` → kaitu.io 帮助页。
- 共享池/普通 app 用户：A/B/C/D 全不触达，零变化。
