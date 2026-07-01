# 家庭流量档升级 (2T→4T) 设计

- **日期**: 2026-06-14
- **状态**: 设计待评审
- **作者**: 与 David 协作 brainstorming 产出
- **关联**: 承接 [[project_private_node_router_product]] / [[project_dedicated_line_reframe_phase1]];实现 task #18(Phase 2)。本设计与 NodeOperation 队列正交(`#18 升档纯改配额数字,不产生运维任务`,见 node-operation-queue 设计 §2.2)。

---

## 1. 背景与问题

定制线路(专属节点)按 Option A「大底座 VPS + 卖配额数字」模型设计:所有家庭线开在能覆盖最高档的大 bundle VPS 上,`2T`/`4T` 只是卖出的配额数字(`PrivateNodeSubscription.TrafficTotalBytes`)。用户买了 2T 线,本月跑满后被服务端 95% 断流(`slave_api_usage.go` verdict=stop),线路从 `/api/subs` 剔除(`isTunnelOverQuota`),路由器断连。

**缺口**:用户跑满 2T 想本月继续用,目前**无升档路径**——`api_order.go:18-30 validatePurchase` 老客禁改 tier(`IsFirstOrderDone=true` 且 `plan.Tier != buyer.Tier` 直接拒);`createPrivateNodeSubscription` 只在创建时写一次 `TrafficTotalBytes`(`provision_private_node.go:59`),无更新路径。用户只能等下月 epoch 清零,或弃用。

**目标**:让家庭线用户原地从 2T 升到 4T,补差价支付,当期立即恢复服务并拿到更高额度,往后续费按 4T。

---

## 2. 产品决策(已与 David 对齐)

1. **计费 = 补差价**:一次性付 `4T价 − 2T价` **全额**,**到期日不变**,档位**永久**升到 4T(续费起按 4T 价)。**已用流量不清零**:升级只抬高上限,用户立刻拿回 `4T − 已用` 的剩余额度。**零 prorate**(codebase 无 prorate 逻辑;边际成本≈0——大 bundle 已覆盖 4T——无需按比例保护毛利)。
2. **仅 active 可升级**:耗尽是主场景,而**配额耗尽 ≠ 时间到期**——耗尽用户仍是 `active`,正好被覆盖。`grace`/`suspended`/`deprovisioned`/`pending`/`provisioning`/`failed` 一律拒绝(无运行实例可 bump,或语义混乱)。
3. **不做降档**(YAGNI)。
4. **升级不接 campaign 优惠码**(保持金额计算单一:纯差价)。
5. **补差价必须走真实支付**:升级**不是**直接 mutate 端点,而是创建一张「升级订单」(差价金额)走现有 WordGate 支付,webhook 付款成功后才 bump。复用整套支付/幂等/审计基础设施。

---

## 3. 核心架构:升级 = 一张特殊订单 + webhook 应用

与购买完全平行。**不新建第二张 sub**,而是原地抬高现有 sub 的档位。

```
用户(active, 2T 耗尽)
  │ 1. GET /api/user/private-nodes → DTO.upgradeOptions[{targetPlan,priceDelta}]
  │ 2. POST /api/user/private-nodes/:id/upgrade {targetPlan}
  ▼
Center 校验(owner / active / 合法升级 / 成本不变式 / priceDelta>0)
  │ 创建 Order(PayAmount=差价, Meta.privateNodeUpgrade={subId,targetPlanId})
  │ → WordGate CreateAppCustomOrder → 返 payUrl
  ▼
用户支付 → WordGate webhook (api_webhook.go)
  │ MarkOrderAsPaid → applyOrderToBuyer
  │   private_node 分支:Meta 带 upgrade 标记 → applyPrivateNodeUpgrade(非 createPrivateNodeSubscription)
  │     FOR UPDATE 锁 sub + re-validate
  │     幂等守卫:sub.TrafficTotalBytes >= target → skip
  │     bump sub.TrafficTotalBytes=target + sub.PlanID=targetPlanId
  │     bump CloudInstance.TrafficTotalBytes=target (CloudInstanceID 非空时;不碰 used/epoch/resetAt)
  ▼
节点下个心跳读 CI.TrafficTotalBytes=4T → verdict serve → 线路回 /api/subs → 路由器自动重连(零代码)
```

### 3.1 致命同步点(设计成败处)

`CloudInstance.TrafficTotalBytes` 是与 `PrivateNodeSubscription.TrafficTotalBytes` **分离的另一份拷贝**。节点断流判决读的是 **CI 那份**:

```go
// slave_api_usage.go:89-90
if ci.TrafficTotalBytes > 0 && ci.TrafficUsedBytes*100 >= ci.TrafficTotalBytes*95 {
    verdict = "stop"
}
```

开通时两份同值(self-register 把卖出额写到 CI,见 Fix B);provider sync 对 private 实例**跳过** traffic 字段(`worker_cloud.go:326-334 isPrivateCloudInstance`),所以平时不漂移。**升级若只改 sub 不改 CI,节点仍按 2T×95% 断流 → 用户付了钱拿不到 4T。** `applyPrivateNodeUpgrade` 必须**同时 bump 两处**。这正是"恢复链路零代码"的前提。

---

## 4. 数据模型(零 schema 改,纯配置)

无新表、无新列。2T/4T 用**两个** `Plan`(Kind=private_node)+ 两个 `PrivateNodePlanSpec`(`model_private_node.go:83-95`),**共用同一大 bundle**:

| 字段 | 2T 档 | 4T 档 | 约束 |
|---|---|---|---|
| `Provider` | aws_lightsail | aws_lightsail | **必须相同** |
| `BundleID` | (大 bundle) | (同一大 bundle) | **必须相同** |
| `IPType` | non_residential | non_residential | **必须相同** |
| `AllowedRegions` | [...] | [...] | (校验目标 sub.Region ∈ 之内) |
| `TrafficTotalBytes` | 2T | 4T | 目标 > 当前 |
| `BundleTransferBytes` | >4T | >4T | **4T < BundleTransferBytes**(成本不变式) |
| `Plan.Price`(model.go:646) | 2T 价 | 4T 价 | `priceDelta = 4T价 − 2T价 > 0` |

运营用现有 MCP/DB 建这两档(本设计不含建档 handler——`PrivateNodePlanSpec` 无创建 handler 是既有事实,见 Plan 8 G1)。**"共用同一大 bundle" 是配置纪律,由升级端点的校验在运行时强制**(见 §6),而非数据库约束。

---

## 5. 升级选项暴露(改动 ①)

`GET /api/user/private-nodes`(`api_user_private_node.go:13-70`)的 DTO `DataPrivateNodeSubscription`(`type.go:587-606`)新增字段:

```go
UpgradeOptions []DataPrivateNodeUpgradeOption `json:"upgradeOptions"` // 可空数组,永不为 null

type DataPrivateNodeUpgradeOption struct {
    TargetPlan        string `json:"targetPlan"`        // 目标 Plan.PID
    Label             string `json:"label"`             // 目标档名(如 "4TB 家庭线路")
    TrafficTotalBytes int64  `json:"trafficTotalBytes"` // 目标配额
    PriceDelta        uint64 `json:"priceDelta"`        // 补差价(美分)
}
```

**计算**(仅当 `sub.Status == active` 时非空,否则空数组):
1. 加载 sub 当前 plan 的 spec(经 `sub.PlanID` → `PrivateNodePlanSpec`)。
2. 查所有 `Plan.Kind=private_node` 且其 spec 满足:同 `Provider`+`BundleID`+`IPType`、`TrafficTotalBytes > sub.TrafficTotalBytes`、`TrafficTotalBytes < BundleTransferBytes`、`sub.Region ∈ AllowedRegions`、`Plan.Price > 当前 plan.Price`。
3. 每个合格目标产出一条 option,`PriceDelta = targetPlan.Price − currentPlan.Price`,按 `TrafficTotalBytes` 升序。

> 计算复用 §6 的同一 `validateUpgradeTarget` 判定逻辑(单一真相源,不漂移)。N 通常极小(1-2 档),无性能顾虑;一次性查 private_node plans + specs 在内存过滤。

webapp 据此渲染"升级到 4T(补 $X)"。

---

## 6. 升级下单端点(改动 ②)

```
POST /api/user/private-nodes/:id/upgrade
Auth: AuthRequired() + EnforceDeviceClass()   (与 GET /api/user/private-nodes 同一中间件)
Body: { "targetPlan": "<plan PID>" }
Resp: { "payUrl": "...", "orderUuid": "...", "payAmount": <cents> }
```

**流程**:
1. `userID := ReqUserID(c)`;按 `id` + `user_id=userID` 加载 sub(**owner 隔离**,未命中 404)。
2. `sub.Status != "active"` → `ErrorInvalidOperation`("仅生效中的线路可升级")。
3. `getPlanByPID(c, body.targetPlan)` 加载目标 plan;`Kind != private_node` → `ErrorInvalidArgument`。
4. **`validateUpgradeTarget(sub, currentPlan, currentSpec, targetPlan, targetSpec)`**(§6.1)——非法升级拒。
5. `priceDelta := targetPlan.Price − currentPlan.Price`(校验 4 已保证 >0)。
6. 创建 `Order`:`UserID=userID`、`Title="升级到 "+targetPlan.Label`、`OriginAmount=priceDelta`、`PayAmount=priceDelta`、`PrivateNodeRegion=sub.Region`、Meta 经 `SetOrderMeta(targetPlan, nil, nil, true)` 后**叠加** `privateNodeUpgrade:{subId:sub.ID, targetPlanId:targetPlan.ID}`。落库。
7. 走现有 WordGate 下单(`api_order.go:259-299` 同一 `CreateAppCustomOrder` 路径,Amount=`order.PayAmount`)→ 返 `payUrl`。

**绕开 tier 闸门**:本端点**不调** `validatePurchase`(独立端点,我控制),故老客可升档。

### 6.1 `validateUpgradeTarget` 不变式(单一真相源)

| 校验 | 拒绝码 |
|---|---|
| `targetSpec.Provider == currentSpec.Provider` | `ErrorInvalidArgument`(非同 bundle) |
| `targetSpec.BundleID == currentSpec.BundleID` | 同上 |
| `targetSpec.IPType == currentSpec.IPType` | 同上 |
| `sub.Region ∈ targetSpec.AllowedRegions` | `ErrorInvalidArgument`(地区不支持) |
| `targetSpec.TrafficTotalBytes > sub.TrafficTotalBytes` | `ErrorInvalidOperation`(非升级) |
| `targetSpec.TrafficTotalBytes < targetSpec.BundleTransferBytes` | `ErrorInvalidOperation`(**成本不变式**,防 Lightsail overage) |
| `targetPlan.Price > currentPlan.Price` | `ErrorInvalidOperation`(差价非正) |

> 升级不走 `PrivateNodePlanSpec.BeforeSave`(那是写 spec 时的守卫),故端点**显式 re-validate** 成本不变式。§5 的 upgradeOptions 与本函数共用,保证暴露的选项必然可下单。

---

## 7. webhook 应用升级(改动 ③)

`applyOrderToBuyer`(`logic_member.go:67-152`)的 private_node 分支(行 89-121)**分叉**:

```go
if plan.Kind == PlanKindPrivateNode {
    if up := meta.PrivateNodeUpgrade; up != nil {
        return applyPrivateNodeUpgrade(ctx, tx, order, plan, up)   // 新:升级
    }
    sub, err := createPrivateNodeSubscription(ctx, tx, order, plan, time.Now().Unix())  // 原:新购
    // ... 原有 provisionSubIDs / IsFirstOrderDone 逻辑不变
}
```

> upgrade 订单**不入** `provisionSubIDs`(无需开通新机),且**不**触发装机工单/欢迎邮件(那是新购 onboarding;升级是已有线路的配额变更)。

### 7.1 `applyPrivateNodeUpgrade`

`func applyPrivateNodeUpgrade(ctx, tx *gorm.DB, order *Order, targetPlan *Plan, up *PrivateNodeUpgradeMeta) error`,**全程在 webhook 事务内**(与订单 IsPaid 原子):

1. `SELECT ... WHERE id=? FOR UPDATE` 锁 sub(`up.SubID`)。未命中 → log + 返回 nil(订单已收钱,不回滚;运维据告警补处理——与 best-effort 副作用一致)。
2. **owner 校验**:`sub.UserID != order.UserID` → log error + Slack + 返回 nil(不应发生;防串号)。
3. 加载 `targetSpec`(经 `targetPlan.ID`)。
4. **re-validate 成本不变式**:`targetSpec.TrafficTotalBytes >= targetSpec.BundleTransferBytes` → log error + Slack + **返回 nil 不 bump**(防御纵深:下单到付款间 spec 若被改坏,不吃 overage)。
5. **幂等守卫**:`sub.TrafficTotalBytes >= targetSpec.TrafficTotalBytes` → 已升级(同订单重投 / 已是更高档)→ log debug + 返回 nil(不二次 bump)。
6. bump sub:`UPDATE private_node_subscriptions SET traffic_total_bytes=?, plan_id=? WHERE id=?`(目标值 + targetPlan.ID)。
7. **bump CloudInstance**(若 `sub.CloudInstanceID != nil`):`UPDATE cloud_instances SET traffic_total_bytes=? WHERE id=?`——**仅** traffic_total_bytes 一列,**不碰** used/epoch/reset_at(用户立刻拿回剩余额度,不清零)。`CloudInstanceID == nil`(active 但未回填实例,罕见)→ 仅 bump sub + log warn(下次自注册/sync 时 Fix B 路径会把 sub 卖出额写回 CI)。
8. `sendCloudSlackNotification("Private Node Upgraded", "sub=N user=M 升档 2T→4T order=K")`。

**完成语义**:upgrade 订单的"应用"即 bump 成功,无需节点自注册确认(与 provision 不同——provision 的 done 才靠自注册)。

---

## 8. 恢复链路(零代码,自动)

bump 完 `CloudInstance.TrafficTotalBytes` 后,链路全自动(已存在,本设计不动):
- 节点下个心跳(≤60s)`POST /slave/usage` → Center 算 verdict:`used*100 >= 4T*95` 不再成立 → `verdict=serve`(`slave_api_usage.go`)。
- `isTunnelOverQuota`(`logic_tunnel_score.go`,95% 阈值)不再剔除该线 → 线路回 `/api/subs`。
- 网关 k2r refresh 重新拿到该线 → 自动重连(`gateway.go` Resolve+Pick)。

用户视角:补差价支付完成后约 1 分钟内,路由器自动恢复,本月可再用 `4T − 已用`。

---

## 9. webapp(改动 ④)

- `PrivateNodeSubscriptionView`(TS)加 `upgradeOptions: UpgradeOption[]`(与 Go DTO 逐字对齐)。
- `private-node-service.ts` 加 `upgradePrivateNode(subId, targetPlan): Promise<{payUrl}>`(POST 端点)→ 返回后 `window.location.href = payUrl`(沿用购买跳转模式)。
- `PrivateNodePanel`:**耗尽态**(`quotaExhausted=true`)红 Alert 的 CTA 从"再购买"(→/purchase)改为**优先**渲染"升级到 4T(补 $X)"按钮(有 upgradeOptions 时);**常态**也在面板提供低调的"升级配额"入口(有 upgradeOptions 时)。无 upgradeOptions(已最高档/非 active)则回退原"再购买"或不显示。
- i18n:`privateNode` 命名空间加 `upgrade` 子键,7 语言。中文面向场景**禁裸词 Kaitu**,用「开途/专属线路」([[feedback_brand_chinese_kaitu_forbidden]])。
- iOS IAP 分支不碰(private_node 走 web 支付,与现有购买一致)。

---

## 10. 受影响代码清单

| 文件 | 改动 |
|---|---|
| `logic_private_node_upgrade.go`(新) | `validateUpgradeTarget` + `computeUpgradeOptions` + `applyPrivateNodeUpgrade` + Meta 类型 `PrivateNodeUpgradeMeta` |
| `api_user_private_node.go` | GET DTO 加 `upgradeOptions`(调 `computeUpgradeOptions`);新增 `POST /api/user/private-nodes/:id/upgrade` handler |
| `route.go` | 注册 `POST /api/user/private-nodes/:id/upgrade` |
| `type.go` | `DataPrivateNodeSubscription` 加 `UpgradeOptions`;新增 `DataPrivateNodeUpgradeOption` |
| `logic_member.go` | `applyOrderToBuyer` private_node 分支分叉 upgrade vs create |
| Order Meta(`model.go` / SetOrderMeta 所在) | Meta 增 `privateNodeUpgrade` 字段(typed) |
| `webapp/src/services/private-node-service.ts` | `upgradePrivateNode` + 类型 |
| `webapp/.../PrivateNodePanel.tsx` | 升级 CTA(耗尽态 + 常态) |
| `webapp/src/i18n/*` | `privateNode.upgrade` 7 语言 |

---

## 11. 安全

| 边界 | 设计 |
|---|---|
| owner 隔离 | 端点按 `id + user_id=ReqUserID` 加载 sub;webhook apply re-check `sub.UserID==order.UserID` |
| 越权升级 | 端点不可指定他人 sub(owner 隔离);Meta.subId 在 apply 时再校验 owner |
| 成本不变式 | 端点 + webhook apply **双重** re-validate `target<BundleTransferBytes`(防下单→付款间被改坏) |
| 绕过付款 | bump 只在 webhook 付款成功事务内;端点只建订单不 mutate sub |
| 幂等 | apply 幂等守卫(`>=target` 跳过)→ 同订单重投 / 重复支付不二次 bump |
| 金额防篡改 | priceDelta 服务端算(读两 plan.Price),客户端只传 targetPlan PID,不传金额 |
| best-effort | apply 失败(sub 不存在/owner 不符/不变式破)只 log+Slack 不回滚订单(钱已收) |

---

## 12. 测试策略(可压 10/10 desk)

真 dev MySQL 集成测 + 单测:

| 层 | 测试 |
|---|---|
| `validateUpgradeTarget` | 单测:同 bundle+更高+成本不变式 OK;异 provider/bundle/iptype 拒;非升级拒;`target>=BundleTransfer` 拒;差价非正拒;region 不在 AllowedRegions 拒 |
| `computeUpgradeOptions` | 单测:active+有更高档→返选项(priceDelta 正确);非 active→空;已最高档→空;异 bundle 档不入选 |
| 升级端点 | 集成测:active sub 建升级订单(PayAmount=差价正确)+ 返 payUrl;非 active 拒;跨 owner 404;非法 targetPlan 拒;非 private_node plan 拒 |
| `applyPrivateNodeUpgrade` | 集成测:**双写 sub+CI** traffic_total_bytes;**不碰** CI used/epoch/reset_at;plan_id 更新;CloudInstanceID=nil 时仅 bump sub+warn |
| 幂等 | 集成测:同 upgrade 订单 apply 两次 → 第二次 skip 不二次 bump;已 4T sub 收 4T 升级订单 → skip |
| webhook 分叉 | 集成测:upgrade 订单走 apply 不走 create(不建新 sub、不入 provisionSubIDs、不发装机工单) |
| webapp | vitest:有 upgradeOptions→渲染"升级到 4T"CTA;耗尽态优先升级;无选项回退;`upgradePrivateNode` 调用 + 跳转;7 语言 key 存在;zh 无裸 Kaitu |

**desk 封顶**:真机端到端(真买家撞顶→补差价支付→节点心跳恢复→路由器重连)归 task #20 smoke 三连,本期不做(用户授权 smoke 留最后)。

---

## 13. 部署与迁移

- **DB**:**无 schema 改、无迁移**(纯复用现有表)。
- **数据配置**:运营建 4T 档 `Plan`(Kind=private_node)+ `PrivateNodePlanSpec`(同 2T 档的 Provider/BundleID/IPType/Region,`TrafficTotalBytes=4T`,`BundleTransferBytes>4T`,`Price=4T价`)。**部署后、上线前**配齐,否则 upgradeOptions 永远为空(功能静默不可用,但不报错)。
- **Center**:`make deploy-api`(无 migrate 步骤需要,但与其他未推 commit 同车时仍按既有 deploy 流程)。
- **webapp**:bundle 进各平台(web/desktop/mobile);web 站点 `git push origin main:website`。

---

## 14. 未来(本设计之外)

- **grace 期升级**:v1 仅 active;grace 期(已到期未续费但仍服务)升级需与续费 ExpiresAt 路径(task #19,尚未实现)协调,留后。
- **降档**:不做。
- **prorate**:不做(边际成本≈0)。
- **"换更大 VPS" 升级模型**:若未来某档超出大 bundle 容量,需换实例 = 走 NodeOperation `upgrade_quota`(已预留枚举,未启用)+ 真停机迁移,完全不同的工作。
- **加购按 GB**:不做(决策:满了升档或等月度清零,见 reframe)。

---

## 15. 架构师自评

**设计满意度:10/10**
- 升级 = 特殊订单 + webhook apply,**完全复用**支付/幂等/审计基础设施,零新支付原语,与新购对称。
- 致命同步点(sub vs CI 双写)被探索坐实并显式纳入设计与测试,不是事后补丁。
- `validateUpgradeTarget` 单一真相源,被 upgradeOptions 暴露与端点下单共用——**暴露的选项必然可下单**,无漂移。
- 成本不变式**双重** re-validate(端点 + webhook),覆盖下单→付款时间窗。
- 恢复链路零代码(复用既有心跳/score/refresh),升级只需正确 bump 两个数字。
- 幂等守卫结构性消除重复支付二次 bump。

**产品约束满意度:10/10**
- 计费严格落地用户决策(补差价全额 / 到期不变 / 永久升档 / 不清零 / 零 prorate)。
- 仅 active 精准命中主场景(耗尽≠到期),边界态明确拒绝。
- 中文品牌纪律、iOS 不碰、best-effort 不阻断订单、owner 隔离——全部遵守既有约定。
- 零 schema 改、零迁移,部署面最小;唯一上线前动作是配 4T 档数据(缺失则静默不可用,不报错,安全降级)。
- smoke 留最后符合用户"先建推荐功能"指令;desk 可达 10/10。
