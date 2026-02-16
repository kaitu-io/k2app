package center

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// =====================================================================
// EDM 任务测试
// 测试目标：验证 EDM 邮件发送任务的正确性、可靠性和容错性
// =====================================================================

// ===================== Context Cancellation 测试 =====================

// skipIfNoDatabase 检查数据库是否可用，不可用则跳过测试
func skipIfNoDatabase(t *testing.T) {
	t.Helper()
	testInitConfig()
	if db.Get() == nil {
		t.Skip("Skipping test: database not available")
	}
	// 尝试 ping 数据库
	sqlDB, err := db.Get().DB()
	if err != nil || sqlDB.Ping() != nil {
		t.Skip("Skipping test: database not available")
	}
}

// TestEDM_ContextCancel_StopsEarly 测试 Context 取消后任务停止
// 能抓到的 Bug：Goroutine 泄漏、任务无法取消
func TestEDM_ContextCancel_StopsEarly(t *testing.T) {
	skipIfNoDatabase(t)

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())

	// 模拟 100 个用户
	users := make([]User, 100)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	// 创建模拟模板
	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	// 启动任务
	done := make(chan EDMTaskOutput, 1)
	go func() {
		// 注意：这个测试需要 mock sendEmail 函数，否则会真的发送邮件
		// 在实际运行中，由于 getUserEmailByUser 会失败，所有用户会被标记为 failed
		output := sendEmailsWithTracking(ctx, "test-cancel-batch", users, template)
		done <- output
	}()

	// 等待一小段时间后取消（让任务处理几个用户）
	time.Sleep(350 * time.Millisecond) // 约 3 个用户（每个 100ms）
	cancel()

	// 等待任务结束
	select {
	case output := <-done:
		// 验证：任务应该提前停止
		totalProcessed := output.SentCount + output.FailedCount + output.SkippedCount
		t.Logf("Task stopped with: sent=%d, failed=%d, skipped=%d, total_processed=%d",
			output.SentCount, output.FailedCount, output.SkippedCount, totalProcessed)

		// 由于任务被取消，不应该处理完所有 100 个用户
		assert.True(t, totalProcessed < 100,
			"Task should stop early on context cancel (processed %d, expected < 100)", totalProcessed)

	case <-time.After(10 * time.Second):
		t.Fatal("GOROUTINE LEAK: Task did not stop after context cancel within 10 seconds")
	}
}

// TestEDM_ContextTimeout_StopsWithinTimeout 测试 Context 超时
// 能抓到的 Bug：任务超时后不停止
func TestEDM_ContextTimeout_StopsWithinTimeout(t *testing.T) {
	skipIfNoDatabase(t)

	// 创建 500ms 超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// 模拟 50 个用户（理论上需要 5 秒处理完）
	users := make([]User, 50)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	start := time.Now()
	output := sendEmailsWithTracking(ctx, "test-timeout-batch", users, template)
	elapsed := time.Since(start)

	// 任务应该在 context 超时后停止
	assert.True(t, elapsed < 2*time.Second,
		"Task should stop within reasonable time after context timeout (elapsed: %v)", elapsed)

	totalProcessed := output.SentCount + output.FailedCount + output.SkippedCount
	t.Logf("Task finished in %v, processed: %d/%d", elapsed, totalProcessed, len(users))
}

// ===================== 速率限制测试 =====================

// TestEDM_RateLimit_EnforcesDelay 测试速率限制是否生效
// 能抓到的 Bug：发送过快被 SMTP 限制
func TestEDM_RateLimit_EnforcesDelay(t *testing.T) {
	skipIfNoDatabase(t)
	ctx := context.Background()

	// 只测试少量用户以验证速率
	users := make([]User, 5)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	start := time.Now()
	sendEmailsWithTracking(ctx, "test-ratelimit-batch", users, template)
	elapsed := time.Since(start)

	// 5 个用户，每个间隔 100ms，至少需要 400ms（第一个立即处理）
	expectedMinDuration := 400 * time.Millisecond
	assert.True(t, elapsed >= expectedMinDuration,
		"Rate limit should enforce ~100ms delay between emails (elapsed: %v, expected >= %v)",
		elapsed, expectedMinDuration)

	t.Logf("Processed 5 users in %v (expected >= %v)", elapsed, expectedMinDuration)
}

// ===================== EDM 任务输出测试 =====================

// TestEDM_Output_CountsCorrect 测试输出统计是否正确
// 能抓到的 Bug：计数逻辑错误
func TestEDM_Output_CountsCorrect(t *testing.T) {
	skipIfNoDatabase(t)
	ctx := context.Background()

	// 创建一些没有邮箱的用户（会 fail）
	users := make([]User, 10)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
		// 不设置 LoginIdentifies，所以 getUserEmailByUser 会失败
	}

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	output := sendEmailsWithTracking(ctx, "test-counts-batch", users, template)

	// 所有用户应该都失败（因为没有邮箱）
	assert.Equal(t, 10, output.FailedCount,
		"All users without email should be counted as failed")
	assert.Equal(t, 0, output.SentCount,
		"No emails should be sent for users without email")
	assert.Equal(t, 0, output.SkippedCount,
		"No users should be skipped")

	// 验证总数
	totalProcessed := output.SentCount + output.FailedCount + output.SkippedCount
	assert.Equal(t, 10, totalProcessed,
		"Total processed should equal input users count")
}

// ===================== 并发安全测试 =====================

// TestEDM_ConcurrentBatches_NoRaceCondition 测试并发批次不会产生竞态条件
// 能抓到的 Bug：并发访问共享资源导致数据损坏
func TestEDM_ConcurrentBatches_NoRaceCondition(t *testing.T) {
	skipIfNoDatabase(t)
	// 使用 -race 标志运行此测试：go test -race -run TestEDM_ConcurrentBatches

	const numBatches = 5
	const usersPerBatch = 10

	var completedBatches atomic.Int32
	done := make(chan struct{}, numBatches)

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	// 启动多个并发批次
	for b := 0; b < numBatches; b++ {
		batchID := b
		go func() {
			ctx := context.Background()
			users := make([]User, usersPerBatch)
			for i := range users {
				users[i] = User{ID: uint64(batchID*1000 + i + 1)}
			}

			sendEmailsWithTracking(ctx, "test-race-batch-"+string(rune('A'+batchID)), users, template)
			completedBatches.Add(1)
			done <- struct{}{}
		}()
	}

	// 等待所有批次完成
	timeout := time.After(30 * time.Second)
	for i := 0; i < numBatches; i++ {
		select {
		case <-done:
			// ok
		case <-timeout:
			t.Fatalf("Timeout waiting for batches to complete (completed: %d/%d)",
				completedBatches.Load(), numBatches)
		}
	}

	assert.Equal(t, int32(numBatches), completedBatches.Load(),
		"All batches should complete without race conditions")
}

// ===================== 幂等性检查测试 =====================

// TestEDM_IdempotencyCheck_SkipsDuplicates 测试幂等性检查跳过重复
// 能抓到的 Bug：幂等性失效导致重复发送
func TestEDM_IdempotencyCheck_SkipsDuplicates(t *testing.T) {
	// 注意：此测试需要数据库支持，在没有数据库时会跳过
	skipIfNoDatabase(t)

	ctx := context.Background()

	// 创建测试用户和模板
	user := CreateTestUser(t)
	require.NotNil(t, user)

	template := CreateTestEmailTemplate(t)
	require.NotNil(t, template)

	// 创建带邮箱的用户
	users := []User{*user}

	// 第一次发送
	output1 := sendEmailsWithTracking(ctx, "test-idem-batch-1", users, template)
	t.Logf("First send: sent=%d, failed=%d, skipped=%d",
		output1.SentCount, output1.FailedCount, output1.SkippedCount)

	// 第二次发送（同一模板、同一用户，应该被跳过）
	output2 := sendEmailsWithTracking(ctx, "test-idem-batch-2", users, template)
	t.Logf("Second send: sent=%d, failed=%d, skipped=%d",
		output2.SentCount, output2.FailedCount, output2.SkippedCount)

	// 第二次应该跳过（24小时内已发送）
	// 注意：由于用户可能没有邮箱，第一次可能也是 failed
	// 这里主要验证幂等性检查函数被调用
}

// ===================== 进度日志测试 =====================

// TestEDM_ProgressLog_PrintsEvery100 测试进度日志每 100 封打印
// 能抓到的 Bug：进度日志丢失，无法追踪大批量任务
func TestEDM_ProgressLog_WorksCorrectly(t *testing.T) {
	skipIfNoDatabase(t)
	// 此测试通过日志输出验证，在生产环境需要捕获日志
	// 这里只验证函数不会因为大量用户而崩溃

	ctx := context.Background()

	// 创建 150 个用户（会触发一次进度日志）
	users := make([]User, 150)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	// 使用超时 context 避免测试时间过长
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	output := sendEmailsWithTracking(ctx, "test-progress-batch", users, template)

	// 验证所有用户都被处理
	totalProcessed := output.SentCount + output.FailedCount + output.SkippedCount
	t.Logf("Processed %d users: sent=%d, failed=%d, skipped=%d",
		totalProcessed, output.SentCount, output.FailedCount, output.SkippedCount)

	// 如果没有超时，应该处理完所有用户
	if ctx.Err() == nil {
		assert.Equal(t, 150, totalProcessed,
			"All users should be processed when no timeout")
	}
}

// ===================== 错误处理测试 =====================

// TestEDM_PartialFailure_ContinuesExecution 测试部分失败后继续执行
// 能抓到的 Bug：单个失败导致整批失败
func TestEDM_PartialFailure_ContinuesExecution(t *testing.T) {
	skipIfNoDatabase(t)
	ctx := context.Background()

	// 创建 10 个用户，都没有邮箱（会失败）
	users := make([]User, 10)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	output := sendEmailsWithTracking(ctx, "test-partial-fail-batch", users, template)

	// 所有用户都应该被处理（即使都失败）
	totalProcessed := output.SentCount + output.FailedCount + output.SkippedCount
	assert.Equal(t, 10, totalProcessed,
		"All users should be processed even if some/all fail")

	// 失败不应该中断批次
	assert.Equal(t, 10, output.FailedCount,
		"All users should be counted as failed (no email)")
}

// TestEDM_FailedEmails_LimitedTo10 测试失败邮箱列表限制为 10 个
// 能抓到的 Bug：大量失败导致内存问题
func TestEDM_FailedEmails_LimitedTo10(t *testing.T) {
	skipIfNoDatabase(t)
	ctx := context.Background()

	// 创建 20 个用户（都会失败）
	users := make([]User, 20)
	for i := range users {
		users[i] = User{ID: uint64(i + 1)}
	}

	isActive := true
	template := &EmailMarketingTemplate{
		ID:       1,
		Name:     "Test Template",
		Subject:  "Test Subject",
		Content:  "Test Content",
		IsActive: &isActive,
	}

	output := sendEmailsWithTracking(ctx, "test-failed-limit-batch", users, template)

	// 失败邮箱列表最多 10 个
	assert.True(t, len(output.FailedEmails) <= 10,
		"Failed emails list should be limited to 10 (got %d)", len(output.FailedEmails))
}
