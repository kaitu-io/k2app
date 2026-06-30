package center

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// TestSlaveNodeUpsert_IdentitySingleSource pins the single-source-of-truth contract
// for a private node's identity across registrations (P0 — log-proven leak on
// node 54.66.29.118, 2026-06-18: claimed private → `docker compose down` hard-deleted
// the row → `up` recreated it as shared → served the shared pool ~10h).
//
// Authoritative source = PrivateNodeSubscription. node.{Class,owner,subID} and
// sub.SlaveNodeID are projections re-derived on EVERY registration. Invariants:
//   - I1 (fail-safe): a node carrying a claim token can NEVER be shared. If it can't
//     bind a serviceable sub it stays private-unowned (subID=nil → serves nobody).
//   - I2 (correctness): every registration reconciles the binding from the sub and
//     refreshes both directions (node side + sub.SlaveNodeID), keyed by bound IPv4.
func TestSlaveNodeUpsert_IdentitySingleSource(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	now := time.Now().Unix()

	driveUpsert := func(t *testing.T, ip string, req SlaveNodeUpsertRequest) *TestResponse {
		t.Helper()
		body, err := json.Marshal(req)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "ipv4", Value: ip}}
		c.Request = httptest.NewRequest("PUT", "/slave/nodes/"+ip, bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		api_slave_node_upsert(c)
		resp, err := ParseResponse(w)
		require.NoError(t, err)
		return resp
	}

	// simulateUnregister mimics api_slave_node_unregister: hard-delete tunnels + node
	// (this is what `docker compose down` triggers and what wiped the private identity).
	simulateUnregister := func(t *testing.T, ip string) {
		t.Helper()
		var n SlaveNode
		if err := db.Get().Where("ipv4 = ?", ip).First(&n).Error; err == nil {
			db.Get().Unscoped().Where("node_id = ?", n.ID).Delete(&SlaveTunnel{})
			db.Get().Unscoped().Where("id = ?", n.ID).Delete(&SlaveNode{})
		}
	}

	t.Run("T1_PrivateSurvivesUnregisterRecreate", func(t *testing.T) {
		owner := CreateTestUser(t)
		claimToken := "id-tok-T1-" + generateId("c")
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusProvisioning, Region: "hongkong",
			IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
			ProvisionClaimToken: claimToken,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		ip := "10.99.22.1"
		simulateUnregister(t, ip)
		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
		})

		// 1) First registration with claim → private + bound.
		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "T1", SecretToken: "secret-T1", PrivateClaim: claimToken,
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "first register: %s", resp.Message)

		var first SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&first).Error)
		require.Equal(t, NodeClassPrivate, first.Class, "first register must be private")
		id1 := first.ID

		// 2) Hard-delete (sidecar `down`/unregister).
		simulateUnregister(t, ip)

		// 3) Re-register: sidecar always re-sends the .env token (now blanked in DB).
		resp = driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "T1", SecretToken: "secret-T1", PrivateClaim: claimToken,
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "re-register: %s", resp.Message)

		// MUST come back private (NOT shared) — this is the leak.
		var reborn SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&reborn).Error)
		require.Equal(t, NodeClassPrivate, reborn.Class,
			"re-registered node leaked into shared pool")
		require.NotNil(t, reborn.PrivateOwnerUserID)
		require.Equal(t, owner.ID, *reborn.PrivateOwnerUserID)
		require.NotNil(t, reborn.PrivateSubID)
		require.Equal(t, sub.ID, *reborn.PrivateSubID)
		require.NotEqual(t, id1, reborn.ID, "recreate yields a new node id")

		// sub.SlaveNodeID must be refreshed to the LIVE node id (defect B).
		var reloaded PrivateNodeSubscription
		require.NoError(t, db.Get().First(&reloaded, sub.ID).Error)
		require.NotNil(t, reloaded.SlaveNodeID)
		require.Equal(t, reborn.ID, *reloaded.SlaveNodeID,
			"sub.SlaveNodeID stale → gateway resolves a dead node id")
	})

	t.Run("T2_GatewayLineSurvivesRestart", func(t *testing.T) {
		owner := CreateTestUser(t)
		claimToken := "id-tok-T2-" + generateId("c")
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusProvisioning, Region: "hongkong",
			IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
			ProvisionClaimToken: claimToken,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		ip := "10.99.22.2"
		domain := "t2-" + generateId("d") + ".sslip.io"
		simulateUnregister(t, ip)
		t.Cleanup(func() {
			var n SlaveNode
			if err := db.Get().Where("ipv4 = ?", ip).First(&n).Error; err == nil {
				db.Get().Unscoped().Where("node_id = ?", n.ID).Delete(&SlaveTunnel{})
			}
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Where("domain = ?", domain).Delete(&SlaveTunnel{})
			db.Get().Unscoped().Delete(&sub)
		})

		// attachTunnel inserts a k2v5 tunnel directly (sidesteps GetDomainCert, which
		// needs a CA password unavailable in the test env — the sidecar registers it
		// for real; here we only exercise binding + gateway resolution).
		attachTunnel := func(nodeID uint64) {
			db.Get().Unscoped().Where("domain = ?", domain).Delete(&SlaveTunnel{})
			require.NoError(t, db.Get().Create(&SlaveTunnel{
				NodeID: nodeID, Domain: domain, Protocol: TunnelProtocolK2V5,
				Port: 443, ServerURL: "k2v5://" + domain + ":443",
				IsTest: BoolPtr(false), HasTunnel: BoolPtr(true), SecretToken: "tun-T2",
			}).Error)
		}

		// claim + attach a k2v5 tunnel
		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "T2", SecretToken: "secret-T2", PrivateClaim: claimToken,
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "first register: %s", resp.Message)
		var n1 SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&n1).Error)
		attachTunnel(n1.ID)

		// restart cycle: unregister (wipes node+tunnel) → re-register → re-attach tunnel on new id
		simulateUnregister(t, ip)
		resp = driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "HK", Name: "T2", SecretToken: "secret-T2", PrivateClaim: claimToken,
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "re-register: %s", resp.Message)
		var n2 SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&n2).Error)
		attachTunnel(n2.ID)

		// Gateway resolution must still find the owner's line.
		out, err := ResolveGatewayPrivateTunnels(context.Background(), owner.ID, time.Now().Unix())
		require.NoError(t, err)
		require.Len(t, out, 1, "router private line vanished after restart")
	})

	t.Run("T3_ClaimWithoutSubIsPrivateUnowned", func(t *testing.T) {
		ip := "10.99.22.3"
		simulateUnregister(t, ip)
		t.Cleanup(func() { db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{}) })

		// Token that matches no claimable sub → must NOT fall into shared.
		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "US", Name: "T3", SecretToken: "secret-T3",
			PrivateClaim: "id-tok-T3-nomatch-" + generateId("c"),
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "register: %s", resp.Message)

		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&node).Error)
		require.Equal(t, NodeClassPrivate, node.Class,
			"claim-carrying node must never be shared (fail-safe)")
		require.Nil(t, node.PrivateSubID, "no claimable sub → unowned")
		require.Nil(t, node.PrivateOwnerUserID)
	})

	t.Run("T4_SharedNodeNotHijackedByStaleAnchor", func(t *testing.T) {
		owner := CreateTestUser(t)
		ip := "10.99.22.4"
		// A stale active sub anchored to this IP (e.g. IP was recycled).
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusActive, Region: "hongkong",
			IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
			BoundIpv4: ip,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		simulateUnregister(t, ip)
		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", ip).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
		})

		// Plain shared node (NO claim token) on the recycled IP must stay shared.
		resp := driveUpsert(t, ip, SlaveNodeUpsertRequest{
			Country: "US", Name: "T4", SecretToken: "secret-T4",
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "register: %s", resp.Message)

		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", ip).First(&node).Error)
		require.NotEqual(t, NodeClassPrivate, node.Class,
			"shared node (no token) must not inherit a stale anchor")
		require.Nil(t, node.PrivateSubID)
	})

	t.Run("T5_AntiHijackTokenAtWrongIP", func(t *testing.T) {
		owner := CreateTestUser(t)
		boundIP := "10.99.22.55"
		// Active sub bound to boundIP (token already consumed).
		sub := PrivateNodeSubscription{
			UserID: owner.ID, Status: PNStatusActive, Region: "hongkong",
			IPType: IPTypeNonResidential, PurchasedAt: now, ExpiresAt: now + 86400,
			BoundIpv4: boundIP,
		}
		require.NoError(t, db.Get().Create(&sub).Error)

		attackerIP := "10.99.22.5"
		simulateUnregister(t, attackerIP)
		t.Cleanup(func() {
			db.Get().Unscoped().Where("ipv4 = ?", attackerIP).Delete(&SlaveNode{})
			db.Get().Unscoped().Delete(&sub)
		})

		// Register at a DIFFERENT IP carrying some token → must not bind to the boundIP sub.
		resp := driveUpsert(t, attackerIP, SlaveNodeUpsertRequest{
			Country: "US", Name: "T5", SecretToken: "secret-T5",
			PrivateClaim: "id-tok-T5-stolen-" + generateId("c"),
		})
		require.EqualValues(t, ErrorNone, ErrorCode(resp.Code), "register: %s", resp.Message)

		var node SlaveNode
		require.NoError(t, db.Get().Where("ipv4 = ?", attackerIP).First(&node).Error)
		require.Nil(t, node.PrivateSubID, "must not hijack a sub bound to another IP")

		// The victim sub's binding is untouched.
		var reloaded PrivateNodeSubscription
		require.NoError(t, db.Get().First(&reloaded, sub.ID).Error)
		require.Nil(t, reloaded.SlaveNodeID, "victim sub binding must be untouched")
	})
}
