// Type definitions for k2 VPN control protocol
// Canonical source: k2/engine/error.go (Go engine error codes)

// ==================== Error Code Constants ====================
//
// The numeric values are NOT re-declared here. They live in
// `utils/errorCatalog.ts` as ENGINE_ERROR_CODES, a mirror of
// `k2/engine/error.go` that `services/__tests__/k2-engine-codes.test.ts`
// checks against the Go source. This module used to keep its own hand-typed
// copy, which is how `ErrCodeTimeout = 408` survived here for months after the
// engine settled on 108 — breaking both the error copy and isNetworkError().
// The aliases below exist only so existing imports keep resolving.

import {
  ENGINE_ERROR_CATALOG,
  ENGINE_ERROR_CODES,
  ENGINE_NETWORK_ERROR_CODES,
  ERROR_CODES,
  UNKNOWN_ENGINE_ERROR_KEY,
} from '../utils/errorCatalog';

// Config errors
export const ErrCodeBadConfig = ENGINE_ERROR_CODES.ErrCodeBadConfig;                 // 400 Invalid wire URL, missing auth, bad scheme

// Auth errors
export const ErrCodeUnauthorized = ENGINE_ERROR_CODES.ErrCodeAuthRejected;           // 401 Server rejected authentication
export const ErrCodeMembershipExpired = ENGINE_ERROR_CODES.ErrCodePaymentRequired;   // 402 Membership expired

// Certificate/pin errors
export const ErrCodeForbidden = ENGINE_ERROR_CODES.ErrCodeForbidden;                 // 403 Certificate pin mismatch, blocked CA

// Local environment
export const ErrCodeEnvironmentSetupFailed = ENGINE_ERROR_CODES.ErrCodeEnvironmentSetupFailed; // 412

// Network
export const ErrCodeNetworkUnavailable = ENGINE_ERROR_CODES.ErrCodeNetworkUnavailable; // 101 No route / network down
export const ErrCodeTimeout = ENGINE_ERROR_CODES.ErrCodeTimeout;                     // 108 Connection or handshake timeout — NOT 408

// TLS/Protocol errors
export const ErrCodeProtocolError = ENGINE_ERROR_CODES.ErrCodeProtocolError;         // 502 TLS handshake failure, QUIC dial failure

// Server unreachable
export const ErrCodeServerUnreachable = ENGINE_ERROR_CODES.ErrCodeServerUnreachable; // 503 TCP dial failed, connection refused

// Rule bundle dependency
export const ErrCodeRuleBundlesUnavailable = ENGINE_ERROR_CODES.ErrCodeRuleBundlesUnavailable; // 504

// Fallback
export const ErrCodeConnectionFatal = ENGINE_ERROR_CODES.ErrCodeConnectionFatal;     // 570 Unclassified connection error

// Permission (frontend-only, mobile VPN permission denied)
export const ErrCodeVPNPermissionDenied = ERROR_CODES.VPN_PERMISSION_DENIED;         // 580

/**
 * Whether the error should surface the "check your network" affordance
 * (ServiceAlert's network banner).
 *
 * Derived from ENGINE_NETWORK_ERROR_CODES rather than an inline literal list —
 * the inline version said `code === 408 || code === 503` and therefore answered
 * `false` for every real engine timeout. Read that constant's doc comment for
 * why this set is intentionally NOT Go's CategoryNetwork.
 */
export function isNetworkError(code: number): boolean {
  return ENGINE_NETWORK_ERROR_CODES.includes(code);
}

/**
 * Map an engine/VPN error code to a fully-qualified i18n key (`ns:path`).
 *
 * Returns a FULLY QUALIFIED key — callers pass it straight to `t()`. It used to
 * return a bare key that call sites prefixed with `'common:'`, which structurally
 * barred any code whose copy lives in another namespace (572/573 live in
 * `dashboard`).
 *
 * Fallback is only reached by a code no layer declares; every code in
 * ENGINE_ERROR_CATALOG is asserted non-fallback by
 * `utils/__tests__/errorCatalog.test.ts`.
 */
export function getErrorI18nKey(code: number): string {
  return ENGINE_ERROR_CATALOG[code]?.key ?? UNKNOWN_ENGINE_ERROR_KEY;
}

/**
 * English fallback for `code`, for use as `t()`'s defaultValue.
 * Returns undefined for codes the catalog does not cover.
 */
export function getErrorDefaultText(code: number): string | undefined {
  return ENGINE_ERROR_CATALOG[code]?.defaultValue;
}

// ==================== VPN 控制 ====================

// Service state enum (matches Go backend)
// 'paused' is a mobile-only, handler-broadcast-only value (engine.state itself
// stays 'connected' — see k2/engine/event.go StatePaused comment). It reaches
// the webapp via both the push event and status queries (k2/engine/engine.go
// buildStatusLocked) while Android/iOS have torn down the tunnel for memory
// conservation. See backendStatusToEvent() for how it's mapped.
export type ServiceState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'disconnecting' | 'error' | 'paused';

/**
 * ControlError 错误信息
 * UI 层根据 Code 决定如何处理：
 * - 400=配置错误 → 提示检查配置
 * - 401=登录失效 → 清除 token，跳转登录
 * - 402=会员过期 → 显示续费提示
 * - 403=证书验证失败 → 提示更换节点
 * - 108=连接超时 → 提示检查网络
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
                          // - 网络/连接错误 (101/108/502/503/570): true，K2 每 5 秒重试
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
 * Matches Go config.RuleConfig JSON tags (snake_case).
 */
export interface RuleConfig {
  global?: boolean;
  rule_url?: string;
  geoip_url?: string;
  antiporn?: boolean;
  porn_url?: string;
  cache_dir?: string;
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
