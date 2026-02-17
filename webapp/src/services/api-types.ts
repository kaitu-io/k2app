// 从本地类型文件导入通用响应类型
import type { SResponse } from '../types/kaitu-core';

// Re-export for other modules
export type { SResponse };

// 分页相关类型
export interface PaginationParams {
  page: number; // 页码
  pageSize: number; // 每页数量
}

export interface Pagination {
  page: number; // 当前页码
  pageSize: number; // 每页数量
  total: number; // 总记录数
}

export interface ListResult<T> {
  items: T[]; // 数据列表
  pagination?: Pagination | null; // 分页信息（可选）
}

export interface Device {
  udid: string; // 设备唯一标识
  remark: string; // 设备备注
  tokenLastUsedAt: number; // 最后登录时间
}

export interface DeviceListResponse {
  items: Device[]; // 设备列表
}

export interface UpdateDeviceRemarkRequest {
  udid: string; // 设备唯一标识
  remark: string; // 新的备注
}

export interface LoginIdentify {
  type: "email";
  value: string;
}

export interface User {
  uuid: string; // 统一使用 UUID
  expiredAt: number; // timestamp
  isFirstOrderDone: boolean;
  loginIdentifies: LoginIdentify[];
  device: Device;
  inviteCode?: InviteCode; // 统一使用 inviteCode
  deviceCount: number;
}

export interface RegisterDeviceRequest {
  udid: string;
  remark: string;
  inviteCode?: string;
  language?: string;
}

export interface GetAuthCodeRequest {
  email: string;
  language?: string;
}

export interface SendCodeResponse {
  userExists: boolean; // 用户是否存在
  isActivated: boolean; // 用户是否已激活（是否绑定了邮箱）
  isFirstOrderDone: boolean; // 用户是否完成首单
}

export interface LoginRequest {
  email: string;
  verificationCode: string;
  udid: string;
  remark: string;
  language?: string;
  inviteCode?: string;
}

export interface AuthResult {
  accessToken: string;
  refreshToken: string;
  expiredAt: number;
}

export interface SendVerificationEmailRequest {
  email: string;
}

export interface updateLoginEmailRequest {
  email: string;
  verificationCode: string;
}

// 更新用户邀请码请求
export interface UpdateUserInviteCodeRequest {
  inviteCode: string; // 邀请码
}

// 邀请码配置
export interface InviteConfig {
  purchaseRewardDays: number; // 购买奖励天数
  inviterPurchaseRewardDays: number; // 邀请人购买奖励天数
}

// 邀请码信息
export interface InviteCode {
  code: string; // 邀请码
  createdAt: number; // 创建时间
  remark: string; // 邀请码备注
  link: string; // 邀请码链接
  config: InviteConfig; // 邀请配置
}

// 我的邀请码信息
export interface MyInviteCode {
  code: string;
  createdAt: number;
  remark: string;
  link: string;
  config: InviteConfig;
  registerCount: number; // 注册人数（仅统计，无奖励）
  purchaseCount: number; // 购买人数
  purchaseReward: number; // 购买奖励（天数）
}

// 更新邀请码备注请求
export interface UpdateInviteCodeRemarkRequest {
  remark: string; // 新的备注
}

// 套餐信息
export interface Plan {
  pid: string; // 套餐ID，如 "1y", "2y", "3y"
  label: string; // 套餐名称，如 "1年套餐"
  price: number; // 原价, 美分计算
  originPrice: number; // 原价, 美分计算
  month: number; // 月数
  highlight: boolean; // 是否高亮推荐
}

export interface CreateOrderRequest {
  preview: boolean; // 是否预览
  plan: string; // 套餐ID
  campaignCode?: string; // 优惠码（可选）
  forMyself?: boolean; // 为自己购买
  forUserUUIDs?: string[]; // 为其他用户购买（UUID列表）
}

// 物理节点信息
export interface SlaveNode {
  name: string; // 节点名称
  country: string; // 国家代码
  region: string; // 节点区域
  ipv4: string; // IPv4地址
  ipv6: string; // IPv6地址
  isAlive: boolean; // 节点是否在线
  load: number; // 当前负载

  // Evaluation fields for tunnel scoring
  trafficUsagePercent: number; // Traffic quota usage (0-100)
  bandwidthUsagePercent: number; // Bandwidth usage (0-100)
}

// 隧道信息
// URL 格式: k2wss://domain?addrs=node_ip:tunnel_port[&anonymity=1]
// - domain: 仅用于 SNI/TLS，不包含端口
// - addrs: 格式为 node.ipv4:tunnel.port
export interface Tunnel {
  id: number; // 隧道ID
  domain: string; // 隧道域名（仅用于 SNI/TLS）
  name: string; // 隧道名称
  protocol: string; // 隧道协议 (k2wss)
  port: number; // 隧道端口，用于 addrs 中 node_ip:tunnel_port
  url: string; // 隧道URL（包含 addrs 参数）
  node: SlaveNode; // 关联的物理节点
}

// 隧道列表响应
// GET /api/tunnels/:protocol returns this response
export interface TunnelListResponse {
  items: Tunnel[]; // 隧道列表
  echConfigList?: string; // Base64 encoded ECHConfigList for K2v4 connections (optional)
}

// 优惠活动
export interface Campaign {
  id: string; // 活动ID
  type: string; // 活动类型：discount(折扣), coupon(优惠券)
  value: number; // 优惠值：折扣率或优惠金额
  endAt: number; // 活动结束时间
  description: string; // 活动描述
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
  forUsers?: DataUser[]; // 为哪些用户购买
  forMyself?: boolean; // 是否为自己购买
}

export interface ProHistory {
  type: string;
  days: number;
  reason: string;
  createdAt: number;
  order?: Order;
}

// Pro History 查询参数
export interface ProHistoryParams extends PaginationParams {
  type?: string; // 可选的类型过滤 (recharge, reward, 等)
}

export const ErrorInvalidCampaignCode = 400001;
export const ErrorInvalidArgument = 422;

// 成员管理相关类型
export interface AddMemberRequest {
  memberEmail: string; // 成员邮箱
}

// 分销商返现相关类型
export interface RetailerConfigUpdateRequest {
  cashbackPercent: number; // 返现百分比 (0-100)
  cashbackRule: 'first_order' | 'all_orders'; // 返现规则
}

export interface DataRetailerConfig {
  cashbackPercent: number; // 返现百分比
  cashbackRule: 'first_order' | 'all_orders'; // 返现规则
  isActive: boolean; // 配置是否启用
  updatedAt: number; // 更新时间
}

export interface DataRetailerCashback {
  id: number; // 返现记录ID
  inviteeEmail: string; // 被邀请用户邮箱
  inviteeUuid: string; // 被邀请用户UUID
  inviteCode: string; // 邀请码
  orderUuid: string; // 订单UUID
  amount: number; // 返现金额（美分）
  percent: number; // 返现百分比快照
  status: 'pending' | 'paid' | 'cancelled'; // 状态
  reason?: string; // 返现原因
  createdAt: number; // 创建时间
}

export interface RetailerCashbackSummary {
  totalAmount: number; // 总返现金额
  pendingAmount: number; // 待支付金额
  paidAmount: number; // 已支付金额
  totalCount: number; // 总记录数
}

export interface RetailerCashbackListParams {
  page?: number;
  pageSize?: number;
  status?: 'pending' | 'paid' | 'cancelled';
}


// 统一的 API 服务接口
export interface ApiService {
  // 认证相关
  sendAuthCode(email: string, language?: string): Promise<SResponse<SendCodeResponse>>;

  // 用户相关
  sendVerificationEmail(request: SendVerificationEmailRequest): Promise<SResponse<void>>;
  updateLoginEmail(request: updateLoginEmailRequest): Promise<SResponse<void>>;
  updateUserLanguage(language: string): Promise<SResponse<DataUser>>;

  // 设备相关
  getDevices(): Promise<SResponse<DeviceListResponse>>;
  updateDeviceRemark(request: UpdateDeviceRemarkRequest): Promise<SResponse<void>>;
  deleteDevice(udid: string): Promise<SResponse<void>>;

  // 邀请码相关
  getInviteCode(code: string): Promise<SResponse<InviteCode>>;
  createMyInviteCode(): Promise<SResponse<MyInviteCode>>;
  getMyInviteCodes(params: PaginationParams): Promise<SResponse<ListResult<MyInviteCode>>>;
  getLatestInviteCode(): Promise<SResponse<MyInviteCode | null>>;
  updateMyInviteCodeRemark(code: string, data: UpdateInviteCodeRemarkRequest): Promise<SResponse<void>>;

  // App 配置
  getAppConfig(): Promise<SResponse<AppConfig>>;

  // 订单相关
  getPlans(): Promise<SResponse<ListResult<Plan>>>;
  getProHistories(params: ProHistoryParams): Promise<SResponse<ListResult<ProHistory>>>;
  createOrder(request: CreateOrderRequest): Promise<SResponse<{ payUrl: string; order: Order }>>;

  // 成员管理
  getMembers(): Promise<SResponse<ListResult<DataUser>>>;
  addMember(request: AddMemberRequest): Promise<SResponse<DataUser>>;
  removeMember(userUUID: string): Promise<SResponse<void>>;

  // 分销商返现相关
  getRetailerConfig(): Promise<SResponse<DataRetailerConfig>>;
  updateRetailerConfig(data: RetailerConfigUpdateRequest): Promise<SResponse<DataRetailerConfig>>;
  getRetailerCashbacks(params: RetailerCashbackListParams): Promise<SResponse<ListResult<DataRetailerCashback>>>;
  getRetailerCashbackSummary(): Promise<SResponse<RetailerCashbackSummary>>;
  getRetailerStats(): Promise<SResponse<RetailerStats>>;

  // 钱包相关
  getWallet(): Promise<SResponse<Wallet>>;
  getWalletChanges(params: PaginationParams & { type?: WalletChangeType }): Promise<SResponse<ListResult<WalletChange>>>;
  getWithdrawAccounts(): Promise<SResponse<WithdrawAccount[]>>;
  createWithdrawAccount(data: CreateWithdrawAccountRequest): Promise<SResponse<WithdrawAccount>>;
  setDefaultWithdrawAccount(id: number): Promise<SResponse<void>>;
  deleteWithdrawAccount(id: number): Promise<SResponse<void>>;
  getWithdrawRequests(params: PaginationParams & { status?: WithdrawStatus }): Promise<SResponse<ListResult<Withdraw>>>;
  createWithdrawRequest(data: CreateWithdrawRequest): Promise<SResponse<Withdraw>>;

  // 工单相关
  createTicket(data: CreateTicketRequest): Promise<SResponse<void>>;
}

// 兼容性类型定义（用于现有代码中可能使用的类型）
export interface DataLoginIdentify {
  type: string;
  value: string;
}

export interface DataDevice {
  udid: string;
  remark: string;
  tokenLastUsedAt: number;
}

// DataInviteCode - 邀请码信息（表示"被谁邀请"，用于 DataUser.inviteCode）
// 注意：邀请配置不在此返回，应通过 getAppConfig() 获取全局邀请配置
export interface DataInviteCode {
  code: string;
  createdAt: number;
  remark: string;
  link: string;
}

export interface DataUser {
  uuid: string; // 使用 UUID 而不是 id
  expiredAt: number;
  isFirstOrderDone: boolean;
  inviteCode?: DataInviteCode; // 统一使用 inviteCode
  loginIdentifies: DataLoginIdentify[];
  device?: DataDevice;
  deviceCount: number;
  isRetailer?: boolean; // 是否为分销商
  retailerConfig?: DataRetailerConfig; // 分销商配置
}

// 分销商配置更新请求
export interface RetailerConfigUpdateRequest {
  cashbackPercent: number;
  createdAt: number;
}

// 分销商返现汇总
export interface RetailerCashbackSummary {
  totalAmount: number; // 总返现金额（美分）
  totalCount: number; // 总返现次数
  pendingAmount: number; // 待提现金额（美分）
  pendingCount: number; // 待提现次数
  paidAmount: number; // 已提现金额（美分）
  paidCount: number; // 已提现次数
}

// 分销商统计数据（包含等级信息）
export interface RetailerStats {
  level: number; // 当前等级：1=L1推荐者, 2=L2分销商, 3=L3优质分销商, 4=L4合伙人
  levelName: string; // 等级名称
  firstOrderPercent: number; // 首单分成百分比
  renewalPercent: number; // 续费分成百分比
  paidUserCount: number; // 累计付费用户数

  // 升级进度
  nextLevel?: number; // 下一等级
  nextLevelName?: string; // 下一等级名称
  nextLevelRequirement?: number; // 下一等级所需用户数
  needContentProof: boolean; // 下一等级是否需要内容证明
  progressPercent: number; // 升级进度百分比 (0-100)
}

// ==================== 钱包系统类型定义 ====================

// 钱包信息
export interface Wallet {
  id: number;
  userId: number;
  balance: number; // 总余额（美分）
  totalIncome: number; // 累计收入（美分）
  totalWithdrawn: number; // 累计提现（美分）
  availableBalance: number; // 可用余额（美分，实时计算）
  frozenBalance: number; // 冻结余额（美分，实时计算）
  version: number; // 乐观锁版本号
  createdAt: number;
  updatedAt: number;
}

// 钱包变动类型
export type WalletChangeType = 'income' | 'withdraw' | 'refund';

// 钱包变动记录
export interface WalletChange {
  id: number;
  walletId: number;
  type: WalletChangeType;
  amount: number; // 变动金额（美分，正数=增加，负数=减少）
  balanceBefore: number; // 变动前余额（美分）
  balanceAfter: number; // 变动后余额（美分）
  frozenUntil?: number; // 冻结到期时间（Unix timestamp），仅 income 类型
  orderId?: number; // 关联订单ID（income/refund 类型）
  withdrawId?: number; // 关联提现ID（withdraw 类型）
  parentId?: number; // 父记录ID（refund 关联 income）
  remark?: string; // 备注
  operatorId?: number; // 操作员ID（人工调整）
  createdAt: number;
}

// 提现渠道类型（按网络/支付方式区分）
export type WithdrawAccountType = 'tron' | 'polygon' | 'bsc' | 'arbitrum' | 'paypal';

// 币种
export type Currency = 'usdt' | 'usdc' | 'usd';

// 提现账户
export interface WithdrawAccount {
  id: number;
  userId: number;
  accountType: WithdrawAccountType; // 渠道类型
  accountId: string; // 收款标识（钱包地址或 PayPal 邮箱）
  currency: Currency; // 币种
  label?: string; // 用户自定义标签
  isDefault: boolean; // 是否为默认账户
  withdrawCount: number; // 使用次数
  lastUsedAt?: number; // 最后使用时间（Unix timestamp）
  createdAt: number;
  updatedAt: number;
}

// 提现状态
export type WithdrawStatus = 'pending' | 'approved' | 'processing' | 'completed' | 'rejected' | 'cancelled';

// 提现申请
export interface Withdraw {
  id: number;
  userId: number;
  walletId: number;
  amount: number; // 申请提现金额（美分）
  feeAmount: number; // 手续费（美分）
  netAmount: number; // 实际到账金额（美分）
  withdrawAccountId: number;
  withdrawAccount?: WithdrawAccount; // 关联的提现账户
  accountType: WithdrawAccountType; // 渠道类型快照
  accountId: string; // 收款标识快照（钱包地址/邮箱）
  currency: Currency; // 币种快照
  status: WithdrawStatus;
  processedBy?: number; // 处理人ID
  processedAt?: number; // 处理时间（Unix timestamp）
  txHash?: string; // 交易凭证（区块链哈希/PayPal交易ID）
  txExplorerUrl?: string; // 查看链接
  rejectReason?: string; // 拒绝原因
  remark?: string; // 备注
  createdAt: number;
  updatedAt: number;
}

// 创建提现账户请求
export interface CreateWithdrawAccountRequest {
  accountType: WithdrawAccountType; // 渠道类型: tron, polygon, bsc, arbitrum, paypal
  accountId: string; // 收款标识（钱包地址或 PayPal 邮箱）
  currency: Currency; // 币种: usdt, usdc（加密货币）或 usd（PayPal）
  label?: string; // 账户标签
}

// 创建提现申请请求
export interface CreateWithdrawRequest {
  amount: number; // 提现金额（美分）
  withdrawAccountId: number;
  userRemark?: string; // 用户备注
}

// ==================== 工单系统类型定义 ====================

// 创建工单请求
export interface CreateTicketRequest {
  subject: string; // 问题标题（1-200字符）
  content: string; // 问题描述（1-5000字符）
}

// ==================== App 配置类型定义 ====================

// App links configuration
export interface AppLinks {
  baseURL: string; // Base URL
  installPath: string; // Install page path
  discoveryPath: string; // Discovery page path
  privacyPath: string; // Privacy policy path
  termsPath: string; // Terms of service path
  walletPath: string; // Wallet page path
  retailerRulesPath: string; // Retailer rules page path
  securitySoftwareHelpPath: string; // Security software whitelist help page path
  changelogPath: string; // Changelog page path
}

// 邀请奖励配置（全局配置）
export interface InviteConfig {
  // 邀请者购买时的奖励天数
  inviterPurchaseRewardDays: number;
  // 被邀请者购买时的奖励天数
  purchaseRewardDays: number;
}

// 公告信息
export interface Announcement {
  id: string; // 公告唯一ID，用于跟踪关闭状态
  message: string; // 公告文字内容
  linkUrl?: string; // 可选：点击跳转链接
  linkText?: string; // 可选：链接文字
  expiresAt?: number; // 可选：公告过期时间戳（Unix秒），为0表示不过期
}

// App 配置（全局配置）
export interface AppConfig {
  appLinks: AppLinks;
  inviteReward: InviteConfig;
  minClientVersion?: string; // 最低客户端版本要求，低于此版本强制升级
  announcement?: Announcement; // 公告信息
}

// 链接有效期选项
export interface LinkExpirationOption {
  label: string;       // 显示文本 "1天"
  days: number;        // 天数
  isDefault?: boolean; // 是否默认选中
}

// ==================== GitHub Issues Types ====================

// Issue in list view
export interface GitHubIssue {
  number: number;
  title: string;
  body: string;
  state: string; // "open" | "closed"
  labels: string[];
  has_official: boolean;
  comment_count: number;
  created_at: string; // ISO date string
  updated_at: string;
}

// Comment on an issue
export interface GitHubComment {
  id: number;
  body: string;
  is_official: boolean;
  created_at: string;
}

// Issue with comments (detail view)
export interface GitHubIssueDetail extends GitHubIssue {
  comments: GitHubComment[];
}

// List issues response
export interface GitHubIssuesListResponse {
  issues: GitHubIssue[];
  page: number;
  per_page: number;
  has_more: boolean;
}

// Create comment request
export interface CreateGitHubCommentRequest {
  body: string;
}

