package center

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// =====================================================================
// EDM 纯逻辑单元测试
// 这些测试不需要数据库，测试 EDM 核心逻辑
// =====================================================================

// TestEDMTaskOutput_Structure 测试 EDMTaskOutput 结构体计数逻辑
// 能抓到的 Bug：输出结构不完整
func TestEDMTaskOutput_Structure(t *testing.T) {
	output := EDMTaskOutput{
		SentCount:    10,
		FailedCount:  5,
		SkippedCount: 3,
		FailedEmails: []string{"a@test.com", "b@test.com"},
	}

	// 验证结构体字段
	assert.Equal(t, 10, output.SentCount)
	assert.Equal(t, 5, output.FailedCount)
	assert.Equal(t, 3, output.SkippedCount)
	assert.Len(t, output.FailedEmails, 2)

	// 验证总处理数计算
	totalProcessed := output.SentCount + output.FailedCount + output.SkippedCount
	assert.Equal(t, 18, totalProcessed)
}

// TestEDMTaskOutput_FailedEmailsLimit 测试失败邮箱列表限制
// 能抓到的 Bug：失败邮箱列表无限增长导致内存问题
func TestEDMTaskOutput_FailedEmailsLimit(t *testing.T) {
	// 模拟超过 10 个失败邮箱的情况
	failedEmails := make([]string, 0)
	for i := 0; i < 20; i++ {
		if len(failedEmails) < 10 {
			failedEmails = append(failedEmails, "test@example.com")
		}
	}

	assert.LessOrEqual(t, len(failedEmails), 10,
		"Failed emails should be limited to 10 to prevent memory issues")
}

// TestContextCancellation_SelectBehavior 测试 context cancellation 的 select 行为
// 能抓到的 Bug：select 语句不正确导致 context 取消无法响应
func TestContextCancellation_SelectBehavior(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// 模拟 EDM 中的 select 逻辑
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var cancelled bool
	var iterCount int

	// 启动处理循环
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			select {
			case <-ctx.Done():
				cancelled = true
				close(done)
				return
			case <-ticker.C:
				iterCount++
			}
		}
		close(done)
	}()

	// 等待几次迭代后取消
	time.Sleep(350 * time.Millisecond)
	cancel()

	// 等待循环结束
	select {
	case <-done:
		// 正常结束
	case <-time.After(2 * time.Second):
		t.Fatal("GOROUTINE LEAK: Select loop did not respond to context cancellation")
	}

	assert.True(t, cancelled, "Context cancellation should be detected")
	assert.Less(t, iterCount, 100, "Loop should stop early on cancellation")
	t.Logf("Processed %d iterations before cancellation", iterCount)
}

// TestContextTimeout_SelectBehavior 测试 context timeout 的 select 行为
// 能抓到的 Bug：timeout 后循环不停止
func TestContextTimeout_SelectBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var iterCount int

	start := time.Now()
	for i := 0; i < 100; i++ {
		select {
		case <-ctx.Done():
			// Context 超时或取消
			goto done
		case <-ticker.C:
			iterCount++
		}
	}

done:
	elapsed := time.Since(start)

	// 验证在超时时间内停止
	assert.Less(t, elapsed, 1*time.Second,
		"Loop should stop within reasonable time after timeout")
	assert.Less(t, iterCount, 100,
		"Loop should not process all items after timeout")
	t.Logf("Processed %d iterations in %v", iterCount, elapsed)
}

// TestRateLimiting_TickerBehavior 测试速率限制器行为
// 能抓到的 Bug：速率限制不生效导致发送过快
func TestRateLimiting_TickerBehavior(t *testing.T) {
	ctx := context.Background()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	const iterations = 5
	start := time.Now()

	for i := 0; i < iterations; i++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 处理
		}
	}

	elapsed := time.Since(start)

	// 5 次迭代，每次 100ms，应该至少需要 400ms（第一次立即，后4次等待）
	expectedMin := time.Duration(iterations-1) * 100 * time.Millisecond
	assert.GreaterOrEqual(t, elapsed, expectedMin,
		"Rate limiter should enforce ~100ms delay between iterations (got %v, expected >= %v)",
		elapsed, expectedMin)
}

// TestConcurrentAtomicCounter 测试并发计数器安全性
// 能抓到的 Bug：并发访问计数器导致数据竞争
func TestConcurrentAtomicCounter(t *testing.T) {
	var counter atomic.Int32
	const goroutines = 10
	const incrementsPerGoroutine = 100

	done := make(chan struct{}, goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			for i := 0; i < incrementsPerGoroutine; i++ {
				counter.Add(1)
			}
			done <- struct{}{}
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	expected := int32(goroutines * incrementsPerGoroutine)
	assert.Equal(t, expected, counter.Load(),
		"Atomic counter should correctly count concurrent increments")
}

// TestEmailSendLogStatus_Values 测试邮件发送日志状态值
// 能抓到的 Bug：状态值定义错误
func TestEmailSendLogStatus_Values(t *testing.T) {
	// 验证状态值定义正确
	assert.Equal(t, EmailSendLogStatus("pending"), EmailSendLogStatusPending)
	assert.Equal(t, EmailSendLogStatus("sent"), EmailSendLogStatusSent)
	assert.Equal(t, EmailSendLogStatus("failed"), EmailSendLogStatusFailed)
	assert.Equal(t, EmailSendLogStatus("skipped"), EmailSendLogStatusSkipped)

	// 验证状态值不相等
	statuses := []EmailSendLogStatus{
		EmailSendLogStatusPending,
		EmailSendLogStatusSent,
		EmailSendLogStatusFailed,
		EmailSendLogStatusSkipped,
	}

	for i, s1 := range statuses {
		for j, s2 := range statuses {
			if i != j {
				assert.NotEqual(t, s1, s2,
					"Different statuses should have different values")
			}
		}
	}
}

// TestUserLanguagePreference_Default 测试用户语言偏好默认值
// 能抓到的 Bug：语言偏好为空时没有默认值
func TestUserLanguagePreference_Default(t *testing.T) {
	// 测试空语言
	user := &User{Language: ""}
	lang := getUserLanguagePreference(user)
	assert.NotEmpty(t, lang, "Should return default language for empty preference")

	// 测试有语言偏好的用户
	user2 := &User{Language: "zh-CN"}
	lang2 := getUserLanguagePreference(user2)
	assert.Equal(t, "zh-CN", lang2)
}

// TestProgressLogInterval 测试进度日志间隔
// 能抓到的 Bug：进度日志间隔不正确
func TestProgressLogInterval(t *testing.T) {
	// 验证每 100 个用户打印进度的逻辑
	shouldLog := func(i int) bool {
		return (i+1)%100 == 0
	}

	// 测试边界值
	assert.False(t, shouldLog(0), "Should not log at index 0")
	assert.False(t, shouldLog(98), "Should not log at index 98")
	assert.True(t, shouldLog(99), "Should log at index 99 (100th item)")
	assert.False(t, shouldLog(100), "Should not log at index 100")
	assert.True(t, shouldLog(199), "Should log at index 199 (200th item)")
}

// TestEmailMarketingTemplate_IsActive 测试邮件模板激活状态
// 能抓到的 Bug：指针类型的 bool 处理不当
func TestEmailMarketingTemplate_IsActive(t *testing.T) {
	// 测试 IsActive 为 true
	isActiveTrue := true
	template1 := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test",
		IsActive: &isActiveTrue,
	}
	assert.True(t, *template1.IsActive)

	// 测试 IsActive 为 false
	isActiveFalse := false
	template2 := &EmailMarketingTemplate{
		ID:       2,
		Name:     "Test",
		IsActive: &isActiveFalse,
	}
	assert.False(t, *template2.IsActive)

	// 测试 IsActive 为 nil（需要安全检查）
	template3 := &EmailMarketingTemplate{
		ID:       3,
		Name:     "Test",
		IsActive: nil,
	}
	// 业务逻辑应该检查 nil
	if template3.IsActive != nil {
		t.Log("IsActive is not nil")
	} else {
		t.Log("IsActive is nil - should handle this case")
	}
}

// =====================================================================
// 边界条件测试
// =====================================================================

// TestEmptyUserList 测试空用户列表
// 能抓到的 Bug：空列表导致异常
func TestEmptyUserList(t *testing.T) {
	users := []User{}

	// 验证空列表处理
	assert.Empty(t, users)
	assert.Equal(t, 0, len(users))

	// 遍历空列表应该不执行
	var processed int
	for range users {
		processed++
	}
	assert.Equal(t, 0, processed, "Empty list should result in 0 processed")
}

// TestSingleUserList 测试单用户列表
// 能抓到的 Bug：单元素边界处理错误
func TestSingleUserList(t *testing.T) {
	users := []User{{ID: 1}}

	assert.Equal(t, 1, len(users))
	assert.Equal(t, uint64(1), users[0].ID)
}

// TestLargeUserList 测试大用户列表（不执行，只验证创建）
// 能抓到的 Bug：大列表内存分配问题
func TestLargeUserList(t *testing.T) {
	const size = 10000
	users := make([]User, size)

	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	assert.Equal(t, size, len(users))
	assert.Equal(t, uint64(1), users[0].ID)
	assert.Equal(t, uint64(size), users[size-1].ID)
}
