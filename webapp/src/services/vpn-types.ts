// Type definitions for k2 VPN control protocol
// Canonical source: k2/engine/error.go (Go engine error codes)

// ==================== Error Code Constants ====================
// Aligned with k2 engine HTTP-aligned error codes
// Source of truth: k2/engine/error.go

// Config errors
export const ErrCodeBadConfig = 400;          // Invalid wire URL, missing auth, bad scheme

// Auth errors
export const ErrCodeUnauthorized = 401;       // Server rejected authentication
export const ErrCodeMembershipExpired = 402;  // Membership expired (Cloud API, not k2)

// Certificate/pin errors
export const ErrCodeForbidden = 403;          // Certificate pin mismatch, blocked CA

// Timeout
export const ErrCodeTimeout = 408;            // Connection or handshake timeout

// TLS/Protocol errors
export const ErrCodeProtocolError = 502;      // TLS handshake failure, QUIC dial failure

// Server unreachable
export const ErrCodeServerUnreachable = 503;  // TCP dial failed, connection refused, network unreachable

// Fallback
export const ErrCodeConnectionFatal = 570;    // Unclassified connection error

/**
 * Whether the error is a network-level error (timeout or unreachable)
 */
export function isNetworkError(code: number): boolean {
  return code === 408 || code === 503;
}

/**
 * Whether the error is a VPN protocol/connection error
 */
export function isVPNError(code: number): boolean {
  return code === 502 || code === 570;
}

/**
 * Whether the error is an auth error (requires re-login or renewal)
 */
export function isAuthError(code: number): boolean {
  return code === 401 || code === 402;
}

/**
 * Map error code to i18n key for user-facing messages
 */
export function getErrorI18nKey(code: number): string {
  const errorMap: Record<number, string> = {
    [ErrCodeBadConfig]: 'errors.config.badConfig',
    [ErrCodeUnauthorized]: 'errors.vpn.authFailed',
    [ErrCodeMembershipExpired]: 'errors.vpn.membershipExpired',
    [ErrCodeForbidden]: 'errors.vpn.forbidden',
    [ErrCodeTimeout]: 'errors.network.timeout',
    [ErrCodeProtocolError]: 'errors.vpn.protocolError',
    [ErrCodeServerUnreachable]: 'errors.network.unreachable',
    [ErrCodeConnectionFatal]: 'errors.vpn.connectionFatal',
  };
  return errorMap[code] || 'errors.unknown';
}

// ==================== VPN 控制 ====================

// Service state enum (matches Go backend)
export type ServiceState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'disconnecting' | 'error';

/**
 * ControlError 错误信息
 * UI 层根据 Code 决定如何处理：
 * - 400=配置错误 → 提示检查配置
 * - 401=登录失效 → 清除 token，跳转登录
 * - 402=会员过期 → 显示续费提示
 * - 403=证书验证失败 → 提示更换节点
 * - 408=连接超时 → 提示检查网络
 * - 502=协议握手失败 → 提示更换节点
 * - 503=服务器不可达 → 提示检查网络
 * - 570=连接失败 → 显示连接错误
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
                          // - 网络/连接错误 (408/502/503/570): true，K2 每 5 秒重试
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
