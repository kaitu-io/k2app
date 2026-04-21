package center

import (
	"strings"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// proxyMembersDeprecationNotice 是代付成员管理下线后，给有历史代付成员的用户
// 展示的单条指引文案（由 api_member_list 作为伪成员的邮箱字段返回）。
const proxyMembersDeprecationNotice = "受益方可在 kaitu.io/purchase 自行指定代付"

// api_member_list 获取成员列表
//
// 代付成员管理已下线：
//   - 用户无历史代付成员 → 返回空列表
//   - 用户有历史代付成员 → 返回单条伪成员，邮箱字段承载指引文案，引导用户改为
//     在 kaitu.io/purchase 下单时由受益方自行指定。UUID 为空，下单/删除均会被
//     已有校验拦住（见 api_order 对 forUserUUIDs 的校验，以及 api_member_remove
//     的 405 返回）。
func api_member_list(c *gin.Context) {
	log.Infof(c, "user request to get member list")

	user := ReqUser(c)

	var count int64
	if err := db.Get().Model(&User{}).
		Where(&User{DelegateID: &user.ID}).
		Count(&count).Error; err != nil {
		log.Errorf(c, "failed to count members: %v", err)
		Error(c, ErrorSystemError, "failed to count members")
		return
	}

	dataMembers := make([]DataUser, 0)
	if count > 0 {
		dataMembers = append(dataMembers, DataUser{
			UUID: "",
			LoginIdentifies: []DataLoginIdentify{{
				Type:  "email",
				Value: proxyMembersDeprecationNotice,
			}},
		})
	}

	log.Infof(c, "member list returned (historical count=%d, synthetic=%d)", count, len(dataMembers))
	ItemsAll(c, dataMembers)
}

// api_member_add 添加代付成员（已下线）。
// 代付成员管理已迁移至 kaitu.io/purchase 下单时由受益方自行指定。
func api_member_add(c *gin.Context) {
	log.Infof(c, "user_member_add rejected: proxy members deprecated")
	Error(c, ErrorProxyMembersDeprecated, "proxy members management is deprecated")
}

// api_member_remove 移除代付成员（已下线）。
// 代付成员管理已迁移至 kaitu.io/purchase 下单时由受益方自行指定。
func api_member_remove(c *gin.Context) {
	log.Infof(c, "user_member_remove rejected: proxy members deprecated")
	Error(c, ErrorProxyMembersDeprecated, "proxy members management is deprecated")
}

// api_admin_member_list 获取指定用户的成员列表（管理员）
//
func api_admin_member_list(c *gin.Context) {
	uuid := c.Param("uuid")
	log.Infof(c, "admin request to get member list for user %s", uuid)

	// 查找目标用户
	var targetUser User
	if err := db.Get().Where(&User{UUID: uuid}).First(&targetUser).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "user %s not found", uuid)
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "failed to query user %s: %v", uuid, err)
		Error(c, ErrorSystemError, "failed to query user")
		return
	}

	var members []User
	if err := db.Get().Model(&User{}).
		Where(&User{DelegateID: &targetUser.ID}).
		Preload("LoginIdentifies").
		Find(&members).Error; err != nil {
		log.Errorf(c, "failed to query members for user %d: %v", targetUser.ID, err)
		Error(c, ErrorSystemError, "failed to query members")
		return
	}

	// 转换为 DataUser 格式，确保返回空数组而不是null
	dataMembers := make([]DataUser, 0)
	for _, member := range members {
		// 构造登录身份列表
		loginIdentifies := make([]DataLoginIdentify, 0)
		for _, loginIdentify := range member.LoginIdentifies {
			value, _ := secretDecryptString(c, loginIdentify.EncryptedValue)
			loginIdentifies = append(loginIdentifies, DataLoginIdentify{
				Type:  loginIdentify.Type,
				Value: value,
			})
		}

		// 处理邀请码
		var inviteCode *DataInviteCode
		if member.InvitedByCode != nil {
			inviteCode = &DataInviteCode{
				Code:      member.InvitedByCode.GetCode(),
				CreatedAt: member.InvitedByCode.CreatedAt.Unix(),
				Remark:    member.InvitedByCode.Remark,
			}
		}

		dataMember := DataUser{
			UUID:             member.UUID,
			ExpiredAt:        member.ExpiredAt,
			IsFirstOrderDone: member.IsFirstOrderDone != nil && *member.IsFirstOrderDone,
			InvitedByCode:    inviteCode,
			LoginIdentifies:  loginIdentifies,
			Device:           nil, // 成员列表不需要设备信息
			DeviceCount:      0,   // 成员列表不需要设备数量
		}
		dataMembers = append(dataMembers, dataMember)
	}

	log.Infof(c, "successfully retrieved %d members for user %s", len(dataMembers), uuid)
	ItemsAll(c, dataMembers)
}

// api_admin_member_add 为指定用户添加成员（管理员）
//
func api_admin_member_add(c *gin.Context) {
	uuid := c.Param("uuid")
	log.Infof(c, "admin request to add member for user %s", uuid)

	var req AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 查找目标用户
	var targetUser User
	if err := db.Get().Where(&User{UUID: uuid}).First(&targetUser).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "user %s not found", uuid)
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "failed to query user %s: %v", uuid, err)
		Error(c, ErrorSystemError, "failed to query user")
		return
	}

	// 转换邮箱为小写
	req.MemberEmail = strings.ToLower(req.MemberEmail)

	// 计算邮箱的哈希索引
	indexID := secretHashIt(c, []byte(req.MemberEmail))

	// 先检查是否已经存在相同邮箱的用户
	var existingLoginIdentify LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).First(&existingLoginIdentify).Error; err == nil {
		log.Warnf(c, "email %s already exists", req.MemberEmail)
		Error(c, ErrorInvalidArgument, "邮箱已被使用")
		return
	} else if err != gorm.ErrRecordNotFound {
		log.Errorf(c, "failed to check existing email: %v", err)
		Error(c, ErrorSystemError, "failed to check existing email")
		return
	}

	// 加密邮箱
	encEmail, err := secretEncryptString(c, req.MemberEmail)
	if err != nil {
		log.Errorf(c, "failed to encrypt email: %v", err)
		Error(c, ErrorSystemError, "failed to encrypt email")
		return
	}

	var memberUser User
	loginIdentify := LoginIdentify{
		Type:           "email",
		IndexID:        indexID,
		EncryptedValue: encEmail,
		User: &User{
			UUID:       generateId("user"),
			ExpiredAt:  0,              // 新用户默认未付费
			DelegateID: &targetUser.ID, // 设置付费委托关系
		},
	}

	if err := db.Get().Create(&loginIdentify).Error; err != nil {
		log.Warnf(c, "failed to create user with email %s: %v", req.MemberEmail, err)
		// 如果是重复键错误，返回 422
		if util.DbIsDuplicatedErr(err) {
			Error(c, ErrorInvalidArgument, "邮箱已被使用")
		} else {
			Error(c, ErrorSystemError, "failed to create user")
		}
		return
	}

	memberUser = *loginIdentify.User

	// 构造返回的 DataUser
	loginIdentifies := []DataLoginIdentify{
		{
			Type:  "email",
			Value: req.MemberEmail,
		},
	}

	dataMember := DataUser{
		UUID:             memberUser.UUID,
		ExpiredAt:        memberUser.ExpiredAt,
		IsFirstOrderDone: memberUser.IsFirstOrderDone != nil && *memberUser.IsFirstOrderDone,
		InvitedByCode:    nil, // 新用户没有邀请码
		LoginIdentifies:  loginIdentifies,
		Device:           nil, // 添加时不返回设备信息
		DeviceCount:      0,   // 新用户没有设备
	}

	log.Infof(c, "successfully added member %s (ID: %d) to user %s (ID: %d)", req.MemberEmail, memberUser.ID, uuid, targetUser.ID)
	Success(c, &dataMember)
	WriteAuditLog(c, "user_add_member", "user", uuid, nil)
}

// api_admin_member_remove 为指定用户移除成员（管理员）
//
func api_admin_member_remove(c *gin.Context) {
	uuid := c.Param("uuid")
	memberUUID := c.Param("memberUUID")
	log.Infof(c, "admin request to remove member %s from user %s", memberUUID, uuid)

	if memberUUID == "" {
		log.Warnf(c, "empty member UUID")
		Error(c, ErrorInvalidArgument, "invalid member UUID")
		return
	}

	// 查找目标用户
	var targetUser User
	if err := db.Get().Where(&User{UUID: uuid}).First(&targetUser).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "user %s not found", uuid)
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "failed to query user %s: %v", uuid, err)
		Error(c, ErrorSystemError, "failed to query user")
		return
	}

	// 查找成员用户
	var memberUser User
	if err := db.Get().Model(&User{}).Where("uuid = ?", memberUUID).First(&memberUser).Error; err != nil {
		log.Warnf(c, "member user with UUID %s not found", memberUUID)
		Error(c, ErrorNotFound, "member not found")
		return
	}

	// 检查是否确实是该用户的成员
	if memberUser.DelegateID == nil || *memberUser.DelegateID != targetUser.ID {
		log.Warnf(c, "user %s is not a member of user %s", memberUUID, uuid)
		Error(c, ErrorForbidden, "not a member of this user")
		return
	}

	// 在事务中取消付费委托关系
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		// 取消付费委托关系
		memberUser.DelegateID = nil

		if err := tx.Save(&memberUser).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Errorf(c, "failed to remove member in transaction: %v", err)
		Error(c, ErrorSystemError, "failed to remove member")
		return
	}

	log.Infof(c, "successfully removed member %s from user %s", memberUUID, uuid)
	SuccessEmpty(c)
	WriteAuditLog(c, "user_remove_member", "user", uuid, nil)
}
