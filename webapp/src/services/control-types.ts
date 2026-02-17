/**
 * Control Protocol Types
 * Type definitions aligned with Rust service (kaitu-control)
 *
 * Canonical Rust sources:
 * - rust/crates/kaitu-core/src/types.rs (core types)
 * - rust/crates/kaitu-core/src/config.rs (Config struct)
 * - rust/client/kaitu-control/src/actions.rs (action router)
 */

// Type definitions aligned with Rust service protocol

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

export interface StartParams {
  mode?: 'socks5' | 'tun';
  socksPort?: number;
}

export interface StartResponseData {
  startAt?: number;
}

export interface StopParams {}

export interface StopResponseData {}

export interface StatusParams {}

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
 * ServiceMetrics runtime metrics for monitoring
 * Used to detect resource leaks (goroutines, memory)
 *
 * Memory fields are provided in both MB (legacy) and KB (precise) for compatibility.
 * KB fields provide better precision for memory optimization targeting 30MB on iOS.
 */
export interface ServiceMetrics {
  goroutines: number; // Current number of goroutines

  // Legacy MB fields (for backward compatibility)
  heapAllocMB: number; // Heap memory allocated (MB)
  heapSysMB: number;   // Heap memory obtained from OS (MB)

  // Precise KB fields (for memory optimization)
  heapAllocKB: number;  // Heap memory allocated (KB)
  heapSysKB: number;    // Heap memory obtained from OS (KB)
  heapInuseKB: number;  // Heap memory in use spans (KB)
  heapIdleKB: number;   // Heap memory idle, can be released (KB)
  stackInuseKB: number; // Stack memory in use (KB)

  // GC metrics
  numGC: number;         // Number of completed GC cycles
  lastGCTimeSec: number; // Last GC time (Unix seconds)
  lastGCPauseUs: number; // Last GC pause duration (microseconds)
  nextGCKB: number;      // Heap size target for next GC (KB)
}

/**
 * MemoryDump detailed memory breakdown for optimization debugging
 * Provides comprehensive runtime.MemStats data organized by category
 */
export interface MemoryDump {
  // Goroutines
  goroutines: number; // Current number of goroutines

  // Heap memory (KB) - main optimization target
  heapAllocKB: number;    // Bytes allocated and in use
  heapSysKB: number;      // Bytes obtained from OS
  heapIdleKB: number;     // Bytes in idle spans (can be released)
  heapInuseKB: number;    // Bytes in in-use spans
  heapReleasedKB: number; // Bytes released to OS
  heapObjects: number;    // Number of allocated objects

  // Stack memory (KB)
  stackInuseKB: number; // Bytes in stack spans
  stackSysKB: number;   // Bytes obtained from OS for stacks

  // Off-heap memory (KB) - runtime overhead
  mspanInuseKB: number;  // Bytes in mspan structures
  mspanSysKB: number;    // Bytes obtained from OS for mspan
  mcacheInuseKB: number; // Bytes in mcache structures
  mcacheSysKB: number;   // Bytes obtained from OS for mcache
  buckHashSysKB: number; // Bytes for profiling bucket hash table
  gcSysKB: number;       // Bytes for GC metadata
  otherSysKB: number;    // Other system allocations

  // Total system memory (KB)
  totalSysKB: number; // Total bytes obtained from OS

  // GC metrics
  numGC: number;         // Completed GC cycles
  lastGCPauseUs: number; // Last GC pause (microseconds)
  nextGCKB: number;      // Heap size target for next GC
  gcCPUPercent: number;  // GC CPU usage percentage

  // Allocator statistics
  totalAllocKB: number; // Cumulative bytes allocated
  mallocs: number;      // Cumulative malloc count
  frees: number;        // Cumulative free count
}

/**
 * ForceGCResult result of force_gc action showing memory changes
 */
export interface ForceGCResult {
  beforeHeapAllocKB: number; // Heap before GC (KB)
  afterHeapAllocKB: number;  // Heap after GC (KB)
  beforeHeapIdleKB: number;  // Idle heap before GC (KB)
  afterHeapIdleKB: number;   // Idle heap after GC (KB)
  freedKB: number;           // Memory freed by GC (KB)
  releasedToOSKB: number;    // Memory released to OS (KB)
  numGC: number;             // GC cycle count after operation
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

// DNS resolution mode
export type DNSMode = 'fake-ip' | 'real-ip';

// ==================== K2V4 Protocol Config ====================

/**
 * K2V4Config - K2V4 protocol configuration
 * Controls protocol selection and features
 * JSON keys match Rust backend: tcp_ws, quic_pcc
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

// ==================== 隧道评估 ====================

/** Input tunnel data for evaluation (snake_case for Rust compatibility) */
export interface TunnelInput {
  /** Tunnel domain identifier */
  domain: string;
  /** Node load score (0-100) */
  node_load: number;
  /** Traffic quota usage percentage */
  traffic_usage_percent: number;
  /** Bandwidth usage percentage */
  bandwidth_usage_percent: number;
  /** Route type for user -> server direction */
  upstream_route_type?: string | null;
  /** Route type for server -> user direction */
  downstream_route_type?: string | null;
}

/** Output for a single evaluated tunnel (snake_case from Rust) */
export interface EvaluatedTunnelOutput {
  /** Tunnel domain */
  domain: string;
  /** Final score after adjustments */
  final_score: number;
  /** Route quality category */
  route_quality: string;
  /** Whether the tunnel is overloaded */
  is_overloaded: boolean;
}

/** Response with evaluated tunnels (snake_case from Rust) */
export interface EvaluateTunnelsResponse {
  /** Evaluated tunnels sorted by recommendation order */
  evaluated_tunnels: EvaluatedTunnelOutput[];
  /** Recommended tunnel domain (first in sorted list) */
  recommended_domain?: string;
  /** Whether relay fallback was triggered */
  should_use_relay: boolean;
  /** Reason for relay fallback (if triggered) */
  relay_reason?: string;
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
 * ConfigResponseData - Configuration data from get_config/set_config
 * Uses snake_case to match Rust/Go backend serialization
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
  // DNS mode: "fake-ip" or "real-ip"
  dns_mode?: string;
  // Skip TLS verification (for self-hosted servers)
  insecure?: boolean;
}

export interface GetConfigParams {}

export interface SetConfigParams extends Partial<ConfigResponseData> {}

// ==================== 认证管理 ====================

export interface RegisterDeviceParams {
  inviteCode?: string;
}

export interface GetAuthCodeParams {
  email: string;
  language?: string;
}

export interface LoginParams {
  email: string;
  verificationCode: string;
  remark: string;
  inviteCode?: string;
  language?: string;
}

export interface AuthStatusChangeData {
  isAuthenticated: boolean;
  email?: string;
  deviceID?: string;
}

export interface GetAuthStatusParams {}

export interface LogoutParams {}

// ==================== 存储管理 ====================

export interface StorageSetParams {
  key: string;
  value: any;
}

export interface StorageGetParams {
  key: string;
}

export interface StorageDeleteParams {
  key: string;
}

// ==================== Self Hosted Tunnel Management ====================
// Removed: Backend actions deleted, use set_config({ active_tunnel: "url" }) instead

// ==================== 系统信息 ====================

export interface VersionParams {}

export interface VersionResponseData {
  version: string;
  gitCommit?: string;
  buildTime?: string;
}

// ==================== Developer Config ====================
// Removed: Use set_config({ log: { level: "TRACE" } }) instead

// ==================== API 请求 ====================

export interface ApiRequestParams {
  url: string;
  method: 'GET' | 'POST' | 'PUT' | 'DELETE';
  headers?: Record<string, string>;
  body?: any;
}

export interface ApiRequestResponseData {
  code: number;
  message: string;
  data: any;
}

// ==================== 网络与测速 ====================

export interface SpeedtestParams {
  forceDirect?: boolean;
}

export interface SpeedtestResponseData {
  started: boolean;
  forced_direct: boolean;
}

// 测速状态（轮询用）
export type SpeedtestStatusType = 'idle' | 'running' | 'completed' | 'error';

// 测速进度
export interface SpeedtestProgress {
  stage: string;
  message: string;
  percentage: number;
  current_speed: number;
}

// 测速结果
export interface SpeedtestResult {
  success: boolean;
  server_name?: string;
  server_id?: string;
  latency_ms?: number;
  jitter_ms?: number;
  download_mbps?: number;
  upload_mbps?: number;
  packet_loss?: number;
  duration_ms?: number;
  error?: string;
}

// 测速状态响应（get_speedtest_status 返回）
export interface SpeedtestStatusResponseData {
  status: SpeedtestStatusType;
  forced_direct: boolean;
  started_at?: number;
  completed_at?: number;
  progress?: SpeedtestProgress;
  result?: SpeedtestResult;
}

// ==================== 系统操作 ====================

export interface FixNetworkParams {}

// REMOVED: quit_service and upgrade_service actions
// Service management is now handled by Tauri via 'svc up' command
// export interface QuitServiceParams {}
// export interface UpgradeServiceParams {}
// export interface UpgradeServiceResponseData {
//   upgrading: boolean;
//   message: string;
// }

// ==================== Evaluate Tunnels ====================

export interface EvaluateTunnelsParams {
  tunnels: TunnelInput[];
  has_relays: boolean;
}

// ==================== Action 类型映射 ====================

/**
 * Action 参数类型映射
 * 用于类型推导和验证
 */
export interface ActionParamsMap {
  // VPN 控制
  start: StartParams;
  stop: StopParams;
  status: StatusParams;
  reconnect: {};

  // 配置管理
  set_config: SetConfigParams;
  get_config: GetConfigParams;
  get_config_options: {};

  // 认证管理
  register_device: RegisterDeviceParams;
  get_auth_code: GetAuthCodeParams;
  login: LoginParams;
  get_auth_status: GetAuthStatusParams;
  logout: LogoutParams;

  // 存储管理
  storage_set: StorageSetParams;
  storage_get: StorageGetParams;
  storage_delete: StorageDeleteParams;

  // 系统信息
  version: VersionParams;
  get_user_info: {};
  get_app_config: {};
  get_tunnels: {};
  get_latest_invite_code: {};

  // API 请求
  api_request: ApiRequestParams;

  // 网络与测速
  speedtest: SpeedtestParams;
  get_speedtest_status: {};

  // 系统操作
  fix_network: FixNetworkParams;

  // Metrics and evaluation
  get_metrics: {};
  evaluate_tunnels: EvaluateTunnelsParams;
}

/**
 * Action 响应类型映射
 */
export interface ActionResponseMap {
  start: StartResponseData;
  stop: StopResponseData;
  status: StatusResponseData;
  reconnect: { accepted: boolean; message: string };
  set_config: ConfigResponseData;
  get_config: ConfigResponseData;
  get_config_options: any;
  register_device: any;
  get_auth_code: any;
  login: any;
  get_auth_status: AuthStatusChangeData;
  logout: any;
  storage_set: null;
  storage_get: any;
  storage_delete: null;
  version: VersionResponseData;
  get_user_info: any;
  get_app_config: any;
  get_tunnels: any;
  get_latest_invite_code: any;
  api_request: ApiRequestResponseData;
  speedtest: SpeedtestResponseData;
  get_speedtest_status: SpeedtestStatusResponseData;
  fix_network: any;
  get_metrics: any;
  evaluate_tunnels: EvaluateTunnelsResponse;
}

/**
 * Action 类型常量
 */
export type ControlAction = keyof ActionParamsMap;

