// Type definitions for k2 daemon control protocol
// Canonical source: k2/daemon/api.go (Go daemon HTTP API)

// ==================== 错误码常量 ====================
// 与 Go service/core/control/types.go 对齐

// 网络错误（100-109）
export const ErrCodeNetworkTimeout = 100;      // 网络请求超时
export const ErrCodeNetworkUnreachable = 101;  // 网络不可达（无网络连接）
export const ErrCodeNetworkReset = 102;        // 连接被重置
export const ErrCodeNetworkDNS = 103;          // DNS 解析失败
export const ErrCodeNetworkTLS = 104;          // TLS/SSL 握手失败
export const ErrCodeNetworkRefused = 105;      // 连接被拒绝

// 服务器相关错误（110-119）
export const ErrCodeServerUnavailable = 110;   // 服务器不可用
export const ErrCodeServerOverload = 111;      // 服务器过载
export const ErrCodeServerMaintenance = 112;   // 服务器维护中

// 客户端错误（400 系列，与 HTTP 对齐）
export const ErrCodeBadRequest = 400;          // 请求参数错误
export const ErrCodeUnauthorized = 401;        // 未授权
export const ErrCodeForbidden = 403;           // 禁止访问
export const ErrCodeNotFound = 404;            // 资源不存在

// 服务端错误（500 系列，与 HTTP 对齐）
export const ErrCodeInternalError = 500;       // 内部错误
export const ErrCodeNotImplemented = 501;      // 功能未实现

// VPN 服务相关错误 (510-519)
export const ErrCodeVPNStopFailed = 510;       // VPN 停止失败
export const ErrCodeVPNStartFailed = 511;      // VPN 启动失败
export const ErrCodeVPNReconnectFailed = 512;  // VPN 重连失败
export const ErrCodeVPNTimeout = 513;          // VPN 操作超时

// 连接错误 (570-579)
export const ErrCodeConnectionFatal = 570;     // 致命连接错误
export const ErrCodeAllAddrsFailed = 571;      // 所有地址连接失败

// 会员相关 (402)
export const ErrCodeMembershipExpired = 402;   // 会员过期

/**
 * 判断是否为网络相关错误（100-109）
 */
export function isNetworkError(code: number): boolean {
  return code >= 100 && code < 110;
}

/**
 * 判断是否为服务器相关错误（110-119）
 */
export function isServerError(code: number): boolean {
  return code >= 110 && code < 120;
}

/**
 * 判断是否为 VPN 连接错误（510-579）
 */
export function isVPNError(code: number): boolean {
  return (code >= 510 && code < 520) || (code >= 570 && code < 580);
}

/**
 * 判断是否为认证错误（需要重新登录或续费）
 */
export function isAuthError(code: number): boolean {
  return code === 401 || code === 402;
}

/**
 * 获取错误码对应的 i18n 键名
 */
export function getErrorI18nKey(code: number): string {
  const errorMap: Record<number, string> = {
    // 网络错误（100-109）
    [ErrCodeNetworkTimeout]: 'errors.network.timeout',
    [ErrCodeNetworkUnreachable]: 'errors.network.unreachable',
    [ErrCodeNetworkReset]: 'errors.network.reset',
    [ErrCodeNetworkDNS]: 'errors.network.dns',
    [ErrCodeNetworkTLS]: 'errors.network.tls',
    [ErrCodeNetworkRefused]: 'errors.network.refused',
    // 服务器错误（110-119）
    [ErrCodeServerUnavailable]: 'errors.server.unavailable',
    [ErrCodeServerOverload]: 'errors.server.overload',
    [ErrCodeServerMaintenance]: 'errors.server.maintenance',
    // 客户端错误（400系列）
    [ErrCodeBadRequest]: 'errors.client.badRequest',
    [ErrCodeUnauthorized]: 'errors.vpn.authFailed',
    [ErrCodeMembershipExpired]: 'errors.vpn.membershipExpired',
    [ErrCodeForbidden]: 'errors.client.forbidden',
    [ErrCodeNotFound]: 'errors.client.notFound',
    // 服务端错误（500系列）
    [ErrCodeInternalError]: 'errors.server.internal',
    [ErrCodeNotImplemented]: 'errors.server.notImplemented',
    // VPN 服务错误（510-519）
    [ErrCodeVPNStopFailed]: 'errors.vpn.stopFailed',
    [ErrCodeVPNStartFailed]: 'errors.vpn.startFailed',
    [ErrCodeVPNReconnectFailed]: 'errors.vpn.reconnectFailed',
    [ErrCodeVPNTimeout]: 'errors.vpn.timeout',
    // 连接错误（570-579）
    [ErrCodeConnectionFatal]: 'errors.vpn.connectionFatal',
    [ErrCodeAllAddrsFailed]: 'errors.vpn.allAddrsFailed',
  };
  return errorMap[code] || 'errors.unknown';
}

// ==================== VPN 控制 ====================

// Service state enum (matches Go backend)
export type ServiceState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'disconnecting' | 'error';

/**
 * ControlError 错误信息
 * UI 层根据 Code 决定如何处理：
 * - 401=登录失效 → 清除 token，跳转登录
 * - 402=会员过期 → 显示续费提示
 * - 570=连接失败 → 显示连接错误
 * - 571=所有地址失败 → 显示所有地址失败提示
 */
export interface ControlError {
  code: number;    // 错误码
  message: string; // 错误消息
}

/**
 * ComponentStatus represents the initialization status of a single component
 * Aligns with Go backend control.ComponentStatus
 */
export interface ComponentStatus {
  ready: boolean;           // True if component is ready to use
  loading?: boolean;        // True if component is still loading
  error?: string;           // Error message if component failed to load
}

/**
 * InitializationStatus represents the overall app initialization status
 * Aligns with Go backend control.InitializationStatus
 */
export interface InitializationStatus {
  ready: boolean;           // True if all components are ready
  geoip: ComponentStatus;   // GeoIP database status
  rules: ComponentStatus;   // Proxy rules status
  antiblock: ComponentStatus; // Antiblock configuration status
}

export interface StatusResponseData {
  state: ServiceState;    // 详细状态：disconnected, connecting, connected, reconnecting, disconnecting, error
  running: boolean;       // 用户意图：true=用户想运行 VPN, false=用户主动停止
  startAt?: number;       // VPN 启动时间戳（Unix seconds，0 表示未启动）
  error?: ControlError;   // 错误信息（state=error 时有值）
  retrying?: boolean;     // K2 层是否正在重试（仅 state=error 时有意义）
                          // - 网络错误 (570/571): true，K2 每 5 秒重试
                          // - 认证错误 (401/402): false，需用户操作
  serviceVersion?: string; // kaitu-service 版本号（用于检测更新后版本不匹配）
  networkAvailable: boolean; // Whether network is available for VPN connection
  initialization?: InitializationStatus; // App initialization status (GeoIP, Rules, Antiblock)
}

// ==================== 配置管理 ====================
// Note: Legacy types SimpleTunnel, K2Config, TunnelAuth, parseSimpleTunnelURL, buildSimpleTunnelURL
// have been removed. Use active_tunnel URL string directly in config.

// ==================== K2V4 Protocol Config ====================

/**
 * K2V4Config - K2V4 protocol configuration
 * Controls protocol selection and features
 * JSON keys match Go backend: tcp_ws, quic_pcc
 */
export interface K2V4Config {
  /** Enable TCP-WebSocket protocol */
  tcp_ws: boolean;
  /** Enable QUIC-PCC protocol */
  quic_pcc: boolean;
  /** Device UDID (single source of truth, managed by backend) */
  udid?: string;
  /** Auth token (single source of truth, managed by backend) */
  token?: string;
}

// ==================== Rule Config ====================

/**
 * RuleConfig - 代理规则配置
 * 替代旧的 proxyRule 字段
 */
export interface RuleConfig {
  /** Rule type: "chnroute", "global", "gfwlist" */
  type: string;
  /** Anti-porn filter (reserved for future use) */
  antiporn: boolean;
}

// ==================== Tunnel Config ====================

/**
 * TunnelMode - how tunnels are sourced
 */
export type TunnelMode = 'cloud' | 'subscription' | 'self_hosted';

/**
 * TunnelConfig - unified tunnel configuration
 * Replaces active_tunnel and tunnels fields
 */
export interface TunnelConfig {
  /** Source mode: "cloud", "subscription", or "self_hosted" */
  mode: TunnelMode;
  /** Tunnel URLs when mode='cloud' or mode='self_hosted' */
  items?: string[];
  /** Subscription URL when mode='subscription' */
  subscription_url?: string;
}

// ==================== 配置管理 ====================

/** Log configuration */
export interface LogConfig {
  /** Log level: "TRACE", "DEBUG", "INFO", "WARN", "ERROR" */
  level: string;
  /** Log file path (None = stdout) */
  file?: string | null;
}

/**
 * ConfigResponseData - Configuration data from status response
 * Uses snake_case to match Go backend serialization
 */
export interface ConfigResponseData {
  // VPN mode: "tun" or "socks5"
  mode?: string;
  // SOCKS5 proxy address (snake_case)
  socks5_addr?: string;
  // HTTP API listen address (read-only, set by service)
  listen?: string;
  /** Tunnel configuration (replaces active_tunnel and tunnels) */
  tunnel?: TunnelConfig;

  // ==================== Rule Config ====================
  /** Proxy rule configuration */
  rule?: RuleConfig;
  /** K2V4 protocol configuration */
  k2v4?: K2V4Config;
  /** Log configuration */
  log?: LogConfig;

  // ==================== Other Config ====================
  // Enable IPv6
  ipv6?: boolean;
  // Skip TLS verification (for self-hosted servers)
  insecure?: boolean;
}
