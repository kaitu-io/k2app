package center

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MockDB 封装 sqlmock 和 GORM 的 mock 数据库
type MockDB struct {
	DB       *gorm.DB
	Mock     sqlmock.Sqlmock
	SqlDB    *sql.DB
	original *gorm.DB // 保存原始 DB 用于恢复
}

// mockGlobalDB 用于测试的全局 mock DB 引用
var mockGlobalDB *MockDB

// SetupMockDB 创建 mock 数据库用于测试
// 返回 MockDB 结构和清理函数
func SetupMockDB(t *testing.T) *MockDB {
	t.Helper()

	// 创建 sqlmock
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("Failed to create sqlmock: %v", err)
	}

	// 使用 sqlmock 创建 GORM DB
	dialector := mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	})

	gormDB, err := gorm.Open(dialector, &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		sqlDB.Close()
		t.Fatalf("Failed to create GORM with sqlmock: %v", err)
	}

	mockDB := &MockDB{
		DB:    gormDB,
		Mock:  mock,
		SqlDB: sqlDB,
	}

	// 保存到全局变量供测试使用
	mockGlobalDB = mockDB

	// 注册清理函数
	t.Cleanup(func() {
		mockGlobalDB = nil
		sqlDB.Close()
	})

	return mockDB
}

// GetMockDB 获取当前测试的 mock DB
// 如果没有设置 mock，返回 nil
func GetMockDB() *MockDB {
	return mockGlobalDB
}

// ExpectQuery 便捷方法：期望一个查询
func (m *MockDB) ExpectQuery(query string) *sqlmock.ExpectedQuery {
	return m.Mock.ExpectQuery(query)
}

// ExpectExec 便捷方法：期望一个执行
func (m *MockDB) ExpectExec(query string) *sqlmock.ExpectedExec {
	return m.Mock.ExpectExec(query)
}

// ExpectBegin 便捷方法：期望开始事务
func (m *MockDB) ExpectBegin() *sqlmock.ExpectedBegin {
	return m.Mock.ExpectBegin()
}

// ExpectCommit 便捷方法：期望提交事务
func (m *MockDB) ExpectCommit() *sqlmock.ExpectedCommit {
	return m.Mock.ExpectCommit()
}

// ExpectRollback 便捷方法：期望回滚事务
func (m *MockDB) ExpectRollback() *sqlmock.ExpectedRollback {
	return m.Mock.ExpectRollback()
}

// ExpectationsWereMet 检查所有期望是否满足
func (m *MockDB) ExpectationsWereMet(t *testing.T) {
	t.Helper()
	if err := m.Mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Mock expectations not met: %v", err)
	}
}

// ===================== 常用 Mock 数据生成 =====================

// MockUserRow 返回用户查询的 mock 行
func MockUserRow(id uint64, uuid, accessKey, language string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "uuid", "access_key", "language", "created_at", "updated_at",
	}).AddRow(id, uuid, accessKey, language, nil, nil)
}

// MockDeviceRow 返回设备查询的 mock 行
func MockDeviceRow(id uint64, udid string, userID uint64, tokenIssueAt int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "udid", "user_id", "remark", "token_issue_at", "created_at", "updated_at",
	}).AddRow(id, udid, userID, "Test Device", tokenIssueAt, nil, nil)
}

// MockLoginIdentifyRow 返回登录标识查询的 mock 行
func MockLoginIdentifyRow(id uint64, loginType, indexID string, userID uint64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "type", "index_id", "user_id", "created_at", "updated_at",
	}).AddRow(id, loginType, indexID, userID, nil, nil)
}

// MockEmailSendLogRow 返回邮件发送日志的 mock 行
func MockEmailSendLogRow(id uint64, batchID string, templateID, userID uint64, status EmailSendLogStatus) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "batch_id", "template_id", "user_id", "email", "language", "status", "created_at",
	}).AddRow(id, batchID, templateID, userID, "test@example.com", "en", status, nil)
}
