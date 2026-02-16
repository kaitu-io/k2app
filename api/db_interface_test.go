package center

import (
	"gorm.io/gorm"
)

// =====================================================================
// 数据库接口抽象（仅测试使用）
// 允许在测试中注入 mock 数据库，而不修改生产代码
// =====================================================================

// DBGetter 数据库获取接口
// 生产环境使用 qtoolkit/db.Get()，测试环境可注入 mock
type DBGetter interface {
	GetDB() *gorm.DB
}

// testDBGetter 测试用的数据库获取器
var testDBGetter DBGetter

// SetTestDBGetter 设置测试用数据库获取器
func SetTestDBGetter(getter DBGetter) {
	testDBGetter = getter
}

// ClearTestDBGetter 清除测试用数据库获取器
func ClearTestDBGetter() {
	testDBGetter = nil
}

// GetTestDB 获取测试数据库（如果设置了 mock 则返回 mock，否则返回 nil）
func GetTestDB() *gorm.DB {
	if testDBGetter != nil {
		return testDBGetter.GetDB()
	}
	return nil
}

// MockDBGetter 实现 DBGetter 接口的 mock
type MockDBGetter struct {
	db *gorm.DB
}

// NewMockDBGetter 创建 mock 数据库获取器
func NewMockDBGetter(db *gorm.DB) *MockDBGetter {
	return &MockDBGetter{db: db}
}

// GetDB 实现 DBGetter 接口
func (m *MockDBGetter) GetDB() *gorm.DB {
	return m.db
}
