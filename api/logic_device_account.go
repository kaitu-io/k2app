package center

import "gorm.io/gorm"

// userHasIdentity 判定用户是否有登录身份（login_identifies 至少一行）。
// 零身份 = 设备账号；这是唯一判定入口，别处不要手写计数。
func userHasIdentity(tx *gorm.DB, userID uint64) (bool, error) {
	var n int64
	if err := tx.Model(&LoginIdentify{}).Where("user_id = ?", userID).Limit(1).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
