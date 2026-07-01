# 专属节点 AI-Agent 开通与部署（Agent-Driven Provisioning）

- **日期**: 2026-06-11
- **状态**: 设计待评审
- **关联**: 承接 `2026-06-09-private-node-router-product-design.md` §7。本 spec 定义**消费侧**（agent），Center 侧（producer）见母 spec §7。

---

## 1. 定位

专属节点开通的「建机 + 部署」是**低频、高价值、易失败**的运维操作。与其固化成 cloud-init 盲注脚本（失败无重试、无观测、无补救），不如交给一个**挂 `kaitu-center` MCP 的 Claude Code agent**：它能读 SSH 输出、判断失败、重跑、按需调整。

**职责边界**：Center 只发出 `ProvisioningIntent`（运维意图）。本 spec 的 agent 把意图变成一台跑着 k2s 的专属节点，节点 sidecar 自注册带 claim 后，Center 自动激活订阅——**激活不在 agent 手里**，agent 只负责"把机器和服务搞起来"。

```
Center(producer) ──emit ProvisioningIntent──▶ [MCP 队列] ──claim──▶ AI agent(consumer)
                                                                        │ create_cloud_instance
                                                                        │ exec_on_node (SSH deploy)
                                                                        ▼
                                              节点 sidecar 自注册带 claim ──▶ Center 激活 sub
```

## 2. 消费契约：ProvisioningIntent（由 Center 提供的 3 个 MCP 工具）

| MCP 工具 | 作用 | 返回 |
|----------|------|------|
| `list_provisioning_intents(status=queued)` | 列待开通意图 | `[{id, sub_id, status, spec, identity, emitted_at}]` |
| `claim_provisioning_intent(id, holder)` | 原子租约 `queued→claimed`，设 lease deadline | 完整 intent（含 spec + identity）；已被他人认领则失败 |
| `report_provisioning(id, {status, instance_id?, ipv4?, error?})` | 上报进度/结果 | ok |

**intent 形态**（母 spec §7.1）：

```
spec     { region, bundle_id, image_id, compose_variant, k2_version, traffic_total_bytes, ip_type }
identity { claim_token, center_url, domain }   // node_secret 不在此——agent 自己生成
```

- `claim_token` 是激活的唯一钥匙：必须原样写入目标机 `.env` 的 `K2_PRIVATE_CLAIM`。
- `domain` 为空则部署时让 sidecar 用 sslip.io 自生成。
- `node_secret` 由 agent 生成并写入 `.env` 的 `K2_NODE_SECRET`；节点首次自注册时把它当 SecretToken 带给 Center，Center 在那一刻才学到——**agent 无需也不应把 node_secret 回传 Center**。

## 3. 开通流程（agent 主循环）

```
loop:
  1. intents = list_provisioning_intents(queued)；空则 sleep/退出
  2. 选一条 → claim_provisioning_intent(id, holder=agent-id)
       └─ 失败（被抢）→ 下一条
  3. 幂等探测：按确定性名 pn-<sub_id> 查 list_cloud_instances
       ├─ 已存在 running → 跳过建机，复用（上次中断重入）
       └─ 不存在 → create_cloud_instance(region, bundle_id, image_id, name=pn-<sub_id>)
  4. report_provisioning(id, {status:provisioning, instance_id, ipv4})
  5. 等机器 SSH 可达（exec_on_node 探活）
  6. 部署（§4）：exec_on_node 跑 provision-node.sh + 写 .env + 起专属 compose
  7. 验证：exec_on_node 'docker ps' 确认 k2v5 + k2-sidecar Up
  8. 等节点自注册（轮询 sub 状态或节点出现）——成功即 Center 已激活
  9. report_provisioning(id, {status:succeeded})
  失败任一步 → report_provisioning(id, {status:failed, error}) + 不阻塞超时闸门
```

**确定性命名 `pn-<sub_id>`** 是幂等的根：重入时先探测再建机，杜绝孤儿 VPS。

## 4. 部署细节（exec_on_node SSH）

复用现有 `docker/` 部署链，**不重造**：

1. **SSH 凭据**：agent 凭 `instance_id` 通过云 API 取（如 Lightsail 默认 key pair）。新机 SSH 在 22 端口；`provision-node.sh` 步骤 13 会切到 1022——切换后续连接走 1022。
2. **跑 `provision-node.sh`**（16 步幂等：Docker CE + IPv6 + BBR + SSH 加固 + journald + crash monitor + auto-update cron）。
3. **写 `/apps/kaitu-slave/.env`**：
   ```
   K2_NODE_SECRET=<agent 生成>
   K2_PRIVATE_CLAIM=<intent.identity.claim_token>
   K2_CENTER_URL=<intent.identity.center_url>
   K2_DOMAIN=<intent.identity.domain 或空>
   K2_VERSION=<intent.spec.k2_version>          # 钉版本，不用 :latest
   ```
4. **专属节点版 compose**（`compose_variant`）：与共享池 compose 的关键区别是 **sidecar 带 §9.3 流量计量断流配置**（`traffic_total_bytes` + 95% Center 断流）。`compose_variant` 决定拉哪份 compose / 传哪些 sidecar env。
5. `docker compose --env-file /apps/kaitu-slave/.env up -d`。

> **待 Plan 3 对齐**：专属 compose 的精确 sidecar 配置（计量上报端点、断流阈值）依赖 Plan 3「k2s 流量计量 + usage heartbeat 断流」落地。在 Plan 3 前，`compose_variant=private` 可先等同共享池 compose + 占位 env，真机 smoke 时补齐。

## 5. 安全

- `claim_token` / `node_secret` 是机密：**禁止写入 agent 日志 / report 调用 / 任何持久输出**。exec_on_node 写 `.env` 用 heredoc，不在命令行明文传参（避免进程列表泄漏）。
- agent 身份（认领 holder）应可审计：每条 intent 的 lease 记录 holder + 时间。
- `claim_provisioning_intent` / `report_provisioning` 走 admin 鉴权的 Center 端点（MCP 已是 admin token）。
- 每台机一个唯一 claim：即便客户 root 读到自己机器的 claim，也只能认领自己那台（母 spec §7.4）。

## 6. 失败与租约语义

| 失败 | 处理 |
|------|------|
| 认领竞争（被抢） | claim 返失败 → 跳过 |
| 建机失败 | `report_provisioning(failed, error)` → admin 介入 |
| SSH/部署失败 | agent 可在 lease 内自重试（部署步骤幂等）；仍失败 → report failed |
| agent 认领后掉线 | lease deadline 到 → Center 把 intent 回 `queued` 供再认领 |
| 节点始终不自注册 | **Center 超时清扫 cron**（sub provisioning > T 分钟）置 sub=failed——权威闸门，不依赖 agent 自报 |

**关键不变量**：sub 的终态（active / failed）由 Center 掌控（自注册激活 + 超时清扫），agent 的 report 只影响 intent 的运维可见性。agent 任意时刻崩溃都不会让 sub 永久卡死。

## 7. 本 spec 要建的东西（与 Center producer 解耦清单）

Center 侧（母 spec §7，单独 Plan）：`ProvisioningIntent` 模型 + `emitProvisioningIntent` + 3 个 HTTP 端点 + 自注册 CloudInstance 回填 + intent 生命周期。

**本 spec（agent 侧）要建**：
1. `tools/kaitu-center` 3 个 MCP 工具包装（list/claim/report provisioning intents），对接 Center 新端点。
2. agent 开通 runbook / prompt（§3 主循环固化为可复用提示词或脚本）。
3. 专属节点版 compose（`compose_variant=private`）—— 与 Plan 3 协同。
4. SSH key 获取适配（按 provider 取默认 key pair）。
5. 真机 smoke：真 Lightsail 起机 → agent 部署 → 节点自注册 → sub 激活 → 路由器接入跑流量 + 断流验证。

## 8. 待决问题

- **agent 触发方式**：常驻轮询 `list_provisioning_intents` vs 事件推送（webhook/通知）触发 agent run？首版倾向常驻轮询（简单、无需 Center 反向连 agent）。
- **compose_variant 取值集**：`private` 一种够否，还是按 ip_type（住宅/非住宅）/ region 再分？倾向单值 + env 参数化。
- **多 provider SSH key 抽象**：Lightsail 有默认 key API，其它 provider（住宅 IP / Plan 6）的 SSH 接入方式待定。
