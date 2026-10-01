package center

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// chatTestValues 返回 n 个唯一标识值，并在测试结束时清掉这些值牵出的 guest 及其簇、合并记录。
func chatTestValues(t *testing.T, n int) []string {
	t.Helper()
	require.NoError(t, Migrate())
	vals := make([]string, n)
	for i := range vals {
		vals[i] = generateId("idt")
	}
	t.Cleanup(func() {
		d := db.Get()
		var ids []uint64
		d.Model(&GuestIdentity{}).Where("value IN ?", vals).Distinct().Pluck("guest_id", &ids)
		if len(ids) == 0 {
			return
		}
		var cluster []uint64
		d.Model(&Guest{}).Where("id IN ? OR merged_into_id IN ?", ids, ids).Pluck("id", &cluster)
		cluster = append(cluster, ids...)
		d.Where("from_guest_id IN ? OR into_guest_id IN ?", cluster, cluster).Delete(&GuestMerge{})
		d.Where("guest_id IN ?", cluster).Delete(&GuestIdentity{})
		d.Where("id IN ?", cluster).Delete(&Guest{})
	})
	return vals
}

// newOrderedGuest 建一个 guest，并把 first_seen_at 钉成 base+offset，让合并方向可确定。
func newOrderedGuest(t *testing.T, brand Brand, cid string, offset time.Duration) uint64 {
	t.Helper()
	id, err := resolveGuest(context.Background(), brand, cid, "", "zh-CN", "CN")
	require.NoError(t, err)
	base := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	require.NoError(t, db.Get().Model(&Guest{}).Where("id = ?", id).
		Update("first_seen_at", base.Add(offset)).Error)
	return id
}

// countGuestsOwningCID 通过 cid 标识 join 到 guests，数出持有该 (brand, cid) 的 guest 行数。
func countGuestsOwningCID(t *testing.T, brand Brand, cid string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Table("guests").
		Joins("JOIN guest_identities gi ON gi.guest_id = guests.id").
		Where("gi.brand = ? AND gi.kind = ? AND gi.value = ?", string(brand), IdentityCID, cid).
		Count(&n).Error)
	return n
}

func loadGuest(t *testing.T, id uint64) Guest {
	t.Helper()
	var g Guest
	require.NoError(t, db.Get().First(&g, id).Error)
	return g
}

func TestResolveGuest_NewAndStable(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 1)
	ctx := context.Background()

	id1, err := resolveGuest(ctx, BrandKaitu, v[0], "", "zh-CN", "CN")
	require.NoError(t, err)
	id2, err := resolveGuest(ctx, BrandKaitu, v[0], "", "", "")
	require.NoError(t, err)
	assert.Equal(t, id1, id2)

	assert.EqualValues(t, 1, countGuestsOwningCID(t, BrandKaitu, v[0]), "同一 cid 只对应一行 guests")
	// 空 locale/country 不覆盖
	g := loadGuest(t, id1)
	assert.Equal(t, "zh-CN", g.Locale)
	assert.Equal(t, "CN", g.Country)
	assert.Equal(t, string(BrandKaitu), g.Brand)

	var ident GuestIdentity
	require.NoError(t, db.Get().Where("kind = ? AND value = ?", IdentityCID, v[0]).First(&ident).Error)
	assert.Equal(t, StrengthVerified, ident.Strength)

	// 非空 locale 会更新
	_, err = resolveGuest(ctx, BrandKaitu, v[0], "", "en-US", "")
	require.NoError(t, err)
	assert.Equal(t, "en-US", loadGuest(t, id1).Locale)

	_, err = resolveGuest(ctx, BrandKaitu, "", "", "", "")
	assert.Error(t, err)
}

func TestResolveGuest_ConcurrentSameCid(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 1)
	ctx := context.Background()

	const N = 10
	ids := make([]uint64, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = resolveGuest(ctx, BrandKaitu, v[0], "", "", "")
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < N; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, ids[0], ids[i])
	}
	// 恰好一个 guest 持有该 (brand, cid)，且它就是所有调用方拿到的那个
	assert.EqualValues(t, 1, countGuestsOwningCID(t, BrandKaitu, v[0]))
	var owners []uint64
	require.NoError(t, db.Get().Model(&GuestIdentity{}).
		Where("brand = ? AND kind = ? AND value = ?", string(BrandKaitu), IdentityCID, v[0]).
		Pluck("guest_id", &owners).Error)
	require.Len(t, owners, 1)
	assert.Equal(t, ids[0], owners[0])
}

func TestResolveGuest_SameSidAutoMerges(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3) // cidA cidB sid
	ctx := context.Background()

	g1, err := resolveGuest(ctx, BrandKaitu, v[0], v[2], "", "")
	require.NoError(t, err)
	g2, err := resolveGuest(ctx, BrandKaitu, v[1], v[2], "", "")
	require.NoError(t, err)
	assert.Equal(t, g1, g2, "同 sid 的新 cid 应并入较早的根")

	// 之后用 cid B 单独解析也落在同一个根
	g3, err := resolveGuest(ctx, BrandKaitu, v[1], "", "", "")
	require.NoError(t, err)
	assert.Equal(t, g1, g3)

	var merges []GuestMerge
	require.NoError(t, db.Get().Where("into_guest_id = ?", g1).Find(&merges).Error)
	require.Len(t, merges, 1)
	assert.Equal(t, MergeSameSID, merges[0].Reason)
	require.NotNil(t, merges[0].EvidenceIdentityID)
	assert.Nil(t, merges[0].ActorID)
}

func TestResolveGuest_BrandIsolated(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3) // cidA cidB sid
	ctx := context.Background()

	gk, err := resolveGuest(ctx, BrandKaitu, v[0], v[2], "", "")
	require.NoError(t, err)
	go1, err := resolveGuest(ctx, BrandOverleap, v[1], v[2], "", "")
	require.NoError(t, err)
	assert.NotEqual(t, gk, go1)

	// 同一 cid 在另一品牌下也是另一个 guest
	go2, err := resolveGuest(ctx, BrandOverleap, v[0], "", "", "")
	require.NoError(t, err)
	assert.NotEqual(t, gk, go2)

	var n int64
	db.Get().Model(&GuestMerge{}).Where("from_guest_id IN ? OR into_guest_id IN ?",
		[]uint64{gk, go1, go2}, []uint64{gk, go1, go2}).Count(&n)
	assert.EqualValues(t, 0, n)
}

func TestClaimedEmailDoesNotMerge(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3) // cidA cidB email
	ctx := context.Background()
	email := v[2] + "@x.io"

	g1, err := resolveGuest(ctx, BrandKaitu, v[0], "", "", "")
	require.NoError(t, err)
	g2, err := resolveGuest(ctx, BrandKaitu, v[1], "", "", "")
	require.NoError(t, err)
	require.NoError(t, addGuestEmail(ctx, g1, BrandKaitu, email))
	require.NoError(t, addGuestEmail(ctx, g2, BrandKaitu, email))
	require.NoError(t, addGuestEmail(ctx, g2, BrandKaitu, email), "幂等")

	r1, _ := guestRootID(ctx, g1)
	r2, _ := guestRootID(ctx, g2)
	assert.NotEqual(t, r1, r2)

	var idents []GuestIdentity
	require.NoError(t, db.Get().Where("guest_id = ? AND kind = ?", g2, IdentityEmail).Find(&idents).Error)
	require.Len(t, idents, 1)
	assert.Equal(t, StrengthClaimed, idents[0].Strength)
	assert.Equal(t, email, guestEmail(ctx, g1))
	assert.Equal(t, "", guestEmail(ctx, 0))
}

func TestMergeChainStaysFlat(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3)
	ctx := context.Background()
	a := newOrderedGuest(t, BrandKaitu, v[0], 0)
	b := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)
	c := newOrderedGuest(t, BrandKaitu, v[2], 2*time.Hour)

	m1, err := mergeGuests(ctx, b, a, MergeManual, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, m1)
	m2, err := mergeGuests(ctx, c, b, MergeManual, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, m2)

	for _, id := range []uint64{a, b, c} {
		r, err := guestRootID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, a, r)
	}
	for _, id := range []uint64{b, c} {
		g := loadGuest(t, id)
		require.NotNil(t, g.MergedIntoID)
		assert.Equal(t, a, *g.MergedIntoID, "深度必须为 1")
	}
	assert.Nil(t, loadGuest(t, a).MergedIntoID)

	ids, err := guestClusterIDs(ctx, a)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint64{a, b, c}, ids)

	// 同根再合并：无操作
	m3, err := mergeGuests(ctx, c, a, MergeManual, nil, nil)
	assert.NoError(t, err)
	assert.Nil(t, m3)
}

// 调用方传入顺序不影响结果：较早创建的根存活。
func TestMergeDirectionIgnoresArgOrder(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 2)
	ctx := context.Background()
	old := newOrderedGuest(t, BrandKaitu, v[0], 0)
	young := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)

	m, err := mergeGuests(ctx, old, young, MergeManual, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, old, m.IntoGuestID)
	assert.Equal(t, young, m.FromGuestID)
	r, _ := guestRootID(ctx, young)
	assert.Equal(t, old, r)
}

func TestUndoMerge(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 4)
	ctx := context.Background()
	a := newOrderedGuest(t, BrandKaitu, v[0], 0)
	b := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)
	c := newOrderedGuest(t, BrandKaitu, v[2], 2*time.Hour)
	d := newOrderedGuest(t, BrandKaitu, v[3], 3*time.Hour)

	_, err := mergeGuests(ctx, c, b, MergeManual, nil, nil) // c -> b
	require.NoError(t, err)
	m2, err := mergeGuests(ctx, d, a, MergeManual, nil, nil) // d -> a
	require.NoError(t, err)
	m3, err := mergeGuests(ctx, b, a, MergeManual, nil, nil) // {b,c} -> a
	require.NoError(t, err)
	require.NotNil(t, m2)
	require.NotNil(t, m3)

	// 压平：c 原本挂在 b 下，b 并入 a 后必须直接指向 a
	for _, id := range []uint64{b, c, d} {
		g := loadGuest(t, id)
		require.NotNil(t, g.MergedIntoID)
		assert.Equal(t, a, *g.MergedIntoID, "深度必须为 1")
	}

	// 撤销 m3：b 恢复为根，c 回到 b 之下；d 仍在 a 之下，不误伤
	require.NoError(t, undoGuestMerge(ctx, m3.ID, 7))
	assert.Nil(t, loadGuest(t, b).MergedIntoID)
	gc := loadGuest(t, c)
	require.NotNil(t, gc.MergedIntoID)
	assert.Equal(t, b, *gc.MergedIntoID)
	gd := loadGuest(t, d)
	require.NotNil(t, gd.MergedIntoID)
	assert.Equal(t, a, *gd.MergedIntoID)
	assert.Nil(t, loadGuest(t, a).MergedIntoID)

	var rec GuestMerge
	require.NoError(t, db.Get().First(&rec, m3.ID).Error)
	require.NotNil(t, rec.UndoneAt)
	require.NotNil(t, rec.UndoneBy)
	assert.EqualValues(t, 7, *rec.UndoneBy)

	rb, _ := guestRootID(ctx, c)
	assert.Equal(t, b, rb)
}

func TestUndoMerge_Twice(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 2)
	ctx := context.Background()
	a := newOrderedGuest(t, BrandKaitu, v[0], 0)
	b := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)
	m, err := mergeGuests(ctx, b, a, MergeManual, nil, nil)
	require.NoError(t, err)
	require.NoError(t, undoGuestMerge(ctx, m.ID, 1))

	var before GuestMerge
	require.NoError(t, db.Get().First(&before, m.ID).Error)
	err = undoGuestMerge(ctx, m.ID, 2)
	assert.ErrorIs(t, err, errGuestMergeAlreadyUndone)

	var after GuestMerge
	require.NoError(t, db.Get().First(&after, m.ID).Error)
	assert.Equal(t, before.UndoneBy, after.UndoneBy)
	assert.Nil(t, loadGuest(t, b).MergedIntoID)
	assert.Nil(t, loadGuest(t, a).MergedIntoID)
}

// 损坏数据（环）也必须终止。
func TestGuestRootID_TerminatesOnCycle(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 2)
	ctx := context.Background()
	a := newOrderedGuest(t, BrandKaitu, v[0], 0)
	b := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)
	require.NoError(t, db.Get().Model(&Guest{}).Where("id = ?", a).Update("merged_into_id", b).Error)
	require.NoError(t, db.Get().Model(&Guest{}).Where("id = ?", b).Update("merged_into_id", a).Error)
	_, err := guestRootID(ctx, a)
	assert.Error(t, err)
}

// 撤销为 LIFO：之后还有合并碰过同一批 guest 时拒绝，状态不变。
func TestUndoMerge_RefusesWithLaterMerge(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3)
	ctx := context.Background()
	z := newOrderedGuest(t, BrandKaitu, v[0], 0)
	a := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)
	b := newOrderedGuest(t, BrandKaitu, v[2], 2*time.Hour)

	m1, err := mergeGuests(ctx, b, a, MergeManual, nil, nil) // b -> a
	require.NoError(t, err)
	m2, err := mergeGuests(ctx, a, z, MergeManual, nil, nil) // {a,b} -> z
	require.NoError(t, err)

	err = undoGuestMerge(ctx, m1.ID, 1)
	assert.ErrorIs(t, err, errGuestMergeHasLaterMerges)
	for _, id := range []uint64{a, b} {
		g := loadGuest(t, id)
		require.NotNil(t, g.MergedIntoID)
		assert.Equal(t, z, *g.MergedIntoID, "拒绝后状态不变")
	}
	var rec GuestMerge
	require.NoError(t, db.Get().First(&rec, m1.ID).Error)
	assert.Nil(t, rec.UndoneAt)

	// 先撤 m2，再撤 m1：成功，三个独立根
	require.NoError(t, undoGuestMerge(ctx, m2.ID, 1))
	require.NoError(t, undoGuestMerge(ctx, m1.ID, 1))
	for _, id := range []uint64{z, a, b} {
		assert.Nil(t, loadGuest(t, id).MergedIntoID)
	}
}

func TestUndoMerge_RefusesWhenLaterMergeMovedChild(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3)
	ctx := context.Background()
	a := newOrderedGuest(t, BrandKaitu, v[0], 0)
	b := newOrderedGuest(t, BrandKaitu, v[1], time.Hour)
	c := newOrderedGuest(t, BrandKaitu, v[2], 2*time.Hour)

	m1, err := mergeGuests(ctx, c, b, MergeManual, nil, nil) // c -> b
	require.NoError(t, err)
	_, err = mergeGuests(ctx, b, a, MergeManual, nil, nil) // {b,c} -> a
	require.NoError(t, err)

	assert.ErrorIs(t, undoGuestMerge(ctx, m1.ID, 1), errGuestMergeHasLaterMerges)
	gc := loadGuest(t, c)
	require.NotNil(t, gc.MergedIntoID)
	assert.Equal(t, a, *gc.MergedIntoID)
}

// 撤销的 same_sid 合并不会被访客的下一次请求自动重做。
func TestResolveGuest_UndoneSameSidNotRemerged(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 3) // cidA cidB sid
	ctx := context.Background()

	g1, err := resolveGuest(ctx, BrandKaitu, v[0], v[2], "", "")
	require.NoError(t, err)
	g2, err := resolveGuest(ctx, BrandKaitu, v[1], v[2], "", "")
	require.NoError(t, err)
	require.Equal(t, g1, g2)

	var m GuestMerge
	require.NoError(t, db.Get().Where("into_guest_id = ? AND reason = ?", g1, MergeSameSID).First(&m).Error)
	require.NoError(t, undoGuestMerge(ctx, m.ID, 9))

	r1, err := resolveGuest(ctx, BrandKaitu, v[0], v[2], "", "")
	require.NoError(t, err)
	r2, err := resolveGuest(ctx, BrandKaitu, v[1], v[2], "", "")
	require.NoError(t, err)
	assert.NotEqual(t, r1, r2, "撤销后根保持独立")

	var n int64
	require.NoError(t, db.Get().Model(&GuestMerge{}).Where("from_guest_id IN ? OR into_guest_id IN ?",
		[]uint64{r1, r2}, []uint64{r1, r2}).Count(&n).Error)
	assert.EqualValues(t, 1, n, "不应产生新的合并记录")
}

func TestMergeGuests_CrossBrandRefused(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 2)
	ctx := context.Background()
	k := newOrderedGuest(t, BrandKaitu, v[0], 0)
	o := newOrderedGuest(t, BrandOverleap, v[1], time.Hour)
	m, err := mergeGuests(ctx, o, k, MergeManual, nil, nil)
	assert.ErrorIs(t, err, errGuestMergeCrossBrand)
	assert.Nil(t, m)
	assert.Nil(t, loadGuest(t, o).MergedIntoID)
}

// 并发合并 W→Y 与 Y→X：无论谁先提交，都不得留下指向非根的行。
func TestMergeGuests_ConcurrentKeepsDepthOne(t *testing.T) {
	skipIfNoConfig(t)
	const rounds = 6
	v := chatTestValues(t, rounds*3)
	ctx := context.Background()
	for i := 0; i < rounds; i++ {
		x := newOrderedGuest(t, BrandKaitu, v[i*3], 0)
		y := newOrderedGuest(t, BrandKaitu, v[i*3+1], time.Hour)
		w := newOrderedGuest(t, BrandKaitu, v[i*3+2], 2*time.Hour)

		var wg sync.WaitGroup
		errs := make([]error, 2)
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, errs[0] = mergeGuests(ctx, w, y, MergeManual, nil, nil) }()
		go func() { defer wg.Done(); <-start; _, errs[1] = mergeGuests(ctx, y, x, MergeManual, nil, nil) }()
		close(start)
		wg.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		for _, id := range []uint64{y, w} {
			g := loadGuest(t, id)
			require.NotNil(t, g.MergedIntoID, "round %d", i)
			assert.Equal(t, x, *g.MergedIntoID, "round %d: 深度必须为 1", i)
		}
		assert.Nil(t, loadGuest(t, x).MergedIntoID)
	}
}

// 撤销的合并不能绕道第三个 guest 被悄悄重做。
func TestResolveGuest_UndoneMergeNotRedoneViaThirdGuest(t *testing.T) {
	skipIfNoConfig(t)
	v := chatTestValues(t, 4) // cid1 cid2 cid3 sid
	ctx := context.Background()

	g1, err := resolveGuest(ctx, BrandKaitu, v[0], v[3], "", "")
	require.NoError(t, err)
	// 钉住时间顺序，g1 最老
	require.NoError(t, db.Get().Model(&Guest{}).Where("id = ?", g1).
		Update("first_seen_at", time.Now().Add(-48*time.Hour)).Error)
	g2, err := resolveGuest(ctx, BrandKaitu, v[1], v[3], "", "")
	require.NoError(t, err)
	require.Equal(t, g1, g2)
	var m GuestMerge
	require.NoError(t, db.Get().Where("into_guest_id = ? AND reason = ?", g1, MergeSameSID).First(&m).Error)
	require.NoError(t, undoGuestMerge(ctx, m.ID, 9))
	g2 = m.FromGuestID
	require.NotEqual(t, g1, g2)

	g3, err := resolveGuest(ctx, BrandKaitu, v[2], v[3], "", "")
	require.NoError(t, err)
	r1, err := guestRootID(ctx, g1)
	require.NoError(t, err)
	r2, err := guestRootID(ctx, g2)
	require.NoError(t, err)
	assert.NotEqual(t, r1, r2, "g1 与 g2 之间撤销过的合并不得被重做")
	assert.Contains(t, []uint64{r1, r2}, g3, "g3 的根是二者之一")
}
