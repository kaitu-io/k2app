# 开途 iOS：设备账号（免登录开始试用 / 订阅）— 设计

日期：2026-10-20 · 状态：草稿（多设备规则已由 David 定），待 David 整体确认 · 关联：`2026-10-19-kaitu-ios-apple-trial-design.md`（试用）、`2026-10-20-social-sign-in-design.md`（Apple / Google 登录）

## 1. 为什么做

- 开途 App 10-09 ~ 10-19：首次打开 971 → 登录 430（**流失 56%，最大的一个漏口**）→ 连上 45。iOS 占登录的 91%。
- 现在 iOS 购买前必须邮箱验证码登录（`webapp/src/components/IosSubscribePanel.tsx:16-17,283`）。收不到验证码、嫌麻烦的人在这一步就走了。
- Apple 审核 5.1.1：购买非账号类内容不得强制注册；注册须可选，用途是"在其他设备上使用"。我们现在的做法本身就有被拒风险。
- David（2026-10-20）：试用不和账号挂钩，只在当前设备起作用，不要邮箱；要用其他设备时再引导设邮箱 / Apple 登录，之后才能用满 5 台。

成功标准：iOS 新用户不输入任何东西即可点「免费试用」；「首次打开 → 开始试用 / 订阅」不再经过登录；已付费用户换手机、重装不丢会员。

## 2. 目标 / 非目标 / 反目标

**目标**
- 开途 iOS 首次打开、未登录时，自动建一个**设备账号**（没有邮箱、没有任何登录身份），直接可试用 / 订阅。
- 设备账号只能在这一台设备上用（设备上限 = 1）。绑定邮箱或 Apple / Google 登录后变成普通账号，设备上限恢复套餐值（5 台）。绑定是**在原账号上补身份**，订阅和时长不动。
- 重装 App 回到同一个设备账号。
- 换手机、未绑定：新手机「恢复购买」能接管原设备账号（Apple 审核 3.1.2 要求订阅在用户所有设备可用，且不得用强制注册来满足）。
- 「已有账号？登录」入口始终可见，老用户照旧登录。

**非目标**
- 安卓 / 桌面 / 网页 / 路由器：照旧先登录。
- Overleap：本期不开（品牌开关默认关，以后单独决定）。
- 账号合并工具（设备账号与已有邮箱账号合并时长）——见 §5 冲突规则。
- 给设备账号发邮件（没有邮箱；Apple 自己发收据和续订通知）。

**反目标**
- 已登录用户的任何行为变化。
- 设备账号被别人接管（凭一个交易号、一个 udid 就能登进别人的付费设备账号）。
- 付费设备账号因为卸载、换机、登录别的账号而"丢了"，且无路可找回。
- 漏斗里"登录"一步消失后看不出转化（要有替代事件）。

## 3. 现状（2026-10-20 摸底）

- **udid**：webapp 随机 UUID → sha256 截 16 字节（`webapp/src/services/device-udid.ts:20-42`），存在 App 私有 JSON 文件（`capacitor-storage.ts`，不进 iCloud 备份）。**卸载即丢**；新 udid 生成时清掉 token（`device-udid.ts:44`）。没有任何 Keychain 代码。
- **token**：同一个存储（`auth-service.ts:32,154-159`），卸载即丢。
- **设备**：`Device.UDID` 唯一、`UserID` 非空（`api/model.go:162-190`），一台设备只属于一个用户。邮箱登录时若该 udid 属于别人，删旧设备行并给旧主人发邮件（`api/api_auth.go:257-282`），设备上限踢最旧（`logic_auth.go:510`）。
- **身份**：`User` 没有 email 列，身份全在 `LoginIdentify`（`model.go:145-159`）。零身份的用户在库层面合法。发邮件遇到无邮箱用户软失败（`logic_email.go:265-271`，调用方只记日志）。后台若干视图以 `len(LoginIdentifies)>0` 兜底；`lookup_user` 只能按 UUID 找到无邮箱用户。
- **购买**：`appAccountToken = uuidv5(ns, user.UUID)`（`logic_apple_iap.go:35-43`），首笔绑定时服务端校验不符即拒（`:159-168`）——设备账号天然适配，只要有 user UUID。
- **绑邮箱**：`POST /api/user/email/update-email`（`api_user.go:87-196`）对无邮箱用户会新建 email 身份——已可用；邮箱已属别的账号时报 `ErrorEmailAlreadyInUse`。
- **Apple / Google 绑定**：社交登录 spec §5.3 的 `/api/user/social/bind` 绑到当前账号——设备账号可直接用，需确认该 spec 实现不假设当前用户已有 email 身份。
- **漏斗**：App 路线"登录"一步 = `auth_done`（`logic_funnel_paths.go:165`）。

## 4. 设计

### 4.1 设备凭据（Keychain）

- 新建设备账号时服务端签发一个**设备密钥**（随机 32 字节，库里只存哈希），客户端把 `{udid, deviceSecret, userUUID}` 写进 iOS **Keychain**，可访问性 `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`（卸载后仍在；不随 iCloud 钥匙串同步到别的设备）。
- K2Plugin 新增 `secureGet / secureSet / secureRemove`（iOS Keychain；Android/桌面不实现，webapp 按能力探测）。属于原生能力 → 随第 ② 步的新 iOS 版本发布，bump `BRIDGE_API_VERSION`。
- udid 生成改为：先读 Keychain 里的 udid，有就用（重装后设备身份不变）；Keychain 没有时**先把现有存储里的 udid 迁进 Keychain**（老用户升级新壳不能换 udid——换 udid 会触发 `clearStaleAuthTokens` 把所有 iOS 用户登出），两处都没有才随机生成。只对有 `secureGet` 能力的壳生效；旧壳行为不变。
- deviceSecret 的哈希存在**按用户的独立小表**（`device_account_credentials{user_id, udid, secret_hash, revoked_at}`），不放 Device 行——登出、设备被踢都会删 Device 行，凭据必须比它活得久，`/resume` 才能把设备行重建回来。（我的判断）

### 4.2 接口

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/auth/device-account` | 无鉴权。body `{udid, remark, language, platform}`。该 udid 的设备行属于有身份的用户 → 拒绝（走登录）；属于零身份用户或该 udid 有未作废凭据 → 拒绝（应走 `/resume`，防重复建号）。否则建 User（零身份）+ Device，返回 `DataAuthResult` + `deviceSecret`。品牌开关关闭时返回明确错误码。 |
| POST | `/api/auth/device-account/resume` | 无鉴权。body `{udid, deviceSecret}`。重装后用 Keychain 凭据换新 token。密钥哈希与 udid 都匹配、且该用户仍是零身份才放行；绑定过身份的用户返回"请登录"错误码（之后走普通登录）。 |
| POST | `/api/auth/device-account/restore` | 无鉴权。换机恢复：body `{udid, signedTransaction(JWS), deviceVerification 相关字段}`。见 §4.4。 |

- 限流：`/device-account` 按 IP 与 udid 限流（builder 定阈值）；零身份、未试用未付费的设备账号连不了任何节点，批量注册拿不到免费服务，主要防库膨胀。
- 后台清理：从未试用、未付费、30 天无活动的设备账号可清理（worker，builder 定）。

### 4.3 设备上限与绑定

- `checkDeviceLimitOrKick`：用户零身份时上限 = 1。
- 绑定（邮箱验证码 / Apple / Google）成功 = 在当前用户上补 `LoginIdentify` 行；之后上限按套餐，用户可在别的设备用该身份登录。
- 绑定入口：会员页显眼位置「在其他设备上使用 → 设置邮箱或 Apple 登录」；用户在设备管理页、或因上限被拒时也引导到这里。
- 绑定成功后 Keychain 里的 deviceSecret 作废（服务端标记），该设备之后靠普通 token；重装后走普通登录。

### 4.4 换机恢复（未绑定身份）

新手机上用户点「恢复购买」，StoreKit 2 拿到当前有效交易的 JWS。服务端：
1. 验签 JWS（Apple 根证书链，已有能力），并向 Apple 复核该交易仍有效。
2. **证明是在这台设备上拿到的**：StoreKit 2 交易含 `deviceVerification = SHA384(deviceVerificationNonce + identifierForVendor)`。客户端同时送 `identifierForVendor`，服务端核对。没有这一步，任何拿到交易号 / JWS 的人都能接管别人的付费账号。（builder 先在真机确认字段可用；不可用则此接口不上线，降级为客服处理，并告诉 David。）
3. 交易链的主人（按 appAccountToken / 订阅行）若是**零身份**设备账号 → 把这台设备登进该账号（旧设备按上限 1 被踢）、签发新 deviceSecret。主人已绑定身份 → 返回"请用 xxx 方式登录"（脱敏提示：邮箱 / Apple）。
4. 试用中的链同样适用。

### 4.4.1 同一 Apple ID 的多台设备

见文末「已定」。

### 4.5 冲突规则（我的判断）

| 情况 | 处理 |
|---|---|
| 设备账号**没有**任何订阅 / 试用记录，用户选择登录已有邮箱账号 | 直接登录已有账号，设备账号丢弃（设备行转给已有账号，按现有登录流程） |
| 设备账号**有**订阅或试用，用户要绑定的邮箱 / Apple ID 已属别的账号 | 拒绝绑定，提示"该邮箱已有账号，请换一个邮箱绑定；如需合并请联系客服"。不做自动合并（Apple 交易已永久绑定设备账号的 appAccountToken，合并要搬订阅链，风险大于收益） |
| 设备账号有订阅，用户在这台设备上点「登录已有账号」 | 先弹 MUI 对话框警告"这台设备上的订阅属于当前设备账号，登录别的账号后可通过「恢复购买」回来"，确认后照常登录；设备账号保留，可经 §4.4 恢复 |
| 零身份用户点「退出登录」 | 不提供普通退出：该位置换成「设置邮箱或 Apple 登录」和「登录其他账号」（后者走上一行的警告） |
| 试用中的设备账号在这台手机上登录了邮箱账号，之后试用转付费 | 沿用试用 spec 的改绑规则：从没付过费的链在首笔付费时归当前登录的账号。§4.4 恢复时"链的主人"= 订阅行当前的 user，不看原 appAccountToken |
| 邮箱登录时 udid 属于一个设备账号（现有的"设备转移"邮件） | 设备账号无邮箱，跳过发邮件（现在本就软失败，改为显式跳过、不记错误日志） |

### 4.6 客户端（webapp）

- 启动时：iOS + 开途 + 有 `secureGet` 能力 + 未登录：
  1. Keychain 有 deviceSecret → `/resume`；
  2. 否则 → `/device-account` 静默建号；
  3. 都失败 → 退回现在的登录界面（不阻塞）。
- 付费页 / 试用面板：设备账号视为已登录，不再显示内嵌 `EmailLoginForm`，订阅按钮直接可点。顶部保留「已有账号？登录」。
- 会员页：零身份时显示「这台设备专用 · 设置邮箱或 Apple 登录后可在 5 台设备使用」。
- 账号相关页面（设置、设备管理）在零身份时隐藏邮箱相关项，显示绑定入口。
- 旧壳（无 `secureGet`）：行为完全不变（仍需登录）。

### 4.6.1 客服

- 零身份用户提工单时表单要求填一个联系邮箱（只用于回复，不绑定为登录身份）；`api_ticket.go` 读用户邮箱失败时用这个。
- 会员页 / 设置页显示短「支持码」（用户 UUID 前 8 位），客服用 `lookup_user` 按 UUID 前缀查。

### 4.7 漏斗

- 新事件 `device_account_created`、`identity_bound` 由**服务端**在建号 / 首次补身份成功时写入（不靠客户端上报，避免漏报）；App 路线中"登录"一步对开途 iOS 新壳改为"建号或登录"（`device_account_created ∪ auth_done`），另加"绑定身份"一步（`identity_bound`）作为后置观察。
- `auth_done` 语义不变（真实身份登录）。

### 4.8 品牌开关

- `brandRegistry` 加 `DeviceAccount bool`（开途 true，Overleap false），经 App 配置接口下发；客户端以服务端开关为准。改品牌数据后重生成 `contracts/api-contract.json`。

## 4.9 发布门（我的判断）

- 计划第一个任务是**真机验证 StoreKit 2 `deviceVerification`**。验证失败 → 停下回报 David，不带着"换机丢会员"的缺口上线。
- 绑定方式第一版只要求邮箱（接口已有）；Apple / Google 绑定随社交登录那边上线，不互相阻塞。

## 5. 与其他工作的关系

- **试用 spec**：设备账号是试用的入口；试用期 = 设备账号 + 体验线路；转正走 §3.9 升级。两者都随第 ② 步 iOS 版本发布。
- **社交登录 spec**：`/api/user/social/bind` 必须支持零身份当前用户；"邮箱 / 第三方身份已属别的账号"走本 spec §4.5 第二行。需与该会话对齐（我来同步）。
- 发布顺序：API（开关默认关）→ iOS 新版（含 Keychain、试用、设备账号）提审 → 过审后打开开途开关 + ASC 试用。

## 6. 测试

- API（DB 真跑 0 SKIP）：建号（新 udid / 已存在 udid 拒绝 / 开关关闭）、resume（对 / 错密钥、已绑身份拒绝）、上限 1、绑定后上限恢复、绑定冲突、restore（deviceVerification 对 / 错、主人已绑身份、主人零身份）、无邮箱跳过转移邮件、清理 worker 不删有订阅的账号。
- webapp：启动分支矩阵（有 / 无能力 × 有 / 无 Keychain 凭据 × 已登录）、付费页不显示内嵌登录、旧壳不变。
- 真机 smoke：首次打开 → 试用 → 卸载重装仍是同一账号 → 绑邮箱 → 另一台设备登录；第二台 iPhone 同 Apple ID 恢复购买接管。

## 7. 留给 builder

- deviceSecret 存放（Device 新列 vs 小表）、限流阈值、清理 worker 周期。
- 新错误码数值（按 `api/response.go` 分段，同步 `errorCatalog.ts` 与全部语言）。
- Keychain 封装细节；restore 的 deviceVerification 校验实现。

## 已定：同一 Apple ID 的多台设备（David 2026-10-20）

「付费后要多台设备，必须登录我们的账号；同一个账号才能共享 5 台。不登录，账号系统就不分那 5 台。」

- 零身份设备账号**无论试用还是付费，上限都是 1 台**。苹果能证明是同一 Apple ID 也不例外。
- §4.4 换机恢复照旧可用，但它是**挪走**这唯一的一台：新设备登进来，旧设备被踢。
- 恢复时如果检测到旧设备最近还活跃（同一 Apple ID 的 iPhone + iPad 来回切），在新设备上提示「设置邮箱或 Apple 登录，就能多台同时使用（最多 5 台）」。旧设备被踢时显示同样的引导，不显示普通的"在别处登录"。
- 审核口径：订阅在用户的任何设备上都能通过「恢复购买」取回，并且不强制注册；同时使用多台设备需要账号，这是可选功能。
