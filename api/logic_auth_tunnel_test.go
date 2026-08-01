package center

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// generateTestTunnelToken 按设备当前 TunnelIssueAt 锚点手工签一个 tunnel token。
// 与 GenerateTestToken（access）平行；expiry 可为负制造过期 token。
func generateTestTunnelToken(t *testing.T, dev *Device, expiry time.Duration) string {
	t.Helper()
	testInitConfig()
	jwtConfig := configJwt(nil)
	claims := TokenClaims{
		UserID:       dev.UserID,
		DeviceID:     dev.UDID,
		Exp:          time.Now().Add(expiry).Unix(),
		Type:         TokenTypeTunnel,
		TokenIssueAt: dev.TunnelIssueAt,
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(jwtConfig.Secret))
	require.NoError(t, err)
	return tok
}

// TestGenerateTunnelToken_ClaimsShape：纯签名/解析，不需要 DB。
func TestGenerateTunnelToken_ClaimsShape(t *testing.T) {
	testInitConfig()
	before := time.Now().Unix()
	tok, err := generateTunnelToken(nil, 42, "udid-shape", 3, 1234567890)
	require.NoError(t, err)

	jwtConfig := configJwt(nil)
	parsed, err := jwt.ParseWithClaims(tok, &TokenClaims{}, func(*jwt.Token) (interface{}, error) {
		return []byte(jwtConfig.Secret), nil
	})
	require.NoError(t, err)
	claims := parsed.Claims.(*TokenClaims)
	require.Equal(t, TokenTypeTunnel, claims.Type)
	require.EqualValues(t, 42, claims.UserID)
	require.Equal(t, "udid-shape", claims.DeviceID)
	require.EqualValues(t, 1234567890, claims.TokenIssueAt, "issueAt 必须原样进 claims（吊销锚点）")
	require.EqualValues(t, 3, claims.Roles)
	// 默认寿命 7776000s（90 天）：允许 ±5s 时钟余量。
	want := before + 7776000
	require.InDelta(t, want, claims.Exp, 5, "默认 tunnel_token_expiry 必须是 7776000")
}

// TestMaybeRenewTunnelToken：50% 阈值门。纯函数，不需要 DB。
func TestMaybeRenewTunnelToken(t *testing.T) {
	testInitConfig()
	dev := &Device{UserID: 7, UDID: "udid-renew", TunnelIssueAt: 1111}

	t.Run("FreshToken_NoRenew", func(t *testing.T) {
		claims := &TokenClaims{
			UserID: 7, DeviceID: "udid-renew", Type: TokenTypeTunnel,
			TokenIssueAt: 1111,
			Exp:          time.Now().Unix() + 7776000 - 60, // 几乎满寿命
		}
		tok, renewed, err := maybeRenewTunnelToken(nil, claims, dev)
		require.NoError(t, err)
		require.False(t, renewed)
		require.Empty(t, tok)
	})

	t.Run("PastHalfLife_Renews_AnchorUnchanged", func(t *testing.T) {
		claims := &TokenClaims{
			UserID: 7, DeviceID: "udid-renew", Type: TokenTypeTunnel,
			TokenIssueAt: 1111,
			Exp:          time.Now().Unix() + 3000000, // 剩 ~34 天 < 45 天
		}
		tok, renewed, err := maybeRenewTunnelToken(nil, claims, dev)
		require.NoError(t, err)
		require.True(t, renewed)
		require.NotEmpty(t, tok)
		// 续期只延长 exp，不重置吊销锚点。
		jwtConfig := configJwt(nil)
		parsed, err := jwt.ParseWithClaims(tok, &TokenClaims{}, func(*jwt.Token) (interface{}, error) {
			return []byte(jwtConfig.Secret), nil
		})
		require.NoError(t, err)
		nc := parsed.Claims.(*TokenClaims)
		require.EqualValues(t, 1111, nc.TokenIssueAt, "续期不得重置 TunnelIssueAt 锚点")
		require.Equal(t, TokenTypeTunnel, nc.Type)
	})

	t.Run("NonTunnelClaims_NoRenew", func(t *testing.T) {
		claims := &TokenClaims{Type: TokenTypeAccess, Exp: time.Now().Unix() + 10}
		tok, renewed, err := maybeRenewTunnelToken(nil, claims, dev)
		require.NoError(t, err)
		require.False(t, renewed)
		require.Empty(t, tok)
	})
}

// TestValidateTunnelToken：需要 DB（Device 行 + TunnelIssueAt 锚点比对）。
func TestValidateTunnelToken(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := CreateTestUser(t)
	device := CreateTestDevice(t, user.ID, "udid-vtt-"+time.Now().Format("150405.000000"))
	anchor := time.Now().Unix()
	require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
		Update("tunnel_issue_at", anchor).Error)
	device.TunnelIssueAt = anchor

	t.Run("Valid_OK", func(t *testing.T) {
		tok := generateTestTunnelToken(t, device, time.Hour)
		claims, dev, err := validateTunnelToken(nil, tok)
		require.NoError(t, err)
		require.Equal(t, TokenTypeTunnel, claims.Type)
		require.Equal(t, device.UDID, dev.UDID)
	})

	t.Run("AccessTokenRejected", func(t *testing.T) {
		tok := GenerateTestToken(user.ID, device.UDID, time.Hour) // Type=access
		_, _, err := validateTunnelToken(nil, tok)
		require.Error(t, err, "access token 不得通过 tunnel 校验")
	})

	t.Run("AnchorBump_Revokes", func(t *testing.T) {
		tok := generateTestTunnelToken(t, device, time.Hour)
		// 模拟"登出所有设备"：递增 TunnelIssueAt。
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", device.ID).
			Update("tunnel_issue_at", anchor+1).Error)
		t.Cleanup(func() {
			db.Get().Model(&Device{}).Where("id = ?", device.ID).Update("tunnel_issue_at", anchor)
		})
		_, _, err := validateTunnelToken(nil, tok)
		require.Error(t, err, "锚点递增后旧 tunnel token 必须失效")
	})

	t.Run("ZeroAnchor_Rejected", func(t *testing.T) {
		other := CreateTestDevice(t, user.ID, "udid-vtt0-"+time.Now().Format("150405.000000"))
		// other.TunnelIssueAt == 0（从未签发过）。伪造 issueAt=0 的 token 也不得通过。
		tok := generateTestTunnelToken(t, other, time.Hour)
		_, _, err := validateTunnelToken(nil, tok)
		require.Error(t, err, "TunnelIssueAt=0（未签发）的设备不得通过")
	})

	t.Run("DeviceDeleted_Rejected", func(t *testing.T) {
		// 吊销手段之一（spec §4.1）：删 Device 行 → validateTunnelToken 走
		// util.DbIsNotFoundErr 分支 → 401。
		gone := CreateTestDevice(t, user.ID, "udid-vttgone-"+time.Now().Format("150405.000000"))
		require.NoError(t, db.Get().Model(&Device{}).Where("id = ?", gone.ID).
			Update("tunnel_issue_at", anchor).Error)
		gone.TunnelIssueAt = anchor
		tok := generateTestTunnelToken(t, gone, time.Hour)
		require.NoError(t, db.Get().Unscoped().Delete(&Device{}, gone.ID).Error)
		_, _, err := validateTunnelToken(nil, tok)
		require.Error(t, err, "删 Device 行后 tunnel token 必须失效（device-not-found → 401）")
	})

	t.Run("Expired_Rejected", func(t *testing.T) {
		tok := generateTestTunnelToken(t, device, -time.Hour)
		_, _, err := validateTunnelToken(nil, tok)
		require.Error(t, err)
	})
}

// TestIssueTunnelTokenForDevice：首次签发写锚点；再次签发复用锚点。
func TestIssueTunnelTokenForDevice(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	user := CreateTestUser(t)
	device := CreateTestDevice(t, user.ID, "udid-issue-"+time.Now().Format("150405.000000"))
	require.Zero(t, device.TunnelIssueAt)

	tok1, err := issueTunnelTokenForDevice(nil, device, user.Roles)
	require.NoError(t, err)
	require.NotZero(t, device.TunnelIssueAt, "首次签发必须落锚点")

	var fresh Device
	require.NoError(t, db.Get().First(&fresh, device.ID).Error)
	require.Equal(t, device.TunnelIssueAt, fresh.TunnelIssueAt, "锚点必须持久化")

	// 签出的 token 必须能通过 validateTunnelToken。
	_, _, err = validateTunnelToken(nil, tok1)
	require.NoError(t, err)

	// 二次签发不改锚点。
	anchor := device.TunnelIssueAt
	_, err = issueTunnelTokenForDevice(nil, device, user.Roles)
	require.NoError(t, err)
	require.Equal(t, anchor, device.TunnelIssueAt)
}
