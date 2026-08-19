/**
 * Single source of truth: error code → user-facing i18n key.
 *
 * WHY THIS FILE EXISTS
 * --------------------
 * Error copy used to be written twice, by hand, in two places that never knew
 * about each other:
 *
 *   utils/errorCode.ts   getErrorMessage()   69-case switch   (API / response domain)
 *   services/vpn-types.ts getErrorI18nKey()   9-entry literal  (VPN / engine domain)
 *
 * The second one lagged badly. 40 of the 47 codes declared in the 100–599 range
 * had no entry, so a genuine connection failure rendered "未知错误" while precise
 * copy for the same code already sat in the first map. And one of the nine
 * entries it did have was simply the wrong number — `ErrCodeTimeout = 408`,
 * where k2/engine/error.go has said 108 since 2026-08-02. That one typo broke
 * both the copy and `isNetworkError()`.
 *
 * This is the third bug of the same shape in this repo ("one enum, N
 * hand-written predicates/maps enumerating it"), so the fix is structural
 * rather than another round of filling in missing rows: both functions now
 * derive from the tables below, and `utils/__tests__/errorCatalog.test.ts`
 * fails if a declared code has no resolvable copy in all 7 locales.
 *
 * WHY TWO CATALOGS AND NOT ONE
 * ----------------------------
 * Both the Center API (api/response.go) and the k2 engine (k2/engine/error.go)
 * chose HTTP-aligned codes, independently. Their code spaces therefore COLLIDE
 * with different meanings, and no single string can serve both:
 *
 *   400  API "invalid operation / bad parameters"  vs  engine "bad wire config"
 *   401  API "authentication required"             vs  engine "server rejected auth"
 *   403  API "permission denied"                   vs  engine "certificate pin mismatch"
 *   503  API "service unavailable"                 vs  engine "node unreachable"
 *
 * Collapsing them would silently mistranslate whichever side lost. So the
 * catalogs stay split by ORIGIN, and each consumer picks the one matching the
 * channel it reads from:
 *
 *   API_ERROR_CATALOG    ← cloudApi / SResponse.code   (getErrorMessage)
 *   ENGINE_ERROR_CATALOG ← ControlError from _k2.run   (getErrorI18nKey)
 *
 * Codes carrying the same meaning in both (402, 504, 570, 572, 573, 580 …) are
 * declared in both tables; the coverage test checks both.
 */

/**
 * Error codes — backend API codes synced with api/response.go, plus
 * frontend-only codes for VPN/network/action errors.
 *
 * Constitution: every backend error code MUST have an entry here and an entry
 * in API_ERROR_CATALOG (or be listed in LOG_ONLY_CODES).
 */
export const ERROR_CODES = {
  SUCCESS: 0,

  // === Backend API codes (sync with api/response.go) ===
  INVALID_OPERATION: 400,
  UNAUTHORIZED: 401,
  PAYMENT_REQUIRED: 402,
  FORBIDDEN: 403,
  NOT_FOUND: 404,
  NOT_SUPPORTED: 405,
  UPGRADE_REQUIRED: 406,
  CONFLICT: 409,
  INVALID_ARGUMENT: 422,
  TOO_EARLY: 425,
  TOO_MANY_REQUESTS: 429,
  INTERNAL_SERVER_ERROR: 500,
  SERVICE_UNAVAILABLE: 503,

  // Backend custom codes (400000+ range, sync with api/response.go)
  INVALID_CAMPAIGN_CODE: 400001,
  INVALID_CLIENT_CLOCK: 400002,
  INVALID_VERIFICATION_CODE: 400003,
  INVALID_INVITE_CODE: 400004,
  SELF_INVITATION: 400005,
  INVALID_CREDENTIALS: 400006,
  LICENSE_KEY_NOT_FOUND: 400007,
  LICENSE_KEY_USED: 400008,
  LICENSE_KEY_EXPIRED: 400009,
  LICENSE_KEY_NOT_MATCH: 400010,
  LICENSE_KEY_ALREADY_REDEEMED: 400011,
  PROXY_MEMBERS_DEPRECATED: 400012,
  VERIFICATION_CODE_EXPIRED: 400013,

  // Tier system error codes (added 2026-04-20)
  TIER_MISMATCH: 422001,
  PROXY_PURCHASE_DEPRECATED: 422002,

  // Router / device-class error codes (added 2026-05-22)
  PLAN_NO_ROUTER: 402001,
  ROUTER_DEVICE_LIMIT: 403001,
  DEVICE_CLASS_MISMATCH: 403002,
  BRAND_MISMATCH: 403003,
  INVALID_CLIENT_CLASS: 422003,
  PAYMENT_CHANNEL_UNAVAILABLE: 405001,

  // Resource conflict — uniqueness already taken within the brand (added 2026-07-29)
  EMAIL_ALREADY_IN_USE: 409001,

  // === Frontend-only codes (NOT from backend API) ===

  // 网络错误 (100-109) - 来自 classifyNetworkError
  NETWORK_TIMEOUT: 100,
  NETWORK_UNREACHABLE: 101,
  NETWORK_RESET: 102,
  NETWORK_DNS: 103,
  NETWORK_TLS: 104,
  NETWORK_REFUSED: 105,
  // 108 is NOT frontend-only — it is the k2 engine's timeout. See
  // ENGINE_ERROR_CODES.ErrCodeTimeout. Declared there, not here.

  // 服务器相关错误 (110-119)
  SERVER_UNAVAILABLE: 110,
  SERVER_OVERLOAD: 111,
  SERVER_MAINTENANCE: 112,

  // Engine errors from k2 core (HTTP-aligned, 5xx = server/dependency-side)
  RULE_BUNDLES_UNAVAILABLE: 504, // Rule-bundle CDN dependency unreachable (transient, retryable)

  // VPN 服务相关错误 (510-519) — frontend-synthesized
  VPN_START_FAILED: 511,
  VPN_RECONNECT_FAILED: 512,
  VPN_TIMEOUT: 513,

  // 网络修复相关错误 (520-529)
  NETWORK_REPAIR_FAILED: 520,
  NETWORK_REPAIR_DNS: 521,
  NETWORK_REPAIR_ROUTE: 522,
  NETWORK_REPAIR_PRE_FAILED: 523,

  // 连接错误 (570-579)
  CONNECTION_FATAL: 570,
  ALL_ADDRS_FAILED: 571,
  NO_TUNNEL_AVAILABLE_AUTO: 572,
  NO_TUNNEL_AVAILABLE_FILTERED: 573, // auto-pick pool emptied by user country filter

  // VPN 权限错误 (580-589)
  VPN_PERMISSION_DENIED: 580,

  // IAP (iOS StoreKit) 错误 (590-599) — frontend-synthesized
  IAP_PURCHASE_FAILED: 590,
  IAP_VERIFY_FAILED: 591,
  IAP_FINISH_FAILED: 592,   // log-only, no user message
  IAP_NOT_AVAILABLE: 593,   // log-only, no user message

  // 认证相关错误 (530-539)
  LOGOUT_FAILED: 530,
  TOKEN_REFRESH_FAILED: 531,

  // 资源/隧道相关错误 (540-549)
  TUNNEL_LIST_FAILED: 540,
  TUNNEL_CONNECT_FAILED: 541,

  // Action 执行相关错误 (550-559)
  ACTION_TIMEOUT: 550,
  ACTION_PARSE_FAILED: 551,

  // API 请求相关错误 (560-569)
  API_REQUEST_FAILED: 560,
  API_RESPONSE_FAILED: 561,

  // 网络层错误 (cloudApi 返回)
  CLOUD_NETWORK_ERROR: -1,
} as const;

/**
 * k2 engine error codes — MIRROR of `k2/engine/error.go`, names and values
 * verbatim. Do not "fix" a value here; fix it in the Go file and re-mirror.
 * `services/__tests__/k2-engine-codes.test.ts` parses the Go source and fails
 * on any divergence — but only where the k2 submodule is checked out (not in
 * the webapp CI job). See that file's BLIND SPOT note.
 *
 * Ranges are load-bearing and must never be mixed: 1xx network, 4xx client,
 * 5xx server. Timeout is 108, NOT 408 — it is a network fault.
 */
export const ENGINE_ERROR_CODES = {
  ErrCodeNetworkUnavailable: 101,
  ErrCodeTimeout: 108,
  ErrCodeBadConfig: 400,
  ErrCodeAuthRejected: 401,
  ErrCodePaymentRequired: 402,
  ErrCodeForbidden: 403,
  ErrCodeEnvironmentSetupFailed: 412,
  ErrCodeProtocolError: 502,
  ErrCodeServerUnreachable: 503,
  ErrCodeRuleBundlesUnavailable: 504,
  ErrCodeConnectionFatal: 570,
} as const;

/** One row of the catalog. */
export interface ErrorEntry {
  /** Fully-qualified i18n key, `namespace:dotted.path`. */
  readonly key: string;
  /**
   * English fallback handed to `t()` as `defaultValue`. Belt-and-braces only:
   * the coverage test already requires `key` to resolve in all 7 locales, so
   * this should never render. It exists so a locale file lost in a bad merge
   * degrades to English rather than to a raw key on screen.
   */
  readonly defaultValue: string;
}

/**
 * Codes that are deliberately silent — logged, never shown. Listing a code here
 * is the ONLY way to keep it out of API_ERROR_CATALOG without failing the
 * coverage test, so the omission has to be a decision rather than an oversight.
 */
export const LOG_ONLY_CODES: readonly number[] = [
  ERROR_CODES.IAP_FINISH_FAILED,   // 592 — StoreKit finish() failure, user already charged & entitled
  ERROR_CODES.IAP_NOT_AVAILABLE,   // 593 — StoreKit unavailable; caller renders its own affordance
];

/** Rendered when a code matches no API catalog row and no caller default. */
export const UNKNOWN_ERROR_KEY = 'common:common.unknownError';
/** Rendered when a code matches no ENGINE catalog row. */
export const UNKNOWN_ENGINE_ERROR_KEY = 'common:errors.unknown';

/**
 * API / Center-response domain. Keyed by `SResponse.code`.
 * Reached through `getErrorMessage()` / `handleResponseError()`.
 */
export const API_ERROR_CATALOG: Readonly<Record<number, ErrorEntry>> = {
  [ERROR_CODES.SUCCESS]: { key: 'common:common.success', defaultValue: 'Success' },

  // === Backend API codes (sync with api/response.go) ===
  [ERROR_CODES.INVALID_OPERATION]: { key: 'common:errors.client.badRequest', defaultValue: 'Invalid operation' },
  [ERROR_CODES.UNAUTHORIZED]: { key: 'auth:auth.unauthorized', defaultValue: 'Authentication required' },
  [ERROR_CODES.PAYMENT_REQUIRED]: { key: 'common:errors.client.paymentRequired', defaultValue: 'Membership expired, please renew' },
  [ERROR_CODES.FORBIDDEN]: { key: 'auth:auth.forbidden', defaultValue: 'Permission denied' },
  [ERROR_CODES.NOT_FOUND]: { key: 'common:common.notFound', defaultValue: 'Resource not found' },
  [ERROR_CODES.NOT_SUPPORTED]: { key: 'common:errors.client.notSupported', defaultValue: 'Feature not supported' },
  [ERROR_CODES.UPGRADE_REQUIRED]: { key: 'common:errors.client.upgradeRequired', defaultValue: 'Please upgrade to the latest version' },
  [ERROR_CODES.CONFLICT]: { key: 'common:errors.client.conflict', defaultValue: 'Operation conflict, please try again' },
  // 422 also carries two message-routed sub-cases (password strength); see
  // getErrorMessage(). This row is the fall-through.
  [ERROR_CODES.INVALID_ARGUMENT]: { key: 'common:errors.client.invalidArgument', defaultValue: 'Invalid parameters' },
  [ERROR_CODES.TOO_EARLY]: { key: 'common:errors.client.tooEarly', defaultValue: 'Please wait a moment and try again' },
  [ERROR_CODES.TOO_MANY_REQUESTS]: { key: 'common:errors.client.tooManyRequests', defaultValue: 'Too many requests, please try later' },
  [ERROR_CODES.INTERNAL_SERVER_ERROR]: { key: 'common:common.serverError', defaultValue: 'Internal server error' },
  [ERROR_CODES.SERVICE_UNAVAILABLE]: { key: 'common:errors.server.unavailable', defaultValue: 'Server unavailable' },

  // Backend rejects this BEFORE issuing or mailing a verification code, so the
  // user never receives an email. The copy must say so — the old generic 422
  // ("参数错误") read as a transient glitch and users retried for hours.
  [ERROR_CODES.EMAIL_ALREADY_IN_USE]: { key: 'auth:updateEmail.emailAlreadyInUse', defaultValue: 'This email is already used by another account' },

  // Backend custom codes (400000+ range)
  [ERROR_CODES.INVALID_CAMPAIGN_CODE]: { key: 'purchase:purchase.invalidCampaignCode', defaultValue: 'Invalid promo code' },
  [ERROR_CODES.INVALID_CLIENT_CLOCK]: { key: 'common:errors.client.invalidClock', defaultValue: 'Device clock is incorrect, please adjust' },
  [ERROR_CODES.INVALID_VERIFICATION_CODE]: { key: 'auth:auth.invalidVerificationCode', defaultValue: 'Invalid verification code' },
  [ERROR_CODES.VERIFICATION_CODE_EXPIRED]: { key: 'auth:auth.verificationCodeExpired', defaultValue: 'Verification code expired or not sent. Please request a new one.' },
  [ERROR_CODES.INVALID_INVITE_CODE]: { key: 'auth:auth.inviteCodeIncorrect', defaultValue: 'Invalid invite code' },
  [ERROR_CODES.SELF_INVITATION]: { key: 'common:errors.client.selfInvitation', defaultValue: 'Cannot use your own invite code' },
  // Wrong password / unknown email / no-password-set all share this code (the
  // backend keeps it generic to prevent email enumeration). Show a specific,
  // actionable message that still does NOT reveal which field was wrong —
  // never the vague loginFailed, which users read as an unspecified error.
  [ERROR_CODES.INVALID_CREDENTIALS]: { key: 'auth:auth.invalidCredentials', defaultValue: 'Incorrect email or password' },
  [ERROR_CODES.LICENSE_KEY_NOT_FOUND]: { key: 'common:errors.client.licenseKeyNotFound', defaultValue: 'License key not found' },
  [ERROR_CODES.LICENSE_KEY_USED]: { key: 'common:errors.client.licenseKeyUsed', defaultValue: 'License key already used' },
  [ERROR_CODES.LICENSE_KEY_EXPIRED]: { key: 'common:errors.client.licenseKeyExpired', defaultValue: 'License key expired' },
  [ERROR_CODES.LICENSE_KEY_NOT_MATCH]: { key: 'common:errors.client.licenseKeyNotMatch', defaultValue: 'Not eligible for this license key' },
  // 400011: the ACCOUNT already redeemed a key (globally, or another key from
  // the same batch) — distinct from 400008, where the KEY was consumed by
  // someone else. Anti-abuse limit, not a bad key.
  [ERROR_CODES.LICENSE_KEY_ALREADY_REDEEMED]: { key: 'common:errors.client.licenseKeyAlreadyRedeemed', defaultValue: 'You have already redeemed a license key (one per account)' },
  [ERROR_CODES.PROXY_MEMBERS_DEPRECATED]: { key: 'common:errors.client.proxyMembersDeprecated', defaultValue: 'Proxy member management has been removed. Specify the recipient at {{brandDomain}}/purchase during checkout.' },

  // Tier system error codes
  [ERROR_CODES.TIER_MISMATCH]: { key: 'common:errors.client.tierMismatch', defaultValue: 'Your current tier cannot purchase this plan. Please contact support to switch tiers.' },
  [ERROR_CODES.PROXY_PURCHASE_DEPRECATED]: { key: 'common:errors.client.proxyPurchaseDeprecated', defaultValue: 'Proxy purchase is no longer supported. Please ask the recipient to purchase with their own account.' },

  // Router / device-class error codes
  [ERROR_CODES.PLAN_NO_ROUTER]: { key: 'auth:auth.planNoRouter', defaultValue: 'Your plan does not include router support' },
  [ERROR_CODES.ROUTER_DEVICE_LIMIT]: { key: 'auth:auth.routerLimitReached', defaultValue: 'Router device limit reached' },
  [ERROR_CODES.DEVICE_CLASS_MISMATCH]: { key: 'auth:auth.deviceClassMismatch', defaultValue: 'This device class is not allowed for your plan' },
  [ERROR_CODES.INVALID_CLIENT_CLASS]: { key: 'auth:auth.invalidClientClass', defaultValue: 'Unsupported client type' },
  // 403003: account was born on the other brand — baked-brand clients should
  // never see this except with restored/stale token storage.
  [ERROR_CODES.BRAND_MISMATCH]: { key: 'auth:auth.brandMismatch', defaultValue: 'This account belongs to a different app' },
  // 405001: payment channel not allowed for this brand (e.g. WordGate on an
  // overleap account). User-actionable: buy on the brand website.
  [ERROR_CODES.PAYMENT_CHANNEL_UNAVAILABLE]: { key: 'purchase:purchase.paymentChannelUnavailable', defaultValue: 'This payment method is unavailable for your account' },

  // 网络错误 (100-109)
  [ERROR_CODES.NETWORK_TIMEOUT]: { key: 'common:errors.network.timeout', defaultValue: 'Network request timed out' },
  [ERROR_CODES.NETWORK_UNREACHABLE]: { key: 'common:errors.network.unreachable', defaultValue: 'Network unreachable' },
  [ERROR_CODES.NETWORK_RESET]: { key: 'common:errors.network.reset', defaultValue: 'Connection reset' },
  [ERROR_CODES.NETWORK_DNS]: { key: 'common:errors.network.dns', defaultValue: 'DNS resolution failed' },
  [ERROR_CODES.NETWORK_TLS]: { key: 'common:errors.network.tls', defaultValue: 'Secure connection failed' },
  [ERROR_CODES.NETWORK_REFUSED]: { key: 'common:errors.network.refused', defaultValue: 'Connection refused' },

  // 服务器相关错误 (110-119)
  [ERROR_CODES.SERVER_UNAVAILABLE]: { key: 'common:errors.server.unavailable', defaultValue: 'Server unavailable' },
  [ERROR_CODES.SERVER_OVERLOAD]: { key: 'common:errors.server.overload', defaultValue: 'Server overloaded' },
  [ERROR_CODES.SERVER_MAINTENANCE]: { key: 'common:errors.server.maintenance', defaultValue: 'Server under maintenance' },

  // Engine errors from k2 core
  [ERROR_CODES.RULE_BUNDLES_UNAVAILABLE]: { key: 'common:errors.engine.ruleBundlesUnavailable', defaultValue: 'Failed to download routing rules. Check network and retry.' },

  // VPN 服务相关错误 (511-519)
  [ERROR_CODES.VPN_START_FAILED]: { key: 'common:errors.vpn.startFailed', defaultValue: 'Failed to start service' },
  [ERROR_CODES.VPN_RECONNECT_FAILED]: { key: 'common:errors.vpn.reconnectFailed', defaultValue: 'Failed to reconnect' },
  [ERROR_CODES.VPN_TIMEOUT]: { key: 'common:errors.vpn.timeout', defaultValue: 'Operation timed out' },

  // 网络修复相关错误 (520-529)
  [ERROR_CODES.NETWORK_REPAIR_FAILED]: { key: 'common:errors.network.repairFailed', defaultValue: 'Network repair failed' },
  [ERROR_CODES.NETWORK_REPAIR_DNS]: { key: 'common:errors.network.repairDNS', defaultValue: 'DNS repair failed' },
  [ERROR_CODES.NETWORK_REPAIR_ROUTE]: { key: 'common:errors.network.repairRoute', defaultValue: 'Route repair failed' },
  [ERROR_CODES.NETWORK_REPAIR_PRE_FAILED]: { key: 'common:errors.network.repairPreFailed', defaultValue: 'Failed to prepare for network repair' },

  // 连接错误 (570-579)
  [ERROR_CODES.CONNECTION_FATAL]: { key: 'common:errors.vpn.connectionFatal', defaultValue: 'Connection failed' },
  [ERROR_CODES.ALL_ADDRS_FAILED]: { key: 'common:errors.vpn.allAddrsFailed', defaultValue: 'All server addresses failed' },
  [ERROR_CODES.NO_TUNNEL_AVAILABLE_AUTO]: { key: 'dashboard:auto.noTunnelAvailable', defaultValue: 'No server available' },
  [ERROR_CODES.NO_TUNNEL_AVAILABLE_FILTERED]: { key: 'dashboard:auto.allExcluded', defaultValue: 'All available servers are excluded by your filter. Adjust the country/region filter.' },

  // VPN 权限错误 (580-589)
  [ERROR_CODES.VPN_PERMISSION_DENIED]: { key: 'common:errors.vpn.permissionDenied', defaultValue: 'VPN permission denied. Please enable VPN in system settings.' },

  // IAP (iOS StoreKit) 错误 (590-599). 592/593 are log-only — see LOG_ONLY_CODES.
  [ERROR_CODES.IAP_PURCHASE_FAILED]: { key: 'purchase:purchase.iap.purchaseFailed', defaultValue: 'Purchase failed, please try again' },
  [ERROR_CODES.IAP_VERIFY_FAILED]: { key: 'purchase:purchase.iap.verifyFailed', defaultValue: 'Could not verify your purchase, please try again' },

  // 认证相关错误 (530-539)
  [ERROR_CODES.LOGOUT_FAILED]: { key: 'common:errors.auth.logoutFailed', defaultValue: 'Logout failed' },
  [ERROR_CODES.TOKEN_REFRESH_FAILED]: { key: 'common:errors.auth.tokenRefreshFailed', defaultValue: 'Token refresh failed' },

  // 资源/隧道相关错误 (540-549)
  [ERROR_CODES.TUNNEL_LIST_FAILED]: { key: 'common:errors.tunnel.listFailed', defaultValue: 'Failed to get tunnel list' },
  [ERROR_CODES.TUNNEL_CONNECT_FAILED]: { key: 'common:errors.tunnel.connectFailed', defaultValue: 'Failed to connect to tunnel' },

  // Action 执行相关错误 (550-559)
  [ERROR_CODES.ACTION_TIMEOUT]: { key: 'common:errors.action.timeout', defaultValue: 'Operation timed out' },
  [ERROR_CODES.ACTION_PARSE_FAILED]: { key: 'common:errors.action.parseFailed', defaultValue: 'Request parsing failed' },

  // API 请求相关错误 (560-569)
  [ERROR_CODES.API_REQUEST_FAILED]: { key: 'common:errors.api.requestFailed', defaultValue: 'API request failed' },
  [ERROR_CODES.API_RESPONSE_FAILED]: { key: 'common:errors.api.responseFailed', defaultValue: 'API response parsing failed' },

  // cloudApi 网络层错误 (fetch 失败、超时)
  [ERROR_CODES.CLOUD_NETWORK_ERROR]: { key: 'common:errors.network.unreachable', defaultValue: 'Network unreachable' },
};

/**
 * VPN / engine domain. Keyed by `ControlError.code` as delivered by
 * `_k2.run()` and the status stream. Reached through `getErrorI18nKey()`.
 *
 * Covers every code in ENGINE_ERROR_CODES plus the webapp- and bridge-
 * synthesized codes that travel the SAME channel (BACKEND_ERROR dispatches in
 * connection.store.ts, code 580 from capacitor-k2.ts).
 */
export const ENGINE_ERROR_CATALOG: Readonly<Record<number, ErrorEntry>> = {
  // --- k2/engine/error.go ---
  [ENGINE_ERROR_CODES.ErrCodeNetworkUnavailable]: { key: 'common:errors.network.unavailable', defaultValue: 'No network connection. Check your device network and try again.' },
  [ENGINE_ERROR_CODES.ErrCodeTimeout]: { key: 'common:errors.network.timeout', defaultValue: 'Connection timed out, please check your network' },
  [ENGINE_ERROR_CODES.ErrCodeBadConfig]: { key: 'common:errors.config.badConfig', defaultValue: 'Configuration error, please check the server address' },
  [ENGINE_ERROR_CODES.ErrCodeAuthRejected]: { key: 'common:errors.vpn.authFailed', defaultValue: 'Authentication failed, please sign in again' },
  [ENGINE_ERROR_CODES.ErrCodePaymentRequired]: { key: 'common:errors.vpn.membershipExpired', defaultValue: 'Membership expired, please renew' },
  [ENGINE_ERROR_CODES.ErrCodeForbidden]: { key: 'common:errors.vpn.forbidden', defaultValue: 'Server certificate verification failed' },
  [ENGINE_ERROR_CODES.ErrCodeEnvironmentSetupFailed]: { key: 'common:errors.vpn.environmentSetupFailed', defaultValue: 'Local network setup failed. Check system permissions and try again.' },
  [ENGINE_ERROR_CODES.ErrCodeProtocolError]: { key: 'common:errors.vpn.protocolError', defaultValue: 'Protocol handshake failed, please try another node' },
  [ENGINE_ERROR_CODES.ErrCodeServerUnreachable]: { key: 'common:errors.network.unreachable', defaultValue: 'Server unreachable, please check your network connection' },
  [ENGINE_ERROR_CODES.ErrCodeRuleBundlesUnavailable]: { key: 'common:errors.engine.ruleBundlesUnavailable', defaultValue: 'Failed to download routing rules. Check network and retry.' },
  [ENGINE_ERROR_CODES.ErrCodeConnectionFatal]: { key: 'common:errors.vpn.connectionFatal', defaultValue: 'Connection failed, please retry or switch node' },

  // --- webapp / bridge synthesized, same channel ---
  [ERROR_CODES.VPN_START_FAILED]: { key: 'common:errors.vpn.startFailed', defaultValue: 'Failed to start service' },
  [ERROR_CODES.VPN_RECONNECT_FAILED]: { key: 'common:errors.vpn.reconnectFailed', defaultValue: 'Failed to reconnect' },
  [ERROR_CODES.VPN_TIMEOUT]: { key: 'common:errors.vpn.timeout', defaultValue: 'Operation timed out' },
  [ERROR_CODES.ALL_ADDRS_FAILED]: { key: 'common:errors.vpn.allAddrsFailed', defaultValue: 'All server addresses failed' },
  [ERROR_CODES.NO_TUNNEL_AVAILABLE_AUTO]: { key: 'dashboard:auto.noTunnelAvailable', defaultValue: 'No server available' },
  // 573 — auto-pick had candidates but the user's country filter removed them
  // all. The copy has to name the fix, otherwise the user just sees a dead
  // Connect button. Dispatched by connection.store.ts.
  [ERROR_CODES.NO_TUNNEL_AVAILABLE_FILTERED]: { key: 'dashboard:auto.allExcluded', defaultValue: 'All available servers are excluded by your filter. Adjust the country/region filter.' },
  [ERROR_CODES.VPN_PERMISSION_DENIED]: { key: 'common:errors.vpn.permissionDenied', defaultValue: 'VPN permission denied. Please enable VPN in system settings.' },
};

/**
 * Engine codes that should surface the "check your network" affordance.
 *
 * NOT the same set as Go's `CategoryNetwork` (101, 108). 503 is CategoryServer
 * in the engine — the blame lies with the node — but from the user's seat "the
 * server can't be reached" and "the network is down" call for the same action,
 * and ServiceAlert has shown the network banner for 503 since it was written.
 * Keeping the sets distinct is deliberate: this one answers "which banner do we
 * show", not "whose fault is it".
 */
export const ENGINE_NETWORK_ERROR_CODES: readonly number[] = [
  ENGINE_ERROR_CODES.ErrCodeNetworkUnavailable, // 101
  ENGINE_ERROR_CODES.ErrCodeTimeout,            // 108
  ENGINE_ERROR_CODES.ErrCodeServerUnreachable,  // 503
];
