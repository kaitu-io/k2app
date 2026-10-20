package center

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// socialErrorCode 把联合登录哨兵错误映射到对外错误码；未知错误 → ErrorSystemError。
func socialErrorCode(err error) ErrorCode {
	switch {
	case errors.Is(err, errSocialTokenInvalid):
		return ErrorSocialTokenInvalid
	case errors.Is(err, errSocialEmailUnverified):
		return ErrorSocialEmailUnverified
	case errors.Is(err, errSocialProviderDisabled):
		return ErrorSocialProviderDisabled
	case errors.Is(err, errSocialIdentityTaken):
		return ErrorSocialIdentityTaken
	case errors.Is(err, errSocialProviderAlreadyBound):
		return ErrorSocialProviderAlreadyBound
	case errors.Is(err, errSocialHandoffExpired):
		return ErrorSocialHandoffExpired
	}
	return ErrorSystemError
}

// api_social_login App 内第三方（Google / Apple）原生登录，设备绑定，返回 DataAuthResult。
func api_social_login(c *gin.Context) {
	var req DataSocialLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid social login request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 同 api_login：未知 client-class token 提前拒绝，避免建出下一次请求就被拒的设备行。
	if rawHeader := c.GetHeader("X-K2-Client"); rawHeader != "" && parseClientHeader(rawHeader) == nil {
		log.Warnf(c, "social login rejected: invalid client class header=%q remote=%s", rawHeader, c.ClientIP())
		Error(c, ErrorInvalidClientClass, "invalid client class token")
		return
	}

	cfg, cl, userID, err := authenticateSocial(c, req.Provider, req.IDToken, req.Nonce, req.Language)
	if err != nil {
		Error(c, socialErrorCode(err), "social sign-in failed")
		return
	}
	log.Infof(c, "social login: user %d, udid: %s", userID, req.UDID)

	authResult, err := completeDeviceLogin(c, userID, deviceLoginParams{
		UDID:     req.UDID,
		Remark:   req.Remark,
		Language: req.Language,
		Email:    cl.Email,
	})
	if err != nil {
		log.Errorf(c, "social login transaction failed for user %d: %v", userID, err)
		ErrorE(c, err)
		return
	}

	saveAppleRefreshBestEffort(c, cfg, cl, req.Provider, req.AuthorizationCode, userID)

	log.Infof(c, "user %d logged in via %s with device %s", userID, req.Provider, req.UDID)
	Success(c, authResult)
}

// api_social_web_login 官网第三方（Google / Apple）登录：无设备绑定，写 HttpOnly Cookie，返回 DataWebLoginResponse。
func api_social_web_login(c *gin.Context) {
	var req DataSocialWebLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid social web login request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	cfg, cl, userID, err := authenticateSocial(c, req.Provider, req.IDToken, req.Nonce, req.Language)
	if err != nil {
		Error(c, socialErrorCode(err), "social sign-in failed")
		return
	}

	resp, err := completeWebLogin(c, userID, webLoginParams{
		Language: req.Language,
		Email:    cl.Email,
	})
	if err != nil {
		log.Errorf(c, "social web login transaction failed for user %d: %v", userID, err)
		ErrorE(c, err)
		return
	}

	saveAppleRefreshBestEffort(c, cfg, cl, req.Provider, req.AuthorizationCode, userID)

	log.Infof(c, "user %d logged in via %s on web", userID, req.Provider)
	Success(c, resp)
}

// authenticateSocial 验 idToken 并把第三方身份解析为本品牌用户（可能新建）。
// 按现有规则记日志（哨兵错误 Warn，其余 Error）；返回的错误交给 socialErrorCode 映射。
// rawNonce 是客户端原始 nonce（App/官网来自请求，handoff 来自会话）。
func authenticateSocial(c *gin.Context, provider, idToken, rawNonce, language string) (cfg socialAuthConfig, cl *socialClaims, userID uint64, err error) {
	brand := ReqBrand(c)
	cfg = configSocialAuth(brand)

	cl, err = verifySocialIDToken(c, cfg, provider, idToken, rawNonce)
	if err != nil {
		log.Warnf(c, "social sign-in: %s token rejected (brand %s): %v", provider, brand, err)
		return cfg, nil, 0, err
	}

	userID, created, err := resolveSocialUser(c, brand, cl, language, c.GetHeader("Accept-Language"))
	if err != nil {
		if socialErrorCode(err) == ErrorSystemError {
			log.Errorf(c, "social sign-in: resolve %s user failed (email %s): %v", provider, hideEmail(cl.Email), err)
		} else {
			log.Warnf(c, "social sign-in: resolve %s user rejected (email %s): %v", provider, hideEmail(cl.Email), err)
		}
		return cfg, cl, 0, err
	}
	log.Infof(c, "social sign-in: %s identity resolved to user %d (created=%v)", provider, userID, created)
	return cfg, cl, userID, nil
}

// saveAppleRefreshBestEffort Apple：用授权码换 refresh token 留作注销时撤销；
// 非 Apple 或无授权码为 no-op，失败只 Warn，不影响登录。
func saveAppleRefreshBestEffort(c *gin.Context, cfg socialAuthConfig, cl *socialClaims, provider, authorizationCode string, userID uint64) {
	if provider != SocialProviderApple || authorizationCode == "" {
		return
	}
	rt, err := appleExchangeCode(c, cfg, cl.Audience, authorizationCode)
	if err != nil {
		log.Warnf(c, "social sign-in: apple code exchange failed for user %d: %v", userID, err)
		return
	}
	if err := saveAppleRefreshToken(c, cfg.Brand, cl.Sub, cl.Audience, rt); err != nil {
		log.Warnf(c, "social sign-in: save apple refresh token failed for user %d: %v", userID, err)
	}
}
