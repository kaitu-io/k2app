/**
 * Kaitu Core Types
 *
 * 核心接口定义，所有平台通过 window._k2 注入实现
 */

import type { StatusResponseData } from '../services/vpn-types';

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
 * await _platform.storage.set('token', { access: 'xxx', refresh: 'yyy' });
 *
 * // 读取
 * const token = await _platform.storage.get<{ access: string }>('token');
 *
 * // 删除
 * await _platform.storage.remove('token');
 *
 * // 带 TTL
 * await _platform.storage.set('cache', data, { ttl: 60000 });
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
 *
 * Injected as window._platform before React loads.
 * Each platform (Tauri/Capacitor/Web) provides its own implementation.
 */
export interface IPlatform {
  // ====== 平台标识 ======

  os: 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'web';
  version: string;

  // ====== 核心能力 ======

  storage: ISecureStorage;
  getUdid(): Promise<string>;

  // ====== 跨平台能力 ======

  /** 打开外部链接（系统浏览器） */
  openExternal(url: string): Promise<void>;

  /** 写入剪贴板 */
  writeClipboard(text: string): Promise<void>;

  /** 读取剪贴板 */
  readClipboard(): Promise<string>;

  /** 同步语言设置到原生层（Tauri: tray 菜单, Mobile: no-op） */
  syncLocale(locale: string): Promise<void>;

  // ====== 桌面专属（可选）======

  updater?: IUpdater;

  /** 以管理员权限重新安装 daemon service */
  reinstallService?(): Promise<void>;

  /**
   * 获取当前进程 PID
   * 传入 k2 daemon 后，daemon 监控此 PID，进程退出时自动停止 VPN
   */
  getPid?(): Promise<number>;

  // ====== 诊断（可选）======

  /** 上传服务日志用于诊断/反馈 */
  uploadLogs?(params: {
    email?: string | null;
    reason: string;
    failureDurationMs?: number;
    platform?: string;
    version?: string;
    feedbackId?: string;
  }): Promise<{ success: boolean; error?: string }>;
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
 * - up, down, status, version
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
   * await window._k2.run('up', config)
   * await window._k2.run('down')
   * await window._k2.run('status')
   */
  run<T = any>(action: string, params?: any): Promise<SResponse<T>>;

  /**
   * Service 可达性事件（可选）
   * daemon 模式: SSE 连接成功 = available, 断开 = unavailable
   * NE 模式: NE 配置安装后恒 true
   * standalone: 不实现 → 退化为轮询
   *
   * @returns unsubscribe function
   */
  onServiceStateChange?(callback: (available: boolean) => void): () => void;

  /**
   * VPN 状态变更事件（可选）
   * daemon 模式: SSE status events
   * NE 模式: NE state callback → full status
   * standalone: 不实现 → 退化为轮询
   *
   * @returns unsubscribe function
   */
  onStatusChange?(callback: (status: StatusResponseData) => void): () => void;
}

// ==================== 全局类型声明 ====================

declare global {
  interface Window {
    _k2: IK2Vpn;
    _platform: IPlatform;
    __TAURI__?: any;
  }
}

