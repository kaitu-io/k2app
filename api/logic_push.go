package center

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"
)

// =====================================================================
// 推送服务 —— 以用户为最小粒度（spec 2026-11-03-ios-push-design §6）
// =====================================================================
//
// 两段式：
//   1. PushToUser 同步做资格判断（通道 / 路由 / 营销同意 / 有效 token / 去重），
//      通过则写一条 push_send_logs（queued）并入队；不通过返回明确的哨兵错误。
//   2. asynq 任务 handlePushTask → deliverPush 逐个 token 投递 APNs，回写日志与 token 状态。
//
// 返回 nil 只代表「已入队且当时至少有 1 个有效 token」，不代表已送达：异步阶段
// token 全部失效时调用方同步拿不到，只能查 push_send_logs。调用方需要回退（比如改发
// 邮件）只能基于同步返回的哨兵错误。
//
// 目前只有 APNs 通道；FCM / JPush 的 token 不参与发送。

// PushCategory 推送分类。营销类要求用户在 App 内明确同意（App Store 审核规则 4.5.4）。
type PushCategory string

const (
	PushCategoryTransactional PushCategory = "transactional" // 账户提醒：扣款失败、到期等
	PushCategoryMarketing     PushCategory = "marketing"     // 召回、优惠、活动
)

// PushRequest 一条发给某个用户的推送。Title / Body 由调用方按用户语言本地化好；
// 文案不得含金额、邮箱、订单号等敏感信息，也不写品牌名（iOS 自己显示 App 名）。
type PushRequest struct {
	Category PushCategory
	DedupKey string // 必填：同一个 key 只发一次（varchar 191）
	Title    string
	Body     string
	Route    string // 可选：点击后跳转的 webapp 路由，必须在 pushRouteWhitelist 内
	BatchID  string // 可选：后台群发批次
}

var (
	ErrPushChannelUnavailable = errors.New("push: APNs channel not configured")
	ErrPushNoActiveToken      = errors.New("push: user has no active push token")
	ErrPushNotConsented       = errors.New("push: user has not opted in to marketing push")
	ErrPushDuplicate          = errors.New("push: duplicate dedup key")
	ErrPushInvalidRoute       = errors.New("push: route not in whitelist")
	ErrPushInvalidRequest     = errors.New("push: invalid request")
)

// pushRouteWhitelist 推送可以携带的跳转路由；webapp 端有同一份白名单
// （webapp/src/services/push-service.ts PUSH_ROUTE_WHITELIST），两边都只认表内路由。
var pushRouteWhitelist = map[string]bool{
	"/purchase":      true,
	"/account":       true,
	"/pro-histories": true,
}

// 以下三个出口是包级变量，测试替换（绝不打真 Apple / 真队列 / 真 Slack）。
var (
	// apnsClient 为 nil = 未配置 APNs（initAPNs 已打 Error），PushToUser 一律返回 ErrPushChannelUnavailable。
	apnsClient apnsSender

	enqueuePushTask = func(ctx context.Context, logID uint64) error {
		_, err := asynq.EnqueueContext(ctx, TaskTypePushSend, pushTaskPayload{LogID: logID})
		return err
	}

	pushAlert = func(msg string) {
		if err := slack.Send("alert", msg); err != nil {
			log.Errorf(context.Background(), "[PUSH] slack alert failed: %v", err)
		}
	}
)

// pushTaskPayload asynq 载荷：只带日志 ID，内容从日志读。
type pushTaskPayload struct {
	LogID uint64 `json:"logId"`
}

func validatePushRequest(req PushRequest) error {
	if req.Category != PushCategoryTransactional && req.Category != PushCategoryMarketing {
		return fmt.Errorf("%w: unknown category %q", ErrPushInvalidRequest, req.Category)
	}
	if req.DedupKey == "" || len(req.DedupKey) > 191 {
		return fmt.Errorf("%w: dedup key must be 1..191 chars", ErrPushInvalidRequest)
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Body) == "" {
		return fmt.Errorf("%w: title and body are required", ErrPushInvalidRequest)
	}
	if req.Route != "" && !pushRouteWhitelist[req.Route] {
		return fmt.Errorf("%w: %q", ErrPushInvalidRoute, req.Route)
	}
	return nil
}

// activePushTokens 返回用户当前能用的 APNs token。
// 设备归属 join：token 的设备必须仍属于该用户。设备被踢 / 删除 / 换绑别的账号后，
// 旧账号的推送自动不再到这台手机，不需要在每个删设备的地方停用 token。
func activePushTokens(tx *gorm.DB, userID uint64) ([]PushToken, error) {
	var tokens []PushToken
	err := tx.Model(&PushToken{}).
		Joins("JOIN devices ON devices.udid = push_tokens.device_udid AND devices.user_id = push_tokens.user_id").
		Where("push_tokens.user_id = ? AND push_tokens.provider = ? AND push_tokens.status = ?",
			userID, PushProviderAPNs, PushTokenStatusActive).
		Find(&tokens).Error
	return tokens, err
}

// PushToUser 向一个用户的所有有效设备发推送。见文件头注释的返回语义。
func PushToUser(ctx context.Context, userID uint64, req PushRequest) error {
	if err := validatePushRequest(req); err != nil {
		return err
	}
	if apnsClient == nil {
		return ErrPushChannelUnavailable
	}

	if req.Category == PushCategoryMarketing {
		var u User
		if err := db.Get().Select("id", "push_marketing_opt_in").First(&u, userID).Error; err != nil {
			return fmt.Errorf("push: load user %d: %w", userID, err)
		}
		if !u.PushMarketingOptIn {
			return ErrPushNotConsented
		}
	}

	tokens, err := activePushTokens(db.Get(), userID)
	if err != nil {
		return fmt.Errorf("push: query tokens of user %d: %w", userID, err)
	}
	if len(tokens) == 0 {
		return ErrPushNoActiveToken
	}

	entry := PushSendLog{
		DedupKey: req.DedupKey, BatchID: req.BatchID, UserID: userID,
		Category: string(req.Category), Route: req.Route, Title: req.Title, Body: req.Body,
		Status: PushSendStatusQueued,
	}
	if err := db.Get().Create(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isDuplicateKeyErr(err) {
			return ErrPushDuplicate
		}
		return fmt.Errorf("push: create send log: %w", err)
	}

	if err := enqueuePushTask(ctx, entry.ID); err != nil {
		// 删掉日志行释放 DedupKey：调用方（webhook 重放、对账、批量重跑、次日重试）
		// 可以用同一个 key 再来一次，而不是被 ErrPushDuplicate 永久挡掉。
		// 删除也失败才退回标记 failed（此时 key 被占用，日志里留了原因）。
		if derr := db.Get().Delete(&PushSendLog{}, entry.ID).Error; derr != nil {
			log.Errorf(ctx, "[PUSH] log %d: enqueue failed and delete failed: %v", entry.ID, derr)
			markPushLogFailed(ctx, entry.ID, "enqueue: "+err.Error())
		}
		return fmt.Errorf("push: enqueue: %w", err)
	}
	log.Infof(ctx, "[PUSH] queued log=%d user=%d category=%s key=%s devices=%d",
		entry.ID, userID, req.Category, req.DedupKey, len(tokens))
	return nil
}

// PushToUsers 批量推送，每个用户独立判断。DedupKey 按 req.DedupKey + ":" + userID 派生。
func PushToUsers(ctx context.Context, userIDs []uint64, req PushRequest) map[uint64]error {
	results := make(map[uint64]error, len(userIDs))
	base := req.DedupKey
	for _, uid := range userIDs {
		r := req
		r.DedupKey = fmt.Sprintf("%s:%d", base, uid)
		results[uid] = PushToUser(ctx, uid, r)
	}
	return results
}

// handlePushTask asynq 入口。deliverPush 返回错误 = 需要重试；最后一次重试仍失败则把日志标为 failed，
// 不让它永远停在 queued。
func handlePushTask(ctx context.Context, payload []byte) error {
	var p pushTaskPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("push: unmarshal payload: %w", err)
	}
	err := deliverPush(ctx, p.LogID)
	if err != nil {
		retried, _ := hibikenAsynq.GetRetryCount(ctx)
		maxRetry, _ := hibikenAsynq.GetMaxRetry(ctx)
		if retried >= maxRetry {
			markPushLogFailed(ctx, p.LogID, "retries exhausted: "+err.Error())
		}
	}
	return err
}

// tokenOutcome 单个 token 的投递结论
type tokenOutcome int

const (
	outcomeSent      tokenOutcome = iota
	outcomeDead                   // token 失效，已停用，不重试
	outcomeAuth                   // 凭据 / 配置错误，已告警（限流）；按可重试处理，配置修好后 asynq 退避重试能补发
	outcomeRetryable              // 5xx / 429 / 网络错误
)

// deliverPush 投递一条日志。返回 error 表示应当重试。
// 至少一台设备成功就不重试，避免已收到的设备被重复打扰。
func deliverPush(ctx context.Context, logID uint64) error {
	var entry PushSendLog
	if err := db.Get().First(&entry, logID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warnf(ctx, "[PUSH] log %d not found, dropping task", logID)
			return nil
		}
		return fmt.Errorf("push: load log %d: %w", logID, err)
	}
	if entry.Status != PushSendStatusQueued {
		log.Infof(ctx, "[PUSH] log %d already %s, skip", logID, entry.Status)
		return nil
	}
	if apnsClient == nil {
		markPushLogFailed(ctx, logID, ErrPushChannelUnavailable.Error())
		return nil
	}
	// 入队到投递之间（重试时可能是几小时）用户可能撤回了营销同意：投递前再查一次（4.5.4）。
	if entry.Category == string(PushCategoryMarketing) {
		var u User
		if err := db.Get().Select("id", "push_marketing_opt_in").First(&u, entry.UserID).Error; err != nil {
			return fmt.Errorf("push: load user %d: %w", entry.UserID, err)
		}
		if !u.PushMarketingOptIn {
			markPushLogFailed(ctx, logID, ErrPushNotConsented.Error()+" (withdrawn before delivery)")
			return nil
		}
	}

	tokens, err := activePushTokens(db.Get(), entry.UserID)
	if err != nil {
		return fmt.Errorf("push: query tokens: %w", err)
	}
	if len(tokens) == 0 {
		updatePushLog(ctx, logID, map[string]any{"status": PushSendStatusNoToken})
		return nil
	}

	var sent, failed, retryable int
	var lastErr string
	for i := range tokens {
		outcome, detail := deliverToToken(ctx, &tokens[i], &entry)
		switch outcome {
		case outcomeSent:
			sent++
		case outcomeRetryable, outcomeAuth:
			retryable++
			failed++
			lastErr = detail
		default:
			failed++
			lastErr = detail
		}
	}

	if sent == 0 && retryable > 0 {
		updatePushLog(ctx, logID, map[string]any{"last_error": truncate(lastErr, 512)})
		return fmt.Errorf("push: log %d: %d device(s) retryable: %s", logID, retryable, lastErr)
	}

	status := PushSendStatusSent
	if sent == 0 {
		status = PushSendStatusFailed
	}
	updatePushLog(ctx, logID, map[string]any{
		"status": status, "sent_count": sent, "fail_count": failed, "last_error": truncate(lastErr, 512),
	})
	log.Infof(ctx, "[PUSH] log=%d user=%d status=%s sent=%d failed=%d", logID, entry.UserID, status, sent, failed)
	return nil
}

// deliverToToken 投递到一个 token。生产环境返回 BadDeviceToken 时再试一次 sandbox：
// dev 构建拿到的是 sandbox token，客户端无法可靠判断自己的 APNs 环境，由服务端自动识别并记住。
func deliverToToken(ctx context.Context, tok *PushToken, entry *PushSendLog) (tokenOutcome, string) {
	sandbox := tok.Sandbox != nil && *tok.Sandbox
	n := apnsNotification{
		Token: tok.Token, Topic: tok.Topic, Sandbox: sandbox,
		Title: entry.Title, Body: entry.Body, Route: entry.Route, PushID: entry.ID,
	}
	res := apnsClient.Send(ctx, n)

	if !sandbox && res.isBadDeviceToken() {
		n.Sandbox = true
		alt := apnsClient.Send(ctx, n)
		if alt.ok() {
			if err := db.Get().Model(&PushToken{}).Where("id = ?", tok.ID).Update("sandbox", true).Error; err != nil {
				log.Warnf(ctx, "[PUSH] token %d: flip to sandbox failed: %v", tok.ID, err)
			}
			log.Infof(ctx, "[PUSH] token %d is a sandbox token, remembered", tok.ID)
			return outcomeSent, ""
		}
		// sandbox 那边没给结论（5xx / 网络）：不能据生产侧的 BadDeviceToken 停用，
		// 它可能正是个有效的 sandbox token。重试。
		if alt.isRetryable() {
			log.Warnf(ctx, "[PUSH] token %d sandbox retry inconclusive: %s", tok.ID, alt.describe())
			return outcomeRetryable, alt.describe()
		}
	}

	switch {
	case res.ok():
		return outcomeSent, ""
	case res.isDeadToken():
		if err := db.Get().Model(&PushToken{}).Where("id = ?", tok.ID).Update("status", PushTokenStatusInactive).Error; err != nil {
			log.Warnf(ctx, "[PUSH] token %d: deactivate failed: %v", tok.ID, err)
		}
		log.Infof(ctx, "[PUSH] token %d deactivated: %s", tok.ID, res.describe())
		return outcomeDead, res.describe()
	case res.isAuthError():
		msg := fmt.Sprintf("[PUSH-APNS-AUTH] APNs 拒绝了服务端凭据（%s），推送全部发不出去。检查 config.yml apns.team_id / key_id / p8，以及 App ID 是否开启了 Push Notifications。topic=%s",
			res.describe(), tok.Topic)
		log.Errorf(ctx, "%s", msg)
		if pushAuthAlertAllowed(time.Now()) {
			pushAlert(msg)
		}
		return outcomeAuth, res.describe()
	default:
		log.Warnf(ctx, "[PUSH] token %d retryable: %s", tok.ID, res.describe())
		return outcomeRetryable, res.describe()
	}
}

// pushAuthAlertInterval 凭据错误告警的最短间隔。凭据错一次就是全量发不出去，
// 一次群发会对每个 token 各撞一次——不限流就是几千条 Slack。
const pushAuthAlertInterval = 10 * time.Minute

var (
	pushAuthAlertMu   sync.Mutex
	pushAuthAlertLast time.Time
)

func pushAuthAlertAllowed(now time.Time) bool {
	pushAuthAlertMu.Lock()
	defer pushAuthAlertMu.Unlock()
	if !pushAuthAlertLast.IsZero() && now.Sub(pushAuthAlertLast) < pushAuthAlertInterval {
		return false
	}
	pushAuthAlertLast = now
	return true
}

func markPushLogFailed(ctx context.Context, logID uint64, reason string) {
	updatePushLog(ctx, logID, map[string]any{"status": PushSendStatusFailed, "last_error": truncate(reason, 512)})
}

func updatePushLog(ctx context.Context, logID uint64, updates map[string]any) {
	if err := db.Get().Model(&PushSendLog{}).Where("id = ?", logID).Updates(updates).Error; err != nil {
		log.Errorf(ctx, "[PUSH] update log %d failed: %v", logID, err)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
