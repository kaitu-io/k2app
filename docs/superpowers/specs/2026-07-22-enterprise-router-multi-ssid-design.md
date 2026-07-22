# 企业版路由器 — 一 SSID 一节点(多槽固定绑定)设计规格

日期:2026-07-22
状态:brainstorming 完成,待用户 review → writing-plans
范围:api/(三张新表 + subs 扩展 + admin)、k2(submodule:gateway 多槽模式,独立仓任务)、webapp/(Router tab 企业形态)、web/(admin 管理页)、scripts/(固件镜像)
前身:`2026-06-09-private-node-router-product-design.md` §11(Plan 7/Phase 3 草图,本文取代其设计)、`2026-07-17-k2r-headless-app-control-design.md`(已实现合并 `4819f9b0`,本文直接依赖其锚点发现 + controlKey 鉴权 + headless 形态)

## 0. 决策摘要

| 决策点 | 结论 |
|---|---|
| 产品定位 | B2B 报价制,卖给 TikTok 等出海运营工作室;本质诉求 = 账号风控隔离(每账号固定、独立、不漂移的 IP 出口) |
| 商业形态 | 商务谈单 + 运营手工交付,无自助购买;IP/VPS 供给侧不进本设计(国家做成商品由商务侧定义) |
| 品牌 | 归 Overleap(海外面);SSID 前缀 `overleap-`;产品命名留 marketing |
| 核心模型 | 一企业客户 = 一 Center 账号,持有 N 条跨国线路(一线路 = 一专属节点 = 一 IP);一台路由器 8 个固定槽位,每槽一 SSID 钉死一条线路 |
| 确定性绑定 | SSID→节点固定映射,绝不走 subs 加权 Pick;绑定关系只由运营改,subs/k2r 无任何自主决策权 |
| fail-closed | 节点挂 → 该槽断网亮红告警,绝不 failover 换 IP(IP 漂移=封号事故);内核防火墙层再垫一道(k2r 进程死=断网不漏源 IP) |
| 槽位判定 | 应用层按源子网(`10.81.N.0/24` → slot N),内核平面维持现状单 TPROXY/单 fwmark/单表;**不用 VLAN** |
| 下发信道 | 复用 `/api/subs` gateway 分支,响应新增 `slot_bindings` 字段;零新信道 |
| 一个二进制 | 企业模式是同一 k2r 的增量分支(响应含 `slot_bindings` 即进多槽模式),禁止 fork k2r-enterprise |
| 管理 UI | 复用 app Router tab,**不独立 tab**;企业形态由鉴权后 status 的 `slots[]` 自描述 |
| 发现/鉴权 | 原样复用锚点拦截(`10.17.79.1:1779`)+ 账号级 controlKey,零改动 |
| 硬件 | 认证 1~2 款参考机型(倾向 MT7981 / OpenWrt 23.05+),预刷+预配置随方案交付 |
| 配额 | 完全复用现有 95% 预警 + 100% sidecar 硬断,零新增 |
| 有线口 | 不参与线路槽位,仅作管理口(可达锚点,不可上外网);业务设备必须走 WiFi |

## 1. 背景与产品定义

多 SSID 多国家路由器即内部 Plan 7 / Phase 3,2026-06-22 决策推迟,仅有 §11 设计草图、零实现代码。当年三个硬前置现已消解:

1. **多节点订阅(task #18)** → 报价制绕开自助订阅,只需数据模型支持一账号挂 N 节点(gateway 分支已按账号返回全部专属节点);
2. **k2r 接管 WiFi/UCI(全新表面)** → 固定槽位拓扑把运行期 UCI 写压缩到 wireless 域三类操作(改名/改密/开关);VLAN/DSA 碎片化整个绕开(§5.2);
3. **硬件兼容矩阵** → 认证机型白名单 + 预刷交付,适配面收敛到 1~2 款。

另,`2026-07-17` k2r headless + app 直控已合并(`4819f9b0`,k2 → `8f6f1a53`),提供了本设计直接继承的四块地基:headless JSON API、锚点发现、controlKey 鉴权(subs 信道下发 hash)、app Router tab 骨架。

**典型客户画像**:出海运营工作室,持有阿联酋 3 个 IP + 几内亚 2 个 IP 之类的跨国组合;每台业务手机固定连一个 SSID,即固定一个国家出口 IP,永不漂移。运营面独立(品牌/报价/合同/客服),技术底座共享(企业账号 = Center 一种账号形态,不碰消费版 Tier 体系,沿用 Kind=private_node 旁路)。

## 2. 总体架构

```
┌─ 运营 admin (web/ + MCP) ──────────────────────────┐
│ EnterpriseCustomer / EnterpriseLine /              │
│ EnterpriseRouterBinding(唯一写入方)               │
└──────────────┬─────────────────────────────────────┘
               ▼ (单向流:运营写 → subs 读 → k2r 收敛)
Center /api/subs gateway 分支
  tunnels[](账号全部专属节点,凭证已含)
  + control_key_hash(已有)
  + slot_bindings[](新增)
               │ k2subs 订阅信道(已有后台刷新)
               ▼
k2r 多槽模式(同一二进制)
  8 固定槽位:SSID overleap-ae-1 ─ br-line1 (10.81.1.0/24) ─┐
             SSID overleap-gn-1 ─ br-line5 (10.81.5.0/24) ─┤→ 单 TPROXY
  源子网 → slot → outbound(engine Target index)            ┘
               ▲ 锚点 10.17.79.1:1779(已有,零改动)
┌─ App Router tab(企业形态 = slots[] 自描述)─────────┐
│ 8 槽状态列表 + 改名/改密;绑定只读                   │
└────────────────────────────────────────────────────┘
```

设计原则:**清单是"运营写、subs 读、k2r 收敛"的单向流**。路由器与 subs 对绑定内容零自主决策——账号风控隔离场景里,"聪明"(failover/优选/降级)就是事故源。

## 3. 数据模型(api/,三张新表)

复用现状:SlaveNode(Class=private + PrivateOwnerUserID 旁路)、CloudInstance、计量、凭证 mint、配额治理**零改动**。节点开设走现有节点运维流程,本设计只做"挂载"。

```
EnterpriseCustomer            企业客户(一客户一持有账号)
  id, company, contact, status(active/suspended), user_id → User

EnterpriseLine                线路(一线路=一节点=一 IP)
  id, customer_id → EnterpriseCustomer
  node_id → SlaveNode(Kind=private_node,运营手工挂载)
  country_code(ISO 3166-1 alpha-2 小写,如 "ae")
  line_no(同国序号,1 起)
  status
  —— 规范显示名由 country_code+line_no 组合(如 AE-1),不落库

EnterpriseRouterBinding       路由器绑定矩阵
  id, gateway_device_id(k2r udid → Device)
  slot(1..8,同设备唯一)
  line_id → EnterpriseLine
```

- SSID 显示名与 WiFi 密码**不进 Center**——属路由器本地 UCI 状态(§5.4)。
- 一个客户可持有多台路由器(多行 Binding);一条线路可否绑到多台路由器?**本期禁止**(唯一约束:line_id 在 Binding 表唯一)——同 IP 多路由器并发是风控反模式,也简化计量归属。

## 4. 绑定清单下发协议(subs 扩展)

**载体**:`/api/subs` gateway 分支(`api/api_subs.go`,已按账号返回全部专属节点 tunnels + control_key_hash)。企业账号命中同一分支时响应新增:

```json
{
  "tunnels": [ "...(现有形态,凭证已含)..." ],
  "control_key_hash": "…",
  "slot_bindings": [
    { "slot": 1, "country": "ae", "index": 1, "tunnel_index": 0 },
    { "slot": 2, "country": "ae", "index": 2, "tunnel_index": 1 },
    { "slot": 5, "country": "gn", "index": 1, "tunnel_index": 3 }
  ]
}
```

- Center 按请求 udid 查 Binding → join Line → node → 定位 tunnels 下标生成清单。Go snake_case 直出(k2r 内部消费,不过桥,同 `control_key_hash` 先例)。
- 非企业 gateway 账号无 `slot_bindings` 字段;老 k2r 忽略未知字段。**一个二进制的分支判据即此字段**:有 → 多槽模式;无 → 消费版单隧道行为一字不动。
- **subs 永不因健康度/负载/配额改动清单内容**——清单只反映 Binding 表,变更唯一来源是运营改表。

**k2r 收敛行为**(每次订阅刷新):diff 清单 vs 运行态——新增槽位 → 起 outbound + 亮 SSID;消失槽位 → 拆 outbound + 灭 SSID;同槽换 line → 拆旧建新。8 槽网络对象出厂静态,收敛只动 outbound 层与 wireless 域。

**fail-closed 协议语义**:

| 场景 | 行为 |
|---|---|
| 槽位节点不可达 | 保持绑定,该槽断网亮红告警;**绝不 failover**,换 IP 只能来自运营改绑定表 |
| subs 刷新失败(Center 不可达) | 沿用最后已知清单(本地落盘缓存)继续跑,不清空不降级 |
| 配额 100% 硬断 | 复用现有 sidecar 掐断 → 该槽表现为节点不可达 → 同上;Center **不**把节点摘出清单(摘除=换绑语义,必须人工) |
| k2r 重启 | 从落盘缓存起全部槽位,后台等首次刷新校对 |
| 槽位 outbound 不可用时的 LAN 流量 | 拒连(RST/黑洞),**绝不 fallback direct**;DNS 同样拒答 |

## 5. k2r 多槽模式(k2 仓库,gateway/)

### 5.1 内核平面:维持现状,零新对象

§11 草图中的 per-slot fwmark / per-slot 路由表 / per-slot TPROXY 端口 **全部不需要**——那是内核级分流的做法;k2r 是用户态透明代理,所有槽的流量进同一 TPROXY 端口、同一 fwmark→table。现有 `inet k2r` 表结构、锚点 DNAT、DNSRedirect 骨架(`intercept_nft.go`)原样复用。

### 5.2 槽位判定:源子网,应用层一处映射

`conn.RemoteAddr()` ∈ `10.81.N.0/24` → slot N → outbound N。源地址第三个八位组即槽号,零新监听器。**唯一 Go 侧结构性新增** = gateway 在把连接交给 engine 前按源子网选 outbound(engine 多 outbound Target index 路由已就绪,`ConnectionHandler` 携带源地址)。

**不用 VLAN**:VLAN tag 只在有线口分发/dumb AP 扩展才需要;本期 WiFi-only,DSA vs legacy VLAN、MT76 驱动 tag 等碎片化问题整个绕开。将来有线走线路的需求出现时,再做"物理口→槽位"静态映射增量(仅认证机型)。

### 5.3 出厂静态拓扑(烧进预刷固件,运行期不建不删)

1. 8 SSID 挂 `br-line1..8`,子网 `10.81.N.1/24`,dnsmasq 按桥发 DHCP;SSID 出厂全 disabled,名字占位;
2. **防火墙层 fail-closed**:默认无任何 `br-lineN → wan` forward/NAT——流量唯一出路是 TPROXY;k2r 进程崩溃 = 全槽断网,绝不漏家宽源 IP;
3. 槽间隔离:防火墙禁止 `br-lineN` 互访(不同账号设备互不可见);
4. 有线 LAN 口落独立管理子网(默认 br-lan):可达锚点(app 管理/上门排障),同样无 wan forward,不进隧道——业务设备必须走 WiFi,SSID 即线路。

### 5.4 SSID 命名与运行期 UCI 写(仅 wireless 域三类)

- 收敛时对绑定槽位拼名 `overleap-{country}-{index}`(如 `overleap-ae-1`)→ `uci set wireless…ssid` → enable → `wifi reload`;清单外槽位 disabled。
- 客户自助改名/改密后,k2r 本地记"名字已被客户接管"标志,收敛不冲客户改名;密码只在本地,永不进 Center。
- UCI 写全集 = 改名 / 改密 / 按清单开关。无其他任何 UCI 面。

### 5.5 DNS 按槽走隧道

现有 DNSRedirect 骨架保留(LAN:53 → k2r DNS listener);listener 按查询源子网把解析送进对应槽 outbound——每线 **DNS 出口 IP = 流量出口 IP**,不留"DNS 在 A 国、流量在 B 国"指纹。槽位 outbound 不可用时 DNS 拒答(fail-closed 一致)。

### 5.6 status 自描述

`/api/core` status(鉴权后)新增 `slots[]`:每槽 `{slot, ssid, country, index, state(running/failClosed/disabled), traffic…}`。**这是 app 判定企业形态的唯一依据**——`/ping` 不动(无鉴权端点不加企业指纹,LAN 旁观者看不出企业设备)。

## 6. App UI(webapp,Router tab 企业形态)

**不独立 tab**。发现、鉴权、轮询、离线态、设置段(OTA/日志/解绑)全部复用 7-18 落地的骨架;企业形态只是 Router tab 内**连接卡区域的替换**:

- **槽位列表**:每槽一行——SSID 名、国家、线路名(AE-1)、状态灯(🟢 通 / 🔴 fail-closed 附断线时长 / ⚪ 未绑定)、流量。任一槽 fail-closed → Router tab nav 图标红点徽标。
- **每槽操作仅两个**:改 SSID 名、改 WiFi 密码(controlKey 鉴权 API → k2r 写 UCI)。**刻意无"换节点/换国家"入口**——绑定权在运营,杜绝客户误操作致 IP 漂移。
- **设备列表**:保留现有 RouterDevices,按槽分组展示(源子网即槽);MAC allowlist 沿用全局机制,per-slot allowlist 列 P2。
- **解除绑定**:保留,企业形态下 MUI Dialog 强确认(全线断网、需联系客服恢复)。
- **权限边界**(现有机制天然成立):controlKey 账号级——登录企业账号的任何 app 可管;员工个人账号 app 发现路由器但 401,且已绑定 k2r 的 `set-credential` 需鉴权无法抢占。UX 增量:企业设备 401 时不显示"重新配对"CTA,改示"此路由器由企业账号管理"。
- **默认落点**:发现的路由器呈企业形态(鉴权后 status 含非空 `slots[]`)→ app 默认落 Router tab(app 无"企业账号"概念,判定完全来自路由器自描述;Dashboard 对企业账号无套餐无意义);Dashboard 本身不动。
- **多路由器客户**:锚点只能发现手机当前网络路径上的那台;Router tab 主语永远是"你身后这台",跨路由器总览不做(admin 才是全局视角)。

## 7. 运营面(web/ admin + MCP)

admin 新增企业管理区,直接映射三张表:客户列表 / 线路管理(挂 SlaveNode、国家、序号)/ 绑定矩阵(udid × 8 槽 → 线路;改此处 = 下次订阅刷新生效)。按 ops 惯例补 `kaitu-center` MCP 工具(建客户/挂线/改绑定/查槽位状态),与 admin UI 共用同一套 api/ admin 端点。

**交付流程**:商务签单 → 运营开节点(现有流程)→ admin 建客户+线路 → 预配置路由器(刷镜像 + `k2r setup` 写入企业账号 k2subs 凭证,获得 udid)→ admin 录绑定矩阵 → 寄付,客户插电即全线亮灯。

**监控 MVP 不新增上报面**:线路=节点一对一,槽位挂≈节点挂,现有节点健康告警覆盖主体;客户侧 app 红灯自见。k2r per-slot 状态经 beacon 上报 Center(通道保留未拆)列 P2。

## 8. 预刷固件流水线(scripts/)

- **认证机型**:1~2 款定版,倾向 MT7981 双频 / OpenWrt 23.05+;MIPS 老款仅商务硬需求时加。最终选型是商务/供应链输入(开放项)。
- **镜像** = OpenWrt ImageBuilder:官方 release + 出厂静态配置(§5.3)+ k2r 二进制 + init 脚本。k2r 本体不进镜像定制逻辑——走现有 OTA 升级,镜像只保证首启有能自升级的版本。ImageBuilder 配置版本化进 `scripts/`,CI 只管可复现构建,不管刷机。
- **产线**:刷镜像 → `k2r setup`(企业账号凭证)→ 记录 udid 交运营录绑定 → 抽测(连 SSID 看灯)→ 寄付。

## 9. 测试

- **k2 gateway 单测**:清单解析/收敛 diff(增/删/换绑)、源子网→槽→outbound 映射、fail-closed(outbound 不可用拒连不 direct、DNS 拒答)、清单缓存落盘/重启恢复、UCI 命令生成(文本级断言)。`make gateway-check`。
- **gateway-uat(Docker)**:多槽 E2E——两个源子网各绑 mock 隧道;断其一验证 fail-closed 且另一槽不受影响;清单变更收敛;锚点从槽内可达。无 VLAN,容器可测,不需真无线。
- **api/**:三张表 CRUD 与约束(slot 唯一、line 唯一绑定)、subs `slot_bindings` 生成(join 正确、非企业账号无字段)、绑定变更下次 serve 反映。注意 worktree 内 `center/config.yml` 存在性(0 SKIP 判据)。
- **webapp vitest**:`slots[]` → 企业形态渲染、红点徽标、401 企业文案、改名/改密流、默认落 Router tab。
- **真机 smoke(release 门槛)**:参考机型实刷——8 SSID 广播、每槽出口 IP 独立(各槽 curl 外部 IP 服务比对)、DNS 出口一致性、拔节点 fail-closed、进程 kill 断网不漏源 IP、OTA。**代码信心与业务信心分开打分,无真机 smoke 不出货。**

## 10. 明确不做(YAGNI)

- IP/VPS 供给侧、国家商品定义、报价(商务侧)。
- 自助购买/自助换绑/自助加线——一切绑定变更走运营。
- failover / 备用节点 / 智能选路——与产品本质对立。
- VLAN、有线口入槽、dumb AP 扩展(留将来增量)。
- per-slot MAC allowlist、k2r per-slot beacon 上报、fail-closed 推送通知(均 P2)。
- 跨路由器总览客户端、消费版 Tier/套餐体系接入。
- fork k2r-enterprise 二进制、独立 Router tab、独立 app。

## 11. 开放项(进 writing-plans 前拍板或实现期决定)

1. **参考机型最终选型**:商务/供应链输入;设计按 MT7981/23.05+ 假设,不阻塞后端与协议实现。
2. **`10.81.0.0/16` 撞段**:客户上游 LAN 恰用该段的概率低但非零;预配置时可整段偏移(如 `10.83.`),镜像做成参数——实现期定。
3. **slots[] 与 slot_bindings 的字段终形**:实现期与 k2r status 现有 JSON 形态对齐(注意 Go snake_case → 桥 camelCase 惯例只适用于 app 消费面)。
4. **企业账号的建号流程**:复用现有 User 注册 + admin 关联,还是 admin 直建?实现期按 admin 现有用户管理惯例定。
5. **k2 仓库任务拆分与顺序**:k2(gateway 多槽 + 收敛 + UCI)先行出测试版二进制 → k2app(api/ 三表 + subs + admin + webapp)跟进;k2 submodule 父仓只读,各走独立 worktree/分支。
