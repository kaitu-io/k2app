package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 注销 / 硬删除时撤销 Apple 令牌。Apple revoke 用桩替换（stubAppleRevoke），不打外网。

type revokeCall struct {
	brand           Brand
	clientID, token string
}

// captureAppleRevoke 安装桩，把每次调用推入 channel；retErr 为桩的返回值。
func captureAppleRevoke(t *testing.T, retErr error) chan revokeCall {
	t.Helper()
	ch := make(chan revokeCall, 8)
	stubAppleRevoke(t, func(_ context.Context, cfg socialAuthConfig, clientID, token string) error {
		ch <- revokeCall{brand: cfg.Brand, clientID: clientID, token: token}
		return retErr
	})
	return ch
}

func seedAppleWithToken(t *testing.T, e *userSocialEnv, sub, token string) string {
	t.Helper()
	e.tr.seedIdentity(BrandKaitu, e.user.ID, SocialProviderApple, sub)
	aud := BrandKaitu.Config().Social.AppleBundleID
	require.NoError(t, saveAppleRefreshToken(context.Background(), BrandKaitu, sub, aud, token))
	return aud
}

func TestDeleteAccount_RevokesAppleToken(t *testing.T) {
	e := newUserSocialEnv(t)
	aud := seedAppleWithToken(t, e, "a-del-"+e.sfx, "apple-rt-del-"+e.sfx)
	ch := captureAppleRevoke(t, nil)

	resp := deleteAccountAs(t, e.user)
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Empty(t, identitiesOfUser(t, e.user.ID), "身份行已删除")

	select {
	case got := <-ch:
		assert.Equal(t, aud, got.clientID)
		assert.Equal(t, "apple-rt-del-"+e.sfx, got.token)
		assert.Equal(t, BrandKaitu, got.brand)
	case <-time.After(2 * time.Second):
		t.Fatal("appleRevokeToken 未被调用")
	}
}

func TestDeleteAccount_RevokeFailureDoesNotBlock(t *testing.T) {
	e := newUserSocialEnv(t)
	seedAppleWithToken(t, e, "a-delfail-"+e.sfx, "apple-rt-fail-"+e.sfx)
	ch := captureAppleRevoke(t, errors.New("apple down"))

	resp := deleteAccountAs(t, e.user)
	require.Equal(t, 0, resp.Code, resp.Message)
	assert.Empty(t, identitiesOfUser(t, e.user.ID))
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("appleRevokeToken 未被调用")
	}
}

func TestDeleteAccount_NoAppleIdentityNoCall(t *testing.T) {
	e := newUserSocialEnv(t)
	e.tr.seedIdentity(BrandKaitu, e.user.ID, SocialProviderGoogle, "g-delno-"+e.sfx)
	// Apple 行但没有 refresh token：也不应调用
	e.tr.seedIdentity(BrandKaitu, e.user.ID, SocialProviderApple, "a-delno-"+e.sfx)
	ch := captureAppleRevoke(t, nil)

	resp := deleteAccountAs(t, e.user)
	require.Equal(t, 0, resp.Code, resp.Message)
	select {
	case <-ch:
		t.Fatal("没有 Apple refresh token 时不应调用 revoke")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestHardDelete_RevokesAppleTokens(t *testing.T) {
	e := newUserSocialEnv(t)
	aud := seedAppleWithToken(t, e, "a-hard-"+e.sfx, "apple-rt-hard-"+e.sfx)
	ch := captureAppleRevoke(t, nil)

	params, err := json.Marshal(HardDeleteUsersRequest{UserUUIDs: []string{e.user.UUID}})
	require.NoError(t, err)
	require.NoError(t, executeApprovalUserHardDelete(context.Background(), params))

	var n int64
	require.NoError(t, db.Get().Model(&User{}).Where("id = ?", e.user.ID).Count(&n).Error)
	assert.Zero(t, n, "用户已硬删")
	select {
	case got := <-ch:
		assert.Equal(t, aud, got.clientID)
		assert.Equal(t, "apple-rt-hard-"+e.sfx, got.token)
	case <-time.After(2 * time.Second):
		t.Fatal("appleRevokeToken 未被调用")
	}
}

// deleteAccountAs 经最小路由（跳过真实鉴权）调用 api_delete_user_account。
func deleteAccountAs(t *testing.T, u *User) *TestResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/user/delete-account", nil)
	setupDeleteAccountRouter(t, u).ServeHTTP(w, req)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	return resp
}
