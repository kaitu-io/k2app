package center

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// 第三方身份匹配（spec §4）集成测试：真库，0 SKIP。
// 每个测试用唯一 sub / email；t.Cleanup 物理删除相关身份行与用户行。

func socialTestSuffix() string {
	return strings.ReplaceAll(generateId("st"), "-", "")
}

// socialTrack 记录测试涉及的身份索引，Cleanup 时删除这些身份行及其用户（含软删用户）。
type socialTrack struct {
	t        *testing.T
	indexIDs []string
	userIDs  []uint64
}

func newSocialTrack(t *testing.T) *socialTrack {
	tr := &socialTrack{t: t}
	t.Cleanup(func() {
		ids := append([]uint64{}, tr.userIDs...)
		if len(tr.indexIDs) > 0 {
			var rows []LoginIdentify
			db.Get().Where("index_id IN ?", tr.indexIDs).Find(&rows)
			for _, r := range rows {
				ids = append(ids, r.UserID)
			}
		}
		if len(ids) > 0 {
			db.Get().Where("user_id IN ?", ids).Delete(&LoginIdentify{})
			db.Get().Unscoped().Where("id IN ?", ids).Delete(&User{})
		}
		if len(tr.indexIDs) > 0 {
			db.Get().Where("index_id IN ?", tr.indexIDs).Delete(&LoginIdentify{})
		}
	})
	return tr
}

func (tr *socialTrack) email(email string) string {
	tr.indexIDs = append(tr.indexIDs, secretHashIt(context.Background(), []byte(email)))
	return email
}

func (tr *socialTrack) sub(provider, sub string) string {
	tr.indexIDs = append(tr.indexIDs, secretHashIt(context.Background(), []byte(provider+":"+sub)))
	return sub
}

// seedSocialUser 建一个指定品牌的用户；email 非空时附带 email 身份行。
func (tr *socialTrack) seedUser(brand Brand, email string) *User {
	tr.t.Helper()
	u := &User{UUID: generateId("test-social"), Language: "en-US", Brand: string(brand)}
	require.NoError(tr.t, db.Get().Create(u).Error)
	tr.userIDs = append(tr.userIDs, u.ID)
	if email != "" {
		tr.email(email)
		require.NoError(tr.t, db.Get().Create(&LoginIdentify{
			UserID:         u.ID,
			Type:           "email",
			IndexID:        secretHashIt(context.Background(), []byte(email)),
			EncryptedValue: email,
			Brand:          string(brand),
		}).Error)
	}
	return u
}

func (tr *socialTrack) seedIdentity(brand Brand, userID uint64, provider, sub string) {
	tr.t.Helper()
	tr.sub(provider, sub)
	require.NoError(tr.t, db.Get().Create(&LoginIdentify{
		UserID:  userID,
		Type:    provider,
		IndexID: secretHashIt(context.Background(), []byte(provider+":"+sub)),
		Brand:   string(brand),
	}).Error)
}

func countIdentities(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Get().Model(&LoginIdentify{}).Where(where, args...).Count(&n).Error)
	return n
}

func identitiesOfUser(t *testing.T, userID uint64) map[string]int {
	t.Helper()
	var rows []LoginIdentify
	require.NoError(t, db.Get().Where("user_id = ?", userID).Find(&rows).Error)
	out := map[string]int{}
	for _, r := range rows {
		out[r.Type]++
	}
	return out
}

func TestResolveSocialUser_ExistingIdentity(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	u := tr.seedUser(BrandKaitu, "exist-"+sfx+"@example.com")
	tr.seedIdentity(BrandKaitu, u.ID, SocialProviderGoogle, "g-"+sfx)

	// 邮箱与已绑用户的邮箱不同也照样命中第 1 步（身份优先于邮箱）。
	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: "g-" + sfx, Email: tr.email("other-" + sfx + "@example.com"), EmailTrusted: true}
	uid, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	assert.Equal(t, u.ID, uid)
	assert.False(t, created)
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, u.ID))
}

func TestResolveSocialUser_MatchesEmailAccountAndBackfills(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := "match-" + sfx + "@example.com"
	u := tr.seedUser(BrandKaitu, email)

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}
	uid, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	assert.Equal(t, u.ID, uid)
	assert.False(t, created)
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, u.ID))

	var row LoginIdentify
	require.NoError(t, db.Get().Where("user_id = ? AND type = ?", u.ID, SocialProviderGoogle).First(&row).Error)
	assert.Equal(t, secretHashIt(context.Background(), []byte("google:g-"+sfx)), row.IndexID)
	assert.Equal(t, string(BrandKaitu), row.Brand)
	plain, err := secretDecryptString(context.Background(), row.EncryptedValue)
	require.NoError(t, err)
	assert.Equal(t, email, plain)
}

func TestResolveSocialUser_EmailAccountWithOtherIdentityNotOverwritten(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := "keep-" + sfx + "@example.com"
	u := tr.seedUser(BrandKaitu, email)
	tr.seedIdentity(BrandKaitu, u.ID, SocialProviderGoogle, "g-old-"+sfx)

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-new-"+sfx), Email: email, EmailTrusted: true}
	uid, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	assert.Equal(t, u.ID, uid)
	assert.False(t, created)
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, u.ID))
	assert.Equal(t, int64(0), countIdentities(t, "index_id = ?", secretHashIt(context.Background(), []byte("google:g-new-"+sfx))))
}

func TestResolveSocialUser_CreatesNewUser(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := tr.email("new-" + sfx + "@example.com")

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}
	uid, created, err := resolveSocialUser(context.Background(), BrandOverleap, cl, "en-US")
	require.NoError(t, err)
	assert.True(t, created)

	var u User
	require.NoError(t, db.Get().First(&u, uid).Error)
	assert.Equal(t, string(BrandOverleap), u.Brand)
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, uid))
	assert.Equal(t, int64(2), countIdentities(t, "user_id = ? AND brand = ?", uid, string(BrandOverleap)))
}

func TestResolveSocialUser_CrossBrandCreatesSeparateAccount(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := "xbrand-" + sfx + "@example.com"
	ov := tr.seedUser(BrandOverleap, email)

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}
	uid, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, ov.ID, uid)

	var u User
	require.NoError(t, db.Get().First(&u, uid).Error)
	assert.Equal(t, string(BrandKaitu), u.Brand)
	assert.Equal(t, map[string]int{"email": 1}, identitiesOfUser(t, ov.ID), "overleap 用户不得新增身份行")
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, uid))
}

func TestResolveSocialUser_UnverifiedGoogleEmailNeverMatches(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := "unverified-" + sfx + "@example.com"
	u := tr.seedUser(BrandKaitu, email)

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: false}
	_, _, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.ErrorIs(t, err, errSocialEmailUnverified)
	assert.Equal(t, map[string]int{"email": 1}, identitiesOfUser(t, u.ID))
	assert.Equal(t, int64(0), countIdentities(t, "index_id = ?", secretHashIt(context.Background(), []byte("google:g-"+sfx))))

	// 无邮箱同理
	cl2 := &socialClaims{Provider: SocialProviderApple, Sub: tr.sub(SocialProviderApple, "a-"+sfx)}
	_, _, err = resolveSocialUser(context.Background(), BrandKaitu, cl2)
	require.ErrorIs(t, err, errSocialEmailUnverified)
	assert.Equal(t, int64(0), countIdentities(t, "index_id = ?", secretHashIt(context.Background(), []byte("apple:a-"+sfx))))
}

func TestResolveSocialUser_AppleRelayEmailCreatesNew(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := tr.email(sfx + "@privaterelay.appleid.com")

	cl := &socialClaims{Provider: SocialProviderApple, Sub: tr.sub(SocialProviderApple, "a-"+sfx), Email: email, EmailTrusted: true}
	uid, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, map[string]int{"email": 1, "apple": 1}, identitiesOfUser(t, uid))

	found, err := findUserByEmailInBrand(context.Background(), BrandKaitu, email)
	require.NoError(t, err)
	assert.Equal(t, uid, found.ID, "relay 邮箱成为该账号的 email 身份，验证码登录也能进")
}

func TestResolveSocialUser_AfterAccountDeletion(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := tr.email("deleted-" + sfx + "@example.com")
	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}

	oldID, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	require.True(t, created)
	tr.userIDs = append(tr.userIDs, oldID)

	// 注销：用户软删 + 身份硬删（与 api_delete_user_account 一致）
	require.NoError(t, db.Get().Where("user_id = ?", oldID).Delete(&LoginIdentify{}).Error)
	require.NoError(t, db.Get().Delete(&User{}, oldID).Error)

	newID, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, oldID, newID)
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, newID))
}

func TestResolveSocialUser_ConcurrentFirstLogin(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	email := tr.email("race-" + sfx + "@example.com")
	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}

	const n = 8
	ids := make([]uint64, n)
	errs := make([]error, n)
	createdCount := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			uid, created, err := resolveSocialUser(context.Background(), BrandKaitu, cl)
			ids[i], errs[i] = uid, err
			if created {
				mu.Lock()
				createdCount++
				mu.Unlock()
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "goroutine %d", i)
		assert.Equal(t, ids[0], ids[i], "goroutine %d", i)
	}
	assert.Equal(t, 1, createdCount, "恰一个调用报告新建")
	assert.Equal(t, int64(1), countIdentities(t, "type = ? AND index_id = ? AND brand = ?",
		SocialProviderGoogle, secretHashIt(context.Background(), []byte("google:g-"+sfx)), string(BrandKaitu)))
	assert.Equal(t, int64(1), countIdentities(t, "type = ? AND index_id = ?", "email",
		secretHashIt(context.Background(), []byte(email))))
}

func TestBindSocialIdentity_Conflicts(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	ctx := context.Background()
	other := tr.seedUser(BrandKaitu, "other-"+sfx+"@example.com")
	tr.seedIdentity(BrandKaitu, other.ID, SocialProviderGoogle, "g-taken-"+sfx)
	me := tr.seedUser(BrandKaitu, "me-"+sfx+"@example.com")

	// ① 身份已属他人
	err := bindSocialIdentity(ctx, BrandKaitu, me.ID, &socialClaims{Provider: SocialProviderGoogle, Sub: "g-taken-" + sfx})
	require.ErrorIs(t, err, errSocialIdentityTaken)

	// 首次绑定成功
	mine := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-mine-"+sfx)}
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, me.ID, mine))
	// 重复绑同一个 → nil（幂等）
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, me.ID, mine))
	// 已绑另一个 google
	err = bindSocialIdentity(ctx, BrandKaitu, me.ID, &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-second-"+sfx)})
	require.ErrorIs(t, err, errSocialProviderAlreadyBound)

	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, me.ID))
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, other.ID))
}

func TestBindSocialIdentity_ZeroIdentityUser(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	ctx := context.Background()
	u := tr.seedUser(BrandKaitu, "") // 设备账号：无任何 LoginIdentify
	email := tr.email("zero-" + sfx + "@example.com")

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, u.ID, cl))
	assert.Equal(t, map[string]int{"email": 1, "google": 1}, identitiesOfUser(t, u.ID))

	found, err := findUserByEmailInBrand(ctx, BrandKaitu, email)
	require.NoError(t, err)
	assert.Equal(t, u.ID, found.ID, "绑定后验证码登录也能进这个账号")

	// 邮箱不可信 → 只写第三方身份，不写 email 行
	u2 := tr.seedUser(BrandKaitu, "")
	cl2 := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g2-"+sfx), Email: tr.email("zero2-" + sfx + "@example.com"), EmailTrusted: false}
	require.NoError(t, bindSocialIdentity(ctx, BrandKaitu, u2.ID, cl2))
	assert.Equal(t, map[string]int{"google": 1}, identitiesOfUser(t, u2.ID))
}

func TestBindSocialIdentity_EmailOwnedByOtherUser(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	ctx := context.Background()
	email := "owned-" + sfx + "@example.com"
	owner := tr.seedUser(BrandKaitu, email)
	u := tr.seedUser(BrandKaitu, "")

	cl := &socialClaims{Provider: SocialProviderGoogle, Sub: tr.sub(SocialProviderGoogle, "g-"+sfx), Email: email, EmailTrusted: true}
	err := bindSocialIdentity(ctx, BrandKaitu, u.ID, cl)
	require.ErrorIs(t, err, errSocialIdentityTaken)
	assert.Empty(t, identitiesOfUser(t, u.ID))
	assert.Equal(t, map[string]int{"email": 1}, identitiesOfUser(t, owner.ID))
}

func TestUnbindSocialIdentity(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	ctx := context.Background()
	u := tr.seedUser(BrandKaitu, "unbind-"+sfx+"@example.com")
	tr.seedIdentity(BrandKaitu, u.ID, SocialProviderApple, "a-"+sfx)

	row, err := unbindSocialIdentity(ctx, BrandKaitu, u.ID, SocialProviderApple)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, SocialProviderApple, row.Type)
	assert.Equal(t, u.ID, row.UserID)
	assert.Equal(t, map[string]int{"email": 1}, identitiesOfUser(t, u.ID))

	_, err = unbindSocialIdentity(ctx, BrandKaitu, u.ID, SocialProviderApple)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "未绑 → NotFound, got %v", err)
	_, err = unbindSocialIdentity(ctx, BrandKaitu, u.ID, SocialProviderGoogle)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "未绑 → NotFound, got %v", err)
}

func TestSaveAppleRefreshToken(t *testing.T) {
	skipIfNoConfig(t)
	tr := newSocialTrack(t)
	sfx := socialTestSuffix()
	ctx := context.Background()
	u := tr.seedUser(BrandOverleap, "")
	tr.seedIdentity(BrandOverleap, u.ID, SocialProviderApple, "a-"+sfx)

	require.NoError(t, saveAppleRefreshToken(ctx, BrandOverleap, "a-"+sfx, "io.overleap", "rt-"+sfx))
	// 同值再存一次不应误报 NotFound
	require.NoError(t, saveAppleRefreshToken(ctx, BrandOverleap, "a-"+sfx, "io.overleap", "rt-"+sfx))

	var row LoginIdentify
	require.NoError(t, db.Get().Where("user_id = ? AND type = ?", u.ID, SocialProviderApple).First(&row).Error)
	assert.Equal(t, "io.overleap", row.ProviderClientID)
	plain, err := secretDecryptString(ctx, row.ProviderRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "rt-"+sfx, plain)

	// 别的品牌 / 不存在的 sub → NotFound
	err = saveAppleRefreshToken(ctx, BrandKaitu, "a-"+sfx, "io.kaitu.signin", "rt")
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "got %v", err)
}
