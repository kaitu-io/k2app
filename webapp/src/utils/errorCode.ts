/**
 * Error Code Utilities
 * Maps error codes to user-friendly messages
 */

import { TFunction } from 'i18next';

import { PASSWORD_MIN_LENGTH } from './password-strength';

/**
 * Error codes — backend API codes synced with api/response.go,
 * plus frontend-only codes for VPN/network/action errors.
 *
 * Constitution: Every backend error code MUST have an entry here
 * and a corresponding case in getErrorMessage().
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
  PROXY_MEMBERS_DEPRECATED: 400012,
  VERIFICATION_CODE_EXPIRED: 400013,

  // Tier system error codes (added 2026-04-20)
  TIER_MISMATCH: 422001,
  PROXY_PURCHASE_DEPRECATED: 422002,

  // Router / device-class error codes (added 2026-05-22)
  PLAN_NO_ROUTER: 402001,
  ROUTER_DEVICE_LIMIT: 403001,
  DEVICE_CLASS_MISMATCH: 403002,
  INVALID_CLIENT_CLASS: 422003,

  // === Frontend-only codes (NOT from backend API) ===

  // 网络错误 (100-109) - 来自 classifyNetworkError
  NETWORK_TIMEOUT: 100,
  NETWORK_UNREACHABLE: 101,
  NETWORK_RESET: 102,
  NETWORK_DNS: 103,
  NETWORK_TLS: 104,
  NETWORK_REFUSED: 105,

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
 * Get error message by error code
 *
 * `message` is the raw backend `response.message`. It is NOT displayed to users
 * (that would violate webapp/CLAUDE.md "API Error Code Constitution"). It is
 * only used to disambiguate a small set of `ErrorInvalidArgument` (422)
 * sub-cases where the backend exposes a stable enum string — currently the
 * password strength validator (`password_too_short` / `password_too_weak`).
 *
 * @param code - Error code from response
 * @param message - Backend response.message (used only for enum routing)
 * @param t - i18next translation function
 * @param defaultMessage - Default message if no mapping found
 * @returns Localized error message
 */
export function getErrorMessage(
  code: number,
  message: string | undefined,
  t: TFunction,
  defaultMessage?: string
): string {
  switch (code) {
    case ERROR_CODES.SUCCESS:
      return t('common:common.success', 'Success');

    // === Backend API codes (sync with api/response.go) ===

    case ERROR_CODES.INVALID_OPERATION:
      return t('common:errors.client.badRequest', 'Invalid operation');

    case ERROR_CODES.UNAUTHORIZED:
      return t('auth:auth.unauthorized', 'Authentication required');

    case ERROR_CODES.PAYMENT_REQUIRED:
      return t('common:errors.client.paymentRequired', 'Membership expired, please renew');

    case ERROR_CODES.FORBIDDEN:
      return t('auth:auth.forbidden', 'Permission denied');

    case ERROR_CODES.NOT_FOUND:
      return t('common:common.notFound', 'Resource not found');

    case ERROR_CODES.NOT_SUPPORTED:
      return t('common:errors.client.notSupported', 'Feature not supported');

    case ERROR_CODES.UPGRADE_REQUIRED:
      return t('common:errors.client.upgradeRequired', 'Please upgrade to the latest version');

    case ERROR_CODES.CONFLICT:
      return t('common:errors.client.conflict', 'Operation conflict, please try again');

    case ERROR_CODES.INVALID_ARGUMENT:
      // Backend's password strength validator returns a stable enum string in
      // `response.message` (`password_too_short` / `password_too_weak`) — this
      // is the ONLY allowed message-based routing per webapp/CLAUDE.md
      // "API Error Code Constitution". See spec
      // `docs/superpowers/specs/2026-05-21-password-login-completion-design.md` §4.5.
      if (message === 'password_too_short') return t('account:password.tooShort', { length: PASSWORD_MIN_LENGTH });
      if (message === 'password_too_weak') return t('account:password.tooWeak');
      return t('common:errors.client.invalidArgument', 'Invalid parameters');

    case ERROR_CODES.TOO_EARLY:
      return t('common:errors.client.tooEarly', 'Please wait a moment and try again');

    case ERROR_CODES.TOO_MANY_REQUESTS:
      return t('common:errors.client.tooManyRequests', 'Too many requests, please try later');

    case ERROR_CODES.INTERNAL_SERVER_ERROR:
      return t('common:common.serverError', 'Internal server error');

    case ERROR_CODES.SERVICE_UNAVAILABLE:
      return t('common:errors.server.unavailable', 'Server unavailable');

    // Backend custom codes (400000+ range)

    case ERROR_CODES.INVALID_CAMPAIGN_CODE:
      return t('purchase:purchase.invalidCampaignCode', 'Invalid promo code');

    case ERROR_CODES.INVALID_CLIENT_CLOCK:
      return t('common:errors.client.invalidClock', 'Device clock is incorrect, please adjust');

    case ERROR_CODES.INVALID_VERIFICATION_CODE:
      return t('auth:auth.invalidVerificationCode', 'Invalid verification code');

    case ERROR_CODES.VERIFICATION_CODE_EXPIRED:
      return t('auth:auth.verificationCodeExpired',
        'Verification code expired or not sent. Please request a new one.');

    case ERROR_CODES.INVALID_INVITE_CODE:
      return t('auth:auth.inviteCodeIncorrect', 'Invalid invite code');

    case ERROR_CODES.SELF_INVITATION:
      return t('common:errors.client.selfInvitation', 'Cannot use your own invite code');

    case ERROR_CODES.INVALID_CREDENTIALS:
      return t('auth:auth.loginFailed', 'Login failed');

    case ERROR_CODES.LICENSE_KEY_NOT_FOUND:
      return t('common:errors.client.licenseKeyNotFound', 'License key not found');
    case ERROR_CODES.LICENSE_KEY_USED:
      return t('common:errors.client.licenseKeyUsed', 'License key already used');
    case ERROR_CODES.LICENSE_KEY_EXPIRED:
      return t('common:errors.client.licenseKeyExpired', 'License key expired');
    case ERROR_CODES.LICENSE_KEY_NOT_MATCH:
      return t('common:errors.client.licenseKeyNotMatch', 'Not eligible for this license key');
    case ERROR_CODES.PROXY_MEMBERS_DEPRECATED:
      return t('common:errors.client.proxyMembersDeprecated', '代付成员管理已下线，请在 kaitu.io/purchase 下单时指定受益方');

    // Tier system error codes
    case ERROR_CODES.TIER_MISMATCH:
      return t('common:errors.client.tierMismatch', '当前档位无法购买此套餐，请联系客服变更档位');
    case ERROR_CODES.PROXY_PURCHASE_DEPRECATED:
      return t('common:errors.client.proxyPurchaseDeprecated', '代付款功能已下线，请让对方使用自己的账号购买');

    // Router / device-class error codes
    case ERROR_CODES.PLAN_NO_ROUTER:
      return t('auth:auth.planNoRouter');
    case ERROR_CODES.ROUTER_DEVICE_LIMIT:
      return t('auth:auth.routerLimitReached');
    case ERROR_CODES.DEVICE_CLASS_MISMATCH:
      return t('auth:auth.deviceClassMismatch');
    case ERROR_CODES.INVALID_CLIENT_CLASS:
      return t('auth:auth.invalidClientClass');

    // 网络错误 (100-109)
    case ERROR_CODES.NETWORK_TIMEOUT:
      return t('common:errors.network.timeout', 'Network request timed out');
    case ERROR_CODES.NETWORK_UNREACHABLE:
      return t('common:errors.network.unreachable', 'Network unreachable');
    case ERROR_CODES.NETWORK_RESET:
      return t('common:errors.network.reset', 'Connection reset');
    case ERROR_CODES.NETWORK_DNS:
      return t('common:errors.network.dns', 'DNS resolution failed');
    case ERROR_CODES.NETWORK_TLS:
      return t('common:errors.network.tls', 'Secure connection failed');
    case ERROR_CODES.NETWORK_REFUSED:
      return t('common:errors.network.refused', 'Connection refused');

    // 服务器相关错误 (110-119)
    case ERROR_CODES.SERVER_UNAVAILABLE:
      return t('common:errors.server.unavailable', 'Server unavailable');
    case ERROR_CODES.SERVER_OVERLOAD:
      return t('common:errors.server.overload', 'Server overloaded');
    case ERROR_CODES.SERVER_MAINTENANCE:
      return t('common:errors.server.maintenance', 'Server under maintenance');

    // Engine errors from k2 core
    case ERROR_CODES.RULE_BUNDLES_UNAVAILABLE:
      return t('common:errors.engine.ruleBundlesUnavailable',
        'Failed to download routing rules. Check network and retry.');

    // VPN 服务相关错误 (511-519)
    case ERROR_CODES.VPN_START_FAILED:
      return t('common:errors.vpn.startFailed', 'Failed to start service');
    case ERROR_CODES.VPN_RECONNECT_FAILED:
      return t('common:errors.vpn.reconnectFailed', 'Failed to reconnect');
    case ERROR_CODES.VPN_TIMEOUT:
      return t('common:errors.vpn.timeout', 'Operation timed out');

    // 网络修复相关错误 (520-529)
    case ERROR_CODES.NETWORK_REPAIR_FAILED:
      return t('common:errors.network.repairFailed', 'Network repair failed');
    case ERROR_CODES.NETWORK_REPAIR_DNS:
      return t('common:errors.network.repairDNS', 'DNS repair failed');
    case ERROR_CODES.NETWORK_REPAIR_ROUTE:
      return t('common:errors.network.repairRoute', 'Route repair failed');
    case ERROR_CODES.NETWORK_REPAIR_PRE_FAILED:
      return t('common:errors.network.repairPreFailed', 'Failed to prepare for network repair');

    // 连接错误 (570-579)
    case ERROR_CODES.CONNECTION_FATAL:
      return t('common:errors.vpn.connectionFatal', 'Connection failed');
    case ERROR_CODES.ALL_ADDRS_FAILED:
      return t('common:errors.vpn.allAddrsFailed', 'All server addresses failed');
    case ERROR_CODES.NO_TUNNEL_AVAILABLE_AUTO:
      return t('dashboard:auto.noTunnelAvailable');

    // VPN 权限错误 (580-589)
    case ERROR_CODES.VPN_PERMISSION_DENIED:
      return t('common:errors.vpn.permissionDenied', 'VPN permission denied. Please enable VPN in system settings.');

    // IAP (iOS StoreKit) 错误 (590-599)
    // 592 (finish) / 593 (not available) are log-only — no user-facing message.
    case ERROR_CODES.IAP_PURCHASE_FAILED:
      return t('purchase:purchase.iap.purchaseFailed', 'Purchase failed, please try again');
    case ERROR_CODES.IAP_VERIFY_FAILED:
      return t('purchase:purchase.iap.verifyFailed', 'Could not verify your purchase, please try again');

    // 认证相关错误 (530-539)
    case ERROR_CODES.LOGOUT_FAILED:
      return t('common:errors.auth.logoutFailed', 'Logout failed');
    case ERROR_CODES.TOKEN_REFRESH_FAILED:
      return t('common:errors.auth.tokenRefreshFailed', 'Token refresh failed');

    // 资源/隧道相关错误 (540-549)
    case ERROR_CODES.TUNNEL_LIST_FAILED:
      return t('common:errors.tunnel.listFailed', 'Failed to get tunnel list');
    case ERROR_CODES.TUNNEL_CONNECT_FAILED:
      return t('common:errors.tunnel.connectFailed', 'Failed to connect to tunnel');

    // Action 执行相关错误 (550-559)
    case ERROR_CODES.ACTION_TIMEOUT:
      return t('common:errors.action.timeout', 'Operation timed out');
    case ERROR_CODES.ACTION_PARSE_FAILED:
      return t('common:errors.action.parseFailed', 'Request parsing failed');

    // API 请求相关错误 (560-569)
    case ERROR_CODES.API_REQUEST_FAILED:
      return t('common:errors.api.requestFailed', 'API request failed');
    case ERROR_CODES.API_RESPONSE_FAILED:
      return t('common:errors.api.responseFailed', 'API response parsing failed');

    // cloudApi 网络层错误 (fetch 失败、超时)
    case ERROR_CODES.CLOUD_NETWORK_ERROR:
      return t('common:errors.network.unreachable', 'Network unreachable');

    default:
      return defaultMessage || t('common:common.unknownError', 'Unknown error');
  }
}

/**
 * Check if response is successful
 * @param code - Error code from response
 * @returns true if success
 */
export function isSuccess(code: number): boolean {
  return code === ERROR_CODES.SUCCESS;
}

/**
 * Handle response error
 * Throws error with appropriate message based on code
 * @param code - Error code from response
 * @param message - Optional message from response
 * @param t - i18next translation function
 * @param defaultMessage - Default error message
 */
export function handleResponseError(
  code: number,
  message: string | undefined,
  t: TFunction,
  defaultMessage: string
): void {
  if (isSuccess(code)) {
    return;
  }

  // Prefer code-based message over response message. `message` is passed through
  // so getErrorMessage can route `ErrorInvalidArgument` sub-cases (e.g. password
  // strength enum) to specific i18n keys; the raw string is never shown to users.
  const errorMessage = getErrorMessage(code, message, t, defaultMessage);
  throw new Error(errorMessage);
}
