package center

import (
	db "github.com/wordgate/qtoolkit/db"
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

// api_get_invite_code 获取邀请码信息
//
func api_get_invite_code(c *gin.Context) {
	code := c.Query("code")
	log.Infof(c, "request to get invite code: %s", code)
	if code == "" {
		log.Warnf(c, "request with empty invite code")
		Error(c, ErrorNotFound, "code is not found")
		return
	}

	inviteCode := InviteCode{}
	err := db.Get().First(&inviteCode, InviteCodeID(code)).Error
	if util.DbIsDuplicatedErr(err) {
		log.Warnf(c, "invite code %s not found (duplicate error)", code)
		Error(c, ErrorNotFound, "code is not found")
		return
	} else if err != nil {
		log.Errorf(c, "failed to get invite code %s: %v", code, err)
		Error(c, ErrorSystemError, "get invite code failed")
		return
	}
	log.Infof(c, "successfully retrieved invite code %s", code)

	// 返回邀请码基本信息（不含配置，客户端应通过 /api/app/config 获取）
	data := DataInviteCode{
		Code:      inviteCode.GetCode(),
		CreatedAt: inviteCode.CreatedAt.Unix(),
		Remark:    inviteCode.Remark,
		Link:      inviteCode.Link(),
	}

	Success(c, &data)
}
