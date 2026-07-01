package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// SlaveDeviceCheckAuthRequest 节点设备认证请求
//
type SlaveDeviceCheckAuthRequest struct {
	UDID  string `json:"udid" binding:"required" example:"device-123"`                            // 设备唯一标识 (必填)
	Token string `json:"token" binding:"required" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"` // JWT access token
}

// SlaveDeviceCheckAuthResult 节点设备认证结果
//
type SlaveDeviceCheckAuthResult struct {
	UserID           uint64 `json:"userID" example:"123456"`   // 用户ID
	UDID             string `json:"udid" example:"device-123"` // 设备唯一标识
	ServiceExpiredAt int64  `json:"serviceExpiredAt"`          // 服务过期时间
}

// api_slave_device_check_auth 节点设备认证（JWT access token）
func api_slave_device_check_auth(c *gin.Context) {
	var req SlaveDeviceCheckAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid device check auth request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	handleSlaveJWTAuth(c, req.UDID, req.Token)
}

// handleSlaveJWTAuth 处理 JWT token 认证
// udid 参数必填：必须与 token 中的 UDID 匹配
func handleSlaveJWTAuth(c *gin.Context, udid, token string) {
	// 1. 验证 UDID 必填
	if udid == "" {
		log.Warnf(c, "UDID is required for JWT auth")
		Error(c, ErrorInvalidArgument, "udid is required")
		return
	}

	// 2. 验证 token 有效性（401 如果无效）
	claims, device, err := validateToken(c, token, TokenTypeAccess)
	if err != nil {
		log.Warnf(c, "failed to validate access token: %v", err)
		ErrorE(c, err) // 返回 401
		return
	}

	// 3. 验证 UDID 匹配（必须匹配）
	if device.UDID != udid {
		log.Warnf(c, "UDID mismatch: token=%s, request=%s", device.UDID, udid)
		Error(c, ErrorNotLogin, "UDID mismatch")
		return
	}

	// 4. 获取用户信息
	user := User{}
	err = db.Get().First(&user, device.UserID).Error
	if err != nil {
		log.Errorf(c, "failed to get user %d: %v", device.UserID, err)
		ErrorE(c, err)
		return
	}

	// 5. 节点访问授权（能力矩阵授权侧，集中在 AuthorizeNodeAccess）。
	//    node 由 SlaveAuthRequired() 注入（device-check-auth 路由必经该中间件）；
	//    专属节点需额外加载其订阅。共享节点 pnSub=nil，按 user.ExpiredAt 判定。
	node := ReqSlaveNode(c)
	if node == nil {
		log.Warnf(c, "device check auth: missing authenticated node")
		Error(c, ErrorNotLogin, "node context required")
		return
	}

	var pnSub *PrivateNodeSubscription
	if node.Class == NodeClassPrivate {
		if node.PrivateSubID == nil {
			// 数据完整性问题：专属节点必有订阅。500 而非 402，避免伪装成"会员过期"并触发告警。
			log.Errorf(c, "private node %s missing PrivateSubID (data integrity)", node.Ipv4)
			Error(c, ErrorSystemError, "private node misconfigured")
			return
		}
		var s PrivateNodeSubscription
		if err := db.Get().First(&s, *node.PrivateSubID).Error; err != nil {
			// DB 加载失败（dangling 指针或基础设施故障）都是服务端问题 → 500。
			log.Errorf(c, "failed to load private sub %d: %v", *node.PrivateSubID, err)
			Error(c, ErrorSystemError, "failed to load private node subscription")
			return
		}
		pnSub = &s
	}

	if !AuthorizeNodeAccess(&user, node, pnSub, time.Now().Unix()) {
		log.Warnf(c, "node access denied: user=%d node=%s class=%s", user.ID, node.Ipv4, node.Class)
		ErrorE(c, ErrMembershipExpired) // 402 — 真正的授权拒绝
		return
	}

	// 6. 返回认证成功。专属节点的服务到期取其专属订阅 ExpiresAt（独立时钟），
	//    共享节点取 user.ExpiredAt（共享池会员时钟）。
	serviceExpiredAt := user.ExpiredAt
	if node.Class == NodeClassPrivate && pnSub != nil {
		serviceExpiredAt = pnSub.ExpiresAt
	}
	Success(c, &SlaveDeviceCheckAuthResult{
		UserID:           claims.UserID,
		UDID:             claims.DeviceID,
		ServiceExpiredAt: serviceExpiredAt,
	})
}
