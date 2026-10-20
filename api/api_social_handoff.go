package center

import (
	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_social_handoff 发起浏览器 handoff 登录会话（spec §6.3，只用于登录）。
func api_social_handoff(c *gin.Context) {
	var req DataSocialHandoffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid social handoff request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	brand := ReqBrand(c)
	sessionID, pollSecret, openURL, err := createSocialHandoff(c, brand, req.Provider)
	if err != nil {
		log.Errorf(c, "social handoff: create session failed (brand %s): %v", brand, err)
		Error(c, ErrorSystemError, "failed to create handoff session")
		return
	}
	log.Infof(c, "social handoff: session created (brand %s, provider %s)", brand, req.Provider)
	Success(c, &DataSocialHandoffResponse{SessionID: sessionID, PollSecret: pollSecret, URL: openURL})
}

// api_social_handoff_complete 官网 handoff 页交回 idToken：校验并解析用户，结果写回会话等 App 领取。
func api_social_handoff_complete(c *gin.Context) {
	var req DataSocialHandoffCompleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid social handoff complete request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	s, err := loadSocialHandoff(c, req.SessionID)
	if err != nil {
		if code := socialErrorCode(err); code == ErrorSystemError {
			log.Errorf(c, "social handoff complete: %v", err)
		}
		Error(c, socialErrorCode(err), "handoff session expired")
		return
	}
	if s.Status != socialHandoffPending || s.Brand != string(ReqBrand(c)) {
		log.Warnf(c, "social handoff complete rejected: status=%s session brand=%s request brand=%s", s.Status, s.Brand, ReqBrand(c))
		Error(c, ErrorSocialHandoffExpired, "handoff session expired")
		return
	}

	cfg, cl, userID, authErr := authenticateSocial(c, s.Provider, req.IDToken, s.RawNonce, "")
	if authErr != nil {
		s.Status, s.ErrCode = socialHandoffFailed, socialErrorCode(authErr)
	} else {
		s.Status, s.UserID = socialHandoffDone, userID
	}
	if err := updateSocialHandoff(c, req.SessionID, s); err != nil {
		if code := socialErrorCode(err); code == ErrorSystemError {
			log.Errorf(c, "social handoff complete: %v", err)
		}
		Error(c, socialErrorCode(err), "handoff session expired")
		return
	}
	if authErr != nil {
		Error(c, s.ErrCode, "social sign-in failed")
		return
	}

	saveAppleRefreshBestEffort(c, cfg, cl, s.Provider, req.AuthorizationCode, userID)
	log.Infof(c, "social handoff: %s sign-in completed for user %d", s.Provider, userID)
	SuccessEmpty(c)
}

// api_social_handoff_poll App 轮询：pending 原样返回；failed 返回错误码并作废会话；
// done 时先删会话（删除成功者唯一胜出），再走与 /social/login 相同的设备登录事务。
func api_social_handoff_poll(c *gin.Context) {
	var req DataSocialHandoffPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid social handoff poll request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 同 api_login：未知 client-class token 提前拒绝，避免建出下一次请求就被拒的设备行。
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" && parseClientHeader(rawHeader) == nil {
		log.Warnf(c, "social handoff poll rejected: invalid client class header=%q remote=%s", rawHeader, c.ClientIP())
		Error(c, ErrorInvalidClientClass, "invalid client class token")
		return
	}

	s, err := loadSocialHandoff(c, req.SessionID)
	if err != nil {
		if code := socialErrorCode(err); code == ErrorSystemError {
			log.Errorf(c, "social handoff poll: %v", err)
		}
		Error(c, socialErrorCode(err), "handoff session expired")
		return
	}
	if !s.pollSecretMatches(req.PollSecret) || s.Brand != string(ReqBrand(c)) {
		log.Warnf(c, "social handoff poll rejected: bad poll secret or brand mismatch (session brand %s, request brand %s)", s.Brand, ReqBrand(c))
		Error(c, ErrorSocialHandoffExpired, "handoff session expired")
		return
	}

	switch s.Status {
	case socialHandoffPending:
		Success(c, &DataSocialHandoffPoll{Status: socialHandoffPending})
		return
	case socialHandoffFailed:
		if _, err := claimSocialHandoff(c, req.SessionID); err != nil {
			log.Warnf(c, "social handoff poll: delete failed session: %v", err)
		}
		Error(c, s.ErrCode, "social sign-in failed")
		return
	case socialHandoffDone:
		// 落到下面
	default:
		log.Errorf(c, "social handoff poll: unknown session status %q", s.Status)
		Error(c, ErrorSocialHandoffExpired, "handoff session expired")
		return
	}

	won, err := claimSocialHandoff(c, req.SessionID)
	if err != nil {
		log.Errorf(c, "social handoff poll: %v", err)
		Error(c, ErrorSystemError, "failed to claim handoff session")
		return
	}
	if !won {
		Error(c, ErrorSocialHandoffExpired, "handoff session expired")
		return
	}

	authResult, err := completeDeviceLogin(c, s.UserID, deviceLoginParams{
		UDID:     req.UDID,
		Remark:   req.Remark,
		Language: req.Language,
	})
	if err != nil {
		log.Errorf(c, "social handoff login transaction failed for user %d: %v", s.UserID, err)
		ErrorE(c, err)
		return
	}
	log.Infof(c, "user %d logged in via %s handoff with device %s", s.UserID, s.Provider, req.UDID)
	Success(c, &DataSocialHandoffPoll{Status: socialHandoffDone, Auth: authResult})
}
