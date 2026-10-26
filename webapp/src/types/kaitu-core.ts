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

  /** 当前更新通道 (stable/beta) */
  channel: 'stable' | 'beta';

  /** 立即应用更新（重启应用） */
  applyUpdateNow(): Promise<void>;

  /** 手动检查更新 */
  checkUpdateManual?(): Promise<string>;

  /** 监听更新就绪事件 */
  onUpdateReady?(callback: (info: UpdateInfo) => void): () => void;

  /** 切换更新通道，返回新通道名。触发后台更新检查。 */
  setChannel?(channel: 'stable' | 'beta'): Promise<string>;
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

// ==================== IAP (iOS StoreKit) ====================

/** StoreKit 商品（已本地化价格） */
export interface IapProduct {
  id: string;
  displayName: string;
  description: string;
  /** 本地化价格串，如 "US$9.99" — 直接展示，勿自行格式化 */
  displayPrice: string;
  /** 数值价格（当地货币），仅用于排序/比较 */
  price: number;
  periodUnit?: 'day' | 'week' | 'month' | 'year' | 'unknown';
  periodValue?: number;
  /** Bridge v6: StoreKit 首期优惠（如免费试用）。旧壳 / 无优惠时 undefined。
   *  试用天数从这里读，不写死；展示价一律用 displayPrice。 */
  introOffer?: {
    paymentMode: 'freeTrial' | 'payAsYouGo' | 'payUpFront' | 'unknown';
    periodUnit: 'day' | 'week' | 'month' | 'year' | 'unknown';
    periodValue: number;
    displayPrice: string;
  };
  /** Bridge v6: StoreKit `isEligibleForIntroOffer`。所有订阅商品都会带（即使没有优惠），
   *  须与 introOffer 同时存在才代表可试用。旧壳 undefined。 */
  isEligibleForIntroOffer?: boolean;
}

/** 购买结果。result='success' 时携带 transactionId 供 Center 复核 */
export interface IapPurchaseResult {
  result: 'success' | 'cancelled' | 'pending';
  transactionId?: string;
  originalTransactionId?: string;
  productId?: string;
}

/**
 * IIap — iOS StoreKit 2 IAP 能力（仅 iOS 注入；其余平台 undefined）。
 *
 * 信任模型：native 永不发放权益。purchase/restore 返回 transactionId，
 * 由 webapp 调 Center `/api/user/apple-iap/verify`（cloudApi 带鉴权）复核入账，
 * 入账成功后再 `finishTransaction`。app 中途被杀 → `onTransactionUpdate` 重投补单。
 */
export interface IIap {
  /** 拉取商品（本地化价格/标题） */
  getProducts(productIds: string[]): Promise<IapProduct[]>;
  /** 发起购买。accountToken = Center 派生的 RFC UUID（绑定开途账号） */
  purchase(productId: string, accountToken: string): Promise<IapPurchaseResult>;
  /** 恢复购买（Apple 强制要件）→ 当前权益交易，逐笔交 Center 复核 */
  restore(): Promise<Array<{ transactionId: string; productId: string; jws?: string }>>;
  /** Center 入账成功后调用，标记交易完成 */
  finishTransaction(transactionId: string): Promise<void>;
  /** 后台续订/中断补单/Ask-to-Buy 批准的交易更新。返回取消订阅函数 */
  onTransactionUpdate(cb: (data: { transactionId: string; productId: string }) => void): () => void;
}

/**
 * LAN 路由器原生 HTTP 请求参数（绕过 WebView CORS/mixed-content）。
 * 仅允许 http:// + 私网/回环 IPv4 host —— 由各平台实现侧强制校验。
 */
export interface RouterRequestOptions {
  url: string;
  method?: 'GET' | 'POST';
  headers?: Record<string, string>;
  body?: string;
  timeoutMs?: number;
}

/** LAN 路由器原生 HTTP 请求结果 */
export interface RouterResponse {
  status: number;
  body: string;
}

/**
 * 后台服务无法运行的、可由用户解除的原因。
 * - `background-disallowed`：macOS 系统设置 → 通用 → 登录项与扩展 →「允许在后台」
 *   里，开关（按签名开发者名列出，即 itemName）被关掉。重装无效，打开开关后系统
 *   会立即拉起服务（#3911，testlab VM 150 实测）。
 */
export interface ServiceBlocker {
  kind: 'background-disallowed';
  /** 系统设置里该开关显示的名字（开发者名，不是 App 名）。 */
  itemName: string;
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
  platformType: 'desktop' | 'mobile' | 'gateway' | 'web';
  version: string;
  arch?: string;
  commit?: string;

  // ====== 核心能力 ======

  storage: ISecureStorage;

  /** Bridge v6 语义 flag：true 当且仅当 iOS 壳实现了 Keychain 身份存储（secureGet 探测成功）——
   *  udid 与登录 token 跨卸载重装保留。旧壳 / Android / 桌面 / web 为 false 或 undefined。
   *  由 capacitor-k2.ts 唯一供给；组件读这个 flag，不探测方法。 */
  keychainIdentity?: boolean;

  // ====== 跨平台能力 ======

  /** 打开外部链接（系统浏览器） */
  openExternal(url: string): Promise<void>;

  /** 写入剪贴板 */
  writeClipboard(text: string): Promise<void>;

  /** 读取剪贴板 */
  readClipboard(): Promise<string>;

  /** 系统分享（移动端原生分享面板） */
  share?(params: { text: string; title?: string; url?: string }): Promise<void>;

  /** 同步语言设置到原生层（Tauri: tray 菜单, Mobile: no-op） */
  syncLocale(locale: string): Promise<void>;

  // ====== 桌面专属（可选）======

  updater?: IUpdater;

  /** 以管理员权限重新安装 daemon service */
  reinstallService?(): Promise<void>;

  /**
   * 后台服务被用户可操作的系统开关挡住时的原因（bridge v4，桌面 macOS）。
   * null = 没有已知的阻止项。只经 services/service-blocker.ts 使用。
   */
  getServiceBlocker?(): Promise<ServiceBlocker | null>;

  /** 打开解除阻止所需的系统设置页（macOS：登录项与扩展）。bridge v4。 */
  openServiceSettings?(): Promise<void>;

  /**
   * 获取当前进程 PID
   * 传入 k2 daemon 后，daemon 监控此 PID，进程退出时自动停止 VPN
   */
  getPid?(): Promise<number>;

  /** 物理接口（WiFi/以太网）默认网关 IPv4；排除 TUN 虚拟接口。null = 不可用/无网关。 */
  getDefaultGateway?(): Promise<string | null>;

  /** 原生 HTTP 到 LAN 路由器（绕过 WebView CORS/mixed-content；仅 http:// + 私网 IP）。 */
  routerRequest?(opts: RouterRequestOptions): Promise<RouterResponse>;

  // ====== 网关专属（可选）======

  /** 检查网关固件更新（返回当前版本和最新版本） */
  gatewayUpgradeCheck?(): Promise<{ current: string; latest: string } | null>;

  /** 触发网关固件升级（后台下载+替换+重启） */
  gatewayUpgradeApply?(): Promise<boolean>;

  // ====== 诊断（可选）======

  /** 设置日志级别（开发者模式） */
  setLogLevel?(level: 'debug' | 'info' | 'warn' | 'error'): void;

  /** 启用/禁用开发者工具（WebView 调试） */
  setDevEnabled?(enabled: boolean): void;

  /** 上传服务日志用于诊断/反馈 */
  uploadLogs?(params: {
    email?: string | null;
    reason: string;
    failureDurationMs?: number;
    platform?: string;
    version?: string;
    feedbackId?: string;
  }): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>;

  // ====== App List（可选）======

  appList?: IAppListProvider;

  // ====== IAP（iOS StoreKit，可选）======

  /** 仅 iOS 注入。其余平台 undefined → 走外链 WordGate 支付 */
  iap?: IIap;

  // ====== 第三方登录（可选）======

  /** 拿 Google / Apple idToken 的原生能力（spec 2026-10-20 §6.2）。
   *  undefined = 老版本 App / 不支持的平台 → 不显示按钮，不报错。 */
  socialSignIn?: ISocialSignIn;
}

// ==================== Social Sign-In ====================

export type SocialProvider = 'apple' | 'google';

export interface SocialCredential {
  idToken: string;
  /** 仅 Apple 原生：一次性授权码，服务端换 refresh token 供注销时撤销 */
  authorizationCode?: string;
}

export interface ISocialSignIn {
  /** 本设备此刻能用的提供方（如安卓无 Apple、无 GMS 时无 Google）。不含网络可达性。 */
  availableProviders(): Promise<SocialProvider[]>;
  /**
   * 拉起提供方的登录界面。`hashedNonce` = sha256hex(原始 nonce)，原样进 idToken 的
   * nonce claim；原始值只发给服务端。用户取消 → resolve null（不算错误）。
   */
  signIn(provider: SocialProvider, hashedNonce: string): Promise<SocialCredential | null>;
}

// ==================== App List ====================

export interface RunningApp {
  /** macOS: bundle identifier (GUI apps) or exe path (standalone binaries) · Win/Linux: absolute executable path */
  id: string;
  /** Display name */
  label: string;
  /** Names to write into config.process_name — macOS: bundle's full helper set; Win/Linux: [exe basename] */
  processNames: string[];
  /** Directories whose every executable belongs to this app (macOS GUI app: its .app bundle). Absent for standalone binaries. */
  paths?: string[];
  /** Custom-scheme URL or undefined; webapp renders <img src={iconUrl}> */
  iconUrl?: string;
}

export interface InstalledApp {
  /** Platform-stable id. macOS: .app bundle path · Windows: exe path · Linux: .desktop path · Android: packageName */
  id: string;
  /** Display name */
  label: string;
  /** kaitu-icon://… url, or undefined (Avatar falls back to first letter) */
  iconUrl?: string;
  /** Android only: installing package (PackageManager.getInstallSourceInfo). undefined elsewhere. Feeds classify-apps installer match. */
  installerPackageName?: string | null;
  /** Names matched by classify-apps. macOS: [main executable] · Windows: exe basenames · Android: [packageName] */
  processNames: string[];
  /**
   * Directories whose every executable belongs to this app — macOS: the .app
   * bundle. The engine matches a process by where its executable lives
   * (`match.app_paths`), so helpers need not be enumerated. Absent on
   * platforms / shell versions that identify apps by name only.
   */
  paths?: string[];
}

export interface IAppListProvider {
  /** Desktop only (macOS / Windows / Linux desktop) */
  listRunning?(): Promise<RunningApp[]>;
  /** Android only */
  listInstalled?(): Promise<InstalledApp[]>;
}

// ==================== 核心接口 ====================

/** Antiblock 中继：要发往 Center 的 HTTP 请求（节点无关）
 * (承接契约纪律 — 走 _k2.run，不放 _platform/新全局)
 *
 * 节点选择/排序/复用已下沉到 Go 的 RelayManager（webapp 通过 relay-add-nodes
 * 增量喂节点，relay-fetch 只表达 HTTP 请求本身）。ip/pin/ech 保留为可选仅为
 * 类型兼容，webapp 不再发送。 */
export interface RelayRequest {
  centerHost: string;     // 内层 SNI + Host（如 k2.52j.me）
  method: string;
  path: string;
  headers?: Record<string, string>;
  body?: string;
  /** @deprecated 节点选择已下沉 Go；webapp 不再发送这些字段 */
  ip?: string;
  port?: number;
  pin?: string;
  ech?: string;
}

/** Antiblock 中继：单个伪装节点描述符（webapp → Go relay-add-nodes 增量喂入）。
 * Go 的 RelayManager 是节点存储 + 排序 + 健康的唯一权威。 */
export interface RelayNodeDescriptor {
  ip: string;
  port?: number;          // 默认 443
  pin: string;            // sha256:<rsa>,sha256:<ec>
  ech: string;            // base64 ECH 配置（必需）
  score?: number;         // 服务端 recommendScore（冷启动排序先验）
}

export interface RelayResponse {
  status: number;
  headers?: Record<string, string[]>;
  body: string;
}

/**
 * IK2Vpn - VPN-only global interface
 *
 * Slim interface injected as window._k2.
 * All VPN control goes through the single `run` method,
 * which maps to the daemon's POST /api/core endpoint.
 *
 * Supported actions:
 * - up, down, status, version
 * - classify-apps (App Bypass), relay-fetch (antiblock control-plane relay)
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
  // Antiblock relay: send one control-plane HTTP request through a camouflage node
  run(action: 'relay-fetch', params: RelayRequest): Promise<SResponse<RelayResponse>>;
  // Generic fallback (adb-* and other dynamic actions / progressive migration)
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
    /**
     * Injected into the served HTML by the Linux k2 daemon (k2/webui
     * serve.go). Its presence is what distinguishes the Linux webui shell
     * from a plain browser — the webapp reaches that daemon over relative
     * fetch, so there is no bridge global to probe instead.
     */
    __K2_GATEWAY__?: { version: string; commit: string; arch: string };
  }
}

