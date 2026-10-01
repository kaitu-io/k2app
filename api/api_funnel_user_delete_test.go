package center

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// fudSeed：受害用户 + 对照用户，各有一条身份关联和一条带 user_id 的事件。
func fudSeed(t *testing.T) (victim, control *User, marker string) {
	t.Helper()
	marker = generateId("fud")
	mk := func() *User {
		u := &User{UUID: generateId("test-fud"), Language: "zh-CN"}
		require.NoError(t, db.Get().Create(u).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&User{}, u.ID) })
		return u
	}
	victim, control = mk(), mk()
	t.Cleanup(func() {
		factsCleanup(t, db.Get().Where("plan = ?", marker).Delete(&FunnelEvent{}).Error)
		factsCleanup(t, db.Get().Where("user_id IN ?", []uint64{victim.ID, control.ID}).Delete(&FunnelIdentity{}).Error)
	})
	for _, u := range []*User{victim, control} {
		require.NoError(t, db.Get().Create(&FunnelIdentity{
			Kind: funnelAnonKindSid, AnonID: generateId("sid"), UserID: u.ID, Brand: string(BrandKaitu),
		}).Error)
		require.NoError(t, db.Get().Create(&FunnelIdentity{
			Kind: funnelAnonKindDid, AnonID: generateId("did"), UserID: u.ID, Brand: string(BrandKaitu),
		}).Error)
		for _, surface := range []string{FunnelSurfaceWeb, FunnelSurfaceApp} {
			require.NoError(t, db.Get().Create(&FunnelEvent{
				OccurredAt: time.Now(), Brand: string(BrandKaitu), Surface: surface,
				Event: "checkout_start", UserID: u.ID, Plan: marker,
			}).Error)
		}
	}
	return victim, control, marker
}

func fudCounts(t *testing.T, userID uint64) (identities, events int64) {
	t.Helper()
	require.NoError(t, db.Get().Model(&FunnelIdentity{}).Where("user_id = ?", userID).Count(&identities).Error)
	require.NoError(t, db.Get().Model(&FunnelEvent{}).Where("user_id = ?", userID).Count(&events).Error)
	return
}

// 硬删用户：该用户的身份关联与事件一并删除，别的用户不受影响。
func TestHardDeleteUser_RemovesFunnelRows(t *testing.T) {
	skipIfNoDB(t)
	victim, control, _ := fudSeed(t)
	ids, evs := fudCounts(t, victim.ID)
	require.Equal(t, int64(2), ids)
	require.Equal(t, int64(2), evs)

	params, err := json.Marshal(HardDeleteUsersRequest{UserUUIDs: []string{victim.UUID}})
	require.NoError(t, err)
	require.NoError(t, executeApprovalUserHardDelete(context.Background(), params))

	ids, evs = fudCounts(t, victim.ID)
	assert.Equal(t, int64(0), ids, "funnel_identities of a hard-deleted user must be removed")
	assert.Equal(t, int64(0), evs, "funnel_events of a hard-deleted user must be removed")
	ids, evs = fudCounts(t, control.ID)
	assert.Equal(t, int64(2), ids, "other users' identities untouched")
	assert.Equal(t, int64(2), evs, "other users' events untouched")
}

// 自助注销：同样删除该用户的身份关联与事件。
func TestDeleteUserAccount_RemovesFunnelRows(t *testing.T) {
	skipIfNoDB(t)
	victim, control, _ := fudSeed(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/user/delete-account", nil)
	setupDeleteAccountRouter(t, victim).ServeHTTP(w, req)
	resp, err := ParseResponse(w)
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code, resp.Message)

	ids, evs := fudCounts(t, victim.ID)
	assert.Equal(t, int64(0), ids, "funnel_identities must be removed on account deletion")
	assert.Equal(t, int64(0), evs, "funnel_events must be removed on account deletion")
	ids, evs = fudCounts(t, control.ID)
	assert.Equal(t, int64(2), ids)
	assert.Equal(t, int64(2), evs)
}
