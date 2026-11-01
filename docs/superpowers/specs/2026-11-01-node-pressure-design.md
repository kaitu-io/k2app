# 节点真实压力（Pressure = 实际吞吐 / M）

## 意图

客户端应该被引导到压力小的节点。现在做不到，因为线上根本没有可信的压力数据。David 定义的目标：尽量知道每个节点的最高接待能力 **M**，用 **实际压力 / M** 得到一个客观值。

用户感知方式：**后台分流**，用户无感（我方默认，David 尚未明确选择，之后可再决定是否展示）。

## 现状（2026-11-01 线上实测）

| 指标 | 线上值 | 原因 |
|---|---|---|
| 带宽 | 全车队 24h 峰值 < 0.001 Mbps | `collector.go getNetworkStats` 读容器自身 `/proc/net/dev`（veth），未走 `hostProcPath()` |
| 连接数 | 全为 0 | `ss -tan` 在容器内，原因未深究；k2 也主要走 UDP，本设计不再使用此指标 |
| CPU | 日志 2–7% | `getCPUUsage` 是开机累计平均，不是区间值 |
| 丢包 | 全为 0 | 同样读容器网卡，且为累计值 |
| `NetworkSpeedMbps` / `NetworkLatencyMs` | 恒 0 | sidecar 从未赋值 |
| `load` | 16–39，约等于内存占用 | 上述输入全失真；且不参与推荐 |
| `recommendScore` | 只看配额 + 周期进度 | `logic_tunnel_score.go` |

已证实的真实差距：昨日 `jp-tokyo.vultr.wm02` 148 设备 / 76GB，`jp-tokyo.aws.wm06` 5 设备 / 3.4GB。

10/28 以来按 `used_traffic_bytes` 差分算出：每台平均 0.4–22 Mbps，2 分钟均值峰值最高 332 Mbps。没有节点观测到饱和，**历史峰值只是 M 的下限，不能当 M**。

规格抽样：Vultr 1 vCPU/1GB，AWS 2 vCPU/1GB。

## 目标

1. 每个节点有可信的实时吞吐、CPU、steal。
2. 每个节点有一个 M（Mbps），能说清来源和样本量。
3. 压力参与 `recommendScore`，旧客户端不发版即生效。

## 非目标

- 用户可见的「拥挤/空闲」标识。
- 主动压测（David 认为难实施，已放弃）。
- 衡量跨境线路拥堵：M 只代表节点自身能力，线路慢这里看不到。
- 把用户从已连接节点迁走（节点 drain 是另一件事）。
- 解决 Lightsail 池化配额与单台掐断的错配。

## 反目标

- 分数抖动导致流量集体涌向同一批节点。
- 数据缺失时把节点当作高压（缺数据 = 中性，绝不惩罚）。
- 新老 sidecar 的数据混算（老 sidecar 的 CPU 是累计值，混进来 = 静默错误）。
- 压力让节点被隐藏或被拉黑：隐藏仍只由配额/离线决定。

## 方案对比

| 方案 | 结论 |
|---|---|
| A. 厂商标称表 | 否决：Lightsail 带宽和突发 CPU 都不是确定值，标称不等于加密隧道能力 |
| B. 主动压测 | 否决：需要大带宽压测机、消耗配额、影响真实用户 |
| **C. 被动推算** | **David 选定**：修好上报，用日常流量推算 M，不产生测试流量，能跟上版本变化。代价：要全车队升 sidecar、攒几天数据、低流量节点推算不准 |

## 设计

### 第一阶段：可信上报（不影响推荐）

**sidecar**（`docker/sidecar/sidecar/collector.go`、`node.go`）：
- 带宽：用 `hostProcPath()` 读主机网卡，按上报区间算收发 Mbps。
- CPU：缓存上次 `/proc/stat`，算区间值。busy = user+nice+system+irq+softirq+steal；steal 单独上报。
- 丢包：改为主机网卡的区间值。
- 新增 `cpuCores`。
- 新增 `metricsVersion: 1`。**新字段承载新含义，Center 只信 `metricsVersion >= 1` 的上报**，防止和老 sidecar 混算。

**Center**（`api/slave_api_report.go`、`api/model.go`）：
- `SlaveNodeLoad` 增加 `ipv4`、`metrics_version`、`steal_percent`、`cpu_percent`、`cpu_cores`。
- **注意**：普通重新注册是原地更新、NodeID 不变，但节点自注销（`api_slave_node_unregister`）会删节点行和 `slave_node_loads`，再注册时换新 NodeID。计算 M 必须按 `ipv4` 取历史，且自注销路径不再删负载行（`api_admin_node.go` 的管理员删除路径不变；过期由现有 30 天清理兜底）。

**发布**：`make publish-docker` 后 canary + 分批（`kaitu-node-ops`）。sidecar 无测试门，须自跑 `go test ./...`。

### 第二阶段：算 M

新增每日 worker，对每个 `ipv4` 取近 14 天 `metrics_version >= 1` 的样本：

```
拟合  busy% = b + k · Mbps
M = (80 − b) / k        # 80% CPU 视为容量上限
```

- **口径**：主机网卡 rx+tx 之和，M、压力、下列阈值全部用它。本文「现状」里的历史数字来自 `used_traffic_bytes`，各厂商计量方式不同（sum / max），差约 2 倍，仅供参考。
- **M 指持续能力，不是突发能力**：steal > 5% 的样本不进拟合，避免突发额度耗尽时的样本扭曲斜率；这类时刻由压力公式里的 CPU 项兜住。
- 样本够（Mbps ≥ 20 的样本 ≥ 30 个，且 k > 0）→ `source = fitted`。
- 合理范围：M 不低于该节点观测峰值，不超过峰值的 5 倍，超出时截断并记录，避免从几十 Mbps 外推出离谱的值。
- 否则 → 取同组（`provider + cpuCores`）fitted 节点的 M 中位数，`source = group`。
- 组内也没有 → `source = none`，无 M，压力只看 CPU 和内存项。
- 不用厂商标称值做上限。

M 写入 `NodeUsage`（已按 ipv4 持久）：`capacity_mbps`、`capacity_source`、`capacity_samples`、`capacity_at`。

### 校验门（进入第三阶段前必须过）

第一阶段数据攒满至少 7 天且含晚高峰后，**人工确认**：
1. busy% 与 Mbps 确实近似线性：合格样本的节点中，至少 60% 的 R² ≥ 0.6。今晚抽样两台时都没流量，此前提**尚未验证**。
2. 至少 3 台节点出现过 ≥ 100 Mbps 的自然高峰，且当时的 busy% 与拟合预测相差不超过 15 个百分点。没等到这样的高峰就延长观察，不放行。
3. ≥ 80% 的节点已升级到新 sidecar。

不成立就回到这里重新设计，不开第三阶段。

### 第三阶段：算压力并进推荐

每次状态上报时计算并写入 `NodeUsage.pressure_ratio`、`pressure_at`：

```
memTerm = clamp((mem% − 80) / 15, 0, 1)      # 内存常态 45–75%，只有逼近耗尽才计入，否则压力又会约等于内存
raw = max(Mbps / M, busy% / 80, memTerm)     # 取大：任一资源先到顶都算压力；无 M 时跳过第一项
pressure = EWMA(raw, α=0.3)，clamp [0, 1]
```

评分仍由 `ComputeRecommendScore` 唯一负责：`DataTunnelInstance` 增加 `PressureRatio`：

```
p = 按 5% 向下取整后的 pressure          # 0.02 与 0.03 同档，避免噪声改写 top-5
score = 0.9 · (1 − p) + 0.1 · quotaScore
quotaScore < 0.3 时：score = min(score, quotaScore)   # 配额护栏
下限 0.05
```

- **score 是主线，Lightsail 等流量上限是防御（David 定）**：不设起步门槛，压力主导排序；配额不参与竞争，只做同档排序、临近耗尽时的护栏，以及既有的隐藏与硬掐断。`/api/tunnels` 投影保序，top-5 自动选择直接落在压力最低的一批节点。
- 分档只为压住噪声，不是门槛：5% 以上的差距都会改变排序。
- 自动连接本身不分地区，用户可能被引到更远的节点；此风险已接受，国家排除设置仍然生效。
- 取舍：配额不再主导，月末「用量偏高但未近耗尽」的节点不再被明显避开，Lightsail 超额成本风险由护栏和硬掐断兜底。
- 下限 0.05：score 0 是黑名单语义，压力不能触发。
- `listRecommendScore` 在压力之后投影，满配额判定不变。
- 无数据或 `pressure_at` 过期（> 10 分钟）→ pressure 取**全车队中位数**，不取 0：取 0 等于给没测到的节点最高分，反而把新用户引向升级失败的节点。
- 专属节点不参与，沿用现状。
- viper 开关 `node_pressure.enabled`，默认关；关闭时分数与今天完全一致。第一、二阶段据此上线，校验门通过后再开。

### 前端自动选择器（David 定：一并优化）

`pickAutoTunnel` 的「分数前 5 名随机」会把新用户集中到少数节点，且不看线路，改为：

1. **筛候选**：沿用现有规则（score > 0、有 `serverUrl`、不在排除国家）；另外去掉新鲜探测判定为不可达的节点，全被去掉时忽略此条。
2. **线路门**：新鲜探测的节点数 ≥ 3 时，只保留 `avgRttMs ≤ max(最优值 × 1.5, 最优值 + 30)` 的节点；否则跳过此步。
3. **双随机**：从候选里随机抽 2 个不同节点，取分数高者，同分取先抽到的；只有 1 个候选时直接返回。

- 只用已缓存的探测结果，不在连接时新增探测。
- 不依赖压力数据，可先于压力开关上线；通过 `webapp/*` tag 发布。
- `Subscription.Pick`（Go）本次不改：无探测源时它本来就按权重分散，桌面 App 的自动连接走的也是 webapp。

## 留给实现者决定

- 拟合实现（最小二乘或分位数）、异常点处理。
- `provider` 取值来源：优先用云实例记录，非云节点从节点名（`us-la.bwh.wm03` 第二段）解析。
- `calculateServerLoad` 是否保留：输入修好后 `load` 数值会变，但无人用于推荐；若要动，先确认后台展示。
- 上报间隔：沿用现值，EWMA 的 α 可按实际间隔调整。
