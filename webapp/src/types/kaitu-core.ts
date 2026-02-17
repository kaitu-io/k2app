/**
 * Kaitu Core Types
 *
 * 核心接口定义，所有平台通过 window._k2 注入实现
 */

// ==================== 基础类型 ====================

/**
 * 统一响应格式
 */
export interface SResponse<T = any> {
  code: number;
  message?: string;
  data?: T;
}

// ==================== 平台能力 ====================

/**
 * 更新信息
 */
export interface UpdateInfo {
  currentVersion: string;
  newVersion: string;
  releaseNotes?: string | null;
}

/**
 * 应用更新接口（可选能力）
 */
export interface IUpdater {
  /** 是否有更新准备好 */
  isUpdateReady: boolean;

  /** 更新信息 */
  updateInfo: UpdateInfo | null;

  /** 是否正在检查更新 */
  isChecking: boolean;

  /** 错误信息 */
  error: string | null;

  /** 立即应用更新（重启应用） */
  applyUpdateNow(): Promise<void>;

  /** 手动检查更新 */
  checkUpdateManual?(): Promise<string>;

  /** 监听更新就绪事件 */
  onUpdateReady?(callback: (info: UpdateInfo) => void): () => void;
}

// ==================== 安全存储 ====================

/**
 * 存储选项
 */
export interface StorageOptions {
  /**
   * 数据过期时间（毫秒）
   * 超过 TTL 后，get 返回 null
   */
  ttl?: number;
}

/**
 * ISecureStorage - 安全存储接口
 *
 * 比 localStorage 更安全：
 * - 数据在存储前自动加密
 * - WebKit inspector 只能看到密文
 * - 零用户交互（不弹指纹/密码/keychain 授权）
 *
 * 平台实现：
 * - Tauri: tauri-plugin-store (内置 AES-256)
 * - iOS: Swift 文件加密
 * - Android: EncryptedSharedPreferences
 * - Web: AES-GCM + 设备指纹派生密钥
 *
 * @example
 * ```typescript
 * // 存储
 * await _k2.platform.storage.set('token', { access: 'xxx', refresh: 'yyy' });
 *
 * // 读取
 * const token = await _k2.platform.storage.get<{ access: string }>('token');
 *
 * // 删除
 * await _k2.platform.storage.remove('token');
 *
 * // 带 TTL
 * await _k2.platform.storage.set('cache', data, { ttl: 60000 });
 * ```
 */
export interface ISecureStorage {
  /** 获取存储的值 */
  get<T = any>(key: string): Promise<T | null>;

  /** 存储值（自动加密） */
  set<T = any>(key: string, value: T, options?: StorageOptions): Promise<void>;

  /** 删除存储的值 */
  remove(key: string): Promise<void>;

  /** 检查键是否存在（且未过期） */
  has(key: string): Promise<boolean>;

  /** 清除所有存储数据 */
  clear(): Promise<void>;

  /** 获取所有存储键 */
  keys(): Promise<string[]>;
}

/**
 * 平台能力接口
 */
export interface IPlatform {
  /** 操作系统 */
  os: 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'web';

  /** 是否桌面端 */
  isDesktop: boolean;

  /** 是否移动端 */
  isMobile: boolean;

  /** 应用版本 */
  version: string;

  // ==================== 可选能力 ====================

  /** 打开外部链接 */
  openExternal?(url: string): Promise<void>;

  /** 写入剪贴板 */
  writeClipboard?(text: string): Promise<void>;

  /** 读取剪贴板 */
  readClipboard?(): Promise<string>;

  /** 显示 Toast */
  showToast?(message: string, type: 'success' | 'error' | 'info' | 'warning'): Promise<void>;

  /** 同步语言设置到原生层 */
  syncLocale?(locale: string): Promise<void>;

  /** 获取原生层语言设置 */
  getLocale?(): Promise<string>;

  /** 退出应用 */
  exit?(): Promise<void>;

  // ==================== 安全存储 ====================

  /**
   * 安全存储
   * 比 localStorage 更安全，数据自动加密
   */
  storage: ISecureStorage;

  // ==================== 调试能力（可选）====================

  /** 调试日志 */
  debug?(message: string): void;

  /** 警告日志 */
  warn?(message: string): void;

  /** Upload service logs for diagnostics/feedback */
  uploadServiceLogs?(params: {
    email?: string | null;
    reason: string;
    failureDurationMs?: number;
    platform?: string;
    version?: string;
    feedbackId?: string;
  }): Promise<{ success: boolean; error?: string }>;

  /**
   * Native command execution (platform abstraction)
   *
   * Supported actions:
   * - admin_reinstall_service: Admin reinstall service
   *
   * @param action - Command name
   * @param params - Command parameters
   * @returns Execution result
   */
  nativeExec?<T = any>(action: string, params?: Record<string, any>): Promise<T>;

  // ==================== Device ID ====================

  /**
   * Get device unique identifier (UDID)
   * Format: {48 random hex}-{8 fingerprint hash} (57 chars)
   *
   * Implementation:
   * - Tauri: Uses tauriSecureStorage + desktop fingerprint
   * - Capacitor: Uses capacitorSecureStorage + mobile fingerprint
   * - Standalone: Fetches from daemon /api/device/udid
   */
  getUdid(): Promise<string>;

  /**
   * Get current process ID (for VPN auto-stop monitoring)
   *
   * When provided to start action, VPN service monitors this PID
   * and auto-stops VPN when the process exits (prevents orphaned VPN)
   *
   * Implementation:
   * - Tauri: std::process::id()
   * - Others: Not applicable, returns 0
   */
  getPid?(): Promise<number>;
}

// ==================== 核心接口 ====================

/**
 * IK2Vpn - VPN-only global interface
 *
 * Slim interface injected as window._k2.
 * All VPN control goes through the single `run` method,
 * which maps to the daemon's POST /api/core endpoint.
 *
 * Supported actions:
 * - start, stop, reconnect, status
 * - get_config, set_config, get_config_options
 * - version, fix_network
 * - speedtest, get_speedtest_status
 * - get_metrics, evaluate_tunnels
 */
export interface IK2Vpn {
  /**
   * Execute a VPN command
   *
   * @param action - Command name
   * @param params - Command parameters
   * @returns Response
   *
   * @example
   * await window._k2.run('start')
   * await window._k2.run('status')
   * await window._k2.run('set_config', { proxyRule: 'global' })
   */
  run<T = any>(action: string, params?: any): Promise<SResponse<T>>;
}

// ==================== 全局类型声明 ====================

declare global {
  interface Window {
    _k2: IK2Vpn;
    _platform: IPlatform;
  }
}

// ==================== VPN 状态类型 ====================

/**
 * VPN 状态
 */
export type VPNState =
  | 'idle'
  | 'connecting'
  | 'connected'
  | 'disconnecting'
  | 'error';

/**
 * VPN 错误信息
 */
export interface VPNError {
  code: string;
  message: string;
  details?: string;
}

/**
 * VPN 状态响应
 */
export interface StatusResponseData {
  state: VPNState;
  startAt?: number;
  error?: VPNError;
  uploadBytes?: number;
  downloadBytes?: number;
  uploadSpeed?: number;
  downloadSpeed?: number;
}

/**
 * VPN 配置
 */
export interface ConfigResponseData {
  proxyRule: 'global' | 'rule' | 'direct';
  activeTunnelId?: string;
  dnsMode?: 'auto' | 'custom';
  customDns?: string[];
}

/**
 * 设置配置参数
 */
export interface SetConfigParams {
  proxyRule?: 'global' | 'rule' | 'direct';
  activeTunnelId?: string;
  dnsMode?: 'auto' | 'custom';
  customDns?: string[];
}

/**
 * 版本信息
 */
export interface VersionResponseData {
  version: string;
  commit?: string;
  buildTime?: string;
}

/**
 * 简单隧道
 * URL 格式: k2wss://[token@]domain?addrs=ip1,ip2[&anonymity=1][&country=XX]#name
 */
export interface SimpleTunnel {
  id: string;
  name: string;
  url: string;
  country?: string; // ISO 3166-1 alpha-2 国家代码
}

/**
 * 测速结果
 */
export interface SpeedtestResponseData {
  downloadSpeed: number;
  uploadSpeed: number;
  latency: number;
}

/**
 * 测速状态
 */
export interface SpeedtestStatusResponseData {
  running: boolean;
  progress: number;
  result?: SpeedtestResponseData;
}

/**
 * 日志设置
 */
export interface LogSettingsResponseData {
  level: 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';
}
