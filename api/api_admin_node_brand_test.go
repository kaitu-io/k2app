package center

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestAdminListNodesBrandFields drives GET /app/nodes directly (no admin
// middleware — the brand fields and the ?brand= filter are what's under test)
// against dev MySQL. Node A declares both brands, node B declares nothing
// (= kaitu only), so the two differ only in overleap visibility.
func TestAdminListNodesBrandFields(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	gin.SetMode(gin.TestMode)

	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	seg := uniq[len(uniq)-3:]
	ipA := fmt.Sprintf("10.93.%s.1", seg)
	ipB := fmt.Sprintf("10.93.%s.2", seg)

	nodeA := SlaveNode{Ipv4: ipA, SecretToken: "tok-a-" + uniq, Country: "US", Region: "us-west",
		Name: "brandfield-a-" + uniq, Class: NodeClassShared, Brands: "kaitu,overleap"}
	nodeB := SlaveNode{Ipv4: ipB, SecretToken: "tok-b-" + uniq, Country: "US", Region: "us-west",
		Name: "brandfield-b-" + uniq, Class: NodeClassShared, Brands: ""}
	require.NoError(t, db.Get().Create(&nodeA).Error)
	require.NoError(t, db.Get().Create(&nodeB).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Delete(&nodeA)
		db.Get().Unscoped().Delete(&nodeB)
	})

	r := gin.New()
	r.GET("/app/nodes", api_admin_list_nodes)

	type item struct {
		ID              uint64   `json:"id"`
		Brands          []string `json:"brands"`
		VisibleKaitu    bool     `json:"visibleKaitu"`
		VisibleOverleap bool     `json:"visibleOverleap"`
	}
	list := func(t *testing.T, query string) (map[uint64]item, int, int64) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/app/nodes"+query, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp struct {
			Code int `json:"code"`
			Data struct {
				Items      []item `json:"items"`
				Pagination struct {
					Total int64 `json:"total"`
				} `json:"pagination"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
		require.Equal(t, 0, resp.Code, w.Body.String())
		require.NotEmpty(t, resp.Data.Items, "the two seeded nodes guarantee a non-empty list")
		byID := make(map[uint64]item, len(resp.Data.Items))
		for _, it := range resp.Data.Items {
			byID[it.ID] = it
		}
		return byID, len(resp.Data.Items), resp.Data.Pagination.Total
	}

	t.Run("unfiltered_exposes_brand_fields", func(t *testing.T) {
		byID, _, _ := list(t, "")
		a, okA := byID[nodeA.ID]
		b, okB := byID[nodeB.ID]
		require.True(t, okA, "node A must be listed")
		require.True(t, okB, "node B must be listed")

		assert.Equal(t, []string{"kaitu", "overleap"}, a.Brands)
		assert.True(t, a.VisibleKaitu)
		assert.True(t, a.VisibleOverleap)

		assert.Equal(t, []string{"kaitu"}, b.Brands, "empty declaration = kaitu only")
		assert.True(t, b.VisibleKaitu)
		assert.False(t, b.VisibleOverleap, "undeclared brand is never visible, whatever the switch says")
	})

	t.Run("brand_overleap_keeps_only_overleap_visible", func(t *testing.T) {
		byID, n, total := list(t, "?brand=overleap")
		_, okA := byID[nodeA.ID]
		_, okB := byID[nodeB.ID]
		assert.True(t, okA, "node A declares overleap and must survive the filter")
		assert.False(t, okB, "node B is kaitu-only and must be filtered out")
		for id, it := range byID {
			assert.True(t, it.VisibleOverleap, "node %d returned by ?brand=overleap must be overleap-visible", id)
		}
		assert.Equal(t, int64(n), total, "total must reflect the filtered count")
	})

	t.Run("brand_kaitu_and_invalid_keep_both", func(t *testing.T) {
		for _, q := range []string{"?brand=kaitu", "?brand=bogus"} {
			byID, _, _ := list(t, q)
			_, okA := byID[nodeA.ID]
			_, okB := byID[nodeB.ID]
			assert.True(t, okA, "%s: node A", q)
			assert.True(t, okB, "%s: node B", q)
		}
	})
}
