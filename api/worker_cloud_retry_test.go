package center

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 这一组测试锁住 2026-09-02 线上事故的修复:一条 cloud:delete 任务在目标记录
// 已被软删除后扑空,把「记录不存在」当成可重试错误返回,asynq 按 Retry=25 反复
// 重放,withSlackNotify 每次都发一条 Slack,同一个失败刷了 24 条告警。

func newCloudInstanceForTest(t *testing.T) *CloudInstance {
	t.Helper()
	ci := &CloudInstance{
		Provider:    "aws_lightsail",
		AccountName: "test-account-retry",
		InstanceID:  "test-retry-" + time.Now().Format("20060102150405.000000"),
		Name:        "retry-fixture",
		IPAddress:   "10.60.0.1",
		Region:      "ap-northeast-1",
	}
	require.NoError(t, db.Get().Create(ci).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(ci) })
	return ci
}

func mustPayload(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// 删除是幂等的:目标记录已经软删除(管理员连点两次删除,或 syncAll 的孤儿清理
// 抢先删掉)时,任务应当直接成功收尾,而不是报错触发重试。
func TestHandleCloudDelete_AlreadyGoneIsNoOp(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	ci := newCloudInstanceForTest(t)
	require.NoError(t, db.Get().Delete(ci).Error) // 软删除,复现线上状态

	// 确认前置条件成立:默认 scope 下这条记录确实已经查不到了。
	var probe CloudInstance
	require.Error(t, db.Get().First(&probe, ci.ID).Error, "fixture 必须处于软删除态")

	err := handleCloudDelete(context.Background(), mustPayload(t, CloudDeletePayload{
		CloudInstanceID: ci.ID,
	}))
	require.NoError(t, err, "记录已消失=删除目标已达成,必须当成成功而不是可重试失败")
}

// 记录从来不存在时同样不能重试——任务无论重放多少次都是同一个结果。
func TestHandleCloudDelete_UnknownIDDoesNotRetry(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	err := handleCloudDelete(context.Background(), mustPayload(t, CloudDeletePayload{
		CloudInstanceID: 1 << 62, // 不可能存在的 ID
	}))
	require.NoError(t, err, "未知 ID 与已删除同义,同样按幂等成功处理")
}

// 换 IP 没有幂等语义:记录不存在是真失败,但必须标成终态,不能进重试队列。
func TestHandleCloudChangeIP_MissingInstanceIsTerminal(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	err := handleCloudChangeIP(context.Background(), mustPayload(t, CloudChangeIPPayload{
		CloudInstanceID: 1 << 62,
	}))
	require.Error(t, err)
	require.ErrorIs(t, err, hibikenAsynq.SkipRetry,
		"必须包 SkipRetry,否则 asynq 会把同一个必然失败重放 %d 次", cloudTaskMaxRetry)
}

// payload 坏掉是终态:同一份字节重放多少次都还是解不开。
func TestCloudHandlers_BadPayloadIsTerminal(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	bad := []byte("{not json")
	for name, h := range map[string]func(context.Context, []byte) error{
		"delete":    handleCloudDelete,
		"change_ip": handleCloudChangeIP,
		"create":    handleCloudCreate,
	} {
		t.Run(name, func(t *testing.T) {
			err := h(context.Background(), bad)
			require.Error(t, err)
			require.ErrorIs(t, err, hibikenAsynq.SkipRetry)
		})
	}
}

// 账号配置查不到是终态:配置不重载就永远查不到,重试只是把失败复读一遍。
func TestHandleCloudCreate_UnknownAccountIsTerminal(t *testing.T) {
	testInitConfig()
	skipIfNoConfig(t)

	err := handleCloudCreate(context.Background(), mustPayload(t, CloudCreatePayload{
		AccountName: "no-such-account-in-config",
		Region:      "ap-northeast-1",
		Name:        "x",
	}))
	require.Error(t, err)
	require.ErrorIs(t, err, hibikenAsynq.SkipRetry)
}

func TestTerminalf_MarksSkipRetryAndKeepsMessage(t *testing.T) {
	err := terminalf("instance %d not found", 22563)
	require.ErrorIs(t, err, hibikenAsynq.SkipRetry)
	require.Contains(t, err.Error(), "instance 22563 not found",
		"告警正文必须保留可读原因,不能只剩 asynq 的 sentinel 文案")
}

// isFinalAttempt 决定 withSlackNotify 是发告警还是只记一条 warn。
//
// 已知盲区,两条都源于 asynq 的重试计数只能由 server 注入 ctx、没有公开构造
// 函数可以在单测里伪造:
//
//  1. 「中途重试只记 warn 不告警」这条路径覆盖不到——那正是把 24 条告警压回
//     1 条的分支,本测试证明不了它,只能靠 qtoolkit/asynq 的 mux.HandleFunc
//     (`return h(ctx, t.Payload())`)确认 ctx 确实原样透传,以及线上观察。
//  2. 下面两个断言在 context.Background() 下其实走的是同一条 fallback 分支
//     (GetRetryCount 取不到值 → 保守返回 true),所以第一个断言并不能单独证明
//     SkipRetry 分支存在。SkipRetry 的实际效果由 terminalf 那组 ErrorIs 断言
//     和 asynq 自身的归档行为保证。
//
// 因此这个测试锁的是一件事:isFinalAttempt 在拿不到重试上下文时必须偏向告警,
// 绝不能静默吞掉一个真实失败。
func TestIsFinalAttempt(t *testing.T) {
	require.True(t, isFinalAttempt(context.Background(), terminalf("boom")),
		"SkipRetry 不会再重试,必须立刻告警")

	require.True(t, isFinalAttempt(context.Background(), context.DeadlineExceeded),
		"拿不到重试计数时保守告警,宁可多发也不要静默吞掉失败")
}
