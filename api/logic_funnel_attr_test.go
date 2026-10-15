package center

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===================== 出口键 =====================

func TestFunnelAttrIPKey(t *testing.T) {
	same := func(a, b string) {
		t.Helper()
		ka, kb := funnelAttrIPKey(a), funnelAttrIPKey(b)
		require.NotEmpty(t, ka, a)
		assert.Equal(t, ka, kb, "%s vs %s", a, b)
	}
	diff := func(a, b string) {
		t.Helper()
		ka, kb := funnelAttrIPKey(a), funnelAttrIPKey(b)
		require.NotEmpty(t, ka, a)
		require.NotEmpty(t, kb, b)
		assert.NotEqual(t, ka, kb, "%s vs %s", a, b)
	}
	same("203.0.113.7", " 203.0.113.7 ")
	same("203.0.113.7", "::ffff:203.0.113.7") // v4-mapped 归到 v4
	diff("203.0.113.7", "203.0.113.8")        // IPv4 按全地址
	same("2001:db8:1:2::1", "2001:db8:1:2:abcd:ef01:2345:6789")
	diff("2001:db8:1:2::1", "2001:db8:1:3::1") // 不同 /64
	diff("203.0.113.7", "2001:db8:cb00:7107::1")
	for _, bad := range []string{"", "nonsense", "127.0.0.1", "10.1.2.3", "192.168.1.1", "172.16.0.9", "::1", "fe80::1", "fd00::1", "0.0.0.0", "::"} {
		assert.Empty(t, funnelAttrIPKey(bad), bad)
	}
}

func TestFunnelAttrOpenOnTime(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	assert.True(t, funnelAttrOpenOnTime(now, now))
	assert.True(t, funnelAttrOpenOnTime(now.Add(-funnelAttrMaxOpenDelay), now))
	assert.False(t, funnelAttrOpenOnTime(now.Add(-funnelAttrMaxOpenDelay-time.Second), now))
	assert.True(t, funnelAttrOpenOnTime(now.Add(funnelAttrMaxOpenDelay), now))
	assert.False(t, funnelAttrOpenOnTime(now.Add(funnelAttrMaxOpenDelay+time.Second), now))
	assert.False(t, funnelAttrOpenOnTime(time.Time{}, now), "缺 created_at 视为迟到")
}

// ===================== 匹配：随机 fixture + oracle =====================

func faStore(t *testing.T) (funnelAttrStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return funnelAttrStore{rdb: goredis.NewClient(&goredis.Options{Addr: mr.Addr()})}, mr
}

// faNet 是 fixture 里的「出口」：oracle 只按 id 判等，不调用 funnelAttrIPKey。
// id < 0 表示无效出口（私网 / 回环 / 空），永远不参与匹配。
type faNet struct {
	id   int
	addr func(r *rand.Rand) string
}

func faNets() []faNet {
	var nets []faNet
	for i := 0; i < 5; i++ { // IPv4：全地址即出口；一半时候用 v4-mapped 形式上报
		i := i
		nets = append(nets, faNet{id: len(nets), addr: func(r *rand.Rand) string {
			if r.Intn(2) == 0 {
				return fmt.Sprintf("::ffff:203.0.113.%d", 10+i)
			}
			return fmt.Sprintf("203.0.113.%d", 10+i)
		}})
	}
	for i := 0; i < 4; i++ { // IPv6：同 /64、接口标识随机（临时地址轮换）
		i := i
		nets = append(nets, faNet{id: len(nets), addr: func(r *rand.Rand) string {
			return fmt.Sprintf("2001:db8:%x:%x:%x:%x:%x:%x", 0xa0+i, i, r.Intn(0xffff), r.Intn(0xffff), r.Intn(0xffff), r.Intn(0xffff))
		}})
	}
	for _, bad := range []string{"10.0.0.5", "127.0.0.1", "", "fe80::5"} {
		bad := bad
		nets = append(nets, faNet{id: -1, addr: func(*rand.Rand) string { return bad }})
	}
	return nets
}

type faEvent struct {
	click     bool
	at        time.Time // 服务端收到的时刻（秒）
	brand     Brand
	platform  string
	net       faNet
	ip        string
	sid       string // click；"" = GPC
	did       string // open
	createdAt time.Time
}

type faExpect struct {
	outcome string
	sid     string
	clickAt int64
}

func faGenerate(r *rand.Rand) []faEvent {
	nets := faNets()
	brands := []Brand{BrandKaitu, BrandOverleap}
	plats := []string{"windows", "ios", "android"}
	// 跨两个 UTC 零点，覆盖「昨天的盐」。
	base := time.Date(2026, 3, 10, 18, 0, 0, 0, time.UTC)
	span := 60 * 3600
	n := 40 + r.Intn(80)
	evs := make([]faEvent, 0, n)
	for i := 0; i < n; i++ {
		e := faEvent{
			click:    r.Intn(2) == 0,
			at:       base.Add(time.Duration(r.Intn(span)) * time.Second),
			brand:    brands[r.Intn(len(brands))],
			platform: plats[r.Intn(len(plats))],
			net:      nets[r.Intn(len(nets))],
		}
		if r.Intn(4) == 0 { // 把时间压进少数几个点，制造同一时刻 / 窗口边界
			e.at = base.Add(time.Duration(r.Intn(4)) * funnelAttrWindow)
		}
		e.ip = e.net.addr(r)
		if e.click {
			if r.Intn(15) == 0 {
				e.platform = "bogus"
			}
			if r.Intn(8) != 0 {
				e.sid = "s" + strconv.Itoa(r.Intn(8))
			}
		} else {
			e.did = fmt.Sprintf("d%03d", i)
			onTime := []time.Duration{0, 30 * time.Second, funnelAttrMaxOpenDelay, -3 * time.Minute, -funnelAttrMaxOpenDelay}
			late := []time.Duration{funnelAttrMaxOpenDelay + time.Second, 20 * time.Minute, -funnelAttrMaxOpenDelay - time.Second}
			switch k := r.Intn(10); {
			case k < 7:
				e.createdAt = e.at.Add(-onTime[r.Intn(len(onTime))])
			case k < 9:
				e.createdAt = e.at.Add(-late[r.Intn(len(late))])
			}
		}
		evs = append(evs, e)
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].at.Before(evs[j].at) })
	return evs
}

// faOracle：暴力法逐条计算每次首开的期望结果，只看 fixture 的语义字段。
func faOracle(evs []faEvent) map[string]faExpect {
	out := map[string]faExpect{}
	validPlat := map[string]bool{"windows": true, "ios": true, "android": true, "macos": true, "linux": true}
	for i, o := range evs {
		if o.click {
			continue
		}
		d := o.at.Sub(o.createdAt)
		if o.createdAt.IsZero() || d > funnelAttrMaxOpenDelay || d < -funnelAttrMaxOpenDelay {
			out[o.did] = faExpect{outcome: funnelAttrLate}
			continue
		}
		if o.net.id < 0 {
			out[o.did] = faExpect{outcome: funnelAttrNoIP}
			continue
		}
		latest := map[string]int64{}
		for _, c := range evs[:i] { // 只有先于这次首开处理的点击
			if !c.click || c.sid == "" || !validPlat[c.platform] || c.net.id < 0 {
				continue
			}
			if c.brand != o.brand || c.platform != o.platform || c.net.id != o.net.id {
				continue
			}
			if c.at.Unix() < o.at.Add(-funnelAttrWindow).Unix() || c.at.Unix() > o.at.Unix() {
				continue
			}
			if c.at.Unix() > latest[c.sid] {
				latest[c.sid] = c.at.Unix()
			}
		}
		switch len(latest) {
		case 0:
			out[o.did] = faExpect{outcome: funnelAttrNone}
		case 1:
			for s, at := range latest {
				out[o.did] = faExpect{outcome: funnelAttrMatched, sid: s, clickAt: at}
			}
		default:
			out[o.did] = faExpect{outcome: funnelAttrAmbiguous}
		}
	}
	return out
}

func faRun(t *testing.T, s funnelAttrStore, evs []faEvent) map[string]faExpect {
	t.Helper()
	ctx := context.Background()
	got := map[string]faExpect{}
	for _, e := range evs {
		if e.click {
			require.NoError(t, s.recordClick(ctx, e.brand, e.platform, e.ip, e.sid, e.at))
			continue
		}
		link, outcome, err := s.onFirstOpen(ctx, e.brand, e.platform, e.ip, e.did, e.createdAt, e.at)
		require.NoError(t, err)
		g := faExpect{outcome: outcome}
		if link != nil {
			require.Equal(t, funnelAttrMatched, outcome)
			require.Equal(t, e.did, link.DID)
			require.Equal(t, string(e.brand), link.Brand)
			require.Equal(t, e.platform, link.Platform)
			require.Equal(t, funnelAttrMethod, link.Method)
			g.sid, g.clickAt = link.SID, link.ClickAt.Unix()
		} else {
			require.NotEqual(t, funnelAttrMatched, outcome)
		}
		got[e.did] = g
	}
	return got
}

func TestFunnelAttrOracle_RandomFixtures(t *testing.T) {
	seen := map[string]int{}
	for seed := int64(1); seed <= 300; seed++ {
		r := rand.New(rand.NewSource(seed))
		evs := faGenerate(r)
		s, mr := faStore(t)
		want := faOracle(evs)
		got := faRun(t, s, evs)
		require.Equal(t, want, got, "seed %d", seed)

		// 计数器与 oracle 一致（按品牌 × UTC 日）。
		wantCounts := map[string]int{}
		for _, e := range evs {
			if !e.click {
				wantCounts[string(e.brand)+":"+funnelAttrDay(e.at)+":"+want[e.did].outcome]++
				seen[want[e.did].outcome]++
			}
		}
		gotCounts := map[string]int{}
		for _, k := range mr.Keys() {
			if len(k) < 12 || k[:12] != "fattr:stats:" {
				continue
			}
			fields, err := mr.HKeys(k)
			require.NoError(t, err)
			for _, f := range fields {
				n, _ := strconv.Atoi(mr.HGet(k, f))
				gotCounts[k[12:]+":"+f] += n
			}
		}
		require.Equal(t, wantCounts, gotCounts, "seed %d counters", seed)
		mr.Close()
	}
	// fixture 要真的走到每一种结果，否则 oracle 比对是空转。
	for _, o := range funnelAttrOutcomes {
		assert.Greater(t, seen[o], 20, "outcome %s rarely reached: %v", o, seen)
	}
}

// 原始 IP 与 sid 之外的任何东西都不该落进 Redis 的键名；键名里不能出现 IP。
func TestFunnelAttr_NoRawIPInRedis(t *testing.T) {
	s, mr := faStore(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	ip := "203.0.113.77"
	require.NoError(t, s.recordClick(context.Background(), BrandKaitu, "ios", ip, "sidX", now))
	_, _, err := s.onFirstOpen(context.Background(), BrandKaitu, "ios", ip, "didX", now, now)
	require.NoError(t, err)
	for _, k := range mr.Keys() {
		assert.NotContains(t, k, ip)
		assert.NotContains(t, k, "203.0.113")
		if typ := mr.Type(k); typ == "zset" {
			members, _ := mr.ZMembers(k)
			for _, m := range members {
				assert.NotContains(t, m, "203.0.113")
			}
		}
	}
	// 盐与点击都有 TTL，不会永久留存。
	for _, k := range mr.Keys() {
		assert.Greater(t, mr.TTL(k), time.Duration(0), "key %s must expire", k)
	}
}

// 盐每天独立：同一出口在两天的哈希不同（盐过期即无法关联回去）。
func TestFunnelAttr_SaltRotatesDaily(t *testing.T) {
	s, mr := faStore(t)
	d1 := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	require.NoError(t, s.recordClick(ctx, BrandKaitu, "ios", "203.0.113.5", "a", d1))
	require.NoError(t, s.recordClick(ctx, BrandKaitu, "ios", "203.0.113.5", "a", d1.Add(funnelDay)))
	var clickKeys []string
	for _, k := range mr.Keys() {
		if len(k) > 8 && k[:8] == "fattr:c:" {
			clickKeys = append(clickKeys, k)
		}
	}
	assert.Len(t, clickKeys, 2)
	s1, _ := mr.Get("fattr:salt:" + funnelAttrDay(d1))
	s2, _ := mr.Get("fattr:salt:" + funnelAttrDay(d1.Add(funnelDay)))
	assert.NotEmpty(t, s1)
	assert.NotEqual(t, s1, s2)
}

// 共享出口下每个哈希最多保留 funnelAttrMaxPerKey 个 sid（丢最旧的），而且照样判 ambiguous。
func TestFunnelAttr_PerKeyCap(t *testing.T) {
	s, mr := faStore(t)
	ctx := context.Background()
	base := time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC)
	for i := 0; i < funnelAttrMaxPerKey+10; i++ {
		require.NoError(t, s.recordClick(ctx, BrandKaitu, "android", "203.0.113.9", fmt.Sprintf("s%02d", i), base.Add(time.Duration(i)*time.Second)))
	}
	for _, k := range mr.Keys() {
		if len(k) > 8 && k[:8] == "fattr:c:" {
			members, err := mr.ZMembers(k)
			require.NoError(t, err)
			assert.Len(t, members, funnelAttrMaxPerKey)
			assert.NotContains(t, members, "s00")
		}
	}
	_, outcome, err := s.onFirstOpen(ctx, BrandKaitu, "android", "203.0.113.9", "d", base.Add(time.Hour), base.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, funnelAttrAmbiguous, outcome)
}

// ===================== 合并：随机 fixture + 并查集 oracle =====================

// faUnion 是 oracle：节点 = 用户 / did / sid；确定性身份是硬边；推断关联按 did 升序逐条尝试合并，
// 合并会让一个分量含两个不同用户时拒绝（记冲突）。与实现的分情况写法是独立表述。
type faUnion struct{ parent map[string]string }

func (u *faUnion) find(x string) string {
	if u.parent[x] == "" {
		u.parent[x] = x
	}
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *faUnion) users(x string, all []string) map[string]bool {
	out := map[string]bool{}
	root := u.find(x)
	for _, n := range all {
		if n[0] == 'u' && u.find(n) == root {
			out[n] = true
		}
	}
	return out
}

func TestApplyInferredLinksOracle_RandomFixtures(t *testing.T) {
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var totConfirmed, totConflicts, totApplied int
	defer func() {
		// fixture 要真的走到三种情况，否则比对是空转。
		assert.Greater(t, totConfirmed, 30)
		assert.Greater(t, totConflicts, 30)
		assert.Greater(t, totApplied, 300)
	}()
	for seed := int64(1); seed <= 500; seed++ {
		r := rand.New(rand.NewSource(seed))
		nU, nD, nS := 1+r.Intn(4), 1+r.Intn(8), 1+r.Intn(6)
		identities := map[string]uint64{}
		for i := 0; i < nD; i++ {
			if r.Intn(3) == 0 {
				identities["did:d"+strconv.Itoa(i)] = uint64(1 + r.Intn(nU))
			}
		}
		for i := 0; i < nS; i++ {
			if r.Intn(3) == 0 {
				identities["sid:s"+strconv.Itoa(i)] = uint64(1 + r.Intn(nU))
			}
		}
		links := map[string]string{}
		for i := 0; i < nD; i++ {
			if r.Intn(2) == 0 {
				links["d"+strconv.Itoa(i)] = "s" + strconv.Itoa(r.Intn(nS))
			}
		}
		var recs []funnelRecord
		for i := 0; i < 10+r.Intn(30); i++ {
			rec := funnelRecord{At: base.Add(time.Duration(i) * time.Minute), Event: "page_view"}
			switch r.Intn(5) {
			case 0:
				rec.UserID = uint64(1 + r.Intn(nU))
			case 1:
				rec.AnonKind, rec.AnonID = "did", "d"+strconv.Itoa(r.Intn(nD))
				if r.Intn(4) == 0 {
					rec.UserID = uint64(1 + r.Intn(nU))
				}
			case 2, 3:
				rec.AnonKind, rec.AnonID = "sid", "s"+strconv.Itoa(r.Intn(nS))
			default:
				// GPC：没有任何身份
			}
			recs = append(recs, rec)
		}
		recsCopy := append([]funnelRecord(nil), recs...)
		idsCopy := map[string]uint64{}
		for k, v := range identities {
			idsCopy[k] = v
		}

		outRecs, outIDs, sum := applyFunnelInferredLinks(recs, identities, links)
		require.Equal(t, recsCopy, recs, "seed %d: input recs mutated", seed)
		require.Equal(t, idsCopy, identities, "seed %d: input identities mutated", seed)

		// oracle
		u := &faUnion{parent: map[string]string{}}
		var all []string
		for i := 1; i <= nU; i++ {
			all = append(all, "u"+strconv.Itoa(i))
		}
		for i := 0; i < nD; i++ {
			all = append(all, "did:d"+strconv.Itoa(i))
		}
		for i := 0; i < nS; i++ {
			all = append(all, "sid:s"+strconv.Itoa(i))
		}
		for k, v := range identities {
			u.parent[u.find(k)] = u.find("u" + strconv.FormatUint(v, 10))
		}
		dids := make([]string, 0, len(links))
		for d := range links {
			dids = append(dids, d)
		}
		sort.Strings(dids)
		var wantConfirmed, wantConflicts, wantApplied int
		for _, d := range dids {
			dk, sk := "did:"+d, "sid:"+links[d]
			if identities[dk] != 0 && identities[dk] == identities[sk] {
				wantConfirmed++
				continue
			}
			ud, us := u.users(dk, all), u.users(sk, all)
			merged := map[string]bool{}
			for k := range ud {
				merged[k] = true
			}
			for k := range us {
				merged[k] = true
			}
			if len(merged) > 1 {
				wantConflicts++
				continue
			}
			u.parent[u.find(dk)] = u.find(sk)
			wantApplied++
		}
		assert.Equal(t, wantConfirmed, sum.Confirmed, "seed %d confirmed", seed)
		assert.Equal(t, wantConflicts, sum.Conflicts, "seed %d conflicts", seed)
		assert.Equal(t, wantApplied, sum.Applied, "seed %d applied", seed)
		assert.Equal(t, len(links), sum.Links)
		totConfirmed, totConflicts, totApplied = totConfirmed+sum.Confirmed, totConflicts+sum.Conflicts, totApplied+sum.Applied

		// 每条记录的 oracle 节点：有 UserID 就是该用户，否则是它的匿名身份。
		node := func(rec funnelRecord) (string, bool) {
			if rec.UserID != 0 {
				return "u" + strconv.FormatUint(rec.UserID, 10), true
			}
			if rec.AnonID == "" {
				return "", false
			}
			return rec.AnonKind + ":" + rec.AnonID, true
		}
		for i := range recs {
			ni, oki := node(recs[i])
			ki, okI := funnelPersonKey(&outRecs[i], outIDs)
			require.Equal(t, oki, okI, "seed %d rec %d participation", seed, i)
			if !oki {
				continue
			}
			for j := i + 1; j < len(recs); j++ {
				nj, okj := node(recs[j])
				kj, _ := funnelPersonKey(&outRecs[j], outIDs)
				if !okj {
					continue
				}
				require.Equal(t, u.find(ni) == u.find(nj), ki == kj,
					"seed %d: rec %d (%s→%s) vs rec %d (%s→%s)", seed, i, ni, ki, j, nj, kj)
			}
		}
	}
}

func TestApplyInferredLinks_NoLinksIsIdentity(t *testing.T) {
	recs := []funnelRecord{{AnonKind: "did", AnonID: "d"}, {AnonKind: "sid", AnonID: "s"}}
	ids := map[string]uint64{"sid:s": 7}
	out, outIDs, sum := applyFunnelInferredLinks(recs, ids, nil)
	assert.Equal(t, reflect.ValueOf(recs).Pointer(), reflect.ValueOf(out).Pointer(), "no rewrite → same slice")
	assert.Equal(t, ids, outIDs)
	assert.Equal(t, FunnelInferredSummary{Method: funnelAttrMethod}, sum)
}

// ===================== 端到端：指南读者 → 下载 → App 首开 → 登录 → 付款 =====================

func TestCompute_InferredLinkBridgesWebToApp(t *testing.T) {
	t0 := time.Date(2026, 3, 5, 8, 0, 0, 0, time.UTC)
	const uid = 42
	recs := []funnelRecord{
		{At: t0, Event: "page_view", Surface: FunnelSurfaceWeb, AnonKind: "sid", AnonID: "S", Path: "/guides/register-us-apple-id"},
		{At: t0.Add(2 * time.Minute), Event: "install_click", Surface: FunnelSurfaceWeb, AnonKind: "sid", AnonID: "S"},
		{At: t0.Add(10 * time.Minute), Event: "app_first_open", Surface: FunnelSurfaceApp, AnonKind: "did", AnonID: "D"},
		{At: t0.Add(12 * time.Minute), Event: "auth_done", Surface: FunnelSurfaceApp, AnonKind: "did", AnonID: "D", UserID: uid},
		{At: t0.Add(time.Hour), Event: funnelFactPurchase, UserID: uid},
	}
	ids := map[string]uint64{"did:D": uid} // App 登录时写下的确定性身份
	from, to := t0.Add(-time.Hour), t0.Add(24*time.Hour)

	for _, tc := range []struct {
		key           string
		without, with []int
	}{
		{"guide_conversion", []int{1, 0, 0}, []int{1, 1, 1}},
		{"web_to_app", []int{1, 0, 0, 0}, []int{1, 1, 1, 1}},
	} {
		p := fnMustPath(t, tc.key)
		assert.Equal(t, tc.without, fnStepCounts(computeFunnel(p, recs, ids, from, to, "")), "%s without inferred", tc.key)
		r2, ids2, sum := applyFunnelInferredLinks(recs, ids, map[string]string{"D": "S"})
		assert.Equal(t, tc.with, fnStepCounts(computeFunnel(p, r2, ids2, from, to, "")), "%s with inferred", tc.key)
		assert.Equal(t, 1, sum.Applied)
	}

	// 冲突：网页 sid 已确定性属于另一个用户 → 推断被忽略，结果与不带推断相同。
	ids3 := map[string]uint64{"did:D": uid, "sid:S": 99}
	r3, ids3b, sum := applyFunnelInferredLinks(recs, ids3, map[string]string{"D": "S"})
	assert.Equal(t, 1, sum.Conflicts)
	p := fnMustPath(t, "web_to_app")
	assert.Equal(t, fnStepCounts(computeFunnel(p, recs, ids3, from, to, "")), fnStepCounts(computeFunnel(p, r3, ids3b, from, to, "")))
}

// GPC / optout 访客没有 sid：store 层也不写任何点击（不只靠 px 入口那道门）。
func TestFunnelAttr_EmptySidWritesNothing(t *testing.T) {
	s, mr := faStore(t)
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.recordClick(context.Background(), BrandKaitu, "ios", "203.0.113.5", "", now))
	require.NoError(t, s.recordClick(context.Background(), BrandKaitu, "bogus", "203.0.113.5", "sid", now))
	require.NoError(t, s.recordClick(context.Background(), BrandKaitu, "ios", "10.0.0.1", "sid", now))
	for _, k := range mr.Keys() {
		assert.False(t, strings.HasPrefix(k, "fattr:c:"), "unexpected click key %s", k)
	}
}
