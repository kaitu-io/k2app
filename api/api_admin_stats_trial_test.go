package center

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestUserStatistics_InTrial(t *testing.T) {
	skipIfNoConfig(t)
	adminUser, adminKey := createBrandIsoAccessKeyUser(t, BrandKaitu, true)
	_ = adminUser
	router := setupBrandIsolationRouter()
	get := func() int64 {
		req := httptest.NewRequest(http.MethodGet, "/app/users/statistics?brand=overleap", nil)
		req.Header.Set("X-Access-Key", adminKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		data, err := ParseResponseData[UserStatisticsResponse](w)
		require.NoError(t, err)
		return data.InTrial
	}
	before := get()

	mk := func(brand Brand, in bool) {
		u, _ := createBrandIsoAccessKeyUser(t, brand, false)
		require.NoError(t, db.Get().Model(&User{}).Where("id = ?", u.ID).Update("in_trial", in).Error)
	}
	mk(BrandOverleap, true)
	mk(BrandOverleap, false)
	mk(BrandKaitu, true) // 别的品牌不计入

	assert.Equal(t, before+1, get())
}
