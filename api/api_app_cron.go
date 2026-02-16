package center

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_execute_cron_tasks 触发任务队列执行（已弃用）
//
func api_execute_cron_tasks(c *gin.Context) {
	log.Infof(c, "[CRON] /cron/execute called - this endpoint is deprecated, tasks are now handled by Asynq")

	// 返回空响应，告知调用方任务已由 Asynq 处理
	Success(c, &struct {
		Message string `json:"message"`
	}{
		Message: "This endpoint is deprecated. Task scheduling is now handled by Asynq. Use asynqmon UI (/asynq) to monitor tasks.",
	})
}

// executionIDKey 用于 context 中存储执行追踪ID的 key 类型
type executionIDKey struct{}

// WithExecutionID 创建带有执行追踪ID的context
// 使用自定义的 key 类型确保类型安全
func WithExecutionID(ctx context.Context, executionID string) context.Context {
	return context.WithValue(ctx, executionIDKey{}, executionID)
}

// GetExecutionID 从context获取执行追踪ID
func GetExecutionID(ctx context.Context) string {
	if v := ctx.Value(executionIDKey{}); v != nil {
		return v.(string)
	}
	return ""
}

// LogWithExecutionID 创建带有 execution ID 的日志前缀
// 用于在日志消息中包含执行追踪ID
func LogWithExecutionID(ctx context.Context, prefix string) string {
	execID := GetExecutionID(ctx)
	if execID != "" {
		return fmt.Sprintf("[%s] [exec:%s]", prefix, execID)
	}
	return fmt.Sprintf("[%s]", prefix)
}

// safeGo 安全地启动 goroutine（带 panic recovery）
func safeGo(ctx context.Context, name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stackTrace := string(debug.Stack())
				log.Errorf(ctx, "PANIC in goroutine [%s]: %v\nStack trace:\n%s", name, r, stackTrace)
			}
		}()
		fn()
	}()
}

// safeGoWithError 安全地启动 goroutine（带 panic recovery 和错误返回）
func safeGoWithError(ctx context.Context, name string, fn func() error) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stackTrace := string(debug.Stack())
				log.Errorf(ctx, "PANIC in goroutine [%s]: %v\nStack trace:\n%s", name, r, stackTrace)
			}
		}()
		if err := fn(); err != nil {
			log.Errorf(ctx, "Error in goroutine [%s]: %v", name, err)
		}
	}()
}

// recoverWithLog 用于 defer 中的 panic recovery（可复用）
func recoverWithLog(ctx context.Context, name string) {
	if r := recover(); r != nil {
		stackTrace := string(debug.Stack())
		log.Errorf(ctx, "PANIC recovered in [%s]: %v\nStack trace:\n%s", name, r, stackTrace)
	}
}

// panicToError 将 panic 转换为 error（用于需要返回错误的场景）
func panicToError(r interface{}) error {
	if r == nil {
		return nil
	}
	if err, ok := r.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return fmt.Errorf("panic: %v", r)
}
