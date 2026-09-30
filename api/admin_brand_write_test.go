package center

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// createBrandUserWithEmail 建一个指定品牌的用户；email 非空时同时建该品牌的邮箱登录标识。
func createBrandUserWithEmail(t *testing.T, brand Brand, email string) *User {
	t.Helper()
	u := &User{UUID: generateId("user"), Brand: string(brand)}
	require.NoError(t, db.Get().Create(u).Error)
	if email != "" {
		email = strings.ToLower(email)
		li := &LoginIdentify{
			UserID:         u.ID,
			Type:           "email",
			IndexID:        secretHashIt(context.Background(), []byte(email)),
			EncryptedValue: email,
			Brand:          string(brand),
		}
		require.NoError(t, db.Get().Create(li).Error)
	}
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
		db.Get().Unscoped().Delete(&User{}, u.ID)
	})
	return u
}

// adminJSON 直接把 handler 挂在裸 gin 路由上调用（跳过鉴权中间件——这里测的是 handler 语义）。
func adminJSON(t *testing.T, method, pattern, path string, handler gin.HandlerFunc, body any) (*TestResponse, *httptest.ResponseRecorder) {
	t.Helper()
	testInitConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, pattern, handler)
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req, err := http.NewRequest(method, path, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp, err := ParseResponse(w)
	require.NoError(t, err, "body: %s", w.Body.String())
	return resp, w
}

// ---- 3. 管理员改邮箱：唯一性按目标用户品牌判定；新建的邮箱标识继承用户品牌 ----

func TestAdminUpdateUserEmail_OtherBrandOwnerIsNotConflict(t *testing.T) {
	skipIfNoConfig(t)
	email := "admin-email-xbrand-" + generateId("t") + "@test.local"
	createBrandUserWithEmail(t, BrandKaitu, email) // kaitu 侧已有同邮箱账号
	target := createBrandUserWithEmail(t, BrandOverleap, "")

	resp, _ := adminJSON(t, http.MethodPut, "/app/users/:uuid/email", "/app/users/"+target.UUID+"/email",
		api_admin_update_user_email, gin.H{"email": email})
	require.Equal(t, 0, resp.Code, "kaitu 用户占用同邮箱不影响 overleap 用户：两品牌各自独立账号")

	var li LoginIdentify
	require.NoError(t, db.Get().Where("user_id = ? AND type = ?", target.ID, "email").First(&li).Error)
	assert.Equal(t, string(BrandOverleap), li.Brand, "新建邮箱标识必须继承目标用户品牌，不能落 kaitu 默认值")
}

func TestAdminUpdateUserEmail_SameBrandOwnerIsConflict(t *testing.T) {
	skipIfNoConfig(t)
	email := "admin-email-samebrand-" + generateId("t") + "@test.local"
	createBrandUserWithEmail(t, BrandOverleap, email)
	target := createBrandUserWithEmail(t, BrandOverleap, "")

	resp, _ := adminJSON(t, http.MethodPut, "/app/users/:uuid/email", "/app/users/"+target.UUID+"/email",
		api_admin_update_user_email, gin.H{"email": email})
	assert.Equal(t, int(ErrorEmailAlreadyInUse), resp.Code)
}

func TestAdminUpdateUserEmail_NormalizesCase(t *testing.T) {
	skipIfNoConfig(t)
	target := createBrandUserWithEmail(t, BrandKaitu, "")
	email := "Admin-Email-Case-" + generateId("t") + "@Test.Local"

	resp, _ := adminJSON(t, http.MethodPut, "/app/users/:uuid/email", "/app/users/"+target.UUID+"/email",
		api_admin_update_user_email, gin.H{"email": email})
	require.Equal(t, 0, resp.Code)

	var li LoginIdentify
	require.NoError(t, db.Get().Where("user_id = ? AND type = ?", target.ID, "email").First(&li).Error)
	// 登录路径按小写邮箱算 index_id；大小写不归一的话用户用新邮箱永远登不上
	assert.Equal(t, secretHashIt(context.Background(), []byte(strings.ToLower(email))), li.IndexID)
}

// ---- 5/6/8. admin 列表：?brand= 过滤 + 行带 brand；按邮箱查人返回全部品牌的同邮箱账号 ----

// adminListItems 调 list handler，返回 data.items（每行解成 map）。
func adminListItems(t *testing.T, pattern string, handler gin.HandlerFunc, query string) []map[string]any {
	t.Helper()
	resp, w := adminJSON(t, http.MethodGet, pattern, pattern+"?"+query, handler, nil)
	require.Equal(t, 0, resp.Code, "body: %s", w.Body.String())
	var data struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Data, &data))
	return data.Items
}

func itemHas(items []map[string]any, key string, want any) bool {
	for _, it := range items {
		if it[key] == want {
			return true
		}
	}
	return false
}

func TestAdminLists_BrandFilterAndBrandField(t *testing.T) {
	skipIfNoConfig(t)
	uniq := generateId("t")
	short := strings.ToUpper(uniq[len(uniq)-6:])

	cases := []struct {
		name    string
		pattern string
		handler gin.HandlerFunc
		seed    func(t *testing.T, b Brand) any // 建一行并返回它在列表里的 key 值
		key     string
	}{
		{"users", "/app/users", api_admin_list_users, func(t *testing.T, b Brand) any {
			return createBrandUserWithEmail(t, b, "").UUID
		}, "uuid"},
		{"orders", "/app/orders", api_admin_list_orders, func(t *testing.T, b Brand) any {
			u := createBrandUserWithEmail(t, b, "")
			o := &Order{UUID: generateId("ord"), Title: "brand-list-test", UserID: u.ID, Meta: "{}"}
			require.NoError(t, db.Get().Create(o).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(o) })
			return o.UUID
		}, "uuid"},
		{"campaigns", "/app/campaigns", api_admin_list_campaigns, func(t *testing.T, b Brand) any {
			cp := &Campaign{Code: "BL" + short + string(b)[:1], Name: "brand-list-test", Type: "discount", Value: 90, Brand: string(b)}
			require.NoError(t, db.Get().Create(cp).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(cp) })
			return cp.Code
		}, "code"},
		{"announcements", "/app/announcements", api_admin_list_announcements, func(t *testing.T, b Brand) any {
			a := &Announcement{Message: "brand-list-test-" + uniq + string(b), Brand: string(b)}
			require.NoError(t, db.Get().Create(a).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(a) })
			return a.Message
		}, "message"},
		{"license-key-batches", "/app/license-key-batches", api_admin_list_license_key_batches, func(t *testing.T, b Brand) any {
			bt := &LicenseKeyBatch{Name: "brand-list-test-" + uniq + string(b), PlanDays: 1, Quantity: 0, Brand: string(b)}
			require.NoError(t, db.Get().Create(bt).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(bt) })
			return bt.Name
		}, "name"},
		{"license-keys", "/app/license-keys", api_admin_list_license_keys, func(t *testing.T, b Brand) any {
			k := &LicenseKey{UUID: generateId("lk"), Code: short[:5] + strings.ToUpper(string(b)[:3]), Brand: string(b)}
			require.NoError(t, db.Get().Create(k).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(k) })
			return k.UUID
		}, "uuid"},
		{"feedback-tickets", "/app/feedback-tickets", api_admin_list_feedback_tickets, func(t *testing.T, b Brand) any {
			ft := &FeedbackTicket{FeedbackID: generateId("fb"), UDID: "brand-list-test", Content: "x", Meta: "{}", Brand: string(b)}
			require.NoError(t, db.Get().Create(ft).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(ft) })
			return ft.FeedbackID
		}, "feedbackId"},
		{"edm-templates", "/app/edm/templates", api_admin_list_email_templates, func(t *testing.T, b Brand) any {
			tp := &EmailMarketingTemplate{Name: "brand-list-test", Slug: "bl-" + uniq + "-" + string(b), Language: "en-US", Brand: string(b)}
			require.NoError(t, db.Get().Create(tp).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(tp) })
			return tp.Slug
		}, "slug"},
		{"plans", "/app/plans", api_admin_list_plans, func(t *testing.T, b Brand) any {
			p := &Plan{PID: "bl" + short + string(b)[:2], Label: "brand-list-test", Month: 1, Brand: string(b)}
			require.NoError(t, db.Get().Create(p).Error)
			t.Cleanup(func() { db.Get().Unscoped().Delete(p) })
			return p.PID
		}, "pid"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kVal := tc.seed(t, BrandKaitu)
			oVal := tc.seed(t, BrandOverleap)
			for _, b := range []Brand{BrandKaitu, BrandOverleap} {
				items := adminListItems(t, tc.pattern, tc.handler, "pageSize=100&brand="+string(b))
				for _, it := range items {
					assert.Equal(t, string(b), it["brand"], "%s ?brand=%s returned a row of another brand: %v", tc.name, b, it[tc.key])
				}
				mine, other := kVal, oVal
				if b == BrandOverleap {
					mine, other = oVal, kVal
				}
				assert.True(t, itemHas(items, tc.key, mine), "%s ?brand=%s must include the seeded row", tc.name, b)
				assert.False(t, itemHas(items, tc.key, other), "%s ?brand=%s must exclude the other brand's row", tc.name, b)
			}
			// 不带 brand = 全部品牌
			all := adminListItems(t, tc.pattern, tc.handler, "pageSize=100")
			assert.True(t, itemHas(all, tc.key, kVal) && itemHas(all, tc.key, oVal), "%s without ?brand= must list both brands", tc.name)
		})
	}
}

func TestAdminListUsers_EmailLookupReturnsEveryBrand(t *testing.T) {
	skipIfNoConfig(t)
	email := "admin-lookup-" + generateId("t") + "@test.local"
	uK := createBrandUserWithEmail(t, BrandKaitu, email)
	uO := createBrandUserWithEmail(t, BrandOverleap, email)

	items := adminListItems(t, "/app/users", api_admin_list_users, "email="+strings.ToUpper(email))
	require.Len(t, items, 2, "同邮箱两品牌各一个账号，按邮箱查人必须两个都返回（且大小写不敏感）")
	assert.True(t, itemHas(items, "uuid", uK.UUID))
	assert.True(t, itemHas(items, "uuid", uO.UUID))

	items = adminListItems(t, "/app/users", api_admin_list_users, "brand=overleap&email="+email)
	require.Len(t, items, 1)
	assert.Equal(t, uO.UUID, items[0]["uuid"])
	assert.Equal(t, "overleap", items[0]["brand"])
}

func TestAdminListOrders_EmailFilterMatches(t *testing.T) {
	skipIfNoConfig(t)
	email := "admin-order-lookup-" + generateId("t") + "@test.local"
	u := createBrandUserWithEmail(t, BrandOverleap, email)
	o := &Order{UUID: generateId("ord"), Title: "order-email-filter-test", UserID: u.ID, Meta: "{}"}
	require.NoError(t, db.Get().Create(o).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(o) })

	items := adminListItems(t, "/app/orders", api_admin_list_orders, "loginProvider=email&loginIdentity="+email)
	require.Len(t, items, 1, "按邮箱筛订单必须用 index_id 的哈希匹配")
	assert.Equal(t, o.UUID, items[0]["uuid"])
	assert.Equal(t, "overleap", items[0]["brand"])
}

func TestAdminUserDetail_HasBrand(t *testing.T) {
	skipIfNoConfig(t)
	u := createBrandUserWithEmail(t, BrandOverleap, "")
	resp, _ := adminJSON(t, http.MethodGet, "/app/users/:uuid", "/app/users/"+u.UUID, api_admin_get_user_detail, nil)
	require.Equal(t, 0, resp.Code)
	var d map[string]any
	require.NoError(t, json.Unmarshal(resp.Data, &d))
	assert.Equal(t, "overleap", d["brand"])
}

func TestAdminDeviceStatistics_BrandFilter(t *testing.T) {
	skipIfNoConfig(t)
	u := createBrandUserWithEmail(t, BrandOverleap, "")
	// arch 分组不设 Limit（version/model 只取 top 10，测试行进不了榜）
	id := generateId("a")
	arch := "bs" + id[len(id)-12:]
	d := &Device{UDID: generateId("udid"), UserID: u.ID, AppPlatform: "ios", AppArch: arch}
	require.NoError(t, db.Get().Create(d).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(d) })

	has := func(brand string) bool {
		path := "/app/devices/statistics"
		if brand != "" {
			path += "?brand=" + brand
		}
		resp, _ := adminJSON(t, http.MethodGet, "/app/devices/statistics", path, api_admin_get_device_statistics, nil)
		require.Equal(t, 0, resp.Code)
		var r DeviceStatisticsResponse
		require.NoError(t, json.Unmarshal(resp.Data, &r))
		for _, a := range r.ByArch {
			if a.Arch == arch {
				return true
			}
		}
		return false
	}
	assert.True(t, has(""), "no filter = all brands")
	assert.True(t, has("overleap"))
	assert.False(t, has("kaitu"), "?brand=kaitu must not count an overleap user's device")
}
