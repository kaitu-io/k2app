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

	brand := ReqBrand(c)
	cfg := configSocialAuth(brand)

	cl, err := verifySocialIDToken(c, cfg, req.Provider, req.IDToken, req.Nonce)
	if err != nil {
		log.Warnf(c, "social login: %s token rejected (brand %s): %v", req.Provider, brand, err)
		Error(c, socialErrorCode(err), "social sign-in failed")
		return
	}

	userID, created, err := resolveSocialUser(c, brand, cl, req.Language, c.GetHeader("Accept-Language"))
	if err != nil {
		code := socialErrorCode(err)
		if code == ErrorSystemError {
			log.Errorf(c, "social login: resolve %s user failed (email %s): %v", req.Provider, hideEmail(cl.Email), err)
		} else {
			log.Warnf(c, "social login: resolve %s user rejected (email %s): %v", req.Provider, hideEmail(cl.Email), err)
		}
		Error(c, code, "social sign-in failed")
		return
	}
	log.Infof(c, "social login: %s identity resolved to user %d (created=%v), udid: %s", req.Provider, userID, created, req.UDID)

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

	// Apple：用授权码换 refresh token 留作注销时撤销；失败不影响登录。
	if req.Provider == SocialProviderApple && req.AuthorizationCode != "" {
		if rt, err := appleExchangeCode(c, cfg, cl.Audience, req.AuthorizationCode); err != nil {
			log.Warnf(c, "social login: apple code exchange failed for user %d: %v", userID, err)
		} else if err := saveAppleRefreshToken(c, brand, cl.Sub, cl.Audience, rt); err != nil {
			log.Warnf(c, "social login: save apple refresh token failed for user %d: %v", userID, err)
		}
	}

	log.Infof(c, "user %d logged in via %s with device %s", userID, req.Provider, req.UDID)
	Success(c, authResult)
}
