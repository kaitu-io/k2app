package center

// ========================= 角色定义（位运算） =========================
// 使用位掩码支持多角色，一个 uint64 可存储 64 个角色
// JWT 中使用短字段名 "r" 存储角色值

const (
	// RoleUser 普通用户（默认角色）
	RoleUser uint64 = 1 << 0 // 1

	// RoleCMSAdmin CMS 管理员（可以管理 Payload CMS 所有内容）
	RoleCMSAdmin uint64 = 1 << 1 // 2

	// RoleCMSEditor CMS 编辑（只能编辑自己的内容）
	RoleCMSEditor uint64 = 1 << 2 // 4

	// RoleSuper 超级管理员（拥有所有权限）
	RoleSuper uint64 = 1 << 3 // 8

	// 预留 1<<4 到 1<<63 用于未来扩展
)

// RoleNames 角色名称映射（用于调试和日志）
var RoleNames = map[uint64]string{
	RoleUser:      "user",
	RoleCMSAdmin:  "cms_admin",
	RoleCMSEditor: "cms_editor",
	RoleSuper:     "super",
}

// HasRole 检查是否拥有指定角色
func HasRole(roles uint64, role uint64) bool {
	return (roles & role) != 0
}

// AddRole 添加角色
func AddRole(roles uint64, role uint64) uint64 {
	return roles | role
}

// RemoveRole 移除角色
func RemoveRole(roles uint64, role uint64) uint64 {
	return roles &^ role
}

// GetRoleNames 获取角色名称列表（用于调试）
func GetRoleNames(roles uint64) []string {
	var names []string
	for role, name := range RoleNames {
		if HasRole(roles, role) {
			names = append(names, name)
		}
	}
	return names
}

type DataLoginIdentify struct {
	Type  string `json:"type"`  // 登录方式类型
	Value string `json:"value"` // 登录方式值
}

// ContactInfo 联系方式信息
// 支持的类型: telegram, email, signal, whatsapp, wechat, line, other
type ContactInfo struct {
	Type  string `json:"type"`            // 联系方式类型
	Value string `json:"value"`           // 联系方式值（用户名/号码/链接）
	Label string `json:"label,omitempty"` // 自定义标签（type=other时使用）
}

// DataUser API 用户数据结构
type DataUser struct {
	UUID             string               `json:"uuid"`
	ExpiredAt        int64                `json:"expiredAt"`
	IsFirstOrderDone bool                 `json:"isFirstOrderDone"`
	InvitedByCode    *DataInviteCode      `json:"inviteCode"`
	LoginIdentifies  []DataLoginIdentify  `json:"loginIdentifies"` // 登录身份列表
	Device           *DataDevice          `json:"device"`
	DeviceCount      int64                `json:"deviceCount"`
	Language         string               `json:"language"`                 // 用户语言偏好
	IsRetailer       bool                 `json:"isRetailer,omitempty"`     // 是否为分销商（仅管理员可见）
	RetailerConfig   *DataRetailerConfig  `json:"retailerConfig,omitempty"` // 分销商配置（仅管理员可见）
	Wallet           *DataWallet          `json:"wallet,omitempty"`         // 钱包信息（仅管理员可见）
	Roles            uint64               `json:"roles"`                    // 角色位掩码
}

// DataRetailerConfig 分销商配置数据结构
type DataRetailerConfig struct {
	// 等级系统
	Level             int    `json:"level"`             // 等级：1=L1推荐者, 2=L2分销商, 3=L3优质分销商, 4=L4合伙人
	LevelName         string `json:"levelName"`         // 等级名称
	FirstOrderPercent int    `json:"firstOrderPercent"` // 首单分成百分比 (0-100)
	RenewalPercent    int    `json:"renewalPercent"`    // 续费分成百分比 (0-100)
	PaidUserCount     int    `json:"paidUserCount"`     // 累计带来的付费用户数

	// 升级进度
	NextLevel            *int   `json:"nextLevel,omitempty"`            // 下一等级
	NextLevelName        string `json:"nextLevelName,omitempty"`        // 下一等级名称
	NextLevelRequirement *int   `json:"nextLevelRequirement,omitempty"` // 下一等级所需用户数
	NeedContentProof     bool   `json:"needContentProof"`               // 下一等级是否需要内容证明
	ProgressPercent      int    `json:"progressPercent"`                // 升级进度百分比 (0-100)

	// 内容证明（L3/L4审核用）
	ContentProof      string `json:"contentProof,omitempty"`      // JSON: 社媒链接、推广内容等
	ContentVerifiedAt *int64 `json:"contentVerifiedAt,omitempty"` // 内容审核通过时间戳

	// 联系方式
	Contacts []ContactInfo `json:"contacts,omitempty"` // 联系方式列表
}

// DataWallet 钱包数据结构
type DataWallet struct {
	Balance          int64 `json:"balance"`          // 总余额（美分）
	AvailableBalance int64 `json:"availableBalance"` // 可用余额（美分）
	FrozenBalance    int64 `json:"frozenBalance"`    // 冻结余额（美分）
	TotalIncome      int64 `json:"totalIncome"`      // 累计收入（美分）
	TotalWithdrawn   int64 `json:"totalWithdrawn"`   // 累计提现（美分）
}

// DataWalletChange 钱包变更记录数据结构
type DataWalletChange struct {
	ID           uint64 `json:"id"`                     // 变更记录ID
	Type         string `json:"type"`                   // 类型（income/expense）
	Amount       int64  `json:"amount"`                 // 变更金额（美分，正负表示增减）
	BalanceAfter int64  `json:"balanceAfter"`           // 变更后余额（美分）
	Description  string `json:"description,omitempty"`  // 描述
	FrozenUntil  *int64 `json:"frozenUntil,omitempty"`  // 冻结至（Unix时间戳）
	CreatedAt    int64  `json:"createdAt"`              // 创建时间（Unix时间戳）
}

// DataDevice API 设备数据结构
type DataDevice struct {
	UDID            string `json:"udid"`
	Remark          string `json:"remark"`
	TokenLastUsedAt int64  `json:"tokenLastUsedAt"`
}

// AdminDeviceData 管理员设备详情数据结构
type AdminDeviceData struct {
	UDID            string `json:"udid"`            // 设备唯一标识
	Remark          string `json:"remark"`          // 设备备注
	TokenIssueAt    int64  `json:"tokenIssueAt"`    // Token 签发时间
	TokenLastUsedAt int64  `json:"tokenLastUsedAt"` // Token 最后使用时间
	AppVersion      string `json:"appVersion"`      // 应用版本
	AppPlatform     string `json:"appPlatform"`     // 平台（darwin/windows/linux/ios/android）
	AppArch         string `json:"appArch"`         // CPU架构（amd64/arm64）
	CreatedAt       int64  `json:"createdAt"`       // 创建时间
	UpdatedAt       int64  `json:"updatedAt"`       // 更新时间
}

// IssueDeviceTokenResponse 设备 token 签发响应
type IssueDeviceTokenResponse struct {
	AccessToken  string `json:"accessToken"`           // 访问令牌
	RefreshToken string `json:"refreshToken"`          // 刷新令牌
	IssuedAt     int64  `json:"issuedAt"`              // 签发时间（与数据库 TokenIssueAt 一致）
	ExpiresIn    int64  `json:"expiresIn"`             // 过期时间（秒）
	Password     string `json:"password,omitempty"`    // 设备密码（用于 k2oc 协议 RADIUS 认证）
}

// DataAuthResult 认证结果数据结构
type DataAuthResult struct {
	AccessToken  string `json:"accessToken"`            // 访问令牌
	RefreshToken string `json:"refreshToken"`           // 刷新令牌
	IssuedAt     int64  `json:"issuedAt"`               // 签发时间
	Password     string `json:"password,omitempty"`     // 设备密码（已废弃，auth-with-device 接口已停用）
}

// DataWebLoginResponse Web登录响应数据结构
// 注意：Web端使用HttpOnly Cookie认证，tokens已通过cookie设置，response只返回user信息
type DataWebLoginResponse struct {
	User DataWebLoginUser `json:"user"` // 用户信息
}

// DataWebLoginUser Web登录用户信息
type DataWebLoginUser struct {
	ID      uint64 `json:"id"`      // 用户ID
	Email   string `json:"email"`   // 用户邮箱
	IsAdmin bool   `json:"isAdmin"` // 是否管理员（向后兼容）
	Roles   uint64 `json:"roles"`   // 角色位掩码
}

// DataAccessKey API访问密钥数据结构
type DataAccessKey struct {
	AccessKey string `json:"accessKey" example:"ak-c1lxdvyps05pj2qgv090"` // API访问密钥
}

// DataLoginRequest 登录请求数据结构
type DataLoginRequest struct {
	Email            string `json:"email" binding:"required,email"`      // 邮箱
	VerificationCode string `json:"verificationCode" binding:"required"` // 验证码
	UDID             string `json:"udid" binding:"required"`             // 设备ID
	Remark           string `json:"remark"`                              // 设备备注
	Language         string `json:"language"`                            // 用户语言偏好（可选）
	InviteCode       string `json:"inviteCode"`                          // 邀请码（可选，仅未激活用户可设置）
}

// DataWebLoginRequest Web登录请求数据结构（无设备信息）
type DataWebLoginRequest struct {
	Email            string `json:"email" binding:"required,email"`      // 邮箱
	VerificationCode string `json:"verificationCode" binding:"required"` // 验证码
	Language         string `json:"language"`                            // 用户语言偏好（可选）
	InviteCode       string `json:"inviteCode"`                          // 邀请码（可选，仅未激活用户可设置）
}

// DataRefreshTokenRequest 刷新 token 请求数据结构
type DataRefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

// DataLogoutRequest 登出请求数据结构
type DataLogoutRequest struct {
	UDID string `json:"udid" binding:"required"`
}

// SendCodeResponse 发送验证码响应数据结构
type SendCodeResponse struct {
	UserExists       bool `json:"userExists"`       // 用户是否存在
	IsActivated      bool `json:"isActivated"`      // 用户账号是否已激活
	IsFirstOrderDone bool `json:"isFirstOrderDone"` // 用户是否完成首单
}

// AddMemberRequest 添加成员请求数据结构
type AddMemberRequest struct {
	MemberEmail string `json:"memberEmail" binding:"required,email" example:"member@example.com"` // 成员邮箱
}

// DataDelegate 代付人数据结构
type DataDelegate struct {
	UUID            string              `json:"uuid"`            // 代付人UUID
	LoginIdentifies []DataLoginIdentify `json:"loginIdentifies"` // 登录身份列表
}

// CreateOrderForUsersRequest 为指定用户创建订单请求数据结构
type CreateOrderForUsersRequest struct {
	Preview      bool     `json:"preview" example:"false"`                     // 是否预览模式
	Plan         string   `json:"plan" binding:"required" example:"pro_month"` // 套餐ID
	CampaignCode string   `json:"campaignCode" example:"SAVE20"`               // 优惠码（可选）
	TargetUsers  []uint64 `json:"targetUsers" binding:"required"`              // 为哪些用户购买
}


// DataTunnelAuthRequest 节点认证请求数据结构
type DataTunnelAuthRequest struct {
	UDID     string `json:"udid" binding:"required"`     // 设备唯一标识
	TunnelID uint64 `json:"tunnelId" binding:"required"` // 节点ID
}

// DataTunnelAuthResult 节点认证结果数据结构
type DataTunnelAuthResult struct {
	Token     string `json:"token"`      // JWT令牌
	ExpiredAt int64  `json:"expired_at"` // 过期时间（绝对时间戳，秒）
	Ipv4      string `json:"ipv4"`       // IPv4地址
	Ipv6      string `json:"ipv6"`       // IPv6地址
}

// DataSlaveNode API 物理节点数据结构
// Note: All nodes in DB are active by design - no IsAlive field needed.
type DataSlaveNode struct {
	ID        uint64 `json:"id"`        // 节点ID
	Name      string `json:"name"`      // 节点名称
	Country   string `json:"country"`   // 国家代码
	Region    string `json:"region"`    // 服务器区域
	Ipv4      string `json:"ipv4"`      // IPv4地址
	Ipv6      string `json:"ipv6"`      // IPv6地址
	Load      int    `json:"load"`      // 当前负载
	UpdatedAt int64  `json:"updatedAt"` // 最后更新时间（Unix 秒）

	// Evaluation fields for tunnel scoring
	TrafficUsagePercent   float64 `json:"trafficUsagePercent"`   // Traffic quota usage (0-100)
	BandwidthUsagePercent float64 `json:"bandwidthUsagePercent"` // Bandwidth usage (0-100)
}

// DataTunnelInstance contains cloud instance info for a tunnel
// This provides billing and traffic data from the associated CloudInstance
type DataTunnelInstance struct {
	TrafficTotalBytes int64   `json:"trafficTotalBytes"` // Total traffic allowance in bytes
	TrafficRatio      float64 `json:"trafficRatio"`      // Traffic consumption ratio (0-1, e.g., 0.75 = 75% used)
	BillingCycleEndAt int64   `json:"billingCycleEndAt"` // Billing cycle end timestamp (Unix seconds)
	TimeRatio         float64 `json:"timeRatio"`         // Time consumption ratio (0-1, e.g., 0.5 = 50% of cycle elapsed)
}

// DataSlaveTunnel API tunnel data structure
type DataSlaveTunnel struct {
	ID           uint64         `json:"id"`           // Tunnel ID
	Domain       string         `json:"domain"`       // Tunnel domain
	Name         string         `json:"name"`         // Tunnel name
	Protocol     TunnelProtocol `json:"protocol"`     // Tunnel protocol (k2v4, k2wss, k2oc)
	Port         int64          `json:"port"`         // Tunnel port
	HopPortStart int64          `json:"hopPortStart"` // Port hopping range start (0 = disabled)
	HopPortEnd   int64          `json:"hopPortEnd"`   // Port hopping range end (0 = disabled)
	Node         DataSlaveNode  `json:"node"`         // Associated physical node
	Instance     *DataTunnelInstance `json:"instance,omitempty"` // Cloud instance data (if linked via IP)
	ServerUrl    string         `json:"serverUrl,omitempty"` // Computed k2v5 connection URL (only for GET /tunnels/k2v5)
}

// DataSlaveTunnelListResponse 节点列表响应数据结构
type DataSlaveTunnelListResponse struct {
	Items         []DataSlaveTunnel `json:"items"`                   // 节点列表
	ECHConfigList string            `json:"echConfigList,omitempty"` // Base64 encoded ECHConfigList for K2v4 connections
}

// Traffic 设备流量使用情况
type SlaveDeviceAcct struct {
	UDID          string `json:"udid"`          // 设备ID
	UploadBytes   int64  `json:"uploadBytes"`   // 本次上报期间的上传字节数
	DownloadBytes int64  `json:"downloadBytes"` // 本次上报期间的下载字节数
	Seconds       int64  `json:"seconds"`       // 本次上报期间的活跃时间（秒）
	UpdatedAt     int64  `json:"updatedAt"`     // 上报时间戳（秒）
}

// SlaveTunnelHealth 节点健康指标
type SlaveTunnelHealth struct {
	CPUUsage    float64 `json:"cpuUsage"`    // CPU 使用率
	MemoryUsage float64 `json:"memoryUsage"` // 内存使用率
	DiskUsage   float64 `json:"diskUsage"`   // 磁盘使用率
	NetworkIn   int64   `json:"networkIn"`   // 网络入站流量（字节）
	NetworkOut  int64   `json:"networkOut"`  // 网络出站流量（字节）
	Connections int     `json:"connections"` // 当前连接数

	// 网络性能指标（关键指标）
	NetworkSpeedMbps  float64 `json:"networkSpeedMbps"`  // 网络峰值速度 (Mbps)
	BandwidthUpMbps   float64 `json:"bandwidthUpMbps"`   // 上行带宽 (Mbps)
	BandwidthDownMbps float64 `json:"bandwidthDownMbps"` // 下行带宽 (Mbps)
	NetworkLatencyMs  float64 `json:"networkLatencyMs"`  // 网络延迟 (毫秒)
	PacketLossPercent float64 `json:"packetLossPercent"` // 丢包率 (百分比)

	// 月度流量追踪（用于计费和负载计算）
	BillingCycleEndAt        int64 `json:"billingCycleEndAt"`        // 计费周期结束时间戳（Unix秒）
	MonthlyTrafficLimitBytes int64 `json:"monthlyTrafficLimitBytes"` // 月度流量限制（字节），0表示无限制
	UsedTrafficBytes         int64 `json:"usedTrafficBytes"`         // 当前周期已使用流量（字节）
}

// SlaveReportRequest 节点报告请求
type SlaveReportRequest struct {
	UpdatedAt int64             `json:"updatedAt" binding:"required"` // 报告时间
	Devices   []SlaveDeviceAcct `json:"devices"`                      // 设备流量统计
	Health    SlaveTunnelHealth `json:"health"`                       // 节点健康指标（可选）
}

// DataInviteCode 邀请码数据结构（用于 DataUser.InvitedByCode，表示"被谁邀请"）
// 注意：邀请配置不在此返回，客户端应通过 /api/app/config 获取全局邀请配置
type DataInviteCode struct {
	Code      string `json:"code"`      // 邀请码
	CreatedAt int64  `json:"createdAt"` // 创建时间
	Remark    string `json:"remark"`    // 邀请码备注
	Link      string `json:"link"`      // 邀请码链接
}

// DataInviteCode 邀请码数据结构
type DataMyInviteCode struct {
	Code           string       `json:"code"`           // 邀请码
	CreatedAt      int64        `json:"createdAt"`      // 创建时间
	Remark         string       `json:"remark"`         // 邀请码备注
	Link           string       `json:"link"`           // 邀请码链接
	Config         InviteConfig `json:"config"`         // 邀请配置
	RegisterCount  int64        `json:"registerCount"`  // 注册次数（仅统计，无奖励）
	PurchaseCount  int64        `json:"purchaseCount"`  // 购买次数
	PurchaseReward int64        `json:"purchaseReward"` // 购买奖励（天数）
}

type DataOrder struct {
	ID                   string     `json:"id"`
	UUID                 string     `json:"uuid"`
	Title                string     `json:"title"`
	OriginAmount         uint64     `json:"originAmount"`
	CampaignReduceAmount uint64     `json:"campaignReduceAmount"`
	PayAmount            uint64     `json:"payAmount"`
	IsPaid               bool       `json:"isPaid"`
	CreatedAt            int64      `json:"createdAt"`
	Campaign             *Campaign  `json:"campaign"`
	Plan                 *Plan      `json:"plan"`
	PayAt                int64      `json:"payAt"`
	ForUsers             []DataUser `json:"forUsers"`
	ForMyself            bool       `json:"forMyself"`
}

type DataProHistory struct {
	Type      VipChangeType `json:"type"`
	Days      int        `json:"days"`
	Reason    string     `json:"reason"`
	CreatedAt int64      `json:"createdAt"`
	Order     *DataOrder `json:"order"`
}

// AdminOrderListRequest 管理员订单列表请求
type AdminOrderListRequest struct {
	Page           int    `json:"page" form:"page"`
	PageSize       int    `json:"pageSize" form:"pageSize"`
	LoginProvider  string `json:"loginProvider" form:"loginProvider"` // email, google, apple等
	LoginIdentity  string `json:"loginIdentity" form:"loginIdentity"` // 对应的值
	IsPaid         *bool  `json:"isPaid" form:"isPaid"`
	CreatedAtStart int64  `json:"createdAtStart" form:"createdAtStart"`
	CreatedAtEnd   int64  `json:"createdAtEnd" form:"createdAtEnd"`
}

// ==================== Resource 层次结构 ====================

// ResourceUser 用户资源（精简版，用于关联展示）
type ResourceUser struct {
	UUID  string `json:"uuid"`
	Email string `json:"email,omitempty"` // 主邮箱
}

// ResourceCashback 返现资源（用于订单关联展示）
type ResourceCashback struct {
	RetailerUUID  string `json:"retailerUuid"`          // 分销商UUID
	RetailerEmail string `json:"retailerEmail"`         // 分销商Email
	Amount        int64  `json:"amount"`                // 返现金额（美分）
	Status        string `json:"status"`                // 返现状态: pending, completed
	FrozenUntil   int64  `json:"frozenUntil,omitempty"` // 冻结到期时间戳
}

// ResourceWithdrawAccount 提现账户资源
type ResourceWithdrawAccount struct {
	AccountType string `json:"accountType"`           // 渠道类型: tron, polygon, bsc, arbitrum, paypal
	AccountID   string `json:"accountId"`             // 收款标识（钱包地址/PayPal邮箱）
	Currency    string `json:"currency,omitempty"`    // 币种（仅加密货币）: usdt, usdc
}

// ResourceTransaction 交易凭证资源
type ResourceTransaction struct {
	TxHash      string `json:"txHash,omitempty"`      // 交易凭证（区块链哈希/PayPal交易ID）
	ExplorerURL string `json:"explorerUrl,omitempty"` // 查看链接
}

// ResourcePlan 套餐资源（精简版，用于关联展示）
type ResourcePlan struct {
	PID   string `json:"pid"`   // 套餐ID
	Label string `json:"label"` // 套餐名称
	Price uint64 `json:"price"` // 价格（美分）
	Month int    `json:"month"` // 月数
}

// ResourceCampaign 优惠活动资源（精简版，用于关联展示）
type ResourceCampaign struct {
	Code  string `json:"code"`  // 活动代码
	Name  string `json:"name"`  // 活动名称
	Type  string `json:"type"`  // discount, coupon
	Value uint64 `json:"value"` // 优惠值
}

// ResourceDevice 设备资源（精简版，用于关联展示）
type ResourceDevice struct {
	UDID   string `json:"udid"`   // 设备唯一标识
	Remark string `json:"remark"` // 设备备注
}

// ==================== Admin List Items ====================

// AdminOrderListItem 管理员订单列表项
type AdminOrderListItem struct {
	UUID                 string            `json:"uuid"`
	Title                string            `json:"title"`
	OriginAmount         uint64            `json:"originAmount"`
	CampaignReduceAmount uint64            `json:"campaignReduceAmount"`
	PayAmount            uint64            `json:"payAmount"`
	IsPaid               bool              `json:"isPaid"`
	CreatedAt            int64             `json:"createdAt"`
	PaidAt               int64             `json:"paidAt"`
	User                 ResourceUser      `json:"user"`               // 购买用户
	Cashback             *ResourceCashback `json:"cashback,omitempty"` // 分销返现信息（可选）
}

// AdminWithdrawListItem 管理员提现请求列表项
type AdminWithdrawListItem struct {
	ID          uint64                  `json:"id"`
	CreatedAt   int64                   `json:"createdAt"`
	User        ResourceUser            `json:"user"`                  // 提现用户
	Amount      int64                   `json:"amount"`                // 申请提现金额（美分）
	FeeAmount   int64                   `json:"feeAmount"`             // 手续费（美分）
	NetAmount   int64                   `json:"netAmount"`             // 实际到账金额（美分）
	Status      string                  `json:"status"`                // pending, approved, rejected, completed
	Account     ResourceWithdrawAccount `json:"account"`               // 提现账户
	Transaction *ResourceTransaction    `json:"transaction,omitempty"` // 交易凭证（可选）
	Remark      string                  `json:"remark,omitempty"`      // 备注
	ProcessedAt *int64                  `json:"processedAt,omitempty"` // 处理完成时间
}

// AdminOrderListResponse 管理员订单列表响应
type AdminOrderListResponse struct {
	Items      []AdminOrderListItem `json:"items"`
	Pagination Pagination           `json:"pagination"`
}

type DataPlan struct {
	PID         string `json:"pid"`
	Label       string `json:"label"`
	Price       uint64 `json:"price"`
	OriginPrice uint64 `json:"originPrice"`
	Month       int    `json:"month"`
	Highlight   bool   `json:"highlight"`
	IsActive    bool   `json:"isActive"`
}

// Response_SlaveDeviceCheckAuthResult 节点设备认证结果响应
type Response_SlaveDeviceCheckAuthResult struct {
	Code int                        `json:"code" example:"200"`        // 响应码
	Msg  string                     `json:"message" example:"success"` // 响应消息
	Data SlaveDeviceCheckAuthResult `json:"data"`                      // 认证结果数据
}

// Response_ResolveDomainResponse 域名解析响应
type Response_ResolveDomainResponse struct {
	Code int                   `json:"code" example:"200"`        // 响应码
	Msg  string                `json:"message" example:"success"` // 响应消息
	Data ResolveDomainResponse `json:"data"`                      // 域名解析结果
}

// =================== EDM 相关类型定义 ===================

// EmailTemplateRequest 邮件模板请求
type EmailTemplateRequest struct {
	Name        string  `json:"name" binding:"required"`        // 模板名称
	Language    string  `json:"language" binding:"required"`    // BCP 47 语言标签
	Subject     string  `json:"subject" binding:"required"`     // 邮件主题
	Content     string  `json:"content" binding:"required"`     // 邮件内容
	Description string  `json:"description"`                     // 模板描述
	IsActive    bool    `json:"isActive"`                        // 是否启用
	OriginID    *uint64 `json:"originId"`                        // 源模板ID，null表示这是原始模板
}


// EmailTemplateResponse 邮件模板响应
type EmailTemplateResponse struct {
	ID          uint64  `json:"id"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
	Name        string  `json:"name"`
	Language    string  `json:"language"`
	Subject     string  `json:"subject"`
	Content     string  `json:"content"`
	Description string  `json:"description"`
	IsActive    bool    `json:"isActive"`
	OriginID    *uint64 `json:"originId"`    // 源模板ID，null表示这是原始模板
	IsOriginal  bool    `json:"isOriginal"`  // 是否为原始模板（计算字段）
}






// =================== Campaign 相关类型定义 ===================

// CampaignRequest 优惠活动请求
type CampaignRequest struct {
	Code        string `json:"code" binding:"required"`                        // 活动代码
	Name        string `json:"name" binding:"required"`                        // 活动名称
	Type        string `json:"type" binding:"required"`                        // discount, coupon
	Value       uint64 `json:"value" binding:"required"`                       // 优惠值
	StartAt     int64  `json:"startAt" binding:"required"`                     // 开始时间
	EndAt       int64  `json:"endAt" binding:"required"`                       // 结束时间
	Description string `json:"description"`                                     // 活动描述
	IsActive    bool   `json:"isActive"`                                        // 是否启用
	MatcherType string `json:"matcherType" binding:"required"`                 // first_order, vip, all
	MaxUsage    int64  `json:"maxUsage"`                                        // 最大使用次数（0=无限制）
}

// CampaignResponse 优惠活动响应
type CampaignResponse struct {
	ID          uint64 `json:"id"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Value       uint64 `json:"value"`
	StartAt     int64  `json:"startAt"`
	EndAt       int64  `json:"endAt"`
	Description string `json:"description"`
	IsActive    bool   `json:"isActive"`
	MatcherType string `json:"matcherType"`
	UsageCount  int64  `json:"usageCount"`
	MaxUsage    int64  `json:"maxUsage"`
}

// CampaignListResponse 优惠活动列表响应
type CampaignListResponse struct {
	Items      []CampaignResponse `json:"items"`
	Pagination Pagination         `json:"pagination"`
}

// ========================= EDM 邮件营销类型定义 =========================

// UserFilter 用户筛选条件
type UserFilter struct {
	// 基础筛选
	SpecificUsers []string `json:"specificUsers"` // 指定特定用户的UUIDs（如果指定，其他筛选条件将被忽略）

	// 用户状态筛选（单选）(not_activated, activated_no_order, first_order_done, first_order_done_but_expired)
	// - not_activated: 注册但未激活
	// - activated_no_order: 已激活但未完成首单
	// - first_order_done: 已完成首单（不管是否过期）
	// - first_order_done_but_expired: 已完成首单但已过期
	// 空字符串表示不筛选用户状态
	UserStatus string `json:"userStatus"`

	// 日期筛选
	ActivatedDate struct {
		Start string `json:"start"` // 格式: YYYY-MM-DD
		End   string `json:"end"`   // 格式: YYYY-MM-DD
	} `json:"activatedDate"` // 激活日期范围（基于 activated_at 字段）

	// 过期天数筛选（单选）- 精确到天，适合定期任务
	// - expire_in_30: 30天后过期（第30天）
	// - expire_in_14: 14天后过期（第14天）
	// - expire_in_7: 7天后过期（第7天）
	// - expire_in_3: 3天后过期（第3天）
	// - expire_in_1: 1天内过期（第1天）
	// - expired_1: 已过期1天（过期后第1天）
	// - expired_3: 已过期3天（过期后第3天）
	// - expired_7: 已过期7天（过期后第7天）
	// - expired_14: 已过期14天（过期后第14天）
	// - expired_30: 已过期30天（过期后第30天）
	// - expired: 已过期（超过30天）
	// 空字符串表示不筛选过期状态
	ExpireDays string `json:"expireDays"`

	// 分销商等级筛选（多选）
	// - 1: L1 推荐者
	// - 2: L2 分销商
	// - 3: L3 优质分销商
	// - 4: L4 合伙人
	// 空数组表示不筛选分销商等级
	RetailerLevels []int `json:"retailerLevels"`
}


// ========================= 用户语言偏好相关类型定义 =========================

// UpdateLanguageRequest 更新用户语言偏好请求
type UpdateLanguageRequest struct {
	Language string `json:"language" binding:"required"` // 语言代码：en-US, zh-CN, ja 等
}

// ========================= 钱包相关类型定义 =========================

// CreateWithdrawAccountRequest 创建提现账户请求
type CreateWithdrawAccountRequest struct {
	// 渠道类型: tron, polygon, bsc, arbitrum, paypal
	AccountType WithdrawAccountType `json:"accountType" binding:"required"`
	// 收款标识（加密货币=钱包地址，PayPal=邮箱地址）
	AccountID string `json:"accountId" binding:"required"`
	// 币种: usdt, usdc（加密货币）或 usd（PayPal）
	Currency Currency `json:"currency" binding:"required"`
	// 账户标签（可选，如"主账户"、"公司账户"）
	Label string `json:"label"`
}

// CreateWithdrawRequest 创建提现申请请求
type CreateWithdrawRequest struct {
	Amount            int64  `json:"amount" binding:"required,gt=0"`       // 提现金额（美分）
	WithdrawAccountID uint64 `json:"withdrawAccountId" binding:"required"` // 提现账户ID
	UserRemark        string `json:"userRemark"`                           // 用户备注（可选）
}

// ========================= 订单退款相关类型定义 =========================

// RefundOrderRequest 退款订单请求
type RefundOrderRequest struct {
	Reason string `json:"reason" binding:"required"` // 退款原因
}

// ========================= EDM Task Types (基于 Asynq) =========================

// CreateEDMTaskRequest 创建EDM发送任务请求
type CreateEDMTaskRequest struct {
	Name        string      `json:"name" binding:"required"`                         // 任务名称
	TemplateID  uint64      `json:"templateId" binding:"required"`                   // 模板ID
	UserFilters UserFilter  `json:"userFilters" binding:"required"`                  // 用户筛选条件
	Type        string      `json:"type" binding:"required,oneof=once repeat"`       // 任务类型：once=单次, repeat=循环
	ScheduledAt *int64      `json:"scheduledAt"`                                     // 首次发送时间（Unix时间戳），null=立即发送
	RepeatEvery *int64      `json:"repeatEvery"`                                     // 循环间隔（秒），type=repeat时必填
}

// PreviewEDMTargetsRequest 预览EDM目标用户请求
type PreviewEDMTargetsRequest struct {
	UserFilters UserFilter `json:"userFilters" binding:"required"` // 用户筛选条件
}

// PreviewEDMTargetsResponse 预览EDM目标用户响应
type PreviewEDMTargetsResponse struct {
	TotalCount  int              `json:"totalCount"`  // 目标用户总数
	SampleUsers []PreviewEDMUser `json:"sampleUsers"` // 样本用户（最多10个）
}

// PreviewEDMUser 预览用户信息
type PreviewEDMUser struct {
	UUID     string `json:"uuid"`     // 用户UUID
	Email    string `json:"email"`    // 邮箱地址
	Language string `json:"language"` // 语言偏好
	IsPro    bool   `json:"isPro"`    // 是否Pro用户
}

// ========================= 邮件发送日志类型定义 =========================

// EmailSendLogResponse 邮件发送日志响应
type EmailSendLogResponse struct {
	ID           uint64  `json:"id"`
	CreatedAt    int64   `json:"createdAt"`
	BatchID      string  `json:"batchId"`      // 批次ID（asynq任务ID）
	TemplateID   uint64  `json:"templateId"`   // 模板ID
	TemplateName string  `json:"templateName"` // 模板名称
	UserID       uint64  `json:"userId"`       // 用户ID
	UserUUID     string  `json:"userUuid"`     // 用户UUID
	Email        string  `json:"email"`        // 目标邮箱
	Language     string  `json:"language"`     // 使用的语言
	Status       string  `json:"status"`       // 发送状态
	SentAt       *int64  `json:"sentAt"`       // 发送时间
	ErrorMsg     *string `json:"errorMsg"`     // 错误信息
}

// ListEmailSendLogsRequest 邮件发送日志列表请求
type ListEmailSendLogsRequest struct {
	BatchID    *string `json:"batchId" form:"batchId"`         // 按批次ID筛选（asynq任务ID）
	TemplateID *uint64 `json:"templateId" form:"templateId"`   // 按模板ID筛选
	UserID     *uint64 `json:"userId" form:"userId"`           // 按用户ID筛选
	Status     *string `json:"status" form:"status"`           // 按状态筛选 (pending/sent/failed/skipped)
	Email      *string `json:"email" form:"email"`             // 按邮箱筛选（模糊匹配）
	Page       int     `json:"page" form:"page"`               // 页码
	PageSize   int     `json:"pageSize" form:"pageSize"`       // 每页数量（默认100，最大200）
}

// ListEmailSendLogsResponse 邮件发送日志列表响应
type ListEmailSendLogsResponse struct {
	Items      []EmailSendLogResponse `json:"items"`
	Pagination Pagination             `json:"pagination"`
	Stats      EmailSendLogStats      `json:"stats"` // 统计信息
}

// EmailSendLogStats 邮件发送日志统计
type EmailSendLogStats struct {
	TotalCount   int64 `json:"totalCount"`   // 总数
	SentCount    int64 `json:"sentCount"`    // 已发送
	FailedCount  int64 `json:"failedCount"`  // 失败
	PendingCount int64 `json:"pendingCount"` // 待发送
	SkippedCount int64 `json:"skippedCount"` // 跳过
}

// ========================= 工单相关类型定义 =========================

// CreateTicketRequest 创建工单请求
type CreateTicketRequest struct {
	Subject    string `json:"subject" binding:"required,min=1,max=200"`  // Ticket subject
	Content    string `json:"content" binding:"required,min=1,max=5000"` // Ticket content
	FeedbackID string `json:"feedbackId,omitempty"`                      // Feedback ID for log correlation
}

// ========================= Device Statistics Types =========================

// PlatformCount represents device count for a specific platform
type PlatformCount struct {
	Platform string `json:"platform"` // darwin, windows, linux, ios, android, unknown
	Count    int64  `json:"count"`    // Number of devices
}

// VersionCount represents device count for a specific app version
type VersionCount struct {
	Version string `json:"version"` // App version (e.g., "0.3.15")
	Count   int64  `json:"count"`   // Number of devices
}

// ArchCount represents device count for a specific architecture
type ArchCount struct {
	Arch  string `json:"arch"`  // amd64, arm64, unknown
	Count int64  `json:"count"` // Number of devices
}

// OSVersionCount represents device count for a specific OS version
type OSVersionCount struct {
	OSVersion string `json:"osVersion"` // OS version (e.g., "macOS 14.5", "Windows 11")
	Count     int64  `json:"count"`     // Number of devices
}

// DeviceModelCount represents device count for a specific device model
type DeviceModelCount struct {
	DeviceModel string `json:"deviceModel"` // Device model (e.g., "MacBookPro18,1", "iPhone15,2")
	Count       int64  `json:"count"`       // Number of devices
}

// DeviceStatisticsResponse contains aggregated device statistics
type DeviceStatisticsResponse struct {
	// Total counts
	TotalDevices   int64 `json:"totalDevices"`   // Total registered devices
	UnknownDevices int64 `json:"unknownDevices"` // Devices with unknown platform
	DesktopDevices int64 `json:"desktopDevices"` // darwin + windows + linux
	MobileDevices  int64 `json:"mobileDevices"`  // ios + android

	// Breakdown by platform
	ByPlatform []PlatformCount `json:"byPlatform"`

	// Breakdown by version
	ByVersion []VersionCount `json:"byVersion"`

	// Breakdown by architecture
	ByArch []ArchCount `json:"byArch"`

	// Breakdown by OS version (top 10)
	ByOSVersion []OSVersionCount `json:"byOsVersion"`

	// Breakdown by device model (top 10)
	ByDeviceModel []DeviceModelCount `json:"byDeviceModel"`

	// Active devices (by last used time)
	Active24h int64 `json:"active24h"` // Devices used in last 24 hours
	Active7d  int64 `json:"active7d"`  // Devices used in last 7 days
	Active30d int64 `json:"active30d"` // Devices used in last 30 days
}

// ActiveDeviceItem represents a single active device in the list
type ActiveDeviceItem struct {
	UDID            string `json:"udid"`            // Device unique identifier
	UserEmail       string `json:"userEmail"`       // Owner's email
	UserUUID        string `json:"userUUID"`        // Owner's UUID
	AppPlatform     string `json:"appPlatform"`     // Platform (darwin/windows/linux/ios/android)
	AppVersion      string `json:"appVersion"`      // App version
	AppArch         string `json:"appArch"`         // CPU architecture
	OSVersion       string `json:"osVersion"`       // OS version (e.g., "macOS 14.5")
	DeviceModel     string `json:"deviceModel"`     // Device model (e.g., "MacBookPro18,1")
	TokenLastUsedAt int64  `json:"tokenLastUsedAt"` // Last activity timestamp
	CreatedAt       int64  `json:"createdAt"`       // Device registration time
}

// ActiveDevicesResponse contains list of active devices with pagination
type ActiveDevicesResponse struct {
	Items      []ActiveDeviceItem `json:"items"`
	Pagination Pagination         `json:"pagination"`
}

// ========================= Strategy System Types =========================

// StrategyRulesResponse rules sync response
type StrategyRulesResponse struct {
	Version   string           `json:"version"`
	UpdatedAt string           `json:"updatedAt"` // ISO 8601 format
	ETag      string           `json:"etag"`
	Rules     []map[string]any `json:"rules"`
	Protocols map[string]any   `json:"protocols"`
	Default   map[string]any   `json:"default"`
}

// TelemetryBatchRequest telemetry upload request
type TelemetryBatchRequest struct {
	DeviceID   string              `json:"deviceId" binding:"required"`
	AppVersion string              `json:"appVersion" binding:"required"`
	Events     []TelemetryEventDTO `json:"events" binding:"required,dive"`
}

// TelemetryEventDTO single telemetry event in batch
type TelemetryEventDTO struct {
	EventID      string         `json:"eventId" binding:"required"`
	Timestamp    int64          `json:"timestamp" binding:"required"`
	EventType    string         `json:"eventType" binding:"required,oneof=connection session anomaly feedback"`
	Context      map[string]any `json:"context,omitempty"`
	Decision     map[string]any `json:"decision,omitempty"`
	Outcome      map[string]any `json:"outcome,omitempty"`
	Satisfaction *int           `json:"satisfaction,omitempty"` // 1-5 star rating (null = no feedback)
}

// TelemetryBatchResponse telemetry upload response
type TelemetryBatchResponse struct {
	Accepted int      `json:"accepted"`
	Rejected int      `json:"rejected"`
	Errors   []string `json:"errors,omitempty"`
}

// ========================= Batch Script Execution Types =========================

// CreateBatchScriptRequest request to create a new script
type CreateBatchScriptRequest struct {
	Name            string `json:"name" binding:"required,max=128"`
	Description     string `json:"description" binding:"max=512"`
	Content         string `json:"content" binding:"required"` // Plain text script content
	ExecuteWithSudo bool   `json:"executeWithSudo"`            // Execute script with sudo privileges
}

// UpdateBatchScriptRequest request to update a script
type UpdateBatchScriptRequest struct {
	Name            string `json:"name" binding:"required,max=128"`
	Description     string `json:"description" binding:"max=512"`
	Content         string `json:"content" binding:"required"` // Plain text script content
	ExecuteWithSudo bool   `json:"executeWithSudo"`            // Execute script with sudo privileges
}

// BatchScriptResponse script response (without content)
type BatchScriptResponse struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	ExecuteWithSudo bool   `json:"executeWithSudo"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// BatchScriptDetailResponse script detail response (with decrypted content)
type BatchScriptDetailResponse struct {
	ID              uint64 `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Content         string `json:"content"` // Decrypted plain text
	ExecuteWithSudo bool   `json:"executeWithSudo"`
	CreatedAt       int64  `json:"createdAt"`
	UpdatedAt       int64  `json:"updatedAt"`
}

// CreateBatchTaskRequest request to create a batch execution task
type CreateBatchTaskRequest struct {
	ScriptID     uint64   `json:"scriptId" binding:"required"`
	NodeIDs      []uint64 `json:"nodeIds" binding:"required,min=1"` // Explicit node ID list
	ScheduleType string   `json:"scheduleType" binding:"required,oneof=once cron"`
	ExecuteAt    *int64   `json:"executeAt"`  // Required when scheduleType=once (milliseconds timestamp)
	CronExpr     string   `json:"cronExpr"`   // Required when scheduleType=cron
}

// BatchTaskResponse batch task response
type BatchTaskResponse struct {
	ID           uint64   `json:"id"`
	AsynqTaskID  string   `json:"asynqTaskId"`  // Asynq task ID for tracking
	ScriptID     uint64   `json:"scriptId"`
	ScriptName   string   `json:"scriptName"`
	NodeIDs      []uint64 `json:"nodeIds"`
	ScheduleType string   `json:"scheduleType"`
	ExecuteAt    *int64   `json:"executeAt"`
	CronExpr     string   `json:"cronExpr"`
	Status       string   `json:"status"`
	CurrentIndex int      `json:"currentIndex"`
	TotalNodes   int      `json:"totalNodes"`
	CreatedAt    int64    `json:"createdAt"`
	CompletedAt  *int64   `json:"completedAt"`
	ParentTaskID *uint64  `json:"parentTaskId,omitempty"` // Parent task ID for retry tracking
	IsEnabled    bool     `json:"isEnabled"`              // Whether scheduled task is enabled
}

// TaskResultItem single node execution result
type TaskResultItem struct {
	NodeID    uint64  `json:"nodeId"`
	NodeName  string  `json:"nodeName"`  // Joined from SlaveNode.Name
	NodeIPv4  string  `json:"nodeIpv4"`  // Joined from SlaveNode.Ipv4
	NodeIndex int     `json:"nodeIndex"`
	Status    string  `json:"status"`
	Stdout    string  `json:"stdout"`
	Stderr    string  `json:"stderr"`
	ExitCode  int     `json:"exitCode"`
	Error     string  `json:"error"`
	StartedAt *int64  `json:"startedAt"`
	EndedAt   *int64  `json:"endedAt"`
	Duration  *int64  `json:"duration"` // Milliseconds (endedAt - startedAt)
}

// BatchTaskDetailResponse batch task detail with all node results
type BatchTaskDetailResponse struct {
	BatchTaskResponse                  // Embed base response
	Results           []TaskResultItem `json:"results"`
	FailedCount       int              `json:"failedCount"`
	SuccessCount      int              `json:"successCount"`
	ParentTaskID      *uint64          `json:"parentTaskId,omitempty"`
}

// RetryBatchTaskRequest request to retry failed nodes in a task
type RetryBatchTaskRequest struct {
	NodeIDs []uint64 `json:"nodeIds"` // Optional: specific nodes to retry, empty = all failed
}

// RetryBatchTaskResponse response for retry request
type RetryBatchTaskResponse struct {
	TaskID uint64 `json:"taskId"` // New task ID for retry
}

// ScheduleBatchTaskRequest request to update task schedule
type ScheduleBatchTaskRequest struct {
	CronExpr  string `json:"cronExpr" binding:"required"`
	IsEnabled bool   `json:"isEnabled"`
}

// BatchScriptVersionResponse script version info
type BatchScriptVersionResponse struct {
	Version   int    `json:"version"`
	CreatedAt int64  `json:"createdAt"`
	CreatedBy uint64 `json:"createdBy"`
}

// BatchScriptVersionDetailResponse script version with content
type BatchScriptVersionDetailResponse struct {
	Version   int    `json:"version"`
	Content   string `json:"content"` // Decrypted
	CreatedAt int64  `json:"createdAt"`
	CreatedBy uint64 `json:"createdBy"`
}

// TestBatchScriptRequest request to test script on a node
type TestBatchScriptRequest struct {
	NodeID uint64 `json:"nodeId" binding:"required"`
}

// TestBatchScriptResponse response for script test
type TestBatchScriptResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	Duration int64  `json:"duration"` // Milliseconds
	Error    string `json:"error,omitempty"`
}

// CloudOperationResponse cloud operation status
type CloudOperationResponse struct {
	ID          uint64 `json:"id"`
	InstanceID  uint64 `json:"instanceId"`
	Operation   string `json:"operation"`
	Status      string `json:"status"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt *int64 `json:"completedAt,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ScheduledTaskInfo info about scheduled cron task
type ScheduledTaskInfo struct {
	ID           uint64   `json:"id"`
	ScriptID     uint64   `json:"scriptId"`
	ScriptName   string   `json:"scriptName"`
	CronExpr     string   `json:"cronExpr"`
	IsEnabled    bool     `json:"isEnabled"`
	NodeIDs      []uint64 `json:"nodeIds"`
	TotalNodes   int      `json:"totalNodes"`
	NextRunAt    *int64   `json:"nextRunAt,omitempty"` // Next scheduled execution
	LastRunAt    *int64   `json:"lastRunAt,omitempty"` // Last execution time
	LastStatus   string   `json:"lastStatus,omitempty"`
	CreatedAt    int64    `json:"createdAt"`
}
