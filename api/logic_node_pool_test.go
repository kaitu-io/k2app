package center

import (
	"fmt"
	"math/rand"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestNodeAllowedForUser —— 体验线路隔离判定（spec §3.7 + controller ruling）：
// 只有「InTrial 且从未付过费」的用户被限制在 pool=trial；其余一律放行。
func TestNodeAllowedForUser(t *testing.T) {
	regular := &SlaveNode{Pool: ""}
	trial := &SlaveNode{Pool: NodePoolTrial}

	cases := []struct {
		name        string
		user        *User
		wantRegular bool
		wantTrial   bool
	}{
		{"nil user", nil, true, true},
		{"non-trial never paid", &User{InTrial: false}, true, true},
		{"non-trial paid", &User{InTrial: false, IsFirstOrderDone: BoolPtr(true)}, true, true},
		// 试用结束后靠赠送天数/邀请天数仍在有效期：InTrial 已清，照常放行。
		{"trial expired, gifted days left", &User{InTrial: false, ExpiredAt: time.Now().Unix() + 86400}, true, true},
		{"in trial, IsFirstOrderDone nil", &User{InTrial: true}, false, true},
		{"in trial, IsFirstOrderDone false", &User{InTrial: true, IsFirstOrderDone: BoolPtr(false)}, false, true},
		// 付过费又换 Apple ID 开试用：不降级。
		{"in trial but paid elsewhere", &User{InTrial: true, IsFirstOrderDone: BoolPtr(true)}, true, true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.wantRegular, nodeAllowedForUser(regular, tc.user), tc.name+" × regular")
		assert.Equal(t, tc.wantTrial, nodeAllowedForUser(trial, tc.user), tc.name+" × trial")
	}
}

// ---------------------------------------------------------------------------
// Shared integration fixture for the list-endpoint pool tests (needs dev MySQL).
// ---------------------------------------------------------------------------

type poolFixtureNode struct {
	Node   SlaveNode
	Tunnel SlaveTunnel
}

// poolFixtureIP returns a random 10.x address unlikely to collide with other
// tests' fixed fixtures (which live in 10.9x.*).
func poolFixtureIP() string {
	return fmt.Sprintf("10.%d.%d.%d", 60+rand.Intn(20), rand.Intn(250)+1, rand.Intn(250)+1)
}

// seedPoolNode creates a kaitu-visible shared node (with the given pool and
// country) plus one k2v5 relay-capable tunnel, and registers cleanup.
func seedPoolNode(t *testing.T, role, uniq, country, pool string) poolFixtureNode {
	t.Helper()
	ip := poolFixtureIP()
	node := SlaveNode{
		Ipv4: ip, SecretToken: "pool-s-" + role + "-" + uniq, Country: country, Region: "test",
		Name: "pool-" + role + "-" + uniq, Class: NodeClassShared, Pool: pool,
	}
	require.NoError(t, db.Get().Create(&node).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&node) })

	domain := "pool-" + role + "-" + uniq + ".example"
	tun := SlaveTunnel{
		Domain: domain, SecretToken: "pool-t-" + role + "-" + uniq, Name: "pool-tun-" + role + "-" + uniq,
		Protocol: TunnelProtocolK2V5, Port: 443, NodeID: node.ID,
		IsTest: BoolPtr(false), HasRelay: BoolPtr(true),
		ServerURL: "k2v5://" + domain + ":443",
	}
	require.NoError(t, db.Get().Create(&tun).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&tun) })
	return poolFixtureNode{Node: node, Tunnel: tun}
}

// seedPoolFixture: 2 regular-pool nodes + 1 trial-pool node.
func seedPoolFixture(t *testing.T) (uniq string, regular []poolFixtureNode, trial poolFixtureNode) {
	t.Helper()
	uniq = fmt.Sprintf("%d", time.Now().UnixNano())
	regular = []poolFixtureNode{
		seedPoolNode(t, "reg1", uniq, "ZQ", ""),
		seedPoolNode(t, "reg2", uniq, "ZX", ""),
	}
	trial = seedPoolNode(t, "trial", uniq, "ZQ", NodePoolTrial)
	return uniq, regular, trial
}

func poolTrialUser() *User {
	return &User{ID: 999801, Brand: string(BrandKaitu), InTrial: true, ExpiredAt: time.Now().Unix() + 86400}
}

func poolPaidUser() *User {
	return &User{ID: 999802, Brand: string(BrandKaitu), IsFirstOrderDone: BoolPtr(true), ExpiredAt: time.Now().Unix() + 86400}
}

// callHandlerAs drives a handler directly with an injected auth context
// (mirrors TestApiK2Relays_FiltersByUserBrand) and returns the raw body.
func callHandlerAs(t *testing.T, h gin.HandlerFunc, user *User, path string, params gin.Params) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", path, nil)
	c.Params = params
	c.Set("authContext", &authContext{UserID: user.ID, User: user})
	h(c)
	require.Equal(t, 200, w.Code, "body=%s", w.Body.String())
	return w.Body.String()
}

// countPresent reports how many fixture nodes' tunnel domains appear in body.
func countPresent(body string, nodes ...poolFixtureNode) int {
	n := 0
	for _, fn := range nodes {
		if strings.Contains(body, fn.Tunnel.Domain) {
			n++
		}
	}
	return n
}

// countPresentIP is countPresent for responses that carry node IPs instead of
// tunnel domains (/api/relays). Quoted to avoid prefix matches (10.1.2.3 vs 10.1.2.30).
func countPresentIP(body string, nodes ...poolFixtureNode) int {
	n := 0
	for _, fn := range nodes {
		if strings.Contains(body, `"`+fn.Node.Ipv4+`"`) {
			n++
		}
	}
	return n
}
