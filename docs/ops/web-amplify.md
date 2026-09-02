# Web (Next.js) — Amplify Hosting + Media Infra

> 2026-09-02 自 `web/CLAUDE.md` 迁出的 AWS 资源清单。资源 ID 来自当时的手工记录，**未经代码验证**（代码只知道 bucket 名与 CDN 域名：`web/src/payload/payload.config.ts`、`web/.env.example`）；改动前先在 AWS 控制台核对。

## Amplify apps

- 两个 Amplify app（kaitu.io / overleap.io）都构建 `website` 分支 —— 它是 `main` 的纯镜像，**永远不要直接 commit 到它**。部署 = `git push origin main:website`。
- 同一份 `web/amplify.yml`（`appRoot: web`），两个 app 只差 `NEXT_PUBLIC_BRAND`（kaitu 默认 / overleap 必须显式设置）。
- 控制台环境变量只存在于构建 shell；`output: 'standalone'` 不会把它们带进 SSR Lambda，所以 `amplify.yml` preBuild 用 `env | grep -E '^(…)='` 把白名单追加进 `.env.production`。**白名单是权威**：不在里面的变量到不了 Lambda。当前白名单：`NEXT_PUBLIC_BRAND`、`DATABASE_URL`、`PAYLOAD_SECRET`、`CENTER_API_URL`、`CDN_URL`、`S3_BUCKET`、`S3_REGION`、`S3_ACCESS_KEY_ID`、`S3_SECRET_ACCESS_KEY`、`TRANSLATOR_API_KEY`、`TRANSLATOR_BASE_URL`、`TRANSLATOR_MODEL`、`NEXT_PUBLIC_SENTRY_DSN`、`SENTRY_ORG`、`SENTRY_PROJECT`、`SENTRY_AUTH_TOKEN`。
- SSR Lambda 上限 30s —— 这是 Payload 翻译改成"首读惰性填充"的原因（见 `web/CLAUDE.md` CMS 段）。
- Payload schema 迁移**不在** `amplify.yml` 里自动跑（`@payload-enchants/translator` 的 ESM dir-import bug 会让 payload CLI 在 Node 22 上崩；Next 打包器能容忍）。合并 schema 改动后手动：`cd web && PAYLOAD_SECRET=<prod> DATABASE_URL=<prod> yarn payload migrate`。

## Media infra（Payload `media` collection）

- **S3** bucket `kaitu-cms-media`（ap-northeast-1）—— 私有，block-public-access ON，bucket policy 只允许 CloudFront 经 OAC 读。
- **CloudFront** distribution `EDW1KA2NDICCJ` —— alias `media.kaitu.io`，ACM 证书在 us-east-1，Managed-CachingOptimized cache policy，CORS-S3Origin origin-request policy，`CORS-With-Preflight` response-headers policy。
- **IAM** user `payload-cms-media-uploader` —— 仅 `s3:PutObject / DeleteObject / GetObject / ListBucket` 于该 bucket ARN；access key 只存于 Amplify 环境变量。
- **Route 53** A + AAAA alias：`media.kaitu.io.` → `d2cb1b6o656sch.cloudfront.net.`
- 代码侧：`payload.config.ts` 的 `s3Storage` 用 `S3_BUCKET`（默认 `kaitu-cms-media`）+ `generateFileURL` 拼 `CDN_URL`（默认 `https://media.kaitu.io`）；REST 读 media 仅限 admin，公开文章页通过嵌在内容里的 CDN URL 取图。
