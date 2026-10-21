package center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

const devAcctClientHeader = "kaitu-service/0.5.0 (ios; arm64; iOS 18.1; iPhone15,2)"

type devAcctResp struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    DataAuthResult `json:"data"`
}

func devAcctRouter(t *testing.T) *gin.Engine {
	t.Helper()
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	// 每个测试独占限流器：测试共用 httptest 的 RemoteAddr，不重置会互相耗尽配额。
	orig := deviceAccountLimiter
	deviceAccountLimiter = newFunnelIPLimiter(20)
	t.Cleanup(func() { deviceAccountLimiter = orig })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/auth/device-account", api_device_account)
	return r
}

func devAcctUDID(t *testing.T) string {
	t.Helper()
	return devAcctCleanupUDID(t, fmt.Sprintf("devacct-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano()))
}

// devAcctHexUDID 形如真实 iOS udid 的 hex 串（漏斗 device_hash 校验要求 32-64 位 hex）。
func devAcctHexUDID(t *testing.T) string {
	t.Helper()
	return devAcctCleanupUDID(t, fmt.Sprintf("%032x", time.Now().UnixNano()))
}

func devAcctCleanupUDID(t *testing.T, udid string) string {
	t.Helper()
	t.Cleanup(func() {
		var devs []Device
		db.Get().Where("udid = ?", udid).Find(&devs)
		db.Get().Unscoped().Where("udid = ?", udid).Delete(&Device{})
		for _, d := range devs {
			db.Get().Where("user_id = ?", d.UserID).Delete(&LoginIdentify{})
			db.Get().Unscoped().Delete(&User{}, d.UserID)
		}
	})
	return udid
}

func postDeviceAccount(t *testing.T, r *gin.Engine, host, udid string) devAcctResp {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"udid": udid, "remark": "iPhone", "language": "zh-CN"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/device-account", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-K2-Client", devAcctClientHeader)
	req.Host = host
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp devAcctResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	return resp
}

func devAcctDevice(t *testing.T, udid string) Device {
	t.Helper()
	var d Device
	require.NoError(t, db.Get().Where("udid = ?", udid).First(&d).Error)
	return d
}

func TestDeviceAccount_CreateNew(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)

	resp := postDeviceAccount(t, r, "www.kaitu.io", udid)
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.AccessToken)
	require.NotEmpty(t, resp.Data.RefreshToken)

	d := devAcctDevice(t, udid)
	require.Equal(t, "iPhone", d.Remark)
	require.Equal(t, "ios", d.AppPlatform)
	require.Equal(t, "0.5.0", d.AppVersion)
	require.False(t, d.IsGateway)
	require.NotZero(t, d.TokenIssueAt)

	var u User
	require.NoError(t, db.Get().First(&u, d.UserID).Error)
	require.Equal(t, string(BrandKaitu), u.Brand)
	require.Equal(t, "zh-CN", u.Language)
	require.NotEmpty(t, u.UUID)
	require.Equal(t, int64(0), u.ExpiredAt)
	require.NotNil(t, u.IsActivated)
	require.True(t, *u.IsActivated)
	require.NotZero(t, u.ActivatedAt)

	has, err := userHasIdentity(db.Get(), u.ID)
	require.NoError(t, err)
	require.False(t, has, "设备账号零身份")
}

// TestDeviceAccount_ExistingDeviceAccountRequiresProof 设备行属设备账号时不凭 udid 签 token（spec §4.2）：
// 返回 403006，设备行与原主人的 token 锚点都不动。
func TestDeviceAccount_ExistingDeviceAccountRequiresProof(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)

	first := postDeviceAccount(t, r, "www.kaitu.io", udid)
	require.Equal(t, 0, first.Code)
	d1 := devAcctDevice(t, udid)

	second := postDeviceAccount(t, r, "www.kaitu.io", udid)
	require.Equal(t, int(ErrorDeviceAccountProofRequired), second.Code)
	require.Empty(t, second.Data.AccessToken)

	d2 := devAcctDevice(t, udid)
	require.Equal(t, d1.UserID, d2.UserID)
	require.Equal(t, d1.TokenIssueAt, d2.TokenIssueAt, "原主人的 token 照常可用")
	_, _, err := validateToken(context.Background(), first.Data.RefreshToken, TokenTypeRefresh)
	require.NoError(t, err)
}

func TestDeviceAccount_IdentityOwnerRequiresLogin(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)

	u := User{UUID: generateId("user"), Brand: string(BrandKaitu), IsActivated: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&u).Error)
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
		db.Get().Unscoped().Delete(&User{}, u.ID)
	})
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: u.ID, Type: "email",
		IndexID: "devacct-idx-" + udid, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
	require.NoError(t, db.Get().Create(&Device{UDID: udid, UserID: u.ID, TokenIssueAt: 1, TokenLastUsedAt: 1}).Error)

	resp := postDeviceAccount(t, r, "www.kaitu.io", udid)
	require.Equal(t, int(ErrorDeviceAccountLoginRequired), resp.Code)
	require.Empty(t, resp.Data.AccessToken)
	require.Equal(t, int64(1), devAcctDevice(t, udid).TokenIssueAt, "设备行不动")
}

func TestDeviceAccount_AfterBindRequiresLogin(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)

	require.Equal(t, 0, postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
	d := devAcctDevice(t, udid)
	require.NoError(t, db.Get().Create(&LoginIdentify{UserID: d.UserID, Type: "email",
		IndexID: "devacct-bind-" + udid, EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)

	resp := postDeviceAccount(t, r, "www.kaitu.io", udid)
	require.Equal(t, int(ErrorDeviceAccountLoginRequired), resp.Code)
}

func TestDeviceAccount_BrandGate(t *testing.T) {
	r := devAcctRouter(t)

	t.Run("overleap 关闭", func(t *testing.T) {
		udid := devAcctUDID(t)
		resp := postDeviceAccount(t, r, "www.overleap.io", udid)
		require.Equal(t, int(ErrorDeviceAccountDisabled), resp.Code)
		var n int64
		require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", udid).Count(&n).Error)
		require.Equal(t, int64(0), n, "关闭时不建号")
	})

	t.Run("已存在的别品牌设备账号", func(t *testing.T) {
		udid := devAcctUDID(t)
		u := User{UUID: generateId("user"), Brand: string(BrandOverleap), IsActivated: BoolPtr(true)}
		require.NoError(t, db.Get().Create(&u).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, u.ID) })
		require.NoError(t, db.Get().Create(&Device{UDID: udid, UserID: u.ID, TokenIssueAt: 1, TokenLastUsedAt: 1}).Error)

		resp := postDeviceAccount(t, r, "www.kaitu.io", udid)
		require.Equal(t, int(ErrorDeviceAccountProofRequired), resp.Code, "已存在的设备账号一律要凭证，不泄露品牌")
	})
}

func TestDeviceAccount_InvalidClientHeader(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/device-account", strings.NewReader(`{"udid":"`+udid+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-K2-Client", "totally-not-valid")
	req.Host = "www.kaitu.io"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp devAcctResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, int(ErrorInvalidClientClass), resp.Code)
}

func TestDeviceAccount_RateLimited(t *testing.T) {
	r := devAcctRouter(t)
	deviceAccountLimiter = newFunnelIPLimiter(1)
	udid := devAcctUDID(t)
	require.Equal(t, 0, postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
	require.Equal(t, int(ErrorTooManyRequests), postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
}

func TestDeviceAccount_ConcurrentCreateSingleUser(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)

	var usersBefore int64
	require.NoError(t, db.Get().Unscoped().Model(&User{}).Count(&usersBefore).Error)

	const n = 6
	resps := make([]devAcctResp, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resps[i] = postDeviceAccount(t, r, "www.kaitu.io", udid)
		}(i)
	}
	close(start)
	wg.Wait()
	// 只有一个建号成功；其余撞上已存在的设备账号，不凭 udid 拿 token（客户端单飞建号）。
	ok := 0
	for i, rs := range resps {
		if rs.Code == 0 {
			ok++
			continue
		}
		require.Equal(t, int(ErrorDeviceAccountProofRequired), rs.Code, "请求 %d: %s", i, rs.Message)
		require.Empty(t, rs.Data.AccessToken)
	}
	require.Equal(t, 1, ok)

	d := devAcctDevice(t, udid)
	var devCount int64
	require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", udid).Count(&devCount).Error)
	require.Equal(t, int64(1), devCount)
	var u User
	require.NoError(t, db.Get().First(&u, d.UserID).Error)
	// 并发失败方的用户行随事务回滚：全表只多出一个用户。
	var usersAfter int64
	require.NoError(t, db.Get().Unscoped().Model(&User{}).Count(&usersAfter).Error)
	require.Equal(t, usersBefore+1, usersAfter)
}

func TestDeviceAccount_FunnelEvent(t *testing.T) {
	r := devAcctRouter(t)
	orig := funnelQ
	q := newFunnelQueue(16)
	q.startOnce.Do(func() {}) // 不启动消费者，直接从通道读
	funnelQ = q
	t.Cleanup(func() { funnelQ = orig })

	udid := devAcctHexUDID(t)
	drain := func() []FunnelEvent {
		var got []FunnelEvent
		for len(q.ch) > 0 {
			got = append(got, <-q.ch)
		}
		return got
	}
	check := func(source string) {
		t.Helper()
		got := drain()
		require.Len(t, got, 1)
		ev := got[0]
		d := devAcctDevice(t, udid)
		require.Equal(t, "device_account_created", ev.Event)
		require.Equal(t, FunnelSurfaceApp, ev.Surface)
		require.Equal(t, string(BrandKaitu), ev.Brand)
		require.Equal(t, d.UserID, ev.UserID)
		require.Equal(t, udid, ev.AnonID, "app 的 device_hash 就是 udid")
		require.Equal(t, source, ev.Source)
	}

	require.Equal(t, 0, postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
	check("create")
	// 已存在的设备账号被拒，不记事件
	require.Equal(t, int(ErrorDeviceAccountProofRequired), postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
	require.Empty(t, drain())
}

// TestDeviceAccount_FunnelJoinsAnonFirstOpen 建号前的匿名 app_first_open（device_hash=udid）
// 与服务端写的建号事件归到同一个人。
func TestDeviceAccount_FunnelJoinsAnonFirstOpen(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctHexUDID(t)
	t.Cleanup(func() {
		db.Get().Where("anon_id = ?", udid).Delete(&FunnelEvent{})
		db.Get().Where("anon_id = ?", udid).Delete(&FunnelIdentity{})
	})
	from := time.Now().Add(-time.Minute)
	anon := FunnelEvent{OccurredAt: time.Now(), Brand: string(BrandKaitu), Surface: FunnelSurfaceApp,
		Event: "app_first_open", AnonID: udid, OS: "ios", AppVersion: "0.5.0"}
	require.NoError(t, db.Get().Create(&anon).Error)

	require.Equal(t, 0, postDeviceAccount(t, r, "www.kaitu.io", udid).Code)
	funnelFlushForTest()
	uid := devAcctDevice(t, udid).UserID

	recs, err := loadFunnelEvents(context.Background(), BrandKaitu, true, from, time.Now().Add(time.Minute),
		[]string{"app_first_open", "device_account_created"})
	require.NoError(t, err)
	var mine []funnelRecord
	for _, rc := range recs {
		if rc.AnonID == udid {
			mine = append(mine, rc)
		}
	}
	require.Len(t, mine, 2)
	ids, err := loadFunnelIdentities(context.Background(), mine)
	require.NoError(t, err)
	want := fmt.Sprintf("u:%d", uid)
	for _, rc := range mine {
		k, ok := funnelPersonKey(&rc, ids)
		require.True(t, ok)
		require.Equal(t, want, k, rc.Event)
	}
}

// TestDeviceAccount_GatewayClientRejected 路由器不能建设备账号。
func TestDeviceAccount_GatewayClientRejected(t *testing.T) {
	r := devAcctRouter(t)
	udid := devAcctUDID(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/device-account", strings.NewReader(`{"udid":"`+udid+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-K2-Client", "kaitu-router/0.4.5 (linux; arm64; OpenWrt 23.05; mt7620)")
	req.Host = "www.kaitu.io"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp devAcctResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, int(ErrorInvalidClientClass), resp.Code)
	var n int64
	require.NoError(t, db.Get().Model(&Device{}).Where("udid = ?", udid).Count(&n).Error)
	require.Equal(t, int64(0), n)
}

func TestDeviceLimit_ZeroIdentityIsOne(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	ctx := context.Background()
	stamp := time.Now().UnixNano()

	mk := func(withIdentity bool) User {
		u := User{UUID: generateId("user"), Brand: string(BrandKaitu), Tier: TierBasic, IsActivated: BoolPtr(true)}
		require.NoError(t, db.Get().Create(&u).Error)
		t.Cleanup(func() {
			db.Get().Unscoped().Where("user_id = ?", u.ID).Delete(&Device{})
			db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
			db.Get().Unscoped().Delete(&User{}, u.ID)
		})
		if withIdentity {
			require.NoError(t, db.Get().Create(&LoginIdentify{UserID: u.ID, Type: "email",
				IndexID: fmt.Sprintf("devlimit-%d-%d", stamp, u.ID), EncryptedValue: "x", Brand: string(BrandKaitu)}).Error)
		}
		require.NoError(t, db.Get().Create(&Device{UDID: fmt.Sprintf("devlimit-%d-%d-a", stamp, u.ID),
			UserID: u.ID, TokenIssueAt: 1, TokenLastUsedAt: 1}).Error)
		return u
	}
	countDevices := func(uid uint64) int64 {
		var n int64
		require.NoError(t, db.Get().Model(&Device{}).Where("user_id = ?", uid).Count(&n).Error)
		return n
	}

	zero := mk(false)
	require.Greater(t, zero.Quota().MaxDevice, 1, "前提：Tier basic 上限 > 1")
	require.NoError(t, checkDeviceLimitOrKick(ctx, db.Get(), &zero, false))
	require.Equal(t, int64(0), countDevices(zero.ID), "零身份上限 1：已有 1 台 → 踢最旧")

	ident := mk(true)
	require.NoError(t, checkDeviceLimitOrKick(ctx, db.Get(), &ident, false))
	require.Equal(t, int64(1), countDevices(ident.ID), "有身份用户照旧按 Tier")
}
