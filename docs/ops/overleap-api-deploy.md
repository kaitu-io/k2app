# Overleap — Center API 部署清单（代码已在 main，按此顺序上线）

前提：本清单对应 spec `docs/superpowers/specs/2026-09-06-overleap-app-launch-design.md` §3 / §6，实现计划 `docs/superpowers/plans/2026-09-06-overleap-launch-api.md`。两台 center 共享 `/data/app-configs/kaitu/config.yml`（写一次，各自重启）。SSH 与重启走 `center-deploy` skill（数据库只读）。

## 1. 配置（改前先 `cp config.yml config.yml.bak.$(date +%s)`）

```yaml
appstore:
  bundleIds:
    overleap: "io.overleap"        # 缺失 = 每笔 Overleap IAP 校验拒绝（logic_apple_iap.go appleBundleIDForBrand，fail-loud）
mail_overleap:                      # 留空 = 回落全局 mail.* 发件人并一次性 warn（logic_email.go systemSenderForBrand）
  provider: "smtp"
  send_from: "Overleap <noreply@em.overleap.io>"
  smtp_host: "smtpdm-ap-southeast-1.aliyun.com"
  smtp_port: 465
  username: "noreply@em.overleap.io"
  password: "<阿里云 DirectMail 控制台为 em.overleap.io 设置的 SMTP 密码>"
edm:
  overleap_from_email: "news@em.overleap.io"   # 这只是「edm_overleap 已就绪」的门，不是发件地址本身
edm_overleap:
  provider: "smtp"
  send_from: "Overleap <news@em.overleap.io>"
  smtp_host: "smtpdm-ap-southeast-1.aliyun.com"
  smtp_port: 465
  username: "news@em.overleap.io"
  password: "<同上>"
```

- **发件走阿里云 DirectMail，不是 SES。** 整个 AWS 账号 186180737711 的 SES 在所有区域都还在沙盒，且生产权限申请已被拒（case 177513165600562）；开途生产环境本来就走 `smtpdm-ap-southeast-1.aliyun.com`。`qtoolkit/mail` 的 `provider` 缺省即 `smtp`，两个前缀都支持。
- **发信域 `em.overleap.io` 已就绪**（2026-09-08 建好并验证：DirectMail DomainId 227691，MX/SPF/DKIM/DMARC/CNAME 五条记录已进 Route 53 zone `Z0660232TESUJOGFHJV8` 并 INSYNC，`CheckDomain` 返回 `DomainStatus: 0`）。发信地址 `noreply@em.overleap.io`（trigger）与 `news@em.overleap.io`（batch）已创建，reply-to 都是 `support@overleap.io`。**只差 SMTP 密码**——在 DirectMail 控制台「发信地址 → 设置 SMTP 密码」设置后填进上面两个 `password`。
- 用 `em.` 而不是 `mail.`：`mail.overleap.io` 已被 SES 的 MAIL FROM 占用（MX → `feedback-smtp.ap-northeast-1.amazonses.com` + SPF TXT），没去动它。
- `mail_overleap` 与 `edm_overleap` 是两个前缀：前者管系统邮件（验证码 / 登录提醒 / 密码 / 设备踢出 / 工单回复），后者管 EDM。`edm_overleap` 另有 `edm.overleap_from_email` 门，两者要一起填（见 `api/CLAUDE.md` Brand 段）。
- 判据是 `send_from` 非空：填了 `provider` 没填 `send_from` 等于没填。

## 2. 部署二进制

含本清单对应提交的二进制 → `systemctl restart kaitu-center` 逐台；`curl -s localhost:5800/version` 健康后再下一台。启动时 `center.Migrate()` 自动加 `feedback_tickets.brand` 列（默认 `kaitu`，存量行零影响）。

## 2.5 部署前先确认线上到底是哪一版（**品牌功能不是一波上线的**）

品牌能力分三批进 main，线上很可能已经带了前两批：

| 能力 | commit | 日期 |
|---|---|---|
| 403003 品牌隔离中间件 + 按品牌注册（`users.brand` / `login_identifies.brand`） | `77c810d3` | 2026-07-14 |
| `/api/plans` 等 `ScopeBrand` 过滤 | `adbcb0a8` | 2026-07-14 |
| Apple IAP 按品牌取 bundleId（`appleBundleIDForBrand`） | `c0e61b3d` | 2026-07-17 |
| 节点自声明品牌（`SlaveNode.Brands` / `DeclaredBrands`） | `7ccde1a2` | 2026-08-15 |
| admin 节点列表暴露 `brands`/`visibleKaitu`/`visibleOverleap` + `?brand=` | `dd3b67b1` | 2026-09-06 |
| `mail_overleap` 系统发件人（`systemSenderForBrand`） | `9a192b3e` | 2026-09-06 |

**2026-09-09 实测线上（k2.52j.me）= ≥08-15 且 <09-06 的构建**，判别式如下（都不需要改任何东西）：

```
# 已上线：品牌隔离
POST /api/auth/login/password  X-K2-Brand: overleap  → code 0
GET  /api/user/info            X-K2-Brand: kaitu (同 token) → 403003
POST /api/auth/login/password  X-K2-Brand: kaitu     → 400006（查无此人）
# 已上线：按品牌发套餐
GET /api/plans  X-K2-Brand: overleap → overleap-basic-1y / overleap-basic-1m
GET /api/plans  X-K2-Brand: kaitu    → 1y / 2y / 3y / 5y
# 已上线：节点自声明品牌
GET /api/v20260717/tunnels  X-K2-Brand: overleap → 恰好那 15 台 .env 里写了 K2_NODE_BRANDS=kaitu,overleap 的节点
# 未上线（09-06 那批）：admin 节点行缺 brands / visibleKaitu / visibleOverleap 三个字段
GET /app/nodes  X-Access-Key: <key> → 行里只有 id/name/country/region/ipv4/ipv6/ipType/class/updatedAt/tunnels
```

> **别用「admin 少了 brands 字段」推断「品牌功能全没上线」**——2026-09-08 我就是这么误判的，还据此写过一段"迁移会把 Apple 演示账号判给 kaitu、漏了会被拒审"的假警报。真相是：按品牌注册从 7 月就在线上，**审核演示账号是按 overleap 出生的**，不需要任何 SQL 修补。`users.brand` 的 `default:'kaitu'` 只影响 **7 月之前** 就存在的老账号。
>
> 判别一个能力在不在线上，要**直接探它自己的行为**，不要拿同一批代码里另一个字段的存在与否去推。

## 3. 验证（逐条打钩）

- [x] `curl -s -H 'X-K2-Brand: overleap' https://k2.52j.me/api/plans | jq '.data.items[].pid'` → `overleap-basic-1y`, `overleap-basic-1m` —— **2026-09-09 已通过**（这批是 7 月就上线的，不依赖本次部署）
- [ ] 后台 `GET /app/nodes?brand=overleap` 非空且行里带 `brands` / `visibleKaitu` / `visibleOverleap` —— **这三个字段就是本次部署的判别式**（部署前它们不存在）。
  节点侧的声明本身早已生效：`GET /api/v20260717/tunnels` 带 `X-K2-Brand: overleap` 2026-09-09 实测返回 15 台（= 15 台节点 `.env` 里写了 `K2_NODE_BRANDS=kaitu,overleap`；其余 12 台是 11 台 SNI 伪装 `www.<省>.people.cn` 的按设计排除 + 1 台无 tunnel 的 `hk.aliyun.wm01`）
- [ ] `GET /api/antiblock/seed` 不含任何 `visibleKaitu=false` 或未声明 kaitu 的节点 IP（seed 是 kaitu 反封锁通道）
- [ ] 沙盒 IAP：Overleap TestFlight 构建购买一次 → `subscriptions` 表出现 `provider=apple` 行；订单表**不**出现行（沙盒交易只授权益不建 Order，见 `api/CLAUDE.md` Apple IAP 段）
- [ ] `mail_overleap` 填好后：注册一个 overleap 账号，验证码邮件 From 为 `Overleap <noreply@em.overleap.io>`、Reply-To 为 `support@overleap.io`；同时注册一个 kaitu 账号确认 From 仍是 `joshua@mail.allnationconnect.com`
- [ ] 用 overleap 账号提一张工单并后台回复一次 → 用户收到 `[Overleap] New reply on your ticket (#N)`（英文）；kaitu 账号同操作仍是 `[Kaitu] 您的工单有新回复 (#N)`
- [ ] 两台 `app.log` 各 grep 一次 `mail_overleap.send_from not configured`：配置到位后不应再出现

## 4. 回滚

恢复 `config.yml.bak.*` + 上一版二进制，重启。`feedback_tickets.brand` 列留着无害（老二进制不读它）。
