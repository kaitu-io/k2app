package center

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
	"gorm.io/gorm"
)

// api_member_list 获取成员列表
//
func api_member_list(c *gin.Context) {
	log.Infof(c, "user request to get member list")

	user := ReqUser(c)

	var members []User
	if err := db.Get().Model(&User{}).
		Where(&User{DelegateID: &user.ID}).
		Preload("LoginIdentifies").
		Find(&members).Error; err != nil {
		log.Errorf(c, "failed to query members: %v", err)
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

	log.Infof(c, "successfully retrieved %d members", len(dataMembers))
	ItemsAll(c, dataMembers)
}

// api_member_add 添加成员
//
func api_member_add(c *gin.Context) {
	log.Infof(c, "user request to add member")

	var req AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	user := ReqUser(c)

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
			AccessKey:  generateAccessKey(),
			ExpiredAt:  0,        // 新用户默认未付费
			DelegateID: &user.ID, // 设置付费委托关系
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

	log.Infof(c, "successfully added member %s (ID: %d) to user %d", req.MemberEmail, memberUser.ID, user.ID)

	// 发送邮件通知新成员
	go func() {
		// 获取代付人邮箱
		delegateEmail := ""
		var delegateLoginIdentify LoginIdentify
		if err := db.Get().Where("user_id = ? AND type = ?", user.ID, "email").First(&delegateLoginIdentify).Error; err == nil {
			if decEmail, err := secretDecryptString(c, delegateLoginIdentify.EncryptedValue); err == nil {
				delegateEmail = decEmail
			}
		}

		// 获取Web管理后台URL
		webURL := viper.GetString("web.url")
		if webURL == "" {
			webURL = "https://www.kaitu.io" // 默认URL
		}

		// 构造邮件内容
		meta := MemberAddedMeta{
			DelegateEmail: delegateEmail,
			AddedTime:     time.Now().Format("2006-01-02 15:04:05"),
			RejectURL:     webURL + "/account/delegate", // Web管理后台的代付管理页面
		}

		// 发送邮件到新成员的邮箱
		if err := emailTo(c, req.MemberEmail, memberAddedTemplate, meta); err != nil {
			log.Errorf(c, "failed to send member added email to %s: %v", req.MemberEmail, err)
		} else {
			log.Infof(c, "member added email sent to %s", req.MemberEmail)
		}
	}()

	Success(c, &dataMember)
}

// api_member_remove 移除成员
//
func api_member_remove(c *gin.Context) {
	log.Infof(c, "user request to remove member")

	memberUUID := c.Param("userUUID")
	if memberUUID == "" {
		log.Warnf(c, "empty member UUID")
		Error(c, ErrorInvalidArgument, "invalid member UUID")
		return
	}

	user := ReqUser(c)

	// 查找成员用户
	var memberUser User
	if err := db.Get().Model(&User{}).Where("uuid = ?", memberUUID).First(&memberUser).Error; err != nil {
		log.Warnf(c, "member user with UUID %s not found", memberUUID)
		Error(c, ErrorNotFound, "member not found")
		return
	}

	// 检查是否确实是该用户的成员
	if memberUser.DelegateID == nil || *memberUser.DelegateID != user.ID {
		log.Warnf(c, "user %s is not a member of user %d", memberUUID, user.ID)
		Error(c, ErrorForbidden, "not your member")
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

	log.Infof(c, "successfully removed member %s from user %d", memberUUID, user.ID)
	SuccessEmpty(c)
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
			AccessKey:  generateAccessKey(),
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
}

// api_get_delegate 获取我的代付人信息
//
func api_get_delegate(c *gin.Context) {
	log.Infof(c, "user request to get delegate info")

	user := ReqUser(c)

	// 检查是否有代付人
	if user.DelegateID == nil {
		log.Infof(c, "user %d has no delegate", user.ID)
		Error(c, ErrorNotFound, "no delegate")
		return
	}

	// 查询代付人信息
	var delegateUser User
	if err := db.Get().Preload("LoginIdentifies").Where("id = ?", *user.DelegateID).First(&delegateUser).Error; err != nil {
		log.Errorf(c, "failed to query delegate user %d: %v", *user.DelegateID, err)
		Error(c, ErrorSystemError, "failed to query delegate")
		return
	}

	// 构造登录身份列表
	loginIdentifies := make([]DataLoginIdentify, 0)
	for _, loginIdentify := range delegateUser.LoginIdentifies {
		value, _ := secretDecryptString(c, loginIdentify.EncryptedValue)
		loginIdentifies = append(loginIdentifies, DataLoginIdentify{
			Type:  loginIdentify.Type,
			Value: value,
		})
	}

	// 构造返回的代付人信息
	dataDelegate := DataDelegate{
		UUID:            delegateUser.UUID,
		LoginIdentifies: loginIdentifies,
	}

	log.Infof(c, "successfully retrieved delegate info for user %d", user.ID)
	Success(c, &dataDelegate)
}

// api_reject_delegate 拒绝代付
//
func api_reject_delegate(c *gin.Context) {
	log.Infof(c, "user request to reject delegate")

	user := ReqUser(c)

	// 检查是否有代付人
	if user.DelegateID == nil {
		log.Infof(c, "user %d has no delegate to reject", user.ID)
		Error(c, ErrorNotFound, "no delegate")
		return
	}

	// 清除代付关系
	err := db.Get().Transaction(func(tx *gorm.DB) error {
		user.DelegateID = nil
		if err := tx.Save(user).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		log.Errorf(c, "failed to reject delegate for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to reject delegate")
		return
	}

	log.Infof(c, "successfully rejected delegate for user %d", user.ID)
	SuccessEmpty(c)
}
