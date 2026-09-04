# Web (Next.js) — Amplify Hosting + Media Infra

> 2026-09-02 自 `web/CLAUDE.md` 迁出的 AWS 资源清单，资源 ID 来自当时的手工记录，**未经代码验证**；改动前先在 AWS 控制台核对。
> **2026-09-04 起 Payload CMS 已删除**：网站不再使用 PostgreSQL / S3 media / AI 翻译，内容回归 Velite markdown。**同日已完成访问撤销**（见下）；PostgreSQL 是共享 RDS 实例，**不关停**。

## Amplify apps

- **实际部署形态（2026-09-04 AWS 控制台核实）**：kaitu.io 与 overleap.io 两个域名都关联在**同一个** Amplify app（`d3q8wll74rs94h`，ap-northeast-1）的同一个 `website` branch 上 —— 与「两个 app 各带 NEXT_PUBLIC_BRAND」的设计描述不符（该 app 未设 NEXT_PUBLIC_BRAND，构建默认 kaitu；overleap.io 当前服务的是同一份 kaitu 构建）。`website` 分支是 `main` 的纯镜像，**永远不要直接 commit 到它**。部署 = `git push origin main:website`。（ap-east-1 另有一个闲置 `kaitu` app `d2ooo048yr0i1y`，无自定义域名。）
- 同一份 `web/amplify.yml`（`appRoot: web`），两个 app 只差 `NEXT_PUBLIC_BRAND`（kaitu 默认 / overleap 必须显式设置）。
- 控制台环境变量只存在于构建 shell；`output: 'standalone'` 不会把它们带进 SSR Lambda，所以 `amplify.yml` preBuild 用 `env | grep -E '^(…)='` 把白名单追加进 `.env.production`。**白名单是权威**：不在里面的变量到不了 Lambda。当前白名单：`NEXT_PUBLIC_BRAND`、`NEXT_PUBLIC_SENTRY_DSN`、`SENTRY_ORG`、`SENTRY_PROJECT`、`SENTRY_AUTH_TOKEN`。Payload 时代的 DATABASE_URL / PAYLOAD_SECRET / TRANSLATOR_* / AI_* / S3_* / CDN_URL / CENTER_API_URL **已于 2026-09-04 从 Amplify 控制台删除**。
- SSR Lambda 上限 30s。

## 访问撤销记录（2026-09-04）

- Amplify app `d3q8wll74rs94h` 的 payload 相关 env 已全部删除（仅剩 AMPLIFY_* + SENTRY_*）。
- RDS `tokyo-postgres.cj80cay4wi2c.ap-northeast-1.rds.amazonaws.com` 的 `kaitu_web` 用户密码已改为随机值并丢弃（旧凭证散布在历史构建产物中，已验证失效）。**实例是共享的，不关停**；需要重新访问时用 RDS master 重置该用户密码。数据留档：`~/projects/kaitu-io/backups/kaitu_web-payload-final-20260904.sql.gz`。
- IAM user `payload-cms-s3`（注意：不是本档早前记的 `payload-cms-media-uploader`）的 access key `AKIASWWJ4TKX7QMKMGLC` 已置 Inactive；Amplify env 里暴露的旧 key `AKIASWWJ4TKXWD7LJUFZ` 经实测早已删除失效。
- 未在服务商侧轮换（env 中曾暴露、按需处理）：OpenRouter `TRANSLATOR_API_KEY`、DeepSeek `AI_API_KEY`、Sentry `SENTRY_AUTH_TOKEN`（仍在用，保留）。

## Media infra（已退役 —— 原 Payload `media` collection，2026-09-04 起图片入 repo `web/public/images/content/`）

- **S3** bucket `kaitu-cms-media`（ap-northeast-1）—— 私有，block-public-access ON，bucket policy 只允许 CloudFront 经 OAC 读。
- **CloudFront** distribution `EDW1KA2NDICCJ` —— alias `media.kaitu.io`，ACM 证书在 us-east-1，Managed-CachingOptimized cache policy，CORS-S3Origin origin-request policy，`CORS-With-Preflight` response-headers policy。
- **IAM** user `payload-cms-s3`（早前记录的 `payload-cms-media-uploader` 有误）—— access key 已停用（见上方撤销记录）。
- **Route 53** A + AAAA alias：`media.kaitu.io.` → `d2cb1b6o656sch.cloudfront.net.`
- 代码侧引用已全部删除；bucket 里仅存 2 张图（已复制进 repo）。S3 bucket / CloudFront / IAM user / Route 53 记录可整套清理（也可留作他用）。PostgreSQL 侧撤权已完成（见上方撤销记录），实例共享不关停。
