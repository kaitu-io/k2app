package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/db"
)

// trialRecallSeed 描述一条被测 user + Apple subscription。时间字段都是 unix 秒。
type trialRecallSeed struct {
	expiredAt  int64
	isTrial    bool
	autoRenew  bool
	status     string
	env        string
	brand      string
	firstOrder bool
}

func trialRecallDefaults() trialRecallSeed {
	return trialRecallSeed{status: "active", env: "Production", brand: "kaitu", isTrial: true}
}

var trialRecallSeq int

// seedTrialRecallUser 建 user 和每个 trialEnds 一条 subscription 链；cleanup 自动注册。
func seedTrialRecallUser(t *testing.T, s trialRecallSeed, trialEnds ...int64) (User, []Subscription) {
	t.Helper()
	trialRecallSeq++
	uniq := fmt.Sprintf("%d-%d", time.Now().UnixNano(), trialRecallSeq)
	u := User{UUID: "usr-tr-" + uniq, ExpiredAt: s.expiredAt, IsFirstOrderDone: BoolPtr(s.firstOrder), Brand: s.brand}
	require.NoError(t, db.Get().Create(&u).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&u) })
	var subs []Subscription
	for i, te := range trialEnds {
		sub := Subscription{
			UserID: u.ID, Provider: SubscriptionProviderApple,
			ProviderSubscriptionID: fmt.Sprintf("TR-%s-%d", uniq, i),
			ProductID:              "io.kaitu.sub.basic.1y",
			Environment:            s.env, Status: s.status, AutoRenew: s.autoRenew,
			IsTrial: s.isTrial, TrialEndsAt: te,
		}
		require.NoError(t, db.Get().Create(&sub).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(&sub) })
		subs = append(subs, sub)
	}
	return u, subs
}

func trialRecallIDs(t *testing.T, stage trialRecallStage, now time.Time) map[uint64]trialRecallCandidate {
	t.Helper()
	cs, err := trialRecallCandidates(context.Background(), stage, now)
	require.NoError(t, err)
	m := map[uint64]trialRecallCandidate{}
	for _, c := range cs {
		m[c.User.ID] = c
	}
	return m
}

func TestTrialRecallCandidates_S1_SelectsTrialWhoseExpiryEqualsTrialEnd(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = te
	u, subs := seedTrialRecallUser(t, s, te)
	got := trialRecallIDs(t, trialRecallS1, now)
	require.Contains(t, got, u.ID)
	assert.Equal(t, subs[0].ID, got[u.ID].SubscriptionID)
	assert.Equal(t, te, got[u.ID].TrialEndsAt)
}

func TestTrialRecallCandidates_S1_SkipsAutoRenewOn(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.autoRenew = te, true
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS1, now), u.ID)
}

func TestTrialRecallCandidates_S1_SkipsMembershipBeyondTrialByMoreThanADay(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = te + 2*86400
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS1, now), u.ID)
}

func TestTrialRecallCandidates_S2_SelectsStaleActiveStatus(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.status, s.isTrial = te, "active", false
	u, _ := seedTrialRecallUser(t, s, te)
	assert.Contains(t, trialRecallIDs(t, trialRecallS2, now), u.ID)
}

func TestTrialRecallCandidates_S2_SkipsBillingRetry(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.status = te, "billing_retry"
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u.ID)
}

func TestTrialRecallCandidates_S2_SkipsActiveMembership(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = now.Add(5 * 24 * time.Hour).Unix()
	u, _ := seedTrialRecallUser(t, s, te)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u.ID)
}

func TestTrialRecallCandidates_SkipsPaidBefore(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te1 := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.firstOrder = te1, true
	u1, _ := seedTrialRecallUser(t, s, te1)
	te2 := now.Add(-3 * 24 * time.Hour).Unix()
	s.expiredAt = te2
	u2, _ := seedTrialRecallUser(t, s, te2)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS1, now), u1.ID)
	assert.NotContains(t, trialRecallIDs(t, trialRecallS2, now), u2.ID)
}

func TestTrialRecallCandidates_SkipsOverleapAndSandbox(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	te := now.Add(24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt, s.brand = te, "overleap"
	ov, _ := seedTrialRecallUser(t, s, te)
	s.brand, s.env = "kaitu", "Sandbox"
	sb, _ := seedTrialRecallUser(t, s, te)
	got := trialRecallIDs(t, trialRecallS1, now)
	assert.NotContains(t, got, ov.ID)
	assert.NotContains(t, got, sb.ID)
}

func TestTrialRecallCandidates_TwoChainsOneUser(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	older := now.Add(-5 * 24 * time.Hour).Unix()
	newer := now.Add(-3 * 24 * time.Hour).Unix()
	s := trialRecallDefaults()
	s.expiredAt = newer
	u, subs := seedTrialRecallUser(t, s, older, newer)
	cs, err := trialRecallCandidates(context.Background(), trialRecallS2, now)
	require.NoError(t, err)
	n := 0
	for _, c := range cs {
		if c.User.ID == u.ID {
			n++
			assert.Equal(t, newer, c.TrialEndsAt)
			assert.Equal(t, subs[1].ID, c.SubscriptionID)
		}
	}
	assert.Equal(t, 1, n)
}

func TestTrialRecallCandidates_WindowEdges(t *testing.T) {
	skipIfNoConfig(t)
	now := time.Now()
	s := trialRecallDefaults()
	mk := func(te int64) User {
		s.expiredAt = te
		u, _ := seedTrialRecallUser(t, s, te)
		return u
	}
	in47 := mk(now.Add(47 * time.Hour).Unix())
	out49 := mk(now.Add(49 * time.Hour).Unix())
	s1 := trialRecallIDs(t, trialRecallS1, now)
	assert.Contains(t, s1, in47.ID)
	assert.NotContains(t, s1, out49.ID)

	in := mk(now.Add(-24*time.Hour - time.Second).Unix())
	out := mk(now.Add(-10*24*time.Hour - time.Second).Unix())
	s2 := trialRecallIDs(t, trialRecallS2, now)
	assert.Contains(t, s2, in.ID)
	assert.NotContains(t, s2, out.ID)
}

func TestInTrialRecallHoldout_StableAndAboutTenPercent(t *testing.T) {
	hits := 0
	for id := uint64(1); id <= 10000; id++ {
		a := inTrialRecallHoldout(id)
		require.Equal(t, a, inTrialRecallHoldout(id))
		if a {
			hits++
		}
	}
	assert.GreaterOrEqual(t, hits, 800)
	assert.LessOrEqual(t, hits, 1200)
}

func TestTrialRecallDedupKey(t *testing.T) {
	assert.Equal(t, "trial-recall:s2:42", trialRecallDedupKey(trialRecallS2, 42))
	assert.Equal(t, "trial-recall:s1:123", trialRecallDedupKey(trialRecallS1, 123))
}
