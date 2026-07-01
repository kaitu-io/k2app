# 节点运维任务队列 (NodeOperation) + 装机工单联动 设计

- **日期**: 2026-06-14
- **状态**: 设计待评审
- **作者**: 与 David 协作 brainstorming 产出
- **关联**: 承接 [[project_private_node_router_product]] / [[project_dedicated_line_reframe_phase1]];泛化现有 `NodeProvisionJob`(`model_node_provision_job.go`);交付 task #19(生命周期真停机/真销毁)的执行侧;为 #6(住宅 IP 换 IP)提供 `change_ip` 执行底座。

---

## 1. 背景与问题

定制线路(专属节点)产品里,节点的**运维动作**——开通建机、换 IP、停机、销毁——目前没有统一的执行与跟踪机制:

- 开通有 `NodeProvisionJob` 队列,但**只覆盖"开通"一个动作**,且是为外部 AI agent 消费而设计(claim/report MCP 工具,无 admin 面板)。
- 生命周期 cron(`worker_private_node_lifecycle.go`)进入 `suspended`/`deprovisioned` 时**只重贴状态标签,从不调用任何 provider API**——过期的 VPS 实际仍在运行、仍在产生成本(task #19 的洞)。
- 换 IP / 销毁在 cloud 实例 admin 页有按钮,但与专属节点订阅生命周期脱节。

### 1.1 产品决策(已与 David 对齐)

1. **部署/运维动作由人工完成,不自动化**(本期)。路由器装机本身是高技术支持的工作;节点的开通/变更同样需要人工最终把关。**自动化(agent 认领同一队列自动执行)是下一步**。
2. 因此设计落点是:**把运维动作记录成任务,通过 MCP + admin 面板跟进、处理、查看进展**。执行是外部人工(console / SSH),队列只记录意图 + 进度 + 结果。
3. **下单后给用户开装机协助工单**,并通过**内部告警(Slack/邮件)+ 给用户发欢迎邮件**确保高优先级跟进(不在工单上加优先级字段)。

### 1.2 核心洞察

`#18 升档 / #19 停机销毁 / #6 换 IP` 三个需求抛出的是同一个问题:**节点运维动作如何被派发给人工并跟踪**。统一答案 = 一个泛化的运维任务队列。现有 `NodeProvisionJob` 已经是"记录意图 + 认领 + 上报进度 + 租约"的形态,只是被锁死在"开通"单一动作上——泛化它即可。

---

## 2. 范围

### 2.1 In Scope(本设计)

- **A. NodeOperation 运维任务队列**:泛化 `NodeProvisionJob` 为通用运维队列,覆盖 `provision` / `change_ip` / `stop` / `destroy`;人工经 admin 面板 + MCP 认领、标进度、填结果;生命周期 cron 接线派发 stop/destroy(交付 #19 执行侧)。
- **B. 装机工单联动**:private_node 订单付款后自动建客户工单 + 内部 Slack/邮件告警 + 给用户发欢迎/装机引导邮件。

### 2.2 Out of Scope(明确不做)

- **provider 自动化执行**:本期不接 provider API 自动跑(stop/destroy/change_ip 的真正云操作由人工外部完成)。agent 认领同一队列自动化 = 下一独立工作。
- **家庭 2T→4T 升档(#18)**:大底座 VPS 方案下升档 = 纯改 `TrafficTotalBytes` 数字,恢复全自动,**不产生运维任务**。归独立小端点 + 独立 plan。`upgrade_quota` 在 `action` 枚举里**预留但不启用**(留给未来"换更大 VPS"模型)。
- **住宅 IP provider 接入(#6)**:本队列提供 `change_ip` 执行底座,但住宅 provider 本身的接入是独立工作。
- **provider 级 stop API**:provider 接口当前只有 `DeleteInstance` / `ChangeIP`,无 stop。本期 `stop` 是"去 console 停机"的人工工单,不加 provider stop 方法(留给 agent 阶段)。

---

## 3. 数据模型

### 3.1 `NodeOperation`(泛化自 `NodeProvisionJob`)

**prod 当前无 private_node 系列表**(已核对:部署的二进制早于 private-node 功能)——故直接重塑,删 `NodeProvisionJob` 改 `NodeOperation`,**无数据迁移、无兼容桥**(遵循 no-defensive-bridge 约定)。

```go
// 动作类型
const (
    NodeOpProvision = "provision"  // 开通建机 + 部署
    NodeOpChangeIP  = "change_ip"  // 换 IP
    NodeOpStop      = "stop"       // 停机(保 IP)
    NodeOpDestroy   = "destroy"    // 销毁实例 + 释放 IP
    // NodeOpUpgradeQuota = "upgrade_quota" // 预留:未来"换更大 VPS"模型;v1 不启用
)

// 状态机
const (
    NodeOpQueued     = "queued"      // 待认领
    NodeOpClaimed    = "claimed"     // 已认领(持租约)
    NodeOpInProgress = "in_progress" // 执行中
    NodeOpDone       = "done"        // 完成
    NodeOpFailed     = "failed"      // 失败
    NodeOpCanceled   = "canceled"    // 取消(如续费回收撤销待执行的 stop/destroy)
)

// NodeOperation 专属节点运维任务:Center 派发,人工(未来 agent)认领并外部执行,回上报。
// 执行是外部人工动作(console/SSH);本表只记录意图 + 进度 + 结果(运维可见性)。
// sub.status 仍是订阅生命周期的权威视图,与本表 status 解耦。
type NodeOperation struct {
    ID        uint64 `gorm:"primarykey" json:"id"`
    CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
    UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

    Action          string  `gorm:"type:varchar(20);not null;index:idx_sub_action_status,priority:2" json:"action"`
    SubID           uint64  `gorm:"not null;index:idx_sub_action_status,priority:1" json:"subId"` // → PrivateNodeSubscription.ID(非唯一)
    CloudInstanceID *uint64 `gorm:"index" json:"cloudInstanceId"`                                // 目标实例;provision 建机前为 NULL
    Status          string  `gorm:"type:varchar(20);not null;index:idx_sub_action_status,priority:3;index" json:"status"`

    // 租约(人工认领;未来 agent 同机制)
    Holder        string `gorm:"type:varchar(128)" json:"holder"`
    LeasedAt      int64  `gorm:"not null;default:0" json:"leasedAt"`
    LeaseDeadline int64  `gorm:"not null;default:0;index" json:"leaseDeadline"`

    Params    string `gorm:"type:json" json:"params"`              // 动作专属输入(见 §3.2)
    Result    string `gorm:"type:json" json:"result"`              // 动作专属结果(见 §3.2)
    LastError string `gorm:"type:text" json:"lastError,omitempty"`

    CreatedBy   string `gorm:"type:varchar(64);not null" json:"createdBy"` // system:order | system:lifecycle | admin:<email>
    CompletedAt int64  `gorm:"not null;default:0" json:"completedAt"`
}
```

**幂等(取代原 `SubID uniqueIndex`)**:同一 `(SubID, Action)` 至多一条 **open**(queued/claimed/in_progress)记录。MySQL partial unique index 不便,改**事务内 check-before-insert**:`SELECT ... WHERE sub_id=? AND action=? AND status IN (open) FOR UPDATE` 命中则跳过创建。配 `idx_sub_action_status` 复合索引加速。

> provision 动作额外保留 `emitNodeProvisionJob` 原有的"sub 状态原子门控"(pending/provisioning → provisioning),与上面的 open 去重叠加。

### 3.2 `Params` / `Result` JSON 形态(按动作)

| Action | Params | Result |
|---|---|---|
| `provision` | `{region, bundleId, imageId, composeVariant, k2Version, trafficTotalBytes, ipType, domain}`(原 spec 快照) | `{instanceId, ipv4}`(节点自注册回填,见 §4.1) |
| `change_ip` | `{targetRegion?, reason}` | `{oldIp, newIp}` |
| `stop` | `{reason}`(如 "grace ended") | `{stoppedAt}` |
| `destroy` | `{reason}` | `{destroyedAt, ipReleased}` |

> Go 侧用具体 struct `json.Marshal/Unmarshal` 进出 `Params`/`Result`,不裸传 map。每动作一个 typed payload struct,集中在 `model_node_operation.go`。

---

## 4. 状态机与完成语义

### 4.1 按动作的触发与完成

```
queued ──claim──> claimed ──start──> in_progress ──┬──> done
   │                  │                  │          └──> failed
   └──────── cancel ──┴──────────────────┘──> canceled
```

| 动作 | 触发者 | 完成判定 |
|---|---|---|
| `provision` | 订单付款(§6.1)自动 | 操作员可标 in_progress + 填 instanceId/ipv4;**`done` 仅由节点自注册权威翻转**(沿用现有 `slave_api_node.go` claim 匹配激活路径) |
| `change_ip` | **管理员手动创建**(§7 / §8 `create_node_operation`) | 操作员标 done + 填 `{oldIp,newIp}` |
| `stop` | 生命周期 cron(进 suspended) | 操作员去 console 停机后标 done |
| `destroy` | 生命周期 cron(进 deprovisioned) | 操作员销毁 + 释放 IP 后标 done(Center 可选事后 `GetInstanceStatus` 复核;v1 信任操作员 + Slack) |

**provision 完成的特殊性(保留现有不变式)**:`provision` 任务的 `done` 不可由人工/MCP 直接设置——只能由节点带 `ProvisionClaimToken` 自注册时,Center 注册端点(`slave_api_node.go`)在置 `Class=private`+激活 sub 的同一路径里翻转。`update_node_operation` 对 `action=provision` 拒绝 `status=done`(返 `ErrorInvalidOperation`),防 job/sub 状态分裂(沿用原 `report` 端点禁报 succeeded 的设计)。

### 4.2 认领原子性

`claim` 用原子 CAS:`UPDATE node_operations SET status='claimed', holder=?, leased_at=?, lease_deadline=? WHERE id=? AND status='queued'`;`RowsAffected==0` → `ErrorConflict`(已被他人认领)。沿用现有 `adminClaimProvisionJob` 实现。

### 4.3 租约超时回收

`claimed`/`in_progress` 且 `lease_deadline < now` 的任务应能回 `queued`(供再认领)。**字段(lease_deadline + index)本期就位,但租约清扫 cron 本身留到 agent 阶段再加**(人工操作员极少需要自动回收,且人工可在 admin 页手动改状态);plan 阶段确认现有是否已有 reaper,无则不在本期补。

---

## 5. 生命周期 cron 接线(交付 #19)

`handlePrivateNodeLifecycleSweep` 在重贴标签的基础上**派发运维任务**:

- **step 3 `grace→suspended`**:对每条成功转 suspended 的 sub,创建 `NodeOperation(action=stop, sub_id, cloud_instance_id=sub.CloudInstanceID, created_by="system:lifecycle", params={reason:"grace ended"})`——若该 sub 无 open stop 任务(幂等)。
- **step 4 `suspended→deprovisioned`**:创建 `NodeOperation(action=destroy, ..., params={reason:"suspend ended"})`——若无 open destroy 任务。
- **nil 实例守卫**:`sub.CloudInstanceID == nil`(开通从未成功的 sub 走到 suspended/deprovisioned)时**跳过派发**(没有实例可停/可销毁)+ log,避免派出操作员无从下手的空任务。
- **派发幂等的并发保证**:每条 sub 的"查 open 同动作 → 无则插入"包在一个短事务里(`SELECT ... FOR UPDATE` 锁 sub 行),防 cron 与管理员手动创建(§7)同瞬间双插。lifecycle cron 单实例运行,主要竞争来自管理员手动 destroy 与 cron destroy 撞车。
- **step 1 续费回收**:sub 从 grace/suspended 回 active 时,**取消该 sub 所有 open 的 stop/destroy 任务**(`UPDATE ... SET status='canceled' WHERE sub_id IN (recovered) AND action IN ('stop','destroy') AND status IN (open)`)。

> **续费取消是关键安全逻辑**:不取消则已续费客户的机器会被操作员照单停机/销毁,丢数据 + 丢 IP。续费回收(step 1)与派发(step 3/4)在同一次扫描,且 step 1 在最前——已续费的 sub `expires_at` 已是未来,本就不入 graceEnded/suspendEnded cohort,故不会被重新派 stop/destroy;step 1 的取消负责清掉**上一轮**已派发但尚未执行的 stop/destroy。

Slack 通知保留(进 suspended/deprovisioned 各发一条,沿用 `sendCloudSlackNotification`),并在文案里带上新建的运维任务 ID。

> **已知产品限制(EIP 依赖,记录不阻塞)**:`stop` 标语义是"停机保 IP",但 Lightsail 动态公网 IP 在 stop/start 后会变(保 IP 需挂静态 IP / EIP,属 Phase 2)。本期无 EIP 时,`suspended` 阶段机器停了即便用户续费回 active,**重启可能换 IP**,路由器需重新拿凭证才恢复(`suspended` 阶段路由器本就已断连,影响可接受)。真正的"suspend→续费→同 IP 秒恢复"是 Phase 2 EIP 能力。本设计的 `stop` 任务先把"该停机"这件事派出去,IP 连续性留给 Phase 2。

---

## 6. 装机工单联动(子交付 B)

### 6.1 触发点

订单付款的 **post-commit 钩子**(现有 `enqueueProvision` 所在位置,即 `MarkOrderAsPaid` → `applyOrderToBuyer` 收集 `provisionSubIDs` 后、tx.Commit() 之后)扩展:对每个 private_node sub,best-effort 依次执行:

1. **自动建客户工单**(复用 `FeedbackTicket`):
   ```go
   FeedbackTicket{
       FeedbackID:    uuid(),
       UDID:          fmt.Sprintf("pn:sub:%d", sub.ID), // UDID not-null;无设备来源,用合成标识
       UserID:        &order.UserID,
       Email:         buyerEmail,
       Content:       installGuideContent, // 见下方文案要求
       Status:        "open",
       AutoGenerated: true,
       Meta:          `{"type":"private_node_install","subId":N,"orderId":M,"region":"..."}`,
   }
   ```
   **工单文案要求**(实现时定稿,作为常量/模板存 Center):面向用户的中文文案,**必须用「开途/专属线路/路由器」,禁裸词 "Kaitu"**([[feedback_brand_chinese_kaitu_forbidden]]);内容含:① 欢迎语 + 已收到定制线路订单;② 装机大致步骤概述;③ "我们会主动联系协助" 的承诺 + 联系/回复入口。欢迎邮件(§6.1.3)与本工单文案共用同一套素材,语义一致。
2. **内部告警**(Slack + 邮件)给运维/support:`sendCloudSlackNotification("Dedicated Line Order", "新定制线路客户 user=N order=M 已付款,需装机协助 + 节点开通(sub=K, region=R)")`。
3. **欢迎/装机引导邮件**给用户:走**异步 EDM 入队**(非内联,避免 [[reference_edm_lazy_translation_pitfall]] 懒翻译同步卡死 worker),模板 slug `private-node-welcome`,发送前 `templateSlugExists` 早守卫(模板缺失则 skip + log,不报错)。

### 6.2 错误处理

工单/告警/邮件全部 **best-effort**:任一失败仅 `log.Errorf` + Slack,**绝不阻断或回滚订单**(钱已收,订单必须落库)。与现有 invite 奖励 / retailer 返现的 best-effort 副作用一致。运维可据告警手动补开工单。

### 6.3 不加工单优先级字段

按决策,工单模型不加 `priority` 字段;"加急"效果由 §6.1 的内部告警 + 欢迎邮件达成。support 在 `/manager/tickets` 看到 `autoGenerated=true` + Meta.type 即知是定制线路装机单。

---

## 7. Admin 面板

新增 `/manager/node-operations`(复用 `/manager/cloud` 的客户端组件 + `api.*` 代理模式):

- **列表**:过滤 `action` / `status` / 用户;分页。列:id、action、sub/客户(user email)、实例/IP、status、holder、createdBy、时长(now-createdAt)、lastError。
- **行操作**:认领(claim,holder=当前 admin)、标 in_progress、标 done(+ 结果字段表单)、标 failed(+ error)、取消(canceled)。
- **新建运维任务**:页面顶部"新建"按钮,选 sub + action(`change_ip` 为主;也可手动建 ad-hoc `stop`/`destroy`)+ 填 params → 调 §8 `create_node_operation`。`provision` **不可手动建**(只能订单触发,防绕过付款)。
- **详情**:展示 Params(格式化)、客户上下文、**关联装机工单链接**(经 sub 关联 `/manager/tickets`)。
- provision 行的"标 done"按钮置灰(完成只能自注册),tooltip 说明。

---

## 8. MCP 工具

泛化现有 3 个 provision-job 工具(pre-launch 直接改名,不留兼容别名):

| 旧 | 新 | 端点 | 角色 |
|---|---|---|---|
| `list_provisioning_intents` | `list_node_operations(action?, status?, page?, pageSize?)` | `GET /app/node-operations` | `RoleDevopsViewer\|RoleDevopsEditor` |
| `claim_provisioning_intent` | `claim_node_operation(id, holder, leaseSeconds?)` | `POST /app/node-operations/:id/claim` | `RoleDevopsEditor` |
| `report_provisioning` | `update_node_operation(id, status, result?, error?)` | `POST /app/node-operations/:id/update` | `RoleDevopsEditor` |
| (无) | `create_node_operation(subId, action, params)` | `POST /app/node-operations` | `RoleDevopsEditor` |

- `create_node_operation` 用于手动派 `change_ip`(及 ad-hoc `stop`/`destroy`);**拒绝 `action=provision`**(防绕过付款建机)+ 走 §3.1 open 去重 + §5 nil 实例守卫。
- `claim_node_operation` 对 `action=provision` 仍返回 `identity{claimToken, centerUrl, domain}`(注入节点身份用);其他动作不返 identity。
- `update_node_operation` 对 `action=provision` 拒绝 `status=done`(§4.1)。
- Center 端点重命名 `api_admin_provision_job.go` → `api_admin_node_operation.go`,route.go 同步;`/app/provision-jobs*` → `/app/node-operations*`。

---

## 9. 受影响的现有代码(改造清单)

| 文件 | 改动 |
|---|---|
| `model_node_provision_job.go` → `model_node_operation.go` | 删 `NodeProvisionJob`,建 `NodeOperation` + action/status 常量 + typed payload structs |
| `provision_private_node.go` | `emitNodeProvisionJob` → `emitNodeOperation(provision)`;保留 sub 状态原子门控 + G1 配额不变式 backstop |
| `worker_private_node_lifecycle.go` | step3/4 派发 stop/destroy 任务;step1 取消 open stop/destroy(§5) |
| `slave_api_node.go` | 自注册激活路径:把翻转 `NodeProvisionJob.succeeded` 改为翻转 `NodeOperation(provision).done` + 回填 Result |
| `api_admin_provision_job.go` → `api_admin_node_operation.go` | list/claim/update 泛化;新增 `POST /app/node-operations`(create,拒 provision);新增按 action/status 过滤;provision done 守卫;open 去重 + nil 实例守卫 |
| `route.go` | `/app/provision-jobs*` → `/app/node-operations*` |
| `worker_private_node.go` | 超时清扫:`provisioning` 卡死的 sub 同步把对应 `NodeOperation(provision)` 置 failed |
| `logic_member.go` / webhook post-commit | 扩展 private_node 分支 post-commit:建工单 + Slack + 入队欢迎 EDM(§6) |
| `tools/kaitu-center/src/tools/admin-provision-jobs.ts` → `admin-node-operations.ts` | 3 工具改名 + action 参数 |
| `web/src/app/(manager)/manager/node-operations/` | 新增 admin 页 + `api.ts` 方法 |

---

## 10. 安全

| 边界 | 设计 |
|---|---|
| 端点鉴权 | list = DevopsViewer/Editor;claim/update = DevopsEditor(沿用现有) |
| claim 原子性 | CAS `WHERE status='queued'`,防双认领(§4.2) |
| provision claimToken | 仅 `claim_node_operation(provision)` 返回一次;注入节点身份;自注册匹配激活(沿用现有) |
| provision done 防伪 | `update` 拒绝 provision→done,完成只走自注册(§4.1) |
| 续费防误停 | step1 取消 open stop/destroy(§5) |
| 工单/告警/邮件 | best-effort,不阻断订单(§6.2);k2subs/secret 不入日志/工单 |

---

## 11. 测试策略

| 层 | 测试 |
|---|---|
| `NodeOperation` 状态机 | 单测:全动作 queued→claimed→in_progress→done/failed/canceled 转移 + 非法转移拒绝 |
| 幂等 | 单测:同 (sub,action) 第二条 open 创建被跳过;不同 action 可并存 |
| claim 原子性 | 单测:并发两 claim 仅一成功,另一 ErrorConflict |
| provision done 守卫 | 单测:`update(provision, done)` 被拒;自注册路径翻 done 成功 |
| 生命周期 cron 派发 | 集成测(真 dev MySQL):grace→suspended 建 stop、suspended→deprovisioned 建 destroy、幂等不重复建 |
| 续费取消 | 集成测:已派 stop/destroy 的 sub 续费 → open 任务转 canceled,不再被执行 |
| 装机工单联动 | 集成测:private_node 付款 → 建工单(autoGenerated+Meta)+ Slack 调用 + EDM 入队;失败不回滚订单 |
| admin 端点 | 角色鉴权;list 过滤;claim/update/cancel |
| MCP 工具 | 工具→端点映射 + 角色 |

---

## 12. 部署与迁移

- **DB**:`node_operations` 表纯 AutoMigrate 新建(prod 无旧 `node_provision_jobs` 数据,删旧建新无痛)。无手动迁移。
- **EDM 模板**:需在 Center 建 `private-node-welcome` 模板(缺失则 §6.1 守卫 skip,不报错)。
- **MCP**:`tools/kaitu-center` 工具改名 → `npm run build` + `.mcp.json` 生效(独立 npm 工程)。
- **website 镜像**:admin 页 `/manager/node-operations` 在 `web/`,走 `git push origin main:website` → Amplify(与 Center `make deploy-api` 是两条独立管线)。

---

## 13. 未来(本设计之外)

- **agent 认领同一队列自动执行**:把人工 holder 换成 agent;`claim_node_operation` + `update_node_operation` 已是 agent-ready 接口。这是"部署如何自动化"的下一步。
- **provider 级 stop API**:agent 阶段给 `cloudprovider.Provider` 加 `StopInstance`/`StartInstance`,让 stop/destroy 自动执行。
- **destroy 后置复核**:Center 异步 `GetInstanceStatus` 确认实例已销毁,与人工标记互为 defense-in-depth。
- **#18 家庭升档端点**(独立 plan)、**#6 住宅 IP provider 接入**(用本队列 change_ip 执行)。
