# 节点 ip_type 一等属性化 + tunnels 接口日期版本化

**日期**: 2026-06-17
**状态**: Design — 待实现
**作者**: David + Claude

## 背景与问题

两个看似独立的需求,追查后绑在同一个真实数据缺口上:

1. **tunnels 列表接口不要暴露 `k2v5` 这类内容**。
2. **接口加一个反馈字段:该节点是否为「住宅IP」(residential)**。

### 现状追查结论

**`IPType`(`residential` / `non_residential`)在系统里早就有,但断在了节点层:**

| 环节 | 有 IPType? | 位置 |
|------|-----------|------|
| 购买 → `PrivateNodeSubscription.IPType` | ✅ | `api/provision_private_node.go:58` |
| 派发部署 → `ProvisionParams.IPType`(`NodeOperation.Params` JSON) | ✅ | `api/provision_private_node.go:121` |
| 装机 agent 读到、据此买对应 IP | ✅ | NodeOperation 队列 |
| **sidecar 注册回报 `SlaveNodeUpsertRequest`** | ❌ **断点** | `api/slave_api_node.go:21-30` 无此字段 |
| `SlaveNode` / `SlaveTunnel` 表 | ❌ 无列 | `api/model.go:448 / 473` |
| claim 时回写节点(已写 class/owner/subID) | ❌ 漏写 | `api/slave_api_node.go:226-264` |
| `/api/tunnels`、`/api/subs`、`/app/tunnels`、MCP `list_tunnels` | ❌ 读不到 | — |
| `/api/user/private-nodes`(唯一例外) | ✅ | 直接查 subscription |

根因:当初"代码只表达业务意图、部署细节交给 agent"(commit `569673e0`)时,**业务意图→节点的反向回报这一跳被漏掉了**。结果除了用户自己的订阅详情页,任何节点/隧道维度都看不到住宅 IP。

**关键观察:`Country` / `Region` / `IPv6` 本来就是 sidecar 上报、写进 `SlaveNode` 的**(`api/slave_api_node.go:144-145`)。住宅IP 是同一类节点事实,因此有现成、统一的落地路径——**共享池和私有节点走同一套,不分叉**。

**第二个观察:共享云节点同样需要住宅IP 标记**,而现状对共享池完全没设计(`IPType` 只活在私有节点订阅流)。sidecar 上报路径天然覆盖共享池。

## 设计原则

- **ip_type 是节点的一等属性**,归属与 Country/Region 完全同构:sidecar config/env → `SlaveNodeUpsertRequest` → `SlaveNode` 列。
- **业务意图闭环**:装机 agent 把意图(residential)烤进 k2s docker compose 的 `K2_IP_TYPE` env,sidecar 注册时回报,闭上断掉的环。
- **运维可改**:MCP / 后台可覆盖写入,与 sidecar **平权**(last-writer-wins),不互相特殊保护。
- **保兼容**:`k2v5://` wire scheme **保留不动**(客户端 k2 core 要解析它,换 scheme 会 brick 老客户端)。"不要用 k2v5" 仅作用在**显示/元数据层**(`protocol` 标签 `k2v5 → k2s`)。
- **版本化只给滞留旧装机的消费者**(客户端);自己完全控制的消费者(admin/MCP)原地改。

## 消费者分层(决定哪些要版本化)

| 消费者 | 是否控制 | 处理方式 |
|--------|---------|---------|
| admin `/app/tunnels`、MCP `list_nodes`/`list_tunnels` | 完全控制(自己发版) | **原地改**:加 `ipType`、protocol 标签 → `k2s` |
| 客户端 `/api/tunnels` | 旧装机滞留数月 | **冻结 v1** + 新建 `/api/v20260717/tunnels` |
| daemon `/api/subs` | 旧装机滞留 | **增量加** `ipType` 字段(老 daemon 忽略未知字段,不升版本) |
| webapp 隧道列表 | 新 bundle 控制 / 旧装机滞留 | 切到 `/api/v20260717/tunnels`,加「住宅IP」chip |

## 版本化方案:日期标记路径版本

采用 **日期标记的路径版本**:`/api/v20260717/tunnels`。

**核心:不搞版本解析层 / 协商层。** 每个 `v20260717` 就是一组**自包含的 route → handler**:
- 新端点 = 新 route + 新 handler 方法,独立存在。
- 旧端点 `/api/tunnels` = 原 route + 原 handler,冻结不动。
- 弃用 = **直接删 route 和对应方法**,无映射表、无 `X-K2-Client` 分支协商。

格式定为 `v20260717`(无分隔符,按 owner 偏好)。

**为何不用整数 `v2`**:owner 明确倾向日期标记,且日期天然不冲突、可读出铸造时间;由于不维护解析层,日期版本的"重机器"顾虑不成立。

**v1 弃用判据**:`/api/tunnels` 加 deprecation 计数(日志/metric);靠 `X-K2-Client` 遥测显示旧客户端流量 ≈ 0 后删除整条 route。

## 强约束(实现期必须遵守,非建议)

这些是产品契约,不是风格偏好——实现偏离即视为 bug:

- **C1 — ip_type 枚举封闭 + 全入口归一化**:`ip_type` 取值**只能** ∈ {`residential`, `non_residential`, `unknown`}。新增单一函数 `NormalizeIPType(s string) string`(未知/空/非法 → `unknown`)。**所有写入口都过它**:sidecar upsert(`api/slave_api_node.go`)+ admin update(`api/api_admin_node.go`)。配合 last-writer-wins:任何一方写脏值也只会落 `unknown`,DB 永不存非法值。
- **C2 — `k2s` 是纯显示标签,DB/wire 永远 `k2v5`**:**不新增** `TunnelProtocol` 常量、**不改** DB 值、**不改** `serverUrl` 的 `k2v5://`。新增单一函数 `ProtocolDisplay(p TunnelProtocol) string`(`k2v5` → `"k2s"`,其余原样),**唯一**映射点,供 `/api/v20260717/tunnels` handler、admin 输出、MCP `list_*` 三处共用。禁止在各处散写字符串替换。
- **C3 — 新端点无 `:protocol` 参数**:`/api/v20260717/tunnels` 返回全部可服务隧道(现实里 wire 只有 `k2v5` 一种),`serverUrl` 必填、`protocol` 字段经 `ProtocolDisplay` 吐 `"k2s"`。**不复刻**旧 `/api/tunnels/:protocol` 的 legacy 协议路由分叉,也不带 `echConfigList`(k2v4 遗留字段)。
- **C4 — admin/MCP 过滤器按 wire 值,显示按 display**:`/app/tunnels` 及 MCP `list_tunnels` 的 protocol **过滤入参仍是 `k2v5`**(DB 查询用),但**接受 `k2s` 作为别名**(入口归一化 `k2s`→`k2v5` 再查);**输出展示**经 `ProtocolDisplay` 显示 `k2s`。过滤与显示解耦,避免"显示 k2s 却过滤不到"。

## 详细设计

### 1. 数据模型

`api/model.go` — `SlaveNode` 加列:

```go
// IP 类型(节点的实际出口 IP 性质),由 sidecar 上报或运维覆盖
// 不加 index:低基数(3 值)索引近乎无用,且当前无 filter-by-ip_type 查询
IPType string `gorm:"column:ip_type;type:varchar(20);not null;default:'unknown'"`
```

`api/model_private_node.go` — 补常量:

```go
const (
    IPTypeResidential    = "residential"
    IPTypeNonResidential = "non_residential"
    IPTypeUnknown        = "unknown" // 尚未上报/未知
)
```

**迁移**:AutoMigrate 加列,存量行落默认 `unknown`。无需手工迁移脚本(纯加列 + 默认值)。

### 2. 上报路径(sidecar → Center,与 Country/Region 同构)

**sidecar 配置** `docker/sidecar/config/config.go`:
- `NodeSectionConfig` 加 `IPType string` `yaml:"ip_type"`(**默认 `unknown`**,始终上报)。
- 加环境变量读取 `K2_IP_TYPE`,照 `K2_JUMP_PORT_MIN`(config.go:170-182)的模式注入。

**sidecar 注册请求** `docker/sidecar/sidecar/node.go`:
- `Node` 加 `IPType` 字段;`main.go NewSidecar` 从 cfg 拷贝(参照 Region,main.go:72-74)。
- `buildNodeUpsertRequest`(node.go:213-223)带上 `IPType`(始终带,未配置则为 `unknown`)。
- `NodeUpsertRequest` 结构加 `IPType string` `json:"ipType"`。

**Center 接收** `api/slave_api_node.go`:
- `SlaveNodeUpsertRequest` 加 `IPType string` `json:"ipType"`。
- 写库逻辑(create 分支 line 141-152 / update 分支):**无条件写入** `NormalizeIPType(req.IPType)` 到 `SlaveNode.ip_type`(见 C1,空/非法 → `unknown`)。

**同权同变更语义(last-writer-wins,关键正确性点)**:
- sidecar 与运维(MCP/后台)对 `ip_type` **平权**,谁后写谁生效,无任何一方被特殊保护。
- sidecar 每次注册都按自己当前值写,**可覆盖运维改动**;运维经 MCP/后台改也可覆盖当前值——直到下次 register。
- **持久真值源 = sidecar 的 `K2_IP_TYPE` env**:私有节点装机烤入 `residential` → 每次注册稳定断言;共享节点由运维在 compose 设 env 或经 MCP 改。
- MCP/后台修改是**即时生效**手段;若要跨 register 持久,需同步更新节点的 `K2_IP_TYPE` env(runbook 说明)。
- 私有节点真换了 IP 性质:改 compose env,re-register 生效。

**k2s docker compose** `api/docker-compose.yml`:
- sidecar service 加 `K2_IP_TYPE` env(私有节点由装机 cloud-init 注入,值取自 `ProvisionParams.IPType`)。
- `private-node-provisioning` skill / runbook 更新:装机时把意图 IPType 写进 compose env。

### 3. 运维写入(MCP + 后台)

**后台端点** `api/api_admin_node.go`:
- `AdminUpdateNodeRequest`(line 84-90)加 `IPType *string`。
- `api_admin_update_node` 经 `NormalizeIPType`(C1)写入 `updateData["ip_type"]`。
- 路由 `PUT /app/nodes/:ipv4` 已存在(route.go:421),无需新增。
- `list_tunnels`/`/app/tunnels` 的 protocol 过滤入参经归一化别名 `k2s`→`k2v5`(C4)。

**MCP 工具** `tools/kaitu-center/src/tools/`:
- **新增 `update_node`**(`group: 'nodes.write'`,`method: 'PUT'`,`path: /app/nodes/${ipv4}`),参数 `ipv4` + 可选 `name`/`country`/`ipv6`/`ipType`。现状只有 `update_tunnel`(admin-tunnels.ts),没有 update_node。
- `list-nodes.ts`:`NodeInfo` 加 `ipType`;`TunnelInfo.protocol` 经 `ProtocolDisplay`(C2)显示 `k2s`(保留 `url` 的 `k2v5://`)。

### 4. 客户端接口 `/api/v20260717/tunnels`

**新建** `api/route.go` 路由组 + handler:
```go
v20260717 := api.Group("/v20260717")
v20260717.GET("/tunnels", AuthRequired(), EnforceDeviceClass(), ProRequired(), DeviceAuthRequired(), api_v20260717_tunnels)
```

**新 handler**(独立方法,自包含):
- 复用 `api_k2_tunnels` 的查询/打分逻辑,但响应结构改为干净形态。
- 无 `:protocol` 参数,返回全部可服务隧道,`serverUrl` 必填(见 C3)。
- `DataSlaveTunnelV20260717`(新结构,不复用旧 `DataSlaveTunnel`):
  - `protocol` 经 `ProtocolDisplay`(C2)吐 `"k2s"`。
  - 加 `ipType string`(取自 `SlaveNode.ip_type`)。
  - `serverUrl` **保留 `k2v5://`**(客户端连接解析)。
  - 不带 `echConfigList`(k2v4 遗留字段,C3)。

**旧 `/api/tunnels` 冻结**:不改响应;加 deprecation 计数(日志带 `X-K2-Client`)。

### 5. daemon `/api/subs`(增量,不升版本)

`api/api_subs.go`:
- `SubsTunnel`(line 43-55)加 `IPType string` `json:"ipType,omitempty"`。
- `buildPrivateSubsTunnels` / `fetchK2V5Tunnels` 填入 `tunnel.Node.IPType`。
- `ResolveGatewayPrivateTunnels`(entitlement_resolver.go)已 `Preload("Node")`,`Node.IPType` 直接可读。
- 老 daemon 忽略未知字段;新 daemon pick 逻辑可优先住宅 IP(本 spec 不实现 pick 改动,仅暴露字段)。

### 6. webapp 显示

`webapp/`:
- 隧道列表 fetch 改打 `/api/v20260717/tunnels`。
- `webapp/src/services/api-types.ts` `SlaveTunnel` 加 `ipType?: string`。
- 列表/卡片组件:`ipType === 'residential'` 时渲染「住宅IP」label chip(沿用 Dashboard 既有 chip 视觉)。
- 老装机跑老 bundle 打 v1 → 无 `ipType` → 无标签(纯增量,无害)。

## 单元边界

| 单元 | 职责 | 依赖 | 可独立测? |
|------|------|------|-----------|
| `SlaveNode.ip_type` 列 + 常量 | 存储节点 IP 类型 | GORM 迁移 | ✅ 迁移测 |
| sidecar `K2_IP_TYPE` → upsert | 上报 IP 类型 | config/env | ✅ sidecar 单测 |
| Center upsert 写 ip_type | 无条件写入(last-writer-wins) | 请求字段 | ✅ handler 测(覆盖既有值) |
| admin/MCP `update_node` | 运维纠错写入 | PUT 端点 | ✅ 集成测 |
| `/api/v20260717/tunnels` handler | 干净形态 + ipType | 查询逻辑 | ✅ handler 测 |
| `/api/subs` ipType 增量 | 暴露 ip_type | Node preload | ✅ handler 测 |
| `NormalizeIPType` / `ProtocolDisplay` | 归一化/显示映射(C1/C2) | 无 | ✅ 纯函数表驱动测 |
| webapp chip | 住宅IP 显示 | api-types | ✅ vitest |

**测试约定**:Center 侧改动(upsert 归一化、admin update、v20260717 handler、subs 增量)按项目惯例补**真 dev MySQL 集成测**;`NormalizeIPType`/`ProtocolDisplay` 走纯函数表驱动单测(含非法值→unknown、k2s↔k2v5 别名两路)。

## 部署顺序

1. **迁移**:Center 加 `ip_type` 列(AutoMigrate,接客前自迁移)。
2. **Center 上线**:新端点 `/api/v20260717/tunnels` + admin `IPType` 字段 + `/api/subs` 增量 + upsert 接收逻辑。
3. **MCP build**:`update_node` 工具 + `list_nodes`/`list_tunnels` ipType/k2s 标签。
4. **sidecar / k2s**:compose 加 `K2_IP_TYPE` env + `private-node-provisioning` runbook 更新。
5. **webapp bundle**:切 v20260717 + 住宅IP chip(进 desktop/mobile 发版)。

## 不做(YAGNI)

- **不接第三方 IP 情报自动探测**(residential vs datacenter 实时判定):env/config 为准 + 运维覆盖足够;以后可加。
- **不换 wire scheme** `k2v5://` → `k2s://`:大半径 k2 core 改动,无收益(标签层已脱 k2v5)。
- **不做 `/api/subs` 的 pick 优先住宅 IP 逻辑**:本 spec 只暴露字段,pick 改动另议。
- **不全局升版本**:只 `tunnels` 升日期版本,其余 `/api/*` 不动。
- **不加老键→新键兼容桥**:v1 冻结、v2 干净,清晰分界(符合 no-defensive-migration-bridges 原则)。

## 开放风险

- **ip_type 真值准确性**:依赖装机 agent 正确烤入 env / 运维正确设置。错了靠 MCP 纠正。无自动校验(YAGNI 接受)。
- **webapp 端点切换**:新 bundle 打 v20260717,需确认 desktop/mobile 发版节奏;v1 仍可用,过渡无断点。
