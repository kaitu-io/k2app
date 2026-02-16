package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

// api_my_invite_users 获取我邀请的用户列表
//
func api_my_invite_users(c *gin.Context) {
	// 获取分页参数
	pagination := PaginationFromRequest(c)

	// 获取当前用户
	userID := ReqUserID(c)
	log.Infof(c, "user %d requesting invited users list, page: %d, pageSize: %d", userID, pagination.Page, pagination.PageSize)
	if userID == 0 {
		log.Warnf(c, "attempt to get invited users list with user ID 0")
		Error(c, ErrorNotLogin, "not login")
		return
	}

	// 获取邀请码筛选参数
	inviteCodeFilter := c.Query("inviteCode")

	// 获取当前用户的邀请码列表
	var inviteCodes []InviteCode
	query := db.Get().Where("user_id = ?", userID)
	if inviteCodeFilter != "" {
		inviteCodeID := InviteCodeID(inviteCodeFilter)
		query = query.Where("id = ?", inviteCodeID)
		log.Infof(c, "filtering by invite code: %s", inviteCodeFilter)
	}
	err := query.Find(&inviteCodes).Error
	if err != nil {
		log.Errorf(c, "failed to get invite codes for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get invite codes failed")
		return
	}

	if len(inviteCodes) == 0 {
		log.Infof(c, "no invite codes found for user %d", userID)
		List(c, []DataUser{}, pagination)
		return
	}

	// 提取邀请码ID列表
	inviteCodeIDs := util.Map(inviteCodes, func(code InviteCode) uint64 {
		return code.ID
	})

	// 查询被邀请的用户总数
	var total int64
	err = db.Get().Model(&User{}).Where("invited_by_code_id IN ?", inviteCodeIDs).Count(&total).Error
	if err != nil {
		log.Errorf(c, "failed to count invited users for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "count invited users failed")
		return
	}

	// 查询被邀请的用户列表
	var users []User
	err = db.Get().Where("invited_by_code_id IN ?", inviteCodeIDs).
		Preload("InvitedByCode").
		Order("created_at DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&users).Error
	if err != nil {
		log.Errorf(c, "failed to get invited users for user %d: %v", userID, err)
		Error(c, ErrorSystemError, "get invited users failed")
		return
	}

	// 转换为 API 响应格式
	items := make([]DataUser, len(users))
	for i, user := range users {
		dataUser := buildDataUserWithDevice(&user, nil)
		if dataUser != nil {
			items[i] = *dataUser
		}
	}

	// 更新总数
	pagination.Total = total

	log.Infof(c, "successfully retrieved %d invited users for user %d", len(items), userID)
	List(c, items, pagination)
}
