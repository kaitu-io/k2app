package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// parseExpiresInDays 解析有效期天数参数（1-365天）
func parseExpiresInDays(param string) (int, error) {
	days, err := strconv.Atoi(param)
	if err != nil {
		return 0, fmt.Errorf("invalid number format: %w", err)
	}
	if days < 1 || days > 365 {
		return 0, fmt.Errorf("days must be between 1 and 365, got %d", days)
	}
	return days, nil
}

// DataShareLink 分享链接响应数据
//
type DataShareLink struct {
	Code      string `json:"code" example:"ABC123"`                       // 邀请码
	ShareLink string `json:"shareLink" example:"https://s.kaitu.io/xyz"` // 短链接
	ExpiresAt int64  `json:"expiresAt" example:"1234567890"`             // 短链接过期时间（Unix时间戳）
}

// ResponseShareLink 分享链接响应
type ResponseShareLink struct {
	Code int           `json:"code" example:"0"`          // 响应码
	Msg  string        `json:"message" example:"success"` // 响应消息
	Data DataShareLink `json:"data"`                      // 分享链接数据
}

// api_get_share_link 获取邀请码分享链接（微信防红短链接）
//
func api_get_share_link(c *gin.Context) {
	// 获取当前用户
	userID := ReqUserID(c)
	if userID == 0 {
		Error(c, ErrorNotLogin, "not login")
		return
	}

	// 获取邀请码
	code := c.Param("code")
	if code == "" {
		Error(c, ErrorInvalidArgument, "code is required")
		return
	}

	// 将邀请码解码为ID
	inviteCodeID := InviteCodeID(code)
	if inviteCodeID == 0 {
		log.Warnf(c, "invalid invite code format: %s", code)
		Error(c, ErrorInvalidArgument, "invalid invite code")
		return
	}

	// 查询邀请码（验证是否属于当前用户）
	var inviteCode InviteCode
	err := db.Get().Where(&InviteCode{ID: inviteCodeID, UserID: userID}).First(&inviteCode).Error
	if err != nil {
		log.Errorf(c, "failed to get invite code %s (id=%d) for user %d: %v", code, inviteCodeID, userID, err)
		Error(c, ErrorNotFound, "invite code not found")
		return
	}

	// 解析有效期参数（默认7天）
	expiresInDays := 7
	if expiresParam := c.Query("expiresInDays"); expiresParam != "" {
		if days, err := parseExpiresInDays(expiresParam); err == nil {
			expiresInDays = days
		} else {
			log.Warnf(c, "invalid expiresInDays parameter '%s': %v, using default 7 days", expiresParam, err)
		}
	}

	// 生成微信防红短链接
	shareLink, err := createInviteShareLink(c, inviteCode, expiresInDays)
	if err != nil {
		log.Errorf(c, "failed to generate share link for code %s: %v", code, err)
		Error(c, ErrorSystemError, "failed to generate share link")
		return
	}

	// 计算过期时间
	expiresAt := time.Now().Add(time.Duration(expiresInDays) * 24 * time.Hour).Unix()

	data := DataShareLink{
		Code:      code,
		ShareLink: shareLink,
		ExpiresAt: expiresAt,
	}

	log.Infof(c, "successfully generated share link for code %s: %s", code, shareLink)
	Success(c, &data)
}
