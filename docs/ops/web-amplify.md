# Web (Next.js) — Amplify Hosting + Media Infra

> 2026-09-02 自 `web/CLAUDE.md` 迁出的 AWS 资源清单，资源 ID 来自当时的手工记录，**未经代码验证**；改动前先在 AWS 控制台核对。
> **2026-09-04 起 Payload CMS 已删除**：网站不再使用 PostgreSQL / S3 media / AI 翻译，内容回归 Velite markdown。下方 Media infra 段保留为退役资源清单，等待清理。

## Amplify apps

- 两个 Amplify app（kaitu.io / overleap.io）都构建 `website` 分支 —— 它是 `main` 的纯镜像，**永远不要直接 commit 到它**。部署 = `git push origin main:website`。
- 同一份 `web/amplify.yml`（`appRoot: web`），两个 app 只差 `NEXT_PUBLIC_BRAND`（kaitu 默认 / overleap 必须显式设置）。
- 控制台环境变量只存在于构建 shell；`output: 'standalone'` 不会把它们带进 SSR Lambda，所以 `amplify.yml` preBuild 用 `env | grep -E '^(…)='` 把白名单追加进 `.env.production`。**白名单是权威**：不在里面的变量到不了 Lambda。当前白名单：`NEXT_PUBLIC_BRAND`、`NEXT_PUBLIC_SENTRY_DSN`、`SENTRY_ORG`、`SENTRY_PROJECT`、`SENTRY_AUTH_TOKEN`（Payload 时代的 DATABASE_URL / PAYLOAD_SECRET / TRANSLATOR_* / S3_* / CDN_URL / CENTER_API_URL 已随 CMS 删除，控制台里的同名变量可清理）。
- SSR Lambda 上限 30s。

## Media infra（已退役 —— 原 Payload `media` collection，2026-09-04 起图片入 repo `web/public/images/content/`）

- **S3** bucket `kaitu-cms-media`（ap-northeast-1）—— 私有，block-public-access ON，bucket policy 只允许 CloudFront 经 OAC 读。
- **CloudFront** distribution `EDW1KA2NDICCJ` —— alias `media.kaitu.io`，ACM 证书在 us-east-1，Managed-CachingOptimized cache policy，CORS-S3Origin origin-request policy，`CORS-With-Preflight` response-headers policy。
- **IAM** user `payload-cms-media-uploader` —— 仅 `s3:PutObject / DeleteObject / GetObject / ListBucket` 于该 bucket ARN；access key 只存于 Amplify 环境变量。
- **Route 53** A + AAAA alias：`media.kaitu.io.` → `d2cb1b6o656sch.cloudfront.net.`
- 代码侧引用已全部删除；bucket 里仅存 2 张图（已复制进 repo）。S3 bucket / CloudFront / IAM user / Route 53 记录可整套清理（也可留作他用）。**同域待关停的还有 Payload 的 PostgreSQL 实例**（原 `DATABASE_URL` 指向，只在 Amplify 控制台可见 —— 关停前建议 pg_dump 留档）。
