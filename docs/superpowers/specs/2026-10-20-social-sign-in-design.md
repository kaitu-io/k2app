# Google / Apple 联合登录 — 设计

日期：2026-10-20 · 状态：已获 David 同意（2026-10-20）

## 1. 为什么做

用户收不到邮箱验证码（被拦进垃圾箱、企业邮箱过滤、国内邮箱延迟）时，当前**没有任何替代登录方式**，只能提工单。Google / Apple 登录是给这部分人的**兜底通道**，不是营销卖点。

成功标准：收不到验证码的用户能自己登进**原来那个**账号（会员还在），不需要找客服。

## 2. 目标 / 非目标 / 反目标

**目标**
- 开途、Overleap 两个品牌都加，全平台一次完成：iOS、Android、macOS、Windows、Linux、官网（kaitu.io / overleap.io）。
- 第三方登录按邮箱匹配到原账号（David 决定）。
- 已登录用户可在账号页**绑定 / 解绑** Apple、Google；绑定后即使 Apple 用了"隐藏邮箱"也能登回原账号。
- 第三方邮箱在库里不存在 → 直接新建账号（与验证码首次登录即注册一致）。

**非目标**
- 账号合并工具（Apple 隐藏邮箱新建出来的空账号由客服处理，David 接受"对方工单就工单"）。
- 微信 / 其他第三方登录。
- Apple 服务端到服务端通知（用户在 Apple 侧撤销授权、关闭转发邮箱）。
- 存用户姓名 / 头像。

**反目标（做到了也算失败）**
- 跨品牌串号：开途 App 的 Google 登录绝不能进到 Overleap 账号，反之亦然。
- 同一邮箱因为换了登录方式变成两个账号（隐藏邮箱这种拿不到真实邮箱的情况除外）。
- 让验证码登录变得更难或改变现有行为。
- 在连不上 Google 的环境（国内各端）里显示一个点了必失败的按钮——见 §6.1.1 探测。

## 3. 现状（2026-10-20 摸底）

- 各层均无任何 OAuth / 第三方登录代码。
- 账号模型：`User` 表没有 email 列；邮箱只以加密形式存在 `LoginIdentify`（`api/model.go:145`），`Type` 目前只有 `"email"`，唯一索引 `(type, index_id, brand)`。**这张表就是通用身份表，第三方身份直接放这里，不新建表。**
- 查找 / 创建：`findUserByEmailInBrand` / `FindOrCreateUserByEmail(ctx, brand, email, …)`（`api/logic_user.go:199/219`），全部按品牌隔离。
- 设备绑定登录事务在 `api_login`（`api/api_auth.go:254-399`）：设备转移邮件 → 封禁检查 → 激活 → 设备上限 → `generateTokens` → 建 `Device` → 新设备邮件。
- 网页 cookie 登录：`api_web_auth` + `generateWebCookieToken` + `setAuthCookies`。
- 已有自助注销：`DELETE /api/user/delete-account`（`api/route.go:185`）→ 触发 Apple 的撤销令牌要求（见 §5.6）。
- 桌面端（Tauri）没有 deep link 插件；Linux 是 `k2` 二进制在 `127.0.0.1:1777` 上跑的网页，没有原生壳。
- 移动端无社交登录插件；iOS entitlements 没有 Sign in with Apple；Android 无 google-services 配置。
- 品牌标识：iOS 开途 `com.allnationconnect.anc.wgios`、Overleap `io.overleap`；Android `io.kaitu` / `io.overleap`；桌面 `io.kaitu.desktop` / `io.overleap.desktop`；Apple Team `NJT954Q3RH`。

## 4. 账号匹配规则（核心）

所有查找都限定在**请求品牌**内（`ReqBrand(c)`），跨品牌天然隔离——开途里用某 Gmail 登录，若该邮箱只在 Overleap 有账号，会在开途**新建**一个账号，与今天验证码登录的行为一致。

第三方登录成功、拿到经过校验的 `(provider, sub, email)` 后：

1. 查 `LoginIdentify{Type: provider, IndexID: hash(sub), Brand}` → 命中即该用户。
2. 未命中且 email 可信 → 查 `LoginIdentify{Type: "email", IndexID: hash(email), Brand}` → 命中即该用户，并**补写**第三方身份行。
3. 都未命中 → `FindOrCreateUserByEmail(brand, email)` 新建用户（同时生成 email 身份行），再写第三方身份行。

"email 可信"：
- Google：`email_verified == true` 才用；否则视为无邮箱 → 只能走第 1 步，未命中返回错误提示用户改用验证码。
- Apple：Apple 返回的邮箱（真实或 `@privaterelay.appleid.com`）一律视为已验证。隐藏邮箱自然匹配不到老账号 → 新建，邮箱就是 relay 地址。该用户以后用验证码登录 relay 地址也能收到（Apple 转发）。

`hash` 用现有 `secretHashIt`；第三方身份行的 `EncryptedValue` 存加密后的 email（便于客服排查），没有 email 时存空。

**约束**：一个用户每种 provider 最多绑一个；一个 `(provider, sub, brand)` 只属于一个用户（现有唯一索引保证）。

登录成功后，与验证码登录**完全相同**的后续处理（封禁、激活、设备上限、设备转移/新设备邮件、invite code 不适用、漏斗 sid 关联、网页登录邮件）。实现上把 `api_login` / `api_web_auth` 验证码校验之后的部分抽成共享函数，验证码登录与第三方登录共用，**不复制**。

## 5. 服务端（Center API）

### 5.1 令牌校验
- 客户端只把 **ID token（JWT）** 交给服务端；服务端用 Google（`https://www.googleapis.com/oauth2/v3/certs`）/ Apple（`https://appleid.apple.com/auth/keys`）的 JWKS 验签，缓存公钥。
- 校验：`iss`、`aud ∈ 该品牌允许的 client ID 列表`、`exp`、`nonce`（客户端发起时生成随机 nonce，原值随请求一起送来，交给 Google/Apple 的是 `sha256(nonce)`，服务端比对 token 里的 nonce == `sha256(原值)`）。
- `aud` 列表按品牌配置：每品牌有多个 client ID（Google：Web / iOS / Android 各一个；Apple：iOS bundle ID + Web 用的 Services ID）。**`aud` 不在本品牌列表 = 拒绝**，这也是品牌隔离的第二道锁。
- 库：builder 自选（`coreos/go-oidc/v3` 或 `golang-jwt` + JWKS 缓存），不手搓 RSA。

### 5.2 配置
**公开数据与秘密分开放**（我的判断）：
- client ID / Services ID 这类公开标识进 Go `brandRegistry`（`api/brand.go`），由契约门导出到 `contracts/api-contract.json`，webapp 品牌配置与两个官网的 client ID 由契约测试锁定一致——避免客户端烘焙的 ID 与服务端 `aud` 列表漂移。
- 秘密（Apple .p8 key、key_id）留在 `config.yml`。
- **开关以服务端为准**：现有 App 配置接口（`api/api_app_config.go`）下发 `socialSignIn: {apple: bool, google: bool}`（= 该品牌该 provider 配置 `Ready()`）。客户端按钮可见性 = 服务端开关 × 平台能力。这样 API 先上线、秘密未配好期间，新客户端也不会显示点了必失败的按钮。

秘密配置仿照 `appleBundleIDForBrand`（`api/logic_apple_iap.go:28`）的模式，`configSocialAuth(ctx, brand)` 返回带 `Ready()` 的结构；**某品牌某 provider 未配置 = 该功能对该品牌关闭**，接口返回明确错误，不 panic。

```yaml
social_auth:
  kaitu:
    google: { client_ids: [web, ios, android], web_client_id: ... }
    apple:  { client_ids: [com.allnationconnect.anc.wgios, <services id>], team_id: NJT954Q3RH, key_id: ..., private_key_path: ... }
  overleap:
    google: { ... }
    apple:  { ... }
```

Apple 的 `team_id/key_id/private_key` 用于生成 client secret，去换 refresh token 和撤销（§5.6）。客户端 ID 不是秘密，也烘进客户端品牌配置（webapp `brands/<brand>`、web `lib/brands.ts`）。

### 5.3 接口

| 方法 | 路径 | 鉴权 | 用途 |
|---|---|---|---|
| POST | `/api/auth/social/login` | 无 | App 登录（设备绑定，所有端），body：`provider, idToken, nonce, authorizationCode?, redirectURI?, udid, remark, language` → `DataAuthResult` |
| POST | `/api/auth/social/web-login` | 无 | 官网登录（cookie），→ `DataWebLoginResponse` |
| GET | `/api/user/social` | AuthRequired | 列出已绑定的 provider（provider + 脱敏邮箱） |
| POST | `/api/user/social/bind` | AuthRequired | 绑定到当前账号，body 同 login 的令牌部分；所有端 App 与官网账号页（cookie）调用 |
| DELETE | `/api/user/social/:provider` | AuthRequired | 解绑（Apple 解绑时同时撤销令牌） |

新路径加入 webapp `cloud-api.ts` 的 `AUTH_TOKEN_PATHS`，否则令牌不会被保存。

`redirectURI`：Apple **网页流**（`aud` = Services ID）的授权码换 refresh token 时，Apple 要求带上授权时的 `redirect_uri`，所以网页流的 `authorizationCode` 必须伴随 `redirectURI`（缺 → 400）；值必须在品牌注册表 `AppleReturnURLs` 里（https、host ∈ 本品牌 Hosts、路径 `/auth/social/apple`，契约导出）。原生 iOS 码（`aud` = bundle ID）永远不带 `redirect_uri`。（2026-10-21 用真 Apple 令牌发现并修复：不带时 Apple 返回 `invalid_grant`，refresh token 存不下来，撤销形同虚设。）

**没有服务端 handoff**（2026-10-21 定，取代 handoff 会话 / returnCode 方案）：每个端自己按 RFC 8252（原生 App OAuth）拿到 idToken，再调 `/social/login`。服务端只认令牌，不托管任何浏览器会话。

### 5.4 错误码
新增（HTTP 仍 200，code 分段遵循现有约定），并同步 `webapp/src/utils/errorCatalog.ts`、全部语言、契约 golden：
- 第三方令牌无效/过期/nonce 不符（4xx client 段）
- 该 provider 在本品牌未开启
- 绑定冲突：这个 Google/Apple 账号已绑在别的用户上（复用 `409001` 语义或新码，builder 定）
- Google 邮箱未验证且未绑定

具体数值由 builder 按 `api/response.go` 现有分段分配。

### 5.5 数据
不加新表。`LoginIdentify` 新增 `Type` 取值 `"google"` / `"apple"`。Apple 需额外保存 refresh token（加密）用于撤销——加到 `LoginIdentify` 的一个新可空列，或单独一张小表，builder 定；走 `Migrate()` 的 AutoMigrate。

### 5.6 Apple 撤销（App Store 审核硬性要求）
提供 Sign in with Apple 且支持注销的 App，注销时必须调用 Apple 的 `/auth/revoke`。
- 客户端同时送 `authorizationCode`（iOS 原生、网页 Apple JS、`/auth/social/apple` 回调都拿得到）；服务端用 client secret 换取 refresh token 并加密保存（网页流需 `redirectURI`，§5.3）。撤销成功与失败都记日志（不含令牌），线上可据此确认。
- `api_delete_user_account` 与管理员硬删除：对该用户所有 apple 身份调用 revoke，失败只记日志不阻塞注销。
- 解绑 Apple 时也撤销。

## 6. 各端客户端

### 6.1 一览

| 端 | Apple | Google |
|---|---|---|
| iOS（两品牌） | 系统原生（`@capgo/capacitor-social-login`） | Google Sign-In（同一插件） |
| Android | 同一插件的网页流：Custom Tab → 官网 `/auth/social/apple` → App 深链（§6.3） | 同一插件，Credential Manager；**设备无 GMS 时隐藏按钮** |
| macOS / Windows（Tauri） | 系统浏览器 → 官网 `/auth/social/apple` → 本机回环（§6.3） | 系统浏览器 + 本机回环 + PKCE（Google 桌面 client，§6.3） |
| Linux（k2 内嵌网页） | 同桌面，回环即 webui 自身 `127.0.0.1:1777` | 同桌面 |
| 官网 kaitu.io / overleap.io | Apple JS（popup） | Google Identity Services（popup） |

### 6.1.1 Google 连通性探测（David 决定，两品牌所有端）
登录前 VPN 未连接，国内用户在任何端都连不上 Google。因此 Google 按钮的可见性额外加一层：
- 登录界面打开时即后台探测 Google 可达性（对 Google 端点发一个无副作用请求，超时 3 秒），**不阻塞界面**：探测完成前 Google 按钮不显示，成功后再出现；失败则整场不显示，只留 Apple 与验证码。
- 原生 Android 在此之外还需有 GMS；桌面 / Linux 与官网登录页以对 Google 端点的探测或 Google 脚本能否加载为准（同等效果）。
- 结果按"本次界面打开"缓存，不持久化（网络环境会变，比如用户连上 VPN 后再登录）。
- 探测封装成 webapp 里一个独立函数，可在测试中注入结果。

### 6.2 原生（iOS / Android）
- **用成熟插件，不自写原生桥**（David 2026-10-21 定）：`@capgo/capacitor-social-login` v8。它要求 Capacitor 8，所以**先单独把 Capacitor 7→8 升级做完并发版**，再集成插件（两个独立计划）。
- 插件输出 idToken（+ Apple `authorizationCode`），webapp 原样交给 `/social/login`；nonce 由 webapp 生成，传给插件的是 `sha256(nonce)`。
- webapp 侧作为 `IPlatform` 的**可选**成员，`typeof fn === 'function'` 探测；老版本 App 没有这个能力 → 按钮不显示，不报错。
- iOS：两个品牌的 App target 加 `com.apple.developer.applesignin` entitlement；Google 需要每品牌的 reversed client ID URL scheme（走品牌 xcconfig）。App Store 4.8 条款：iOS 上出现 Google 登录就必须同时提供 Apple 登录——本方案满足。
- Android：Google 用本品牌 **Web client ID** 作 `serverClientId`；Google Cloud 里按 flavor 包名 + 签名证书 SHA-1 注册 Android client（已完成，§7.1）。Apple 走插件的网页流，回调页见 §6.3。

### 6.3 浏览器登录（桌面 / Linux / 安卓 Apple）— 原生 App OAuth（RFC 8252）
App 自己完成整个 OAuth，拿到 idToken 后与原生一样调 `/social/login`。服务端无会话、无轮询。

**Google（桌面 / Linux）**：本品牌 **Google 桌面 client**（§7.1），授权码 + PKCE + nonce，`redirect_uri = http://127.0.0.1:<端口>`（Google 桌面 client 接受任意端口的回环）。App 用系统浏览器打开授权页，回环收到 code 后自己去 `oauth2.googleapis.com/token` 换 idToken。桌面 client 的 client secret **嵌在 App 里**（David 2026-10-21 定）——Google 对"已安装应用"明确说明该 secret 不保密，安全性来自 PKCE + 回环。
- macOS / Windows（Tauri）：Rust 侧临时绑定 `127.0.0.1:0`，收到回调即关闭（可用 `tauri-plugin-oauth`）。
- Linux：webui 本身就在 `http://127.0.0.1:1777`，`redirect_uri` 指向 webapp 自己的路由。

**Apple（桌面 / Linux / 安卓）**：Apple 不接受回环或自定义 scheme 作 return URL，所以经本品牌官网中转：
1. App 生成 nonce 与 state，用浏览器（安卓为 Custom Tab）打开 Apple 授权页：`client_id` = 本品牌 Services ID、`redirect_uri` = 官网 `/auth/social/apple`、`response_mode=form_post`、`nonce=sha256(nonce)`。App 要的回传目标编码在 `state` 里。
2. Apple 把 `code`、`id_token`、`state` 以 form POST 发到官网回调页。回调页**只做转发**：从 `state` 解出回传目标，**只允许** `http://127.0.0.1:<端口>/…` 或本品牌 App scheme（深链），其他一律显示错误、不跳转；然后把 `code`、`id_token`、`state` 带过去。
3. App 校验 `state` 后调 `/social/login {provider: apple, idToken, nonce, authorizationCode: code, redirectURI: 官网回调地址}`。

**安全性**：回调页转发的 idToken 绑定了只在发起 App 内存里的 nonce 原值，被别的程序截到也用不了。钓鱼链接只会把令牌送到受害者自己的 127.0.0.1 或受害者自己的 App——攻击者拿不到。这正是之前 handoff 方案要靠 returnCode 才能补上的洞，在原生 OAuth 里天然不存在。

**回调页注意（2026-10-21 线上实测）**：两站的 locale 中间件会把 `POST /auth/social/apple` 307 到 `/<locale>/auth/social/apple`（kaitu.io → `/zh-TW/…`，overleap.io → `/en-GB/…`，后者当前 404）。回调路由必须排除在 locale 中间件之外，且以服务端路由处理 POST（307 虽保留方法和 body，但不要依赖它）。

**绑定也在 App 内直接完成**（我的判断，随方案简化）：所有端的 App 都能自己拿到 idToken，因此账号页"绑定"统一调 `/api/user/social/bind`，不再需要跳官网账号页。

### 6.4 官网
- kaitu.io（`web/src/app/[locale]/login/page.tsx`）与 overleap.io（`sites/overleap/.../LoginForm.tsx`）登录页加两个按钮，调 `/api/auth/social/web-login`；同时新增 §6.3 的 `/auth/social/apple` 回调转发页。两站账号页加"登录方式"区：列出、绑定（Apple JS / Google Identity Services 拿 idToken 后调 `/api/user/social/bind`）、解绑。两站不共享代码（按 overleap 站拆分惯例各写一份，靠契约一致）。
- Google 脚本加载失败（国内）→ 不显示 Google 按钮。Apple JS 在国内可达。
- Apple 网页登录需每品牌一个 Services ID，登记域名与 return URL（见 §7.1）；Google Web client 登记 JS origin。

### 6.5 webapp UI
- `LoginDialog.tsx` 与 `EmailLoginForm.tsx`（两个都要，后者被购买页和 iOS 订阅面板使用）的验证码表单下方加"或"分隔 + "通过 Apple 继续" / "通过 Google 继续"。按钮可见性 = 品牌开关 × 平台能力（原生插件可用性 / 桌面与 Linux 浏览器登录永远可用）× Google 连通性。
- 文案中文用户面不出现 "Kaitu" 裸词；全部语言补齐。
- 账号页（`pages/Account.tsx`）新增"登录方式"区：显示已绑定项，可绑定 / 解绑。所有端都在 App 内拿到 idToken 后直接绑定（§6.3）。解绑直接生效，不弹确认（随时可以重新绑定）。
- client ID 放各品牌 `index.ts`（与 Go 注册表由契约锁定，见 §5.2）；开关来自服务端下发，不在客户端写死。改品牌数据后重生成 `contracts/api-contract.json`。
- **路由器（k2r，`platformType === 'gateway'`）不显示第三方登录按钮**（我的判断：路由器管理页没有"回到 App"的场景，设备上限也另算），需有测试锁住。

## 7. 上线前置（后台配置，非代码）

- **Apple Developer**：两个 iOS App ID 开启 Sign in with Apple；每品牌建一个 Services ID（网页 + 浏览器登录用）并登记域名/return URL；建 Sign in with Apple Key（.p8）放进 Center 配置；在 "Sign in with Apple for Email Communication" 登记发信域名/地址，否则发往 relay 邮箱的邮件（续费、收据、验证码）会被丢弃。
- **Google Cloud**：每品牌一个 OAuth consent screen + Web / iOS / Android / 桌面 client（Android 登记包名 + 所有签名证书 SHA-1）。Overleap 的 consent screen 署名 Wordgate LLC，开途侧沿用现有署名；两品牌不互相提及。
- Center 两台机器配置 `social_auth`，`make deploy-api`。

### 7.1 已完成的后台配置（2026-10-20）

客户端 ID 不是秘密，可直接进代码；私钥除外。

| | 开途 | Overleap |
|---|---|---|
| 官网域名（规范 host） | `www.kaitu.io`（另登记 `kaitu.io`） | `overleap.io`（`www.` 301 到裸域；两者都登记） |
| Google Cloud 项目 | `kaitu-sign-in`，授权页名「开途」，已发布正式版 | `overleap-sign-in`，授权页名「Overleap」，已发布正式版 |
| Google Web client（也是 Android `serverClientId`） | `125062807005-fqpjkq2pjjqdueen84q1kpp3rlkvq5sg.apps.googleusercontent.com` | `808676014960-fvo8an88b4c79f1ua9n0pctr40ta09fm.apps.googleusercontent.com` |
| Google iOS client | `125062807005-r08pi88vadprilgo5mr1oqndpinnkb0l.apps.googleusercontent.com` | `808676014960-ug0b6ahv3lm2s1fboj0dinmtp5p0t6qu.apps.googleusercontent.com` |
| Google Android client | `125062807005-jtpkj6bmt48jrk0bjk80afb0gdh7cqlj`（`io.kaitu`，SHA-1 `95:3C:D4:…:F8:DB`，取自 CDN 0.4.13 APK） | 3 个（Play 当前签名 / 之前签名 / 上传密钥）：`808676014960-pr6d5hsab78ela9mo2h0h7bl8ns8r208`、`…-eu9c9sghcmdq0n795n2vu850mcj4b290`、`…-ignp65u1nvrt12kaar5v8g6hs1dt10u6` |
| Google 桌面 client（2026-10-21 建；secret 嵌 App，暂存 `~/.config/kaitu-secrets/google-desktop-clients.env`） | `125062807005-9vlr5ea2hp42hn5r2sc5prn8m82nnrv7.apps.googleusercontent.com` | `808676014960-kb76trvs3q7q89a5qb914kjv5gfhu841.apps.googleusercontent.com` |
| Apple App ID（已开 Sign in with Apple，primary） | `com.allnationconnect.anc.wgios` | `io.overleap` |
| Apple Services ID（网页 / 浏览器登录的 `aud`） | `io.kaitu.signin`（描述 "KaiTu VPN"） | `io.overleap.signin`（描述 "Overleap"） |
| Apple return URL | `https://www.kaitu.io/auth/social/apple`、`https://kaitu.io/auth/social/apple` | `https://overleap.io/auth/social/apple`、`https://www.overleap.io/auth/social/apple` |
| Sign in with Apple Key ID | `Y47F4Y5BSL` | `8MSCS26D9J` |

- Apple Team ID `NJT954Q3RH`。两个 .p8 私钥暂存 David 本机 `~/.config/kaitu-secrets/`（0600），部署时写进 Center `config.yml`（与现有 `appstore.iap.key` 同样内联），**不进 git**。
- Apple 私密邮件转发已登记发信源 `mail.allnationconnect.com` / `joshua@mail.allnationconnect.com`（SPF 通过）。线上两个品牌目前都由该地址发信（`mail_overleap.send_from` 未配置）；将来换发信域名必须同步在 Apple 登记，否则发往 relay 邮箱的邮件被丢弃。
- Google 授权页的用户支持邮箱只能是 `anc.info7@gmail.com`（Google 限制本账号或群组）；未上传 logo（上传即需 Google 审核）。
- 两个 App ID 开能力使旧描述文件失效；iOS 是自动签名 + `-allowProvisioningUpdates`，下次构建自动重建。
- 发布顺序：API 先上（未配置 = 关闭，安全）→ 官网（含 Apple 回调转发页）→ Capacitor 8 升级发版 → 客户端登录发版 + `webapp/*` OTA。桌面 / 安卓的 Apple 登录依赖官网回调页先上线。

## 8. 测试

- API：令牌校验（伪造签名、错 aud、跨品牌 aud、过期、nonce 不符）、§4 三步匹配各分支、绑定冲突、Apple 网页码缺 / 错 `redirectURI` 被拒而原生码不带、注销 / 解绑触发 revoke。DB 测试必须真跑（0 SKIP）。JWKS 用测试密钥对注入，不打外网。
- webapp：按钮可见性矩阵（品牌 × 平台能力）、老 bridge 无能力时不显示、令牌保存路径。
- 真机 smoke：iOS 两品牌 Apple + Google；Android 有/无 GMS；macOS / Windows / Linux 浏览器登录（含 Apple 回调转发）；两个官网。
- **已用真令牌在本机 Center 验过（2026-10-21）**：Google 桌面回环（两品牌，新建 / sub 匹配 / 邮箱匹配 / 跨品牌 aud 拒 / 错 nonce 拒 / 列表、解绑、绑定）；Apple 网页流（两品牌：relay 邮箱新建、同邮箱匹配已有账号、refresh token 存下、解绑与注销撤销成功、跨品牌拒）。按项目信心框架，没有真机 smoke 不宣布完成。

## 9. 留给 builder 的

- JWKS / OIDC 库选型；Apple refresh token 存放位置（新列 vs 小表）；新错误码具体数值。
- 共享登录事务函数的抽取方式。
- App 内等待 UI 细节、回环本机页的文案。
- iOS Google SDK 引入方式（SPM / CocoaPods，跟随现有工程）。
