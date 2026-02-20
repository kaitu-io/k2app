/**
 * API Client Library with HttpOnly Cookie Authentication
 *
 * ## Authentication Architecture
 *
 * ### Web Authentication (HttpOnly Cookie)
 * - Access token stored in HttpOnly cookie (XSS-safe)
 * - CSRF token in non-HttpOnly cookie (for non-GET requests)
 * - Cookies sent automatically via `credentials: 'include'`
 * - Token refresh handled by server (cookies updated automatically)
 *
 * ### Embed Mode (Bearer Token)
 * - Special case for iframe embedding
 * - Token stored in localStorage as `embed_auth_token`
 * - Sent via Authorization header
 *
 * ## Security Features
 * - HttpOnly cookies prevent XSS token theft
 * - CSRF token validation for state-changing requests
 * - No token exposure in URLs or localStorage
 *
 * ## AuthContext Integration
 * - Event `auth:unauthorized` syncs state on 401 errors
 * - AuthContext manages React state only
 * - API layer handles redirects via `autoRedirectToAuth` option
 *
 * ## Usage
 * ```typescript
 * // Protected endpoint (auto-redirect on 401)
 * await api.getMembers();
 *
 * // Public endpoint (handle 401 manually)
 * await api.getMembers({ autoRedirectToAuth: false });
 * ```
 */

import { toast } from "sonner";
import { appEvents } from "./events";

// ============================================================================
// API Types (moved from @/types/api.ts to avoid conflicts)
// ============================================================================

// API 响应类型
export interface ApiResponse<T = unknown> {
  code: number;
  message?: string;
  data?: T;
}

// 分页相关类型
export interface PaginationParams {
  page: number;
  pageSize: number;
}

export interface Pagination {
  page: number;
  pageSize: number;
  total: number;
}

export interface ListResult<T> {
  items: T[];
  pagination?: Pagination | null;
}

// 用户相关类型
export interface LoginIdentify {
  type: "email";
  value: string;
}

export interface Device {
  udid: string;
  remark: string;
  tokenLastUsedAt: number;
}

// Admin device data with complete information
export interface AdminDeviceData {
  udid: string;
  remark: string;
  tokenIssueAt: number;
  tokenLastUsedAt: number;
  appVersion: string;
  appPlatform: string;
  appArch: string;
  createdAt: number;
  updatedAt: number;
}

// ==================== Device Statistics Types ====================

export interface PlatformCount {
  platform: string; // darwin, windows, linux, ios, android, unknown
  count: number;
}

export interface VersionCount {
  version: string;
  count: number;
}

export interface ArchCount {
  arch: string;
  count: number;
}

export interface DeviceStatisticsResponse {
  totalDevices: number;
  unknownDevices: number;
  desktopDevices: number;
  mobileDevices: number;
  byPlatform: PlatformCount[];
  byVersion: VersionCount[];
  byArch: ArchCount[];
  active24h: number;
  active7d: number;
  active30d: number;
}

export interface ActiveDeviceItem {
  udid: string;
  userEmail: string;
  userUUID: string;
  appPlatform: string;
  appVersion: string;
  appArch: string;
  tokenLastUsedAt: number;
  createdAt: number;
}

export interface ActiveDevicesResponse {
  items: ActiveDeviceItem[];
  pagination: Pagination;
}

// User statistics types
export interface PeriodCount {
  period: string;
  count: number;
}

export interface UserStatisticsResponse {
  totalUsers: number;
  paidUsers: number;
  freeUsers: number;
  activePro: number;
  expiredPro: number;
  neverHadPro: number;
  totalRetailers: number;
  new24h: number;
  new7d: number;
  new30d: number;
  byRegistrationPeriod: PeriodCount[];
}

// Order statistics types
export interface RevenuePeriod {
  period: string;
  revenue: number;
  orders: number;
}

export interface OrderStatisticsResponse {
  totalOrders: number;
  paidOrders: number;
  unpaidOrders: number;
  totalRevenue: number;
  revenue24h: number;
  revenue7d: number;
  revenue30d: number;
  orders24h: number;
  orders7d: number;
  orders30d: number;
  conversionRate: number;
  averageOrderValue: number;
  revenueByPeriod: RevenuePeriod[];
}

export interface AdminTestDeviceData {
  udid: string;
  password: string;
  device: AdminDeviceData;
}

// Issue device token response (for test token generation)
export interface IssueDeviceTokenResponse {
  accessToken: string;
  refreshToken: string;
  issuedAt: number;
  expiresIn: number;
  password?: string;  // MD5 of accessToken, for k2oc RADIUS auth
}

export interface InviteConfig {
  purchaseRewardDays: number;        // 被邀请人购买奖励天数
  inviterPurchaseRewardDays: number; // 邀请人购买奖励天数
}

// InviteCode - 邀请码信息（表示"被谁邀请"，用于 User.invitedByCode）
// 注意：邀请配置不在此返回，应通过 getAppConfig() 获取全局邀请配置
export interface InviteCode {
  code: string;
  createdAt: number;
  remark: string;
  link: string;
}

export interface User {
  uuid: string;
  expiredAt: number;
  isFirstOrderDone: boolean;
  loginIdentifies: LoginIdentify[];
  device?: Device;
  inviteCode?: InviteCode;
  deviceCount: number;
  retailerConfig?: RetailerConfig;  // 分销商配置（仅管理员可见）
  wallet?: Wallet;                  // 钱包信息（仅管理员可见）
}

// 套餐相关类型
export interface Plan {
  pid: string;
  label: string;
  price: number;
  originPrice: number;
  month: number;
  highlight: boolean;
}

// 优惠活动类型
export interface Campaign {
  id: string;
  type: string;
  value: number;
  endAt: number;
  description: string;
}

// 订单相关类型
export interface CreateOrderRequest {
  preview: boolean;
  plan: string;
  campaignCode?: string;
  forMyself?: boolean;
  forUserUUIDs?: string[];
}

export interface Order {
  uuid: string;
  title: string;
  originAmount: number;
  campaignReduceAmount: number;
  payAmount: number;
  isPaid?: boolean;
  payAt?: number;
  plan?: Plan;
  campaign?: Campaign;
  forUsers?: User[];
  forMyself?: boolean;
}

// Full order details response for admin
export interface DataOrder {
  id: string;
  uuid: string;
  title: string;
  originAmount: number;
  campaignReduceAmount: number;
  payAmount: number;
  isPaid: boolean;
  createdAt: number;
  campaign: Campaign | null;
  plan: Plan | null;
  payAt: number;
  forUsers: User[];
  forMyself: boolean;
}

export interface CreateOrderResponse {
  payUrl: string;
  order: Order;
}

// Pro 历史记录
export interface ProHistory {
  type: string;
  days: number;
  reason: string;
  createdAt: number;
  order?: Order;
}

// 成员管理类型
export interface AddMemberRequest {
  memberEmail: string;
}

// 代付人管理类型
export interface Delegate {
  uuid: string;
  loginIdentifies: LoginIdentify[];
}

// 错误码
export const ErrorInvalidCampaignCode = 400001;

// 应用配置相关类型
export interface AppDownload {
  ios: string;
  android: string;
  windows: string;
  mac: string;
  router: string;
}

export interface AppLinks {
  baseURL: string;
  installPath: string;
  discoveryPath: string;
  privacyPath: string;
  termsPath: string;
  walletPath: string;
  retailerRulesPath: string;
}

export interface AppConfig {
  appDownload: AppDownload;
  appLinks: AppLinks;
  inviteReward: InviteConfig;
}

// 认证相关类型
export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
}

export interface AuthUser {
  id: number;
  email: string;
  isAdmin: boolean;
}

export interface SendAuthCodeRequest {
  email: string;
  language?: string;
}

export interface WebLoginRequest {
  email: string;
  verificationCode: string;
  language?: string;
  inviteCode?: string; // 邀请码（可选，仅未激活用户可设置）
}

// Web登录响应 - tokens通过HttpOnly Cookie设置，response只返回user信息
export interface WebLoginResponse {
  user: AuthUser;
}

export interface SendCodeResponse {
  userExists: boolean;       // 用户是否存在
  isActivated: boolean;      // 用户账号是否已激活
  isFirstOrderDone: boolean; // 用户是否完成首单
}

// ============================================================================
// Error Handling Types
// ============================================================================

// Error code constants matching server/center/response.go
export const ErrorCode = {
  None: 0,                    // Success
  InvalidOperation: 400,      // Invalid operation
  NotLogin: 401,              // Not logged in
  PaymentRequired: 402,       // Payment required
  Forbidden: 403,             // Permission denied
  NotFound: 404,              // Not found
  Conflict: 409,              // Conflict
  InvalidArgument: 422,       // Invalid argument
  TooManyRequests: 429,       // Too many requests
  SystemError: 500,           // System error
  ServiceUnavailable: 503,    // Service unavailable

  // Custom error codes
  InvalidCampaignCode: 400001,  // Invalid campaign code
  InvalidClientClock: 400002,    // Invalid client timestamp
} as const;

export type ErrorCodeType = typeof ErrorCode[keyof typeof ErrorCode];

// Custom API Error class with error code
export class ApiError extends Error {
  code: ErrorCodeType;

  constructor(code: ErrorCodeType, message: string) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
  }

  isUnauthorized(): boolean {
    return this.code === ErrorCode.NotLogin;
  }

  isForbidden(): boolean {
    return this.code === ErrorCode.Forbidden;
  }

  isNotFound(): boolean {
    return this.code === ErrorCode.NotFound;
  }
}

// Backward compatibility
export class UnauthorizedError extends ApiError {
  constructor(message = "Unauthorized") {
    super(ErrorCode.NotLogin, message);
    this.name = "UnauthorizedError";
  }
}

// Use the ApiResponse type from types/api.ts
type SResponse<T> = ApiResponse<T>;

// ==================== Resource 类型（用于关联展示） ====================

export interface ResourceUser {
  uuid: string;
  email?: string;
}

export interface ResourceCashback {
  retailerUuid: string;
  retailerEmail: string;
  amount: number;          // 返现金额（美分）
  status: string;          // pending, completed
  frozenUntil?: number;    // 冻结到期时间戳
}

export interface ResourceWithdrawAccount {
  accountType: string;     // 渠道类型: tron, polygon, bsc, arbitrum, paypal
  accountId: string;       // 收款标识（钱包地址/PayPal邮箱）
  currency?: string;       // 币种: usdt, usdc, usd
}

export interface ResourceTransaction {
  txHash?: string;         // 交易哈希
  explorerUrl?: string;    // 区块链浏览器URL
}

// ==================== 钱包相关类型 ====================

// ==================== 分销商等级系统类型 ====================

// 分销商等级常量
export const RetailerLevel = {
  L1_REFERRER: 1,         // L1 推荐者
  L2_RETAILER: 2,         // L2 分销商
  L3_PREMIUM: 3,          // L3 优质分销商
  L4_PARTNER: 4,          // L4 合伙人
} as const;

export type RetailerLevelType = typeof RetailerLevel[keyof typeof RetailerLevel];

// 等级名称映射（与后端保持一致）
export const RetailerLevelNames: Record<number, string> = {
  1: '推荐者',
  2: '分销商',
  3: '优质分销商',
  4: '合伙人',
};

// 等级颜色映射
export const RetailerLevelColors: Record<number, string> = {
  1: '#9E9E9E',  // L1 灰色
  2: '#2196F3',  // L2 蓝色
  3: '#9C27B0',  // L3 紫色
  4: '#FF9800',  // L4 金色
};

// 联系方式类型
export type ContactType = 'telegram' | 'email' | 'signal' | 'whatsapp' | 'wechat' | 'line' | 'other';

// 联系方式信息
export interface ContactInfo {
  type: ContactType;      // 联系方式类型
  value: string;          // 联系方式值（用户名/号码/链接）
  label?: string;         // 自定义标签（type=other时使用）
}

// 获取联系方式跳转链接
export function getContactUrl(contact: ContactInfo): string | null {
  switch (contact.type) {
    case 'telegram':
      // Telegram: 用户名或链接
      if (contact.value.startsWith('http')) return contact.value;
      return `https://t.me/${contact.value.replace('@', '')}`;
    case 'email':
      return `mailto:${contact.value}`;
    case 'signal':
      // Signal: 电话号码
      return `https://signal.me/#p/${contact.value.replace(/[^+\d]/g, '')}`;
    case 'whatsapp':
      // WhatsApp: 电话号码
      return `https://wa.me/${contact.value.replace(/[^+\d]/g, '')}`;
    case 'line':
      // Line: 用户ID
      if (contact.value.startsWith('http')) return contact.value;
      return `https://line.me/ti/p/~${contact.value}`;
    case 'wechat':
      // 微信: 二维码链接，无法直接跳转
      return contact.value.startsWith('http') ? contact.value : null;
    case 'other':
      // 其他: 如果是链接则返回，否则返回null
      return contact.value.startsWith('http') ? contact.value : null;
    default:
      return null;
  }
}

// 获取联系方式显示名称
export function getContactTypeName(type: ContactType): string {
  const names: Record<ContactType, string> = {
    telegram: 'Telegram',
    email: 'Email',
    signal: 'Signal',
    whatsapp: 'WhatsApp',
    wechat: '微信',
    line: 'Line',
    other: '其他',
  };
  return names[type] || type;
}

export interface RetailerConfig {
  // 等级系统
  level: number;                     // 等级：1=L1推荐者, 2=L2分销商, 3=L3优质分销商, 4=L4合伙人
  levelName: string;                 // 等级名称
  firstOrderPercent: number;         // 首单分成百分比 (0-100)
  renewalPercent: number;            // 续费分成百分比 (0-100)
  paidUserCount: number;             // 累计带来的付费用户数

  // 升级进度
  nextLevel?: number;                // 下一等级
  nextLevelName?: string;            // 下一等级名称
  nextLevelRequirement?: number;     // 下一等级所需用户数
  needContentProof: boolean;         // 下一等级是否需要内容证明
  progressPercent: number;           // 升级进度百分比 (0-100)

  // 内容证明（L3/L4审核用）
  contentProof?: string;             // JSON: 社媒链接、推广内容等
  contentVerifiedAt?: number;        // 内容审核通过时间戳

  // 联系方式
  contacts?: ContactInfo[];          // 联系方式列表
}

export interface Wallet {
  balance: number;          // 总余额（美分）
  availableBalance: number; // 可用余额（美分）
  frozenBalance: number;    // 冻结余额（美分）
  totalIncome: number;      // 累计收入（美分）
  totalWithdrawn: number;   // 累计提现（美分）
}

// ==================== Retailer Management Types ====================

// 分销商列表项
export interface AdminRetailerListItem {
  uuid: string;
  email: string;
  level: number;
  levelName: string;
  firstOrderPercent: number;
  renewalPercent: number;
  paidUserCount: number;
  contacts?: ContactInfo[];
  wallet?: Wallet;
  lastCommunicatedAt?: number;
  hasPendingFollowUp: boolean;
  pendingFollowUpCnt?: number;
  totalIncome?: number;
  totalWithdrawn?: number;
  createdAt?: number;
  notes?: string;
}

// 分销商详情
export interface AdminRetailerDetailData {
  uuid: string;
  email: string;
  userDetailLink: string;
  retailerConfig?: RetailerConfig;
  wallet?: Wallet;
  pendingFollowUps: number;
}

// 沟通记录
export interface RetailerNote {
  id: number;
  retailerId: number;
  content: string;
  communicatedAt: number;
  followUpAt?: number;
  isCompleted: boolean;
  operatorId: number;
  operatorName?: string;
  assigneeId?: number;
  assigneeName?: string;
  createdAt: number;
  isOverdue: boolean;
  daysOverdue?: number;
}

// 分销待办事项
export interface RetailerTodoItem {
  noteId: number;
  retailerUuid: string;
  retailerEmail: string;
  level: number;
  levelName: string;
  noteContent: string;
  followUpAt: number;
  daysOverdue: number;
  assigneeId?: number;
  assigneeName?: string;
  operatorId: number;
  operatorName?: string;
}

// 管理员简要信息
export interface AdminUserSimple {
  id: number;
  email: string;
}

// 创建沟通记录请求
export interface CreateRetailerNoteRequest {
  content: string;
  communicatedAt: number;
  followUpAt?: number;
  assigneeId?: number;
}

// 更新沟通记录请求
export interface UpdateRetailerNoteRequest {
  content?: string;
  followUpAt?: number;
  isCompleted?: boolean;
  assigneeId?: number;
}

// ==================== Admin List Items ====================

export interface AdminOrderListItem {
    uuid: string;
    title: string;
    originAmount: number;
    campaignReduceAmount: number;
    payAmount: number;
    isPaid: boolean;
    createdAt: number;
    paidAt: number;
    user: ResourceUser;               // 购买用户
    cashback?: ResourceCashback;      // 分销返现信息（可选）
}

export interface AdminWithdrawListItem {
  id: number;
  createdAt: number;
  user: ResourceUser;                    // 提现用户
  amount: number;                        // 申请提现金额（美分）
  feeAmount: number;                     // 手续费（美分）
  netAmount: number;                     // 实际到账金额（美分）
  status: string;                        // pending, approved, rejected, completed
  account: ResourceWithdrawAccount;      // 提现账户
  transaction?: ResourceTransaction;     // 交易信息（可选）
  remark?: string;                       // 备注
  processedAt?: number;                  // 处理完成时间
}

export interface AdminOrderListResponse {
    items: AdminOrderListItem[];
    pagination: {
        page: number;
        pageSize: number;
        total: number;
    };
}

export interface AdminOrderListParams {
    page?: number;
    pageSize?: number;
    loginProvider?: string;
    loginIdentity?: string;
    isPaid?: boolean;
    createdAtStart?: number;
    createdAtEnd?: number;
}

// Campaign-related interfaces
export interface CampaignRequest {
  code: string;
  name: string;
  type: string; // 'discount' | 'coupon'
  value: number;
  startAt: number;
  endAt: number;
  description?: string;
  isActive: boolean;
  matcherType: string; // 'first_order' | 'vip' | 'all'
  maxUsage?: number;
}

export interface CampaignResponse {
  id: number;
  createdAt: number;
  updatedAt: number;
  code: string;
  name: string;
  type: string;
  value: number;
  startAt: number;
  endAt: number;
  description: string;
  isActive: boolean;
  matcherType: string;
  usageCount: number;
  maxUsage: number;
}

export interface CampaignListResponse {
  items: CampaignResponse[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

export interface CampaignListParams {
  page?: number;
  pageSize?: number;
  type?: string;
  isActive?: boolean;
}

export interface CampaignStats {
  code: string;
  totalUsed: number;
  paidOrders: number;
  totalDiscount: number;
  totalRevenue: number;
  conversionRate: number;
  uniqueUsers: number;
  avgDiscountPerOrder: number;
}

export interface CampaignFunnel {
  applied: number;
  paid: number;
  abandoned: number;
}

// EDM (Email Marketing) related interfaces
export interface EmailTemplateLocalizationRequest {
  language: string;
  subject: string;
  content: string;
}

export interface EmailTemplateRequest {
  name: string;
  language: string;
  subject: string;
  content: string;
  description?: string;
  isActive?: boolean;
  originId?: number | null;
}

export interface EmailTemplateLocalizationResponse {
  id: number;
  language: string;
  subject: string;
  content: string;
}

export interface EmailTemplateResponse {
  id: number;
  createdAt: number;
  updatedAt: number;
  name: string;
  language: string;
  subject: string;
  content: string;
  description: string;
  isActive: boolean;
  originId?: number | null;
  isOriginal?: boolean;
}

export interface EmailTemplateListResponse {
  items: EmailTemplateResponse[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

export interface EmailTemplateListParams {
  page?: number;
  pageSize?: number;
  limit?: number;
  type?: string;
  isActive?: boolean;
}

// ================= Email Template Parameter Interfaces =================

export type EmailTemplateParamCategory = 'user' | 'subscription' | 'order' | 'device' | 'invite' | 'system';
export type EmailTemplateParamDataType = 'string' | 'number' | 'boolean' | 'date' | 'datetime';

export interface EmailTemplateParam {
  key: string;
  name: string;
  description: string;
  category: EmailTemplateParamCategory;
  dataType: EmailTemplateParamDataType;
  templateVar: string;
  example: string;
  isRequired: boolean;
  isConditional: boolean;
}

export interface EmailTemplateParamGroup {
  category: EmailTemplateParamCategory;
  name: string;
  description: string;
  params: EmailTemplateParam[];
}

export interface EmailTemplateParamsResponse {
  groups: EmailTemplateParamGroup[];
  totalCount: number;
}

// ================= Email Task Interfaces =================

// UserFilter interface - matches backend Go struct exactly
export interface UserFilter {
  userStatus: string; // not_activated, activated_no_order, paid (单选)
  activatedDate: {
    start: string;
    end: string;
  };
  // 过期天数筛选（单选）- 精确到天，适合定期任务
  // expire_in_30, expire_in_14, expire_in_7, expire_in_3, expire_in_1,
  // expired_1, expired_3, expired_7, expired_14, expired_30, expired
  expireDays: string;
  specificUsers: string[]; // UUIDs - if set, other filters are ignored
  // 分销商等级筛选（多选）
  // 1: L1 推荐者, 2: L2 分销商, 3: L3 优质分销商, 4: L4 合伙人
  retailerLevels: number[]; // Empty array = no filter
}

// User search interfaces
export interface UserSearchParams {
  email: string;
  page?: number;
  pageSize?: number;
}

export interface UserListResponse {
  items: User[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

// Email task preview interfaces based on real backend
export interface EmailTaskPreviewRequest {
  userFilters: UserFilter;
}

export interface EmailTaskSampleUser {
  userId: number;
  email: string;
  status: string;
  subscriptionStatus: string;
  language: string;
  registeredAt: number;
  lastActiveAt: number;
  deviceCount: number;
}

export interface EmailTaskPreviewResponse {
  totalCount: number;
  sampleUsers: EmailTaskSampleUser[];
}


export interface EmailTaskRequest {
  name: string;
  templateId: number;
  userFilters: UserFilter;
  type: "once" | "repeat"; // 任务类型: once=单次, repeat=循环
  scheduledAt?: number; // Unix timestamp, undefined=立即发送
  repeatEvery?: number; // 循环间隔（秒），type=repeat时必填
}

export interface EmailTaskResponse {
  batchId: string;  // Asynq task ID for tracking
  name: string;
  templateId: number;
  templateName: string;
  targetCount: number;
  scheduledAt?: number;
}


// ========================= Email Send Log Types =========================

export interface EmailSendLogResponse {
  id: number;
  createdAt: number;
  batchId: string;  // Asynq task ID (was taskResultId)
  templateId: number;
  templateName: string;
  userId: number;
  userUuid: string;
  email: string;
  language: string;
  status: 'pending' | 'sent' | 'failed' | 'skipped';
  sentAt: number | null;
  errorMsg: string | null;
}

export interface EmailSendLogStats {
  totalCount: number;
  sentCount: number;
  failedCount: number;
  pendingCount: number;
  skippedCount: number;
}

export interface EmailSendLogListResponse {
  items: EmailSendLogResponse[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
  stats: EmailSendLogStats;
}

export interface EmailSendLogListParams {
  batchId?: string;  // Asynq task ID filter
  templateId?: number;
  userId?: number;
  status?: 'pending' | 'sent' | 'failed' | 'skipped';
  email?: string;
  page?: number;
  pageSize?: number;
}

// Flag to prevent multiple concurrent auth redirects
// Removed authRedirectInProgress - using single login redirect manager

// API request options interface
export interface ApiRequestOptions extends RequestInit {
  autoRedirectToAuth?: boolean;
}

export const api = {
  /**
   * Clear authentication data and sync with AuthContext
   * Note: HttpOnly cookies are cleared by server via logout API
   * This only clears embed mode token (if any)
   */
  clearAuthData(): void {
    console.log('[API] Clearing auth data');
    localStorage.removeItem('embed_auth_token');

    // Emit event to sync AuthContext state only (no redirect)
    appEvents.emit('auth:unauthorized');
  },

  /**
   * Get CSRF token from cookie for non-GET requests
   * The csrf_token cookie is set by the server and is NOT HttpOnly
   */
  getCSRFToken(): string | null {
    if (typeof document === 'undefined') return null;
    const match = document.cookie.match(/csrf_token=([^;]+)/);
    return match ? match[1] : null;
  },

  /**
   * Redirect to login page with current path
   */
  redirectToLogin(currentPath?: string): void {
    if (typeof window === 'undefined') return;

    const pathToUse = currentPath || (window.location.pathname + window.location.search);
    const loginUrl = `/login?next=${encodeURIComponent(pathToUse)}`;
    window.location.href = loginUrl;
  },

  /**
   * Get valid auth header - only for embed mode (Bearer token)
   * Normal web auth uses HttpOnly cookies (sent automatically)
   * Returns empty object {} for normal web auth
   */
  async getValidAuthHeader(): Promise<Record<string, string>> {
    // Only embed mode uses Bearer token header
    // Normal web auth uses HttpOnly cookies (automatic via credentials: 'include')
    const embedToken = localStorage.getItem('embed_auth_token');
    if (embedToken) {
      console.log('[API] Using embed auth token');
      return { Authorization: `Bearer ${embedToken}` };
    }
    return {};
  },

  // Note: Token refresh is now handled server-side via sliding expiration
  // When access_token has < 7 days remaining, server automatically renews the cookie
  // No client-side refresh logic needed for web auth

  async request<T>(
    path: string,
    options: ApiRequestOptions = {}
  ): Promise<T> {
      const { autoRedirectToAuth = true, ...fetchOptions } = options;

      // Get auth header for Bearer token fallback (embed mode)
      const authHeaders = await this.getValidAuthHeader();

      // Build headers with CSRF token for non-GET requests
      const method = fetchOptions.method?.toUpperCase() || 'GET';
      const csrfHeaders: Record<string, string> = {};
      if (method !== 'GET') {
        const csrfToken = this.getCSRFToken();
        if (csrfToken) {
          csrfHeaders['X-CSRF-Token'] = csrfToken;
        }
      }

      const headers = {
        ...authHeaders,
        ...csrfHeaders,
        "Content-Type": "application/json",
        ...fetchOptions.headers,
      };

      // Build absolute URL for API paths
      let apiUrl: string;
      if (path.startsWith('/api/') || path.startsWith('/app/')) {
        const baseUrl = typeof window !== 'undefined' ?
          `${window.location.protocol}//${window.location.host}` : '';
        apiUrl = `${baseUrl}${path}`;
      } else {
        apiUrl = path;
      }

      let response: Response;
      try {
        response = await fetch(apiUrl, {
          ...fetchOptions,
          headers,
          credentials: 'include',
        });
      } catch {
        const errorMessage = "网络连接失败";
        toast.error(errorMessage);
        throw new Error(errorMessage);
      }

      if (!response.ok) {
        const errorMessage = `服务器错误: ${response.status}`;
        toast.error(errorMessage);
        throw new Error(errorMessage);
      }

      if (response.headers.get("Content-Length") === "0" || response.status === 204) {
        return {} as T;
      }

      let data: SResponse<T>;
      try {
        data = await response.json();
      } catch {
        throw new Error('响应格式错误');
      }

      if (data.code !== 0) {
          const errorMessage = data.message || "请求失败";

          // Handle 401 - session expired or invalid
          // Note: Token refresh is handled server-side via sliding expiration
          // If we get 401, the session is truly expired
          if (data.code === ErrorCode.NotLogin) {
              console.log('[API] Auth failed (401), clearing state');
              this.clearAuthData();

              if (autoRedirectToAuth) {
                  const currentPath = typeof window !== 'undefined' ?
                      window.location.pathname + window.location.search : '';
                  this.redirectToLogin(currentPath);
              }

              throw new ApiError(ErrorCode.NotLogin, errorMessage);
          }

          throw new ApiError(data.code as ErrorCodeType, errorMessage);
      }

      return (data.data !== undefined ? data.data : {}) as T;
  },

  // Order management APIs
  async getOrders(params: AdminOrderListParams = {}): Promise<AdminOrderListResponse> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.loginProvider) queryParams.set('loginProvider', params.loginProvider);
    if (params.loginIdentity) queryParams.set('loginIdentity', params.loginIdentity);
    if (params.isPaid !== undefined) queryParams.set('isPaid', params.isPaid.toString());
    if (params.createdAtStart !== undefined) queryParams.set('createdAtStart', params.createdAtStart.toString());
    if (params.createdAtEnd !== undefined) queryParams.set('createdAtEnd', params.createdAtEnd.toString());

    const query = queryParams.toString();
    return this.request<AdminOrderListResponse>(`/app/orders${query ? '?' + query : ''}`);
  },

  async getOrderDetail(uuid: string): Promise<DataOrder> {
    return this.request<DataOrder>(`/app/orders/${uuid}`);
  },

  // Purchase-related APIs (for regular users)
  async getPlans(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<ListResult<Plan>> {
    return this.request<ListResult<Plan>>('/api/plans', options);
  },

  async createOrder(request: CreateOrderRequest, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<CreateOrderResponse> {
    return this.request<CreateOrderResponse>('/api/user/orders', {
      method: 'POST',
      body: JSON.stringify(request),
      ...options,
    });
  },

  async getProHistories(params: PaginationParams): Promise<ListResult<ProHistory>> {
    const queryParams = new URLSearchParams();
    queryParams.set('page', params.page.toString());
    queryParams.set('pageSize', params.pageSize.toString());
    
    return this.request<ListResult<ProHistory>>(`/api/user/pro-histories?${queryParams}`);
  },

  // Member management APIs
  async getMembers(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<ListResult<User>> {
    return this.request<ListResult<User>>('/api/user/members', options);
  },

  async addMember(request: AddMemberRequest, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<User> {
    return this.request<User>('/api/user/members', {
      method: 'POST',
      body: JSON.stringify(request),
      ...options,
    });
  },

  async removeMember(userUUID: string, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<void> {
    return this.request<void>(`/api/user/members/${userUUID}`, {
      method: 'DELETE',
      ...options,
    });
  },

  // Delegate management APIs
  async getDelegate(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<Delegate> {
    return this.request<Delegate>('/api/user/delegate', options);
  },

  async rejectDelegate(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<void> {
    return this.request<void>('/api/user/delegate', {
      method: 'DELETE',
      ...options,
    });
  },

  // User profile API
  async getUserProfile(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<User> {
    return this.request<User>('/api/user/info', options);
  },

  // Update user language preference
  async updateUserLanguage(language: string): Promise<User> {
    return this.request<User>('/api/user/language', {
      method: 'PUT',
      body: JSON.stringify({ language }),
    });
  },

  // Campaign management APIs
  async getCampaigns(params: CampaignListParams = {}): Promise<CampaignListResponse> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.type) queryParams.set('type', params.type);
    if (params.isActive !== undefined) queryParams.set('isActive', params.isActive.toString());

    const query = queryParams.toString();
    return this.request<CampaignListResponse>(`/app/campaigns${query ? '?' + query : ''}`);
  },

  async getCampaign(id: number): Promise<CampaignResponse> {
    return this.request<CampaignResponse>(`/app/campaigns/${id}`);
  },

  async createCampaign(data: CampaignRequest): Promise<CampaignResponse> {
    return this.request<CampaignResponse>('/app/campaigns', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  async updateCampaign(id: number, data: CampaignRequest): Promise<CampaignResponse> {
    return this.request<CampaignResponse>(`/app/campaigns/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  async deleteCampaign(id: number): Promise<void> {
    return this.request<void>(`/app/campaigns/${id}`, {
      method: 'DELETE',
    });
  },

  async getCampaignStats(code: string): Promise<CampaignStats> {
    return this.request<CampaignStats>(`/app/campaigns/code/${code}/stats`);
  },

  async getCampaignOrders(code: string, params: { page?: number; pageSize?: number } = {}): Promise<AdminOrderListResponse> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());

    const query = queryParams.toString();
    return this.request<AdminOrderListResponse>(`/app/campaigns/code/${code}/orders${query ? '?' + query : ''}`);
  },

  async getCampaignFunnel(code: string): Promise<CampaignFunnel> {
    return this.request<CampaignFunnel>(`/app/campaigns/code/${code}/funnel`);
  },

  // Email Template management APIs
  async getEmailTemplates(params: EmailTemplateListParams = {}): Promise<EmailTemplateListResponse> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.limit !== undefined) queryParams.set('limit', params.limit.toString());
    if (params.type) queryParams.set('type', params.type);
    if (params.isActive !== undefined) queryParams.set('isActive', params.isActive.toString());

    const query = queryParams.toString();
    return this.request<EmailTemplateListResponse>(`/app/edm/templates${query ? '?' + query : ''}`);
  },

  async createEmailTemplate(data: EmailTemplateRequest): Promise<EmailTemplateResponse> {
    return this.request<EmailTemplateResponse>('/app/edm/templates', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  async updateEmailTemplate(id: number, data: EmailTemplateRequest): Promise<EmailTemplateResponse> {
    return this.request<EmailTemplateResponse>(`/app/edm/templates/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  async deleteEmailTemplate(id: number): Promise<void> {
    return this.request<void>(`/app/edm/templates/${id}`, {
      method: 'DELETE',
    });
  },

  // Translate email template using DeepL
  async translateEmailTemplate(id: number, targetLanguage: string): Promise<EmailTemplateResponse> {
    return this.request<EmailTemplateResponse>(`/app/edm/templates/${id}/translate/${targetLanguage}`, {
      method: 'POST',
    });
  },

  // Note: Email template parameters are now defined on the frontend
  // See: web/src/app/[locale]/manager/edm/templates/editor/page.tsx
  // Backend reference: server/center/api_admin_edm.go line 245-252

  // Email Send Logs APIs
  async getEmailSendLogs(params: EmailSendLogListParams = {}): Promise<EmailSendLogListResponse> {
    const queryParams = new URLSearchParams();
    if (params.batchId) queryParams.set('batchId', params.batchId);
    if (params.templateId !== undefined) queryParams.set('templateId', params.templateId.toString());
    if (params.userId !== undefined) queryParams.set('userId', params.userId.toString());
    if (params.status) queryParams.set('status', params.status);
    if (params.email) queryParams.set('email', params.email);
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());

    const query = queryParams.toString();
    return this.request<EmailSendLogListResponse>(`/app/edm/send-logs${query ? '?' + query : ''}`);
  },

  async getEmailSendLogStats(params: { batchId?: string; templateId?: number } = {}): Promise<EmailSendLogStats> {
    const queryParams = new URLSearchParams();
    if (params.batchId) queryParams.set('batchId', params.batchId);
    if (params.templateId !== undefined) queryParams.set('templateId', params.templateId.toString());

    const query = queryParams.toString();
    return this.request<EmailSendLogStats>(`/app/edm/send-logs/stats${query ? '?' + query : ''}`);
  },

  // User management APIs
  async searchUsers(params: UserSearchParams): Promise<UserListResponse> {
    const queryParams = new URLSearchParams();
    queryParams.set('email', params.email);
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());

    const query = queryParams.toString();
    return this.request<UserListResponse>(`/app/users${query ? '?' + query : ''}`);
  },

  // Admin member management APIs
  async getAdminMembers(userUUID: string): Promise<ListResult<User>> {
    return this.request<ListResult<User>>(`/app/users/${userUUID}/members`);
  },

  async addAdminMember(userUUID: string, request: AddMemberRequest): Promise<User> {
    return this.request<User>(`/app/users/${userUUID}/members`, {
      method: 'POST',
      body: JSON.stringify(request),
    });
  },

  async removeAdminMember(userUUID: string, memberUUID: string): Promise<void> {
    return this.request<void>(`/app/users/${userUUID}/members/${memberUUID}`, {
      method: 'DELETE',
    });
  },

  // Retailer config management APIs

  // 更新分销商等级（管理员手动调整）
  async updateRetailerLevel(userUUID: string, data: {
    level: number;       // 目标等级 1-4
    reason?: string;     // 变更原因
  }): Promise<void> {
    return this.request<void>(`/app/users/${userUUID}/retailer-config`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  // 更新分销商状态（启用/禁用分销商身份）
  async updateRetailerStatus(userUUID: string, data: {
    isRetailer: boolean;
  }): Promise<void> {
    return this.request<void>(`/app/users/${userUUID}/retailer-status`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  // 更新分销商联系方式
  async updateRetailerContacts(userUUID: string, data: {
    contacts: ContactInfo[];
  }): Promise<void> {
    return this.request<void>(`/app/users/${userUUID}/retailer-contacts`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  // 为用户添加会员时长
  async addUserMembership(userUUID: string, data: {
    months: number;   // 添加的月数（1-120个月）
    reason?: string;  // 变更原因（可选）
  }): Promise<{ expiredAt: number; months: number }> {
    return this.request<{ expiredAt: number; months: number }>(`/app/users/${userUUID}/membership`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  // 更新用户邮箱（管理员）
  async updateUserEmail(userUUID: string, data: {
    email: string;  // 新邮箱地址
  }): Promise<{ email: string }> {
    return this.request<{ email: string }>(`/app/users/${userUUID}/email`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  // Device management APIs
  async getUserDevices(userUUID: string): Promise<ListResult<AdminDeviceData>> {
    return this.request<ListResult<AdminDeviceData>>(`/app/users/${userUUID}/devices`);
  },

  async addTestDevice(userUUID: string): Promise<AdminTestDeviceData> {
    return this.request<AdminTestDeviceData>(`/app/users/${userUUID}/devices/add-test-device`, {
      method: 'POST',
    });
  },

  // Issue test token for existing device (returns accessToken, refreshToken, and password)
  async issueTestToken(userUUID: string, udid: string): Promise<IssueDeviceTokenResponse> {
    return this.request<IssueDeviceTokenResponse>(`/app/users/${userUUID}/devices/${udid}/test-token`, {
      method: 'POST',
    });
  },

  // Withdraw management APIs
  async listWithdrawRequests(params: {
    page?: number;
    pageSize?: number;
    status?: string;
  } = {}): Promise<ListResult<AdminWithdrawListItem>> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.status) queryParams.set('status', params.status);

    const query = queryParams.toString();
    return this.request<ListResult<AdminWithdrawListItem>>(`/app/wallet/withdraws${query ? '?' + query : ''}`);
  },

  async approveWithdraw(id: number, data: {
    action: 'approve' | 'reject';
    remark?: string;
  }): Promise<void> {
    return this.request<void>(`/app/wallet/withdraws/${id}/approve`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  async completeWithdraw(id: number, data: {
    txHash: string;
    remark?: string;
  }): Promise<void> {
    return this.request<void>(`/app/wallet/withdraws/${id}/complete`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  // Email Task management APIs
  async createEmailTask(data: EmailTaskRequest): Promise<EmailTaskResponse> {
    return this.request<EmailTaskResponse>('/app/edm/tasks', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  async previewEmailTask(data: EmailTaskPreviewRequest): Promise<EmailTaskPreviewResponse> {
    return this.request<EmailTaskPreviewResponse>('/app/edm/preview-targets', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },


  async sendCode(data: SendAuthCodeRequest, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<SendCodeResponse> {
    return this.request<SendCodeResponse>('/api/auth/code', {
      method: 'POST',
      body: JSON.stringify(data),
      ...options,
    });
  },

  async webLogin(data: WebLoginRequest, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<WebLoginResponse> {
    return this.request<WebLoginResponse>('/api/auth/web-login', {
      method: 'POST',
      body: JSON.stringify(data),
      ...options,
    });
  },

  /**
   * Logout - clears server-side HttpOnly cookies
   */
  async logout(): Promise<void> {
    try {
      // Call server to clear HttpOnly cookies
      await this.request<void>('/api/auth/logout', {
        method: 'POST',
        autoRedirectToAuth: false,
      });
    } catch (error) {
      console.warn('[API] Logout API failed:', error);
    }
    this.clearAuthData();
  },

  /**
   * Get current user info (used by AuthContext to check auth status)
   */
  async getCurrentUser(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<{
    id: number;
    email?: string;
    isAdmin?: boolean;
  }> {
    return this.request('/api/user/info', {
      ...options,
    });
  },

  // Invite code APIs
  async getInviteCodeInfo(code: string, options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<InviteCode> {
    return this.request<InviteCode>(`/api/invite/code?code=${code}`, {
      ...options,
    });
  },

  // App config API
  async getAppConfig(options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>): Promise<AppConfig> {
    return this.request<AppConfig>('/api/app/config', options);
  },

  // ==================== Wallet APIs ====================

  // Get wallet information
  async getWallet() {
    return this.request(`/api/wallet`);
  },

  // Get wallet changes (history)
  async getWalletChanges(params: { page: number; pageSize: number; type?: string }) {
    const queryParams = new URLSearchParams({
      page: params.page.toString(),
      pageSize: params.pageSize.toString(),
    });
    if (params.type) queryParams.set('type', params.type);
    return this.request(`/api/wallet/changes?${queryParams}`);
  },

  // Get withdraw accounts
  async getWithdrawAccounts() {
    return this.request(`/api/wallet/withdraw-accounts`);
  },

  // Create withdraw account
  // accountType: tron, polygon, bsc, arbitrum, paypal
  // currency: usdt, usdc (加密货币), usd (PayPal 自动设置)
  async createWithdrawAccount(data: { accountType: string; accountId: string; currency: string; label?: string }) {
    return this.request(`/api/wallet/withdraw-accounts`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  // Update withdraw account
  async updateWithdrawAccount(id: number, data: { label?: string; isDefault?: boolean }) {
    return this.request(`/api/wallet/withdraw-accounts/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(data),
    });
  },

  // Delete withdraw account
  async deleteWithdrawAccount(id: number) {
    return this.request(`/api/wallet/withdraw-accounts/${id}`, {
      method: 'DELETE',
    });
  },

  // Set default withdraw account
  async setDefaultWithdrawAccount(id: number) {
    return this.request(`/api/wallet/withdraw-accounts/${id}/set-default`, {
      method: 'POST',
    });
  },

  // Get withdraw requests
  async getWithdrawRequests(params: { page: number; pageSize: number; status?: string }) {
    const queryParams = new URLSearchParams({
      page: params.page.toString(),
      pageSize: params.pageSize.toString(),
    });
    if (params.status) queryParams.set('status', params.status);
    return this.request(`/api/wallet/withdraws?${queryParams}`);
  },

  // Create withdraw request
  async createWithdrawRequest(data: { amount: number; withdrawAccountId: number; userRemark?: string }) {
    return this.request(`/api/wallet/withdraws`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  // ==================== Retailer Management APIs ====================

  // 获取分销商列表
  async getRetailers(params: {
    page?: number;
    pageSize?: number;
    email?: string;
    level?: number;
  } = {}): Promise<ListResult<AdminRetailerListItem>> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.email) queryParams.set('email', params.email);
    if (params.level !== undefined) queryParams.set('level', params.level.toString());

    const query = queryParams.toString();
    return this.request<ListResult<AdminRetailerListItem>>(`/app/retailers${query ? '?' + query : ''}`);
  },

  // 获取分销商详情
  async getRetailerDetail(uuid: string): Promise<AdminRetailerDetailData> {
    return this.request<AdminRetailerDetailData>(`/app/retailers/${uuid}`);
  },

  // 获取分销待办列表
  async getRetailerTodos(params: {
    page?: number;
    pageSize?: number;
  } = {}): Promise<ListResult<RetailerTodoItem>> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());

    const query = queryParams.toString();
    return this.request<ListResult<RetailerTodoItem>>(`/app/retailers/todos${query ? '?' + query : ''}`);
  },

  // 创建沟通记录
  async createRetailerNote(uuid: string, data: CreateRetailerNoteRequest): Promise<RetailerNote> {
    return this.request<RetailerNote>(`/app/retailers/${uuid}/notes`, {
      method: 'POST',
      body: JSON.stringify(data),
    });
  },

  // 获取沟通记录列表
  async getRetailerNotes(uuid: string, params: {
    page?: number;
    pageSize?: number;
  } = {}): Promise<ListResult<RetailerNote>> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());

    const query = queryParams.toString();
    return this.request<ListResult<RetailerNote>>(`/app/retailers/${uuid}/notes${query ? '?' + query : ''}`);
  },

  // 更新沟通记录
  async updateRetailerNote(uuid: string, noteId: number, data: UpdateRetailerNoteRequest): Promise<RetailerNote> {
    return this.request<RetailerNote>(`/app/retailers/${uuid}/notes/${noteId}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  },

  // 删除沟通记录
  async deleteRetailerNote(uuid: string, noteId: number): Promise<void> {
    return this.request<void>(`/app/retailers/${uuid}/notes/${noteId}`, {
      method: 'DELETE',
    });
  },

  // ==================== Device Statistics APIs ====================

  // Get device statistics (aggregated counts)
  async getDeviceStatistics(): Promise<DeviceStatisticsResponse> {
    return this.request<DeviceStatisticsResponse>('/app/devices/statistics');
  },

  // Get active devices list with pagination
  async getActiveDevices(params: {
    page?: number;
    pageSize?: number;
    period?: '24h' | '7d' | '30d';
  } = {}): Promise<ActiveDevicesResponse> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.period) queryParams.set('period', params.period);

    const query = queryParams.toString();
    return this.request<ActiveDevicesResponse>(`/app/devices/active${query ? '?' + query : ''}`);
  },

  // ==================== User Statistics APIs ====================

  // Get user statistics (aggregated counts)
  async getUserStatistics(): Promise<UserStatisticsResponse> {
    return this.request<UserStatisticsResponse>('/app/users/statistics');
  },

  // ==================== Order Statistics APIs ====================

  // Get order statistics (aggregated counts and revenue)
  async getOrderStatistics(): Promise<OrderStatisticsResponse> {
    return this.request<OrderStatisticsResponse>('/app/orders/statistics');
  },

  // ==================== Admin Users ====================

  // Get admin users list (for assignee dropdown)
  async getAdminUsers(): Promise<AdminUserSimple[]> {
    return this.request<AdminUserSimple[]>('/app/admins');
  },

  // ==================== Cloud Instance Management ====================

  // Get cloud instances list
  async getCloudInstances(params: {
    page?: number;
    pageSize?: number;
    provider?: string;
    status?: string;
  } = {}): Promise<CloudInstanceListResponse> {
    const queryParams = new URLSearchParams();
    if (params.page !== undefined) queryParams.set('page', params.page.toString());
    if (params.pageSize !== undefined) queryParams.set('pageSize', params.pageSize.toString());
    if (params.provider) queryParams.set('provider', params.provider);
    if (params.status) queryParams.set('status', params.status);

    const query = queryParams.toString();
    return this.request<CloudInstanceListResponse>(`/app/cloud/instances${query ? '?' + query : ''}`);
  },

  // Sync cloud instances
  async syncCloudInstances(): Promise<void> {
    return this.request<void>('/app/cloud/instances/sync', {
      method: 'POST',
    });
  },

  // Change instance IP
  async changeCloudInstanceIP(instanceId: number): Promise<CloudChangeIPResponse> {
    return this.request<CloudChangeIPResponse>(`/app/cloud/instances/${instanceId}/change-ip`, {
      method: 'POST',
    });
  },

  // List cloud accounts
  async listCloudAccounts(): Promise<CloudAccountListResponse> {
    return this.request<CloudAccountListResponse>('/app/cloud/accounts');
  },

  // List cloud regions
  async listCloudRegions(params: {
    provider?: string;
    account?: string;
  } = {}): Promise<CloudRegionListResponse> {
    const queryParams = new URLSearchParams();
    if (params.provider) queryParams.set('provider', params.provider);
    if (params.account) queryParams.set('account', params.account);
    const query = queryParams.toString();
    return this.request<CloudRegionListResponse>(`/app/cloud/regions${query ? '?' + query : ''}`);
  },

  // List cloud plans
  async listCloudPlans(params: {
    account: string;
    region?: string;
  }): Promise<CloudPlanListResponse> {
    const queryParams = new URLSearchParams();
    queryParams.set('account', params.account);
    if (params.region) queryParams.set('region', params.region);
    const query = queryParams.toString();
    return this.request<CloudPlanListResponse>(`/app/cloud/plans?${query}`);
  },

  // List cloud images
  async listCloudImages(params: {
    account: string;
    region?: string;
  }): Promise<CloudImageListResponse> {
    const queryParams = new URLSearchParams();
    queryParams.set('account', params.account);
    if (params.region) queryParams.set('region', params.region);
    const query = queryParams.toString();
    return this.request<CloudImageListResponse>(`/app/cloud/images?${query}`);
  },

  // Create cloud instance
  async createCloudInstance(params: CloudCreateInstanceRequest): Promise<CloudTaskResponse> {
    return this.request<CloudTaskResponse>('/app/cloud/instances', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },


  // Delete cloud instance
  async deleteCloudInstance(instanceId: number): Promise<CloudTaskResponse> {
    return this.request<CloudTaskResponse>(`/app/cloud/instances/${instanceId}`, {
      method: 'DELETE',
    });
  },

  // Get cloud instance detail
  async getCloudInstance(instanceId: number): Promise<CloudInstanceDetailResponse> {
    return this.request<CloudInstanceDetailResponse>(`/app/cloud/instances/${instanceId}`);
  },

  // Sync single cloud instance
  async syncCloudInstance(instanceId: number): Promise<CloudTaskResponse> {
    return this.request<CloudTaskResponse>(`/app/cloud/instances/${instanceId}/sync`, {
      method: 'POST',
    });
  },

  // ========================= Slave Node Management APIs =========================

  // List slave nodes
  async listSlaveNodes(params: PaginationParams = { page: 1, pageSize: 100 }): Promise<SlaveNodeListResponse> {
    const queryParams = new URLSearchParams();
    queryParams.set('page', params.page.toString());
    queryParams.set('pageSize', params.pageSize.toString());
    return this.request<SlaveNodeListResponse>(`/app/nodes?${queryParams.toString()}`);
  },

  // Get nodes batch matrix (last 5 scripts and their results per node)
  async getNodesBatchMatrix(): Promise<NodeBatchMatrixResponse> {
    return this.request<NodeBatchMatrixResponse>('/app/nodes/batch-matrix');
  },

  // Delete slave node by IPv4
  async deleteSlaveNode(ipv4: string): Promise<void> {
    return this.request<void>(`/app/nodes/${encodeURIComponent(ipv4)}`, {
      method: 'DELETE',
    });
  },

  // Get WebSocket authentication token
  // Used for cross-domain WebSocket connections (e.g., SSH terminal)
  // Returns a short-lived token (5 minutes) to pass as URL query parameter
  async getWsToken(): Promise<WebSocketTokenResponse> {
    return this.request<WebSocketTokenResponse>('/app/ws-token');
  },

  // ========================= Batch Script Management APIs =========================

  // Create batch script
  async createBatchScript(params: CreateBatchScriptRequest): Promise<BatchScriptResponse> {
    return this.request<BatchScriptResponse>('/app/batch-scripts', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },

  // List batch scripts
  async listBatchScripts(params: PaginationParams = { page: 1, pageSize: 20 }): Promise<BatchScriptListResponse> {
    const queryParams = new URLSearchParams();
    queryParams.set('page', params.page.toString());
    queryParams.set('pageSize', params.pageSize.toString());
    return this.request<BatchScriptListResponse>(`/app/batch-scripts?${queryParams.toString()}`);
  },

  // Get batch script detail (with decrypted content)
  async getBatchScript(id: number): Promise<BatchScriptDetailResponse> {
    return this.request<BatchScriptDetailResponse>(`/app/batch-scripts/${id}`);
  },

  // Delete batch script
  async deleteBatchScript(id: number): Promise<{ success: boolean }> {
    return this.request<{ success: boolean }>(`/app/batch-scripts/${id}`, {
      method: 'DELETE',
    });
  },

  // Update batch script
  async updateBatchScript(id: number, params: UpdateBatchScriptRequest): Promise<BatchScriptDetailResponse> {
    return this.request<BatchScriptDetailResponse>(`/app/batch-scripts/${id}`, {
      method: 'PUT',
      body: JSON.stringify(params),
    });
  },

  // Get script version history
  async getBatchScriptVersions(id: number): Promise<BatchScriptVersionListResponse> {
    return this.request<BatchScriptVersionListResponse>(`/app/batch-scripts/${id}/versions`);
  },

  // Get specific version content
  async getBatchScriptVersionDetail(id: number, version: number): Promise<BatchScriptVersionDetailResponse> {
    return this.request<BatchScriptVersionDetailResponse>(`/app/batch-scripts/${id}/versions/${version}`);
  },

  // Restore a previous version
  async restoreBatchScriptVersion(id: number, version: number): Promise<BatchScriptDetailResponse> {
    return this.request<BatchScriptDetailResponse>(`/app/batch-scripts/${id}/versions/${version}/restore`, {
      method: 'POST',
    });
  },

  // Test script on a single node
  async testBatchScript(id: number, params: TestBatchScriptRequest): Promise<TestBatchScriptResponse> {
    return this.request<TestBatchScriptResponse>(`/app/batch-scripts/${id}/test`, {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },

  // ========================= Batch Task Management APIs =========================

  // Create batch task
  async createBatchTask(params: CreateBatchTaskRequest): Promise<BatchTaskResponse> {
    return this.request<BatchTaskResponse>('/app/batch-tasks', {
      method: 'POST',
      body: JSON.stringify(params),
    });
  },

  // List batch tasks (with optional status filter)
  async listBatchTasks(params: PaginationParams & { status?: string } = { page: 1, pageSize: 20 }): Promise<BatchTaskListResponse> {
    const queryParams = new URLSearchParams();
    queryParams.set('page', params.page.toString());
    queryParams.set('pageSize', params.pageSize.toString());
    if (params.status) {
      queryParams.set('status', params.status);
    }
    return this.request<BatchTaskListResponse>(`/app/batch-tasks?${queryParams.toString()}`);
  },

  // Get batch task detail (with all node results)
  async getBatchTask(id: number): Promise<BatchTaskDetailResponse> {
    return this.request<BatchTaskDetailResponse>(`/app/batch-tasks/${id}`);
  },

  // Pause batch task
  async pauseBatchTask(id: number): Promise<{ success: boolean }> {
    return this.request<{ success: boolean }>(`/app/batch-tasks/${id}/pause`, {
      method: 'PUT',
    });
  },

  // Resume batch task
  async resumeBatchTask(id: number): Promise<{ success: boolean }> {
    return this.request<{ success: boolean }>(`/app/batch-tasks/${id}/resume`, {
      method: 'PUT',
    });
  },

  // Delete batch task (only completed/failed)
  async deleteBatchTask(id: number): Promise<{ success: boolean }> {
    return this.request<{ success: boolean }>(`/app/batch-tasks/${id}`, {
      method: 'DELETE',
    });
  },

  // Retry failed nodes in a batch task
  async retryBatchTask(id: number, params?: RetryBatchTaskRequest): Promise<RetryBatchTaskResponse> {
    return this.request<RetryBatchTaskResponse>(`/app/batch-tasks/${id}/retry`, {
      method: 'POST',
      body: JSON.stringify(params || {}),
    });
  },

  // Get scheduled (cron) tasks
  async getScheduledBatchTasks(): Promise<ScheduledTasksListResponse> {
    return this.request<ScheduledTasksListResponse>('/app/batch-tasks/scheduled');
  },

  // Update schedule for a cron task
  async updateBatchTaskSchedule(id: number, params: ScheduleBatchTaskRequest): Promise<{ success: boolean }> {
    return this.request<{ success: boolean }>(`/app/batch-tasks/${id}/schedule`, {
      method: 'PUT',
      body: JSON.stringify(params),
    });
  },

  // Cancel/disable a scheduled cron task
  async cancelBatchTaskSchedule(id: number): Promise<{ success: boolean }> {
    return this.request<{ success: boolean }>(`/app/batch-tasks/${id}/schedule`, {
      method: 'DELETE',
    });
  },

};

// ==================== Cloud Instance Types ====================

// Matches backend DataCloudInstance struct
export interface CloudInstance {
  id: number;
  provider: string;
  account_name: string;
  instance_id: string;
  name: string;
  ip_address: string;
  ipv6_address?: string;
  region: string;
  traffic_used_gb: number;
  traffic_total_gb: number;
  traffic_ratio: number;     // Pre-calculated traffic usage ratio (0-1)
  traffic_reset_at: number;
  expires_at: number;
  time_ratio: number;        // Pre-calculated billing cycle time ratio (0-1)
  last_synced_at: number;
  sync_error?: string;
  node_name?: string;
}

export interface CloudInstanceListResponse {
  items: CloudInstance[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

// CloudInstanceDetailResponse is the same as CloudInstance for now
export type CloudInstanceDetailResponse = CloudInstance;

export interface CloudChangeIPResponse {
  task_id: string;
}

// Matches backend DataCloudAccount struct
export interface CloudAccount {
  name: string;
  provider: string;
  region: string;
}

export interface CloudAccountListResponse {
  items: CloudAccount[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

// Matches backend cloudprovider.RegionInfo struct
export interface CloudRegion {
  slug: string;
  nameEn: string;
  nameZh: string;
  country: string;
  providerId: string;
  available: boolean;
}

export interface CloudRegionListResponse {
  items: CloudRegion[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

// Matches backend cloudprovider.PlanInfo struct
export interface CloudPlan {
  id: string;
  name: string;
  cpu: number;
  memoryMB: number;
  storageGB: number;
  transferTB: number;
  priceMonthly: number;
}

export interface CloudPlanListResponse {
  items: CloudPlan[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

// Matches backend cloudprovider.ImageInfo struct
export interface CloudImage {
  id: string;
  name: string;
  os: string;
  platform: string;
  description: string;
}

export interface CloudImageListResponse {
  items: CloudImage[];
  pagination: {
    page: number;
    pageSize: number;
    total: number;
  };
}

// Request to create cloud instance
export interface CloudCreateInstanceRequest {
  account_name: string;
  region: string;
  plan: string;
  image_id: string;
  name: string;
}

// Task response for async operations
export interface CloudTaskResponse {
  task_id: string;
}

// ========================= Slave Node Management Types =========================

export interface SlaveNode {
  id: number;
  name: string;
  country: string;
  region: string;
  ipv4: string;
  ipv6: string;
  load: number;
  updatedAt: number;
  trafficUsagePercent: number;
  bandwidthUsagePercent: number;
}

export interface SlaveNodeListResponse {
  items: SlaveNode[];
  pagination: Pagination;
}

// ========================= Node Batch Matrix Types =========================

export interface NodeBatchMatrixScript {
  id: number;
  name: string;
}

export interface NodeBatchMatrixResult {
  status: 'success' | 'failed';
  taskId: number;
  executedAt: number;
  exitCode: number;
  stdout: string;
  stderr: string;
}

export interface NodeBatchMatrixTunnel {
  id: number;
  domain: string;
  protocol: string;
  port: number;
}

export interface NodeBatchMatrixNode {
  id: number;
  name: string;
  country: string;
  region: string;
  ipv4: string;
  ipv6: string;
  updatedAt: number;
  tunnelCount: number;
  tunnels: NodeBatchMatrixTunnel[];
  results: Record<string, NodeBatchMatrixResult | null>;
}

export interface NodeBatchMatrixResponse {
  scripts: NodeBatchMatrixScript[];
  nodes: NodeBatchMatrixNode[];
}

// ========================= Batch Script Management Types =========================

export interface CreateBatchScriptRequest {
  name: string;
  description: string;
  content: string; // Plain text script content
  executeWithSudo: boolean; // Execute script with sudo privileges
}

export interface UpdateBatchScriptRequest {
  name: string;
  description: string;
  content: string; // Plain text script content
  executeWithSudo: boolean; // Execute script with sudo privileges
}

export interface BatchScriptResponse {
  id: number;
  name: string;
  description: string;
  executeWithSudo: boolean;
  createdAt: number;
  updatedAt: number;
}

export interface BatchScriptDetailResponse {
  id: number;
  name: string;
  description: string;
  content: string; // Decrypted plain text
  executeWithSudo: boolean;
  createdAt: number;
  updatedAt: number;
}

export interface BatchScriptListResponse {
  items: BatchScriptResponse[];
  pagination: Pagination;
}

// Script version history types
export interface BatchScriptVersionResponse {
  version: number;
  createdAt: number;
  createdBy: number;
}

export interface BatchScriptVersionDetailResponse {
  version: number;
  content: string;
  createdAt: number;
  createdBy: number;
}

export interface BatchScriptVersionListResponse {
  items: BatchScriptVersionResponse[];
  pagination: Pagination;
}

// Script test types
export interface TestBatchScriptRequest {
  nodeId: number;
}

export interface TestBatchScriptResponse {
  stdout: string;
  stderr: string;
  exitCode: number;
  duration: number; // milliseconds
  error: string;
}

// ========================= Batch Task Management Types =========================

export interface CreateBatchTaskRequest {
  scriptId: number;
  nodeIds: number[]; // Explicit node ID list
  scheduleType: 'once' | 'cron';
  executeAt?: number; // Required when scheduleType=once (milliseconds timestamp)
  cronExpr?: string; // Required when scheduleType=cron
}

export interface BatchTaskResponse {
  id: number;
  asynqTaskId: string; // Asynq task ID for tracking
  scriptId: number;
  scriptName: string;
  nodeIds: number[];
  scheduleType: string;
  executeAt?: number | null;
  cronExpr: string;
  status: string;
  currentIndex: number;
  totalNodes: number;
  createdAt: number;
  completedAt?: number | null;
  parentTaskId?: number | null; // Parent task ID for retry tracking
  isEnabled: boolean; // Whether scheduled task is enabled
}

export interface TaskResultItem {
  nodeId: number;
  nodeName: string; // Joined from SlaveNode.Name
  nodeIpv4: string; // Joined from SlaveNode.Ipv4
  nodeIndex: number;
  status: string;
  stdout: string;
  stderr: string;
  exitCode: number;
  error: string;
  startedAt?: number | null;
  endedAt?: number | null;
  duration?: number | null; // Milliseconds (endedAt - startedAt)
}

export interface BatchTaskDetailResponse extends BatchTaskResponse {
  results: TaskResultItem[];
  successCount: number;
  failedCount: number;
}

// Retry batch task request
export interface RetryBatchTaskRequest {
  nodeIds?: number[]; // Optional: specific nodes to retry, empty = all failed
}

// Retry batch task response
export interface RetryBatchTaskResponse {
  taskId: number; // New task ID for retry
}

// Schedule batch task request
export interface ScheduleBatchTaskRequest {
  cronExpr: string;
  isEnabled: boolean;
}

// Scheduled task info
export interface ScheduledTaskInfo {
  id: number;
  scriptId: number;
  scriptName: string;
  cronExpr: string;
  isEnabled: boolean;
  nodeIds: number[];
  totalNodes: number;
  nextRunAt?: number | null;
  lastRunAt?: number | null;
  lastStatus?: string;
  createdAt: number;
}

// Scheduled tasks list response
export interface ScheduledTasksListResponse {
  items: ScheduledTaskInfo[];
  pagination: Pagination;
}

export interface BatchTaskListResponse {
  items: BatchTaskResponse[];
  pagination: Pagination;
}

// WebSocket authentication token response
export interface WebSocketTokenResponse {
  token: string;     // JWT token for WebSocket authentication
  expiresAt: number; // Token expiration timestamp (Unix seconds)
} 