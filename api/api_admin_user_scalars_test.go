package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAdminUserScalars_HasPassword 锁住后台用户 DTO 的 hasPassword。
// 回归背景：列表与详情两个端点各自手写 DataUser 字面量，两处都漏了
// HasPassword，于是 lookup_user 对**任何**用户都报 hasPassword=false，
// 而 /api/user 同一字段是对的——字段在 DTO 里存在，编译器不会提醒。
func TestAdminUserScalars_HasPassword(t *testing.T) {
	assert.False(t, adminUserScalars(&User{}).HasPassword, "无 hash → false")
	assert.True(t, adminUserScalars(&User{PasswordHash: "$2a$10$abcdef"}).HasPassword, "有 hash → true")
}

// TestAdminUserScalars_RowDerivedFields 覆盖其余纯 User 行字段，确保把两处
// 字面量收敛成一个函数时没有漏抄。
func TestAdminUserScalars_RowDerivedFields(t *testing.T) {
	key := "ak_live_x"
	u := &User{
		UUID:               "uuid-1",
		ExpiredAt:          1893456000,
		IsFirstOrderDone:   BoolPtr(true),
		IsRetailer:         BoolPtr(true),
		Roles:              7,
		IsAdmin:            BoolPtr(true),
		IsBlocked:          BoolPtr(true),
		AccessKey:          &key,
		AccessKeyCreatedAt: 1756000000,
		PasswordHash:       "$2a$10$abcdef",
	}
	got := adminUserScalars(u)
	assert.Equal(t, "uuid-1", got.UUID)
	assert.Equal(t, int64(1893456000), got.ExpiredAt)
	assert.True(t, got.IsFirstOrderDone)
	assert.True(t, got.IsRetailer)
	assert.Equal(t, uint64(7), got.Roles)
	assert.True(t, got.IsAdmin)
	assert.True(t, got.IsBlocked)
	assert.True(t, got.HasAccessKey)
	assert.Equal(t, int64(1756000000), got.AccessKeyCreatedAt)
	assert.True(t, got.HasPassword)

	// 空 AccessKey 指针与空串都不算「有 key」。
	empty := ""
	assert.False(t, adminUserScalars(&User{}).HasAccessKey)
	assert.False(t, adminUserScalars(&User{AccessKey: &empty}).HasAccessKey)
}
