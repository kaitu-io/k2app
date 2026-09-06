# Overleap — Center API 部署清单（代码已在 main，按此顺序上线）

前提：本清单对应 spec `docs/superpowers/specs/2026-09-06-overleap-app-launch-design.md` §3 / §6，实现计划 `docs/superpowers/plans/2026-09-06-overleap-launch-api.md`。两台 center 共享 `/data/app-configs/kaitu/config.yml`（写一次，各自重启）。SSH 与重启走 `center-deploy` skill（数据库只读）。

## 1. 配置（改前先 `cp config.yml config.yml.bak.$(date +%s)`）

```yaml
appstore:
  bundleIds:
    overleap: "io.overleap"        # 缺失 = 每笔 Overleap IAP 校验拒绝（logic_apple_iap.go appleBundleIDForBrand，fail-loud）
mail_overleap:                      # SES 域 overleap.io 验证通过后再填；留空 = 回落全局 mail.* 发件人并一次性 warn（logic_email.go systemSenderForBrand）
  provider: "ses"
  send_from: "Overleap <support@overleap.io>"
  region: "ap-northeast-1"
```

- `mail_overleap` 与 `edm_overleap` 是两个前缀：前者管系统邮件（验证码 / 登录提醒 / 密码 / 设备踢出 / 工单回复），后者管 EDM。`edm_overleap` 另有 `edm.overleap_from_email` 门，两者要一起填（见 `api/CLAUDE.md` Brand 段）。
- 判据是 `send_from` 非空：填了 `provider` 没填 `send_from` 等于没填。

## 2. 部署二进制

含本清单对应提交的二进制 → `systemctl restart kaitu-center` 逐台；`curl -s localhost:5800/version` 健康后再下一台。启动时 `center.Migrate()` 自动加 `feedback_tickets.brand` 列（默认 `kaitu`，存量行零影响）。

## 3. 验证（逐条打钩）

- [ ] `curl -s -H 'X-K2-Brand: overleap' https://k2.52j.me/api/plans | jq '.data.items[].pid'` → `overleap-basic-1y`, `overleap-basic-1m`
- [ ] 后台 `GET /app/nodes?brand=overleap` 非空（§6 节点 `K2_NODE_BRANDS` 声明完成后；节点行带 `brands` / `visibleKaitu` / `visibleOverleap`）
- [ ] `GET /api/antiblock/seed` 不含任何 `visibleKaitu=false` 或未声明 kaitu 的节点 IP（seed 是 kaitu 反封锁通道）
- [ ] 沙盒 IAP：Overleap TestFlight 构建购买一次 → `subscriptions` 表出现 `provider=apple` 行；订单表**不**出现行（沙盒交易只授权益不建 Order，见 `api/CLAUDE.md` Apple IAP 段）
- [ ] `mail_overleap` 填好后：注册一个 overleap 账号，验证码邮件 From 为 `support@overleap.io`；同时注册一个 kaitu 账号确认 From 未变
- [ ] 用 overleap 账号提一张工单并后台回复一次 → 用户收到 `[Overleap] New reply on your ticket (#N)`（英文）；kaitu 账号同操作仍是 `[Kaitu] 您的工单有新回复 (#N)`
- [ ] 两台 `app.log` 各 grep 一次 `mail_overleap.send_from not configured`：配置到位后不应再出现

## 4. 回滚

恢复 `config.yml.bak.*` + 上一版二进制，重启。`feedback_tickets.brand` 列留着无害（老二进制不读它）。
