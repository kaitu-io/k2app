package center

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

func TestNormalizeDeclaredPool(t *testing.T) {
	for in, want := range map[string]string{"": "", " ": "", "trial": "trial", " TRIAL ": "trial"} {
		got, ok := normalizeDeclaredPool(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"trail", "vip", "member"} {
		_, ok := normalizeDeclaredPool(bad)
		assert.False(t, ok, bad)
	}
}

// 节点池是节点声明的（K2_NODE_POOL，与 K2_NODE_BRANDS 同机制）：每次注册写 slave_nodes.pool；
// sidecar 关机 Unregister 硬删行之后重注册仍是 trial；后台改的值下次注册被节点声明覆盖；非法值拒绝注册。
func TestUpsert_PoolIsNodeDeclared(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)
	n := time.Now().UnixNano()
	ipv4 := fmt.Sprintf("10.251.%d.%d", (n/1000)%250, n%250)
	base := SlaveNodeUpsertRequest{Country: "JP", Name: "pool-" + ipv4, SecretToken: fmt.Sprintf("tok-pool-%d", n)}
	withPool := func(p string) SlaveNodeUpsertRequest { r := base; r.Pool = p; return r }

	assert.Equal(t, NodePoolTrial, callNodeUpsert(t, ipv4, withPool("trial")).Pool, "新建行写入声明的池")

	// 后台临时改成 ''，节点重注册（重启）→ 节点声明胜出。
	require.NoError(t, db.Get().Model(&SlaveNode{}).Where("ipv4 = ?", ipv4).Update("pool", "").Error)
	assert.Equal(t, NodePoolTrial, callNodeUpsert(t, ipv4, withPool(" Trial ")).Pool, "重注册覆盖后台值")

	// sidecar SIGTERM → Unregister 硬删节点行；重启后重新注册仍是体验线路（本修复的目的）。
	require.NoError(t, db.Get().Unscoped().Where("ipv4 = ?", ipv4).Delete(&SlaveNode{}).Error)
	assert.Equal(t, NodePoolTrial, callNodeUpsert(t, ipv4, withPool("trial")).Pool, "硬删后重建仍按声明")

	// 不再声明（.env 删掉 / 老 sidecar 不发）→ 回到会员池。
	assert.Equal(t, "", callNodeUpsert(t, ipv4, base).Pool)

	// 非法值：拒绝注册，不改行。
	require.NoError(t, db.Get().Model(&SlaveNode{}).Where("ipv4 = ?", ipv4).Update("pool", NodePoolTrial).Error)
	body, _ := json.Marshal(withPool("trail"))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/slave/nodes/"+ipv4, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	nodeUpsertTestRouter().ServeHTTP(w, req)
	var env map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	assert.Equal(t, float64(ErrorInvalidArgument), env["code"], w.Body.String())
	var after SlaveNode
	require.NoError(t, db.Get().Where("ipv4 = ?", ipv4).First(&after).Error)
	assert.Equal(t, NodePoolTrial, after.Pool, "被拒的注册不改任何列")
}
