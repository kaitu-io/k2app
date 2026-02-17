/**
 * Error Code Utilities
 * Maps error codes to user-friendly messages
 */

import { TFunction } from 'i18next';

/**
 * Standard error codes from backend
 * 与 Go 后端 types.go 中的 ErrCode* 常量保持同步
 */
export const ERROR_CODES = {
  SUCCESS: 0,
  INVALID_VERIFICATION_CODE: 400003,
  UNAUTHORIZED: 401,
  FORBIDDEN: 403,
  NOT_FOUND: 404,
  INTERNAL_SERVER_ERROR: 500,
  NOT_IMPLEMENTED: 501,

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
  NETWORK_REPAIR_FAILED: 520,     // 网络修复失败
  NETWORK_REPAIR_DNS: 521,        // DNS 修复失败
  NETWORK_REPAIR_ROUTE: 522,      // 路由修复失败
  NETWORK_REPAIR_PRE_FAILED: 523, // 网络修复前置操作失败

  // 连接错误 (570-579)
  CONNECTION_FATAL: 570,          // 致命连接错误
  ALL_ADDRS_FAILED: 571,          // 所有地址连接失败

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

    case ERROR_CODES.INVALID_VERIFICATION_CODE:
      return t('auth:auth.invalidVerificationCode', 'Invalid verification code');

    case ERROR_CODES.UNAUTHORIZED:
      return t('auth:auth.unauthorized', 'Authentication required');

    case ERROR_CODES.FORBIDDEN:
      return t('auth:auth.forbidden', 'Permission denied');

    case ERROR_CODES.NOT_FOUND:
      return t('common:common.notFound', 'Resource not found');

    case ERROR_CODES.INTERNAL_SERVER_ERROR:
      return t('common:common.serverError', 'Internal server error');

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
      return t('common:errors.vpn.stopFailed', 'Failed to stop VPN');
    case ERROR_CODES.VPN_START_FAILED:
      return t('common:errors.vpn.startFailed', 'Failed to start VPN');
    case ERROR_CODES.VPN_RECONNECT_FAILED:
      return t('common:errors.vpn.reconnectFailed', 'Failed to reconnect VPN');
    case ERROR_CODES.VPN_TIMEOUT:
      return t('common:errors.vpn.timeout', 'VPN operation timed out');

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
