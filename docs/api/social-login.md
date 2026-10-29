# Social Sign-In (Google / Apple) — 细节

> 内容移自 `api/CLAUDE.md`（2026-10 精简）。改联合登录端点、身份匹配、`social_auth` 配置、Apple refresh token 撤销或部署社交登录前读。

- **Spec**: `docs/superpowers/specs/2026-10-20-social-sign-in-design.md`。文件：`api_social*.go` / `logic_social_*.go`（config / apple）/ `logic_login_complete.go`（验证码登录与社交登录共用的登录事务，不含密码登录）。
- **端点**（`route.go`；HTTP 永远 200，错误看 code）:

| Method Path | Auth | 用途 |
|---|---|---|
| `POST /api/auth/social/login` | 无（`socialSensitive` 组） | 原生 App 提交 idToken + rawNonce，登录/建号 |
| `POST /api/auth/social/web-login` | 无（`socialSensitive` 组） | 官网浏览器登录 |
| `GET /api/user/social` | `AuthRequired` + `EnforceDeviceClass` | 已绑定的提供方列表 |
| `POST /api/user/social/bind` | `AuthRequired` + `EnforceDeviceClass`（`socialSensitive` 组） | 绑定（仅原生 token / 官网账号页） |
| `DELETE /api/user/social/:provider` | `AuthRequired` + `EnforceDeviceClass` | 解绑（Apple 同时撤销 refresh token） |

- **nonce**：客户端生成 `rawNonce`，给提供方 `sha256hex(rawNonce)`、给我方 `rawNonce`；服务端要求 `token.nonce == sha256hex(rawNonce)`。
- **aud**：token 必须**恰好一个** `aud` 且在 `BrandConfig.Social.Audiences(provider)`（`brand.go` `SocialClientIDs`）内——品牌隔离的第二道锁（第一道是 `users.brand`）。client ID 是公开值、进跨层契约（`brands.<id>.social`），改动后按 `api/CLAUDE.md` Brand 段的契约门重生成 `contracts/api-contract.json`。所有身份查找限定 `ReqBrand(c)`。
- **匹配顺序**：provider `sub`（`LoginIdentify`，Apple 键 `hash("apple:"+sub)`）→ 已验证邮箱命中同品牌用户 → 建号。**任何不可信 / 缺失的邮箱（Google 未验证、Apple 无邮箱）永不按邮箱匹配、不建号**（400015）。身份键 = `hash(provider+":"+sub)`（Google / Apple 同形）。绑定冲突：该第三方账号已绑他人 409002；本账号已绑同类另一个 409003；**仅当本账号尚无 email 身份（绑定会写 email 行）时**，可信邮箱已属本品牌别的用户 → 409001（不合并账号）；已有 email 身份则不查邮箱。
- **桌面 / Linux / Android-Apple**：App 经系统浏览器（127.0.0.1 回环或官网 Apple 回调页转交）自取 idToken，再调 `/social/login`；绑定走官网账号页（原生 token 才走 `/user/social/bind`）。
- **Config**（viper；缺省 = 功能关；`logic_social_config.go`）:

```yaml
social_auth:
  apple_team_id: NJT954Q3RH            # 两品牌同一开发者账号，可省略
  kaitu:
    google: { enabled: true }          # Google 无秘密，靠显式开关上线
    apple:
      key_id: "XXXXXXXXXX"
      private_key: |                   # 内联 .p8 PEM 字符串
        -----BEGIN PRIVATE KEY-----
        <placeholder>
        -----END PRIVATE KEY-----
  overleap: { google: { enabled: true }, apple: { key_id: "...", private_key: "..." } }
```

  Google Ready = `enabled`；Apple Ready = `key_id` 与 `private_key` 均非空。未就绪 = 提供方关：`/api/app/config` 的 `socialSignIn` 对应 flag 为 false，login / bind 直调端点均回 405002，不 panic。私钥来源 `~/.config/kaitu-secrets/`，**绝不进仓库**；社交代码自身不记 idToken / refresh token，邮箱用 `hideEmail`。`POST /api/auth/social/login`、`/social/web-login`、`/api/user/social/bind` 挂在 `socialSensitive` 组（`requestLogWithoutBodies`，请求体 idToken+rawNonce 可重放登录、响应体含 access/refresh token），`TestSocialTokenRoutes_NotBodyLogged` 守卫。
- **Apple refresh token**：`LoginIdentify.ProviderClientID` + `ProviderRefreshToken`（经 `secretEncryptString`，该函数目前直通 = 库中明文；无 .p8 私钥不可用；默认空串）。撤销点：① 解绑（`api_social.go`）② 用户自助注销（`api_user.go`）③ 管理员硬删（`logic_approval_callbacks.go`）。**新增任何删用户入口必须在删 `LoginIdentify` 行之前调 `loadAppleRevocations`，事务提交后调 `revokeAppleTokensAsync`**（审核要求；漏了不报错，Apple 侧授权永远残留）。
- **网络调用**（JWKS、Apple token / revoke）全经包级 `var` 函数，测试替换，测试不打外网。
- **部署核对**：两台 center 都写好 `social_auth` → `make deploy-api` → `curl -s https://www.kaitu.io/api/app/config` 与 overleap 域各一次，确认 `socialSignIn` 对应 flag 为 `true`。
