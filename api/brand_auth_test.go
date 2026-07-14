package center

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// cleanupTestUserByEmail 按邮箱的 index_id 找到关联的 LoginIdentify + User 并硬删。
// LoginIdentify 本身无软删（见 model.go 注释），User 有 gorm.DeletedAt，必须 Unscoped()
// 才能真正释放 (type, index_id, brand) 唯一索引槽位，避免污染真实 dev DB。
func cleanupTestUserByEmail(t *testing.T, email string) {
	t.Helper()
	indexID := secretHashIt(context.Background(), []byte(strings.ToLower(email)))

	var identifies []LoginIdentify
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).Find(&identifies).Error; err != nil {
		t.Logf("cleanupTestUserByEmail: failed to find identifies for %s: %v", email, err)
		return
	}
	for _, li := range identifies {
		if err := db.Get().Unscoped().Delete(&User{}, li.UserID).Error; err != nil {
			t.Logf("cleanupTestUserByEmail: failed to delete user %d: %v", li.UserID, err)
		}
	}
	if err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).Delete(&LoginIdentify{}).Error; err != nil {
		t.Logf("cleanupTestUserByEmail: failed to delete login_identifies for %s: %v", email, err)
	}
}

// 集成：同一邮箱在两个品牌各自注册，得到两个独立用户
func TestFindOrCreateUserByEmail_BrandIsolation(t *testing.T) {
	skipIfNoConfig(t)
	email := "brandsplit-e2e@test.local"
	t.Cleanup(func() { cleanupTestUserByEmail(t, email) })

	gcK := ginCtxWithBrand(BrandKaitu)
	uK, err := FindOrCreateUserByEmail(gcK, email, "zh-CN")
	require.NoError(t, err)
	assert.Equal(t, string(BrandKaitu), uK.Brand)

	gcO := ginCtxWithBrand(BrandOverleap)
	uO, err := FindOrCreateUserByEmail(gcO, email, "en-US")
	require.NoError(t, err)
	assert.Equal(t, string(BrandOverleap), uO.Brand)

	assert.NotEqual(t, uK.ID, uO.ID, "同邮箱双品牌必须是两个独立账号")

	// 再次以 kaitu 查找：拿回 kaitu 用户而非 overleap 用户
	uK2, err := FindOrCreateUserByEmail(gcK, email, "zh-CN")
	require.NoError(t, err)
	assert.Equal(t, uK.ID, uK2.ID)
}

// ginCtxWithBrand 构造带品牌的 gin context（测试 helper）
func ginCtxWithBrand(b Brand) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Set(brandContextKey, b)
	return c
}

// AuthRequired 品牌强制：overleap 用户带 kaitu 请求品牌 → 403003
func TestAuthRequired_BrandMismatch(t *testing.T) {
	skipIfNoConfig(t)

	// 用 X-Access-Key 路径构造凭据（比 JWT 更简单：不需要 device/token 发行链路）。
	plaintext := accessKeyPrefix + generateId("brand-mismatch")
	hash := HashAccessKey(plaintext)
	user := &User{
		UUID:      generateId("user"),
		Brand:     string(BrandOverleap),
		AccessKey: &hash,
	}
	require.NoError(t, db.Get().Create(user).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(user)
	})

	router := SetupTestRouter()

	// 请求不带 Host / X-K2-Brand header → resolveRequestBrand 回退默认 kaitu，
	// 而该用户是 overleap 账号，二者不符，AuthRequired 必须硬拒 403003。
	req, err := http.NewRequest(http.MethodGet, "/api/user/info", nil)
	require.NoError(t, err)
	req.Header.Set("X-Access-Key", plaintext)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	resp, err := ParseResponse(w)
	require.NoError(t, err)
	assert.Equal(t, int(ErrorBrandMismatch), resp.Code,
		"overleap-brand user authenticating against a default(kaitu)-brand request must be rejected as brand mismatch")
}
