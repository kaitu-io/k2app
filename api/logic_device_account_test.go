package center

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestUserHasIdentity 零身份判定：login_identifies 行数为 0 = 设备账号。
func TestUserHasIdentity(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	uniq := time.Now().UnixNano()
	u := &User{UUID: generateId("user"), Brand: string(BrandKaitu)}
	require.NoError(t, db.Get().Create(u).Error)
	t.Cleanup(func() {
		db.Get().Where("user_id = ?", u.ID).Delete(&LoginIdentify{})
		db.Get().Unscoped().Delete(&User{}, u.ID)
	})

	has, err := userHasIdentity(db.Get(), u.ID)
	require.NoError(t, err)
	require.False(t, has, "0 行 → 无身份")

	for i, typ := range []string{"email", "apple"} {
		li := &LoginIdentify{
			UserID:         u.ID,
			Type:           typ,
			IndexID:        fmt.Sprintf("devacct-%d-%d", uniq, i),
			EncryptedValue: "x",
			Brand:          string(BrandKaitu),
		}
		require.NoError(t, db.Get().Create(li).Error)
		has, err = userHasIdentity(db.Get(), u.ID)
		require.NoError(t, err)
		require.True(t, has, "存在 %s 身份 → true", typ)
		require.NoError(t, db.Get().Delete(li).Error)
	}
}

// TestAppConfig_DeviceAccountFlag app config 按品牌下发 deviceAccount：开途 true，Overleap false。
func TestAppConfig_DeviceAccountFlag(t *testing.T) {
	testInitConfig()
	r := setupBrandIsolationRouter()
	for host, want := range map[string]bool{"www.kaitu.io": true, "www.overleap.io": false} {
		req := httptest.NewRequest(http.MethodGet, "/api/app/config", nil)
		req.Host = host
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		raw, ok := resp.Data["deviceAccount"]
		require.True(t, ok, "%s: deviceAccount 字段缺失", host)
		var got bool
		require.NoError(t, json.Unmarshal(raw, &got))
		require.Equal(t, want, got, host)
	}
}
