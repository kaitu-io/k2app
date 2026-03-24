/**
 * Error Code Utilities
 * Maps error codes to user-friendly messages
 */

import { TFunction } from 'i18next';

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

  // VPN 服务相关错误 (510-519)
  VPN_STOP_FAILED: 510,
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

  // VPN 权限错误 (580-589)
  VPN_PERMISSION_DENIED: 580,

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
 * @param code - Error code from response
 * @param t - i18next translation function
 * @param defaultMessage - Default message if no mapping found
 * @returns Localized error message
 */
export function getErrorMessage(
  code: number,
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

    // VPN 服务相关错误 (510-519)
    case ERROR_CODES.VPN_STOP_FAILED:
      return t('common:errors.vpn.stopFailed', 'Failed to stop service');
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

    // VPN 权限错误 (580-589)
    case ERROR_CODES.VPN_PERMISSION_DENIED:
      return t('common:errors.vpn.permissionDenied', 'VPN permission denied. Please enable VPN in system settings.');

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

  // Prefer code-based message over response message
  const errorMessage = getErrorMessage(code, t, message || defaultMessage);
  throw new Error(errorMessage);
}
