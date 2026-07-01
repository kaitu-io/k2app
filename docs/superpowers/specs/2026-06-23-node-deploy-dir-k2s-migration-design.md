# 节点部署目录 `/apps/kaitu-slave` → `/apps/k2s` 迁移设计

**日期:** 2026-06-23
**状态:** 设计已批准,待写实现 plan

## Goal

把节点的宿主部署目录从 `/apps/kaitu-slave` 收敛为 `/apps/k2s`,并顺手斩断 sidecar 代码里残留的 `kaitu-slave` 命名债务,使部署目录、compose project、卷命名、代码内部命名全部对齐到 `k2s`。同时把"目录名 = compose project 身份"这一根因显式修掉,让未来再换目录不会破坏卷/容器。

## Architecture / 根因

docker compose 在没有 `name:` 字段时,**project 名默认取部署目录的 basename**:
- `/apps/kaitu-slave` → project `kaitu-slave` → 命名卷 `kaitu-slave_config` / `kaitu-slave_k2v5-data`。
- 直接把目录拷到 `/apps/k2s` 再 `up -d` → project 变 `k2s` → 新建一对**空卷** + 与现有 `k2s` / `k2-sidecar` 容器**撞名报错**。

`/etc/kaitu/`(= `config` 卷)里同时存着证书和计量状态(`traffic.state` / `cutoff.state`)。所以裸 `mv` 会破坏卷身份。本设计通过**全新重建 + 显式 `name: k2s`** 规避,并接受证书/计量自然重建(见下)。

## 关键决策

### 1. 迁移方式 = 全新重建(方案 C),非迁卷
- 当月用量**重新核算**(用户已确认),计量 `traffic.state` 重锚到 0 可接受。
- 证书是 Center 用 Kaitu CA **现签现发**的叶子证书(见 §证书),换全新空卷后节点重新注册即重发,**无需迁卷**。
- 因此重建 > 迁卷:更简单、终态更干净(project / 卷 / 目录 / 代码命名全 = `k2s`),代价(秒级 downtime + 计量重锚)正是已接受的两件事。

### 2. 显式 `name: k2s` —— 修根因
`docker/docker-compose.yml` 顶层加 `name: k2s`,把 compose project 身份**与目录解耦**。以后无论目录怎么改,project / 卷命名稳定,不再重演本次的撞名/空卷问题。

### 3. 证书 = 迁移零负担
managed fleet 的信任模型:**客户端内置 Kaitu CA**(`k2/wire/ca.go` `//go:embed kaitu_ca.pem`,always-fallback:chain 到 Kaitu CA 的证书永远被接受)。节点对外证书由 **Center 现签现发**(`api/logic_ca.go` `SignDomainCert` 用 CA 私钥 `ca_key` 签;`slave_api_node.go` 注册时 `GetDomainCert` 返回 `sslCert`/`sslKey`),sidecar 落到 `config` 卷的 `server-cert.pem`。
- 换全新空卷 → 节点重新注册 → Center 重发证书 → 客户端照样验证通过(chain 到内置 Kaitu CA)。
- 旧卷里的叶子证书(1 年期、可重发)**不迁、不保留**。

> **Follow-up(不进本次):** 叶子私钥目前由 Center 生成并随注册回传。可选升级到 CSR 模型(节点本地生成密钥对,只上送 CSR,Center 仅回签好的证书,私钥永不离开节点)。脚手架已有(`/csr/` 路由、`GenerateDomainCert` 造 CSR、`SignDomainCert` 签 pubKey)。安全增量有限(高价值资产是 Center 的 CA 私钥,已集中;叶子 per-node、1 年期、走到 Center 的 HTTPS),故独立小改,不作为本次迁移阻塞项。

### 4. 去债务:扫掉 sidecar 内部的 `kaitu-slave` 命名
不留"半截子改名"。`docker/sidecar/` 中与宿主目录无直接关系、但同属 `kaitu-slave` 命名的字样一并清理(self-deploy / 容器内部概念,改之安全):
- `sidecar/selfcert.go`:自签证书默认 CN `"kaitu-slave"` → `"k2s"`;fallback cert 目录 `/etc/kaitu-slave/certs` → `/etc/k2s/certs`;相关注释。
- `sidecar/selfcert_test.go`:CN 断言改 `"k2s"`。
- `config/config.go`:遗留候选配置名 `kaitu-slave.yml` / `kaitu-slave.yaml` → `k2s.yml` / `k2s.yaml`(`config.yml` 仍是主名,env 驱动的 sidecar 实际不挂文件 → 零影响)。

## 改动面(精确清单)

### A. 生产代码
| 文件 | 改动 |
|------|------|
| `docker/docker-compose.yml` | 顶层加 `name: k2s` |
| `docker/scripts/auto-update.sh` | `COMPOSE_DIR` + cron 注释 → `/apps/k2s` |
| `docker/scripts/k2s-crash-monitor.sh` | `COMPOSE_DIR` → `/apps/k2s` |
| `docker/scripts/setup-journald-crashmon.sh` | prereq 注释 + `SCRIPT` + systemd `ExecStart` → `/apps/k2s`(3 处) |
| `docker/scripts/simple-docker-pull-restart.sh` | `cd` 路径 → `/apps/k2s` |
| `docker/scripts/provision-node.sh` | crash-monitor unit `ExecStart` + warn 文案 + auto-update cron 注册 + 收尾 echo + 部署目录创建 → 全部 `/apps/k2s` |
| `docker/sidecar/sidecar/selfcert.go` | CN `kaitu-slave`→`k2s`;fallback `/etc/kaitu-slave/certs`→`/etc/k2s/certs`;注释 |
| `docker/sidecar/sidecar/selfcert_test.go` | CN 断言 → `k2s` |
| `docker/sidecar/config/config.go` | 候选名 `kaitu-slave.{yml,yaml}` → `k2s.{yml,yaml}` |

### B. Ops 文档(与代码保持一致)
- `.claude/skills/kaitu-node-ops/SKILL.md`
- `.claude/skills/kaitu-node-ops/references/metering.md`
- `.claude/skills/kaitu-node-ops/references/provisioning.md`
- `.claude/skills/kaitu-node-ops/deploy-compose.sh`
- `.claude/skills/kaitu-node-ops/deploy-auto-update.sh`
- `.claude/skills/kaitu-node-ops/update-compose.sh`
- `.claude/skills/kaitu-support/SKILL.md`(日志路径 `/apps/kaitu-slave/logs/k2s.log` → `/apps/k2s/logs/k2s.log`)

### C. 明确不动(避免误伤)
- `api/cmd/main.go`、`scripts/deploy-center.sh` 的 `/apps/kaitu` —— 那是 **Center 服务自己的目录**,与节点无关。
- 历史 spec(`2026-06-09-private-node-router-product-design.md`、`2026-06-11-private-node-agent-provisioning.md`)—— 保留为历史记录。
- `.claude/worktrees/` —— 隔离 worktree,从不 stage。

## 单节点 cutover 流程(运维执行)

```
# 1. 备好新目录(从旧 .env 搬 + 计量重新核算)
mkdir -p /apps/k2s
cp /apps/kaitu-slave/.env /apps/k2s/.env          # 后续按需改 BILLING_START_DATE / TRAFFIC_LIMIT_GB
推 docker-compose.yml + users + auto-update.sh + k2s-crash-monitor.sh 到 /apps/k2s

# 2. 释放旧 project(有意的一次性 down — 释放容器名 + 443 端口)
cd /apps/kaitu-slave && docker compose down

# 3. 全新起(name:k2s,空卷,干净)
cd /apps/k2s && docker compose up -d

# 4. 重指运维挂钩
crash-monitor systemd unit ExecStart → /apps/k2s/k2s-crash-monitor.sh(systemctl daemon-reload + restart)
auto-update cron → /apps/k2s/auto-update.sh

# 5. 验证(见下)→ 清理孤儿卷
docker volume rm kaitu-slave_config kaitu-slave_k2v5-data
```

- **计量**:新 `.env` 写 `K2_NODE_BILLING_START_DATE` / `K2_NODE_TRAFFIC_LIMIT_GB`;需要 mid-cycle 用量时 `set-usage`。
- **downtime** = 秒级(容器重建)。
- 这是改名迁移**唯一**合理的 `docker compose down`;日常更新仍是 `pull + up -d`。

## 验证(每节点)

1. `cd /apps/k2s && docker compose ps` —— `k2s` + `k2-sidecar` 全 Up,sidecar `(healthy)`,无孤儿。
2. `docker logs k2-sidecar | grep "Registration completed"` —— `tunnels=1`。
3. Center 重发证书已落:`/etc/kaitu/certs/server-cert.pem` 存在(卷内)。
4. `docker logs k2-sidecar | grep usage-reporter-cycle-ok` —— `cumulative` 单调爬升。
5. `list_nodes(name=<node>)` —— 节点可见。
6. **真机实连一次** —— 客户端连上(确认 Kaitu CA 链验证通过,证书自愈无碍)。
7. systemd crash-monitor unit `active`;`crontab -l` 指向 `/apps/k2s`。

## 测试策略

- **sidecar Go 改动**:TDD —— 先改 `selfcert_test.go` 断言到 `k2s`(red)→ 改 `selfcert.go`(green)→ `cd docker/sidecar && go test -race ./...` 全绿。
- **脚本 / compose**:本质是配置改动,无单元测试;正确性由 **au-1 canary 的端到端 cutover + §验证清单**保证。

## 发布顺序

1. **合代码改动(A + B)** —— 新节点直接 `/apps/k2s`,零迁移。
2. **Canary**:au-1(或某台无真实用户的节点)跑一遍 cutover 脚本,过 §验证清单。
3. **Fleet 批量**:canary 通过后逐台切;每台过 §验证。

## 风险

| 风险 | 缓解 |
|------|------|
| cutover 期间秒级 downtime | 已接受;逐台切,非全量同时 |
| 计量重锚丢当月用量 | 已决定重新核算;新 `.env` 显式 seed |
| 漏改某处 `/apps/kaitu-slave` 引用 | §改动面清单源自全仓 grep;canary 端到端兜底 |
| `name: k2s` 与存量 project 名不一致致 canary 撞名 | cutover 先 `down` 旧 project 再 `up` 新,容器名/端口已释放 |
| 孤儿卷未清残留磁盘 | cutover 第 5 步显式 `volume rm`,验证后执行 |
