package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// =====================================================================
// 后台手动群发推送（spec 2026-11-03-ios-push-design §6.5）
// =====================================================================
//
// 只能按营销类发：请求里没有 category 字段（传了也被忽略），管理员无法把营销内容伪装成交易类、
// 绕过用户的营销同意。交易类推送只由代码里的触发点发出。
// 收件人是明确的 userIds 列表（与 EDM 一致，由调用方挑人群）；真正发送走审批流 push_send。

const (
	TaskTypePushBatch = "push:batch"
	// adminPushMaxUsers / adminPushMaxParamsBytes：整个请求经 SubmitApproval 存进
	// admin_approvals.params（MySQL TEXT，65535 字节）。超了严格模式 INSERT 直接报错，
	// 非严格模式截断后审批执行时反序列化失败——所以在入口按序列化后的字节数拦。
	// 更大的人群由调用方拆成多个 batchId。
	adminPushMaxUsers       = 5000
	adminPushMaxParamsBytes = 60000
	adminPushMaxBatchIDLen  = 100 // dedup_key = "push_batch:" + batchId + ":" + uid ≤ 191
	adminPushMaxTitleLength = 100
	adminPushMaxBodyLength  = 500
)

// adminPushBatchIDPattern batchId 进 URL 路径（GET /push/batches/:batchId）和 dedup_key，只允许安全字符。
var adminPushBatchIDPattern = regexp.MustCompile(`^[A-Za-z0-9:_.-]+$`)

// AdminPushCopy 某语言的标题与正文
type AdminPushCopy struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// AdminPushSendRequest POST /app/push/send
type AdminPushSendRequest struct {
	BatchID string                   `json:"batchId"`
	Brand   string                   `json:"brand"`
	UserIDs []uint64                 `json:"userIds"`
	Copy    map[string]AdminPushCopy `json:"copy"` // key: 语言（zh-CN / zh-TW / en-US / ja …）
	Route   string                   `json:"route"`
	DryRun  bool                     `json:"dryRun"`
}

// AdminPushDryRunResult 试算：请求了多少人、属于该品牌多少、其中有有效 token 多少、其中已同意营销多少。
type AdminPushDryRunResult struct {
	Requested int `json:"requested"`
	Matched   int `json:"matched"`
	WithToken int `json:"withToken"`
	Consented int `json:"consented"`
}

// AdminPushBatchStats GET /app/push/batches/:batchId —— 按 push_send_logs 汇总
type AdminPushBatchStats struct {
	BatchID string `json:"batchId"`
	Total   int64  `json:"total"`
	Queued  int64  `json:"queued"`
	Sent    int64  `json:"sent"`
	Failed  int64  `json:"failed"`
	NoToken int64  `json:"noToken"`
}

func (r *AdminPushSendRequest) validate() error {
	r.BatchID = strings.TrimSpace(r.BatchID)
	if r.BatchID == "" || len(r.BatchID) > adminPushMaxBatchIDLen {
		return fmt.Errorf("batchId must be 1..%d chars", adminPushMaxBatchIDLen)
	}
	if !adminPushBatchIDPattern.MatchString(r.BatchID) {
		return errors.New("batchId may only contain letters, digits and : _ . -")
	}
	b, err := BrandRequired(r.Brand)
	if err != nil {
		return err
	}
	r.Brand = string(b)
	if len(r.UserIDs) == 0 || len(r.UserIDs) > adminPushMaxUsers {
		return fmt.Errorf("userIds must have 1..%d entries", adminPushMaxUsers)
	}
	if len(r.Copy) == 0 {
		return errors.New("copy is required")
	}
	for lang, c := range r.Copy {
		if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Body) == "" {
			return fmt.Errorf("copy[%s] needs both title and body", lang)
		}
		if len([]rune(c.Title)) > adminPushMaxTitleLength || len([]rune(c.Body)) > adminPushMaxBodyLength {
			return fmt.Errorf("copy[%s] too long (title ≤ %d, body ≤ %d chars)", lang, adminPushMaxTitleLength, adminPushMaxBodyLength)
		}
	}
	if r.Route != "" && !pushRouteWhitelist[r.Route] {
		return fmt.Errorf("route %q not allowed", r.Route)
	}
	if b, err := json.Marshal(r); err != nil {
		return fmt.Errorf("encode request: %w", err)
	} else if len(b) > adminPushMaxParamsBytes {
		return fmt.Errorf("request too large (%d bytes, max %d): split userIds into several batches or shorten copy", len(b), adminPushMaxParamsBytes)
	}
	return nil
}

// pickAdminCopy 选文案：用户语言原样 → 归一后的语言 → 品牌默认语言 → 按 key 排序的第一个。
func pickAdminCopy(copies map[string]AdminPushCopy, userLang string, brand Brand) AdminPushCopy {
	if c, ok := copies[userLang]; ok {
		return c
	}
	if c, ok := copies[pushCopyLang(userLang, brand)]; ok {
		return c
	}
	if c, ok := copies[pushCopyLang("", brand)]; ok {
		return c
	}
	keys := make([]string, 0, len(copies))
	for k := range copies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return copies[keys[0]]
}

// api_admin_push_send POST /app/push/send
func api_admin_push_send(c *gin.Context) {
	var req AdminPushSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := req.validate(); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if req.DryRun {
		res, err := adminPushDryRun(&req)
		if err != nil {
			log.Errorf(c, "push dry run: %v", err)
			Error(c, ErrorSystemError, "dry run failed")
			return
		}
		Success(c, res)
		return
	}

	summary := fmt.Sprintf("发送营销推送：%d 人，品牌：%s，批次：%s，跳转：%s，标题：%s",
		len(req.UserIDs), req.Brand, req.BatchID, req.Route, firstTitle(req.Copy))
	approvalID, executed, err := SubmitApproval(c, "push_send", &req, summary)
	if err != nil {
		log.Errorf(c, "submit push_send approval: %v", err)
		Error(c, ErrorSystemError, "failed to submit approval")
		return
	}
	if !executed {
		PendingApproval(c, approvalID)
		return
	}
	SuccessEmpty(c)
}

func firstTitle(copies map[string]AdminPushCopy) string {
	keys := make([]string, 0, len(copies))
	for k := range copies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return copies[keys[0]].Title
}

// adminPushAudience 返回请求里属于该品牌的用户（id、语言、营销同意）。
func adminPushAudience(req *AdminPushSendRequest) ([]User, error) {
	var users []User
	err := db.Get().Select("id", "brand", "language", "push_marketing_opt_in").
		Where("id IN ? AND brand = ?", req.UserIDs, req.Brand).Find(&users).Error
	return users, err
}

func adminPushDryRun(req *AdminPushSendRequest) (*AdminPushDryRunResult, error) {
	users, err := adminPushAudience(req)
	if err != nil {
		return nil, err
	}
	res := &AdminPushDryRunResult{Requested: len(req.UserIDs), Matched: len(users)}
	if len(users) == 0 {
		return res, nil
	}
	ids := make([]uint64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	// 与 activePushTokens 同一个设备归属 JOIN，一次查完整批，避免逐人查询拖垮 HTTP 请求。
	var withToken []uint64
	if err := db.Get().Model(&PushToken{}).
		Joins("JOIN devices ON devices.udid = push_tokens.device_udid AND devices.user_id = push_tokens.user_id").
		Where("push_tokens.user_id IN ? AND push_tokens.provider = ? AND push_tokens.status = ?",
			ids, PushProviderAPNs, PushTokenStatusActive).
		Distinct().Pluck("push_tokens.user_id", &withToken).Error; err != nil {
		return nil, err
	}
	has := make(map[uint64]bool, len(withToken))
	for _, id := range withToken {
		has[id] = true
	}
	for _, u := range users {
		if !has[u.ID] {
			continue
		}
		res.WithToken++
		if u.PushMarketingOptIn {
			res.Consented++
		}
	}
	return res, nil
}

// executeApprovalPushSend 审批通过（或超管直通）后的执行：入队一个批次任务，逐人发送在 worker 里跑。
func executeApprovalPushSend(ctx context.Context, params json.RawMessage) error {
	var req AdminPushSendRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}
	if err := req.validate(); err != nil {
		return fmt.Errorf("invalid push_send params: %w", err)
	}
	return enqueuePushBatch(ctx, req)
}

// enqueuePushBatch 包级变量，测试替换为同步执行。
var enqueuePushBatch = func(ctx context.Context, req AdminPushSendRequest) error {
	_, err := asynq.EnqueueContext(ctx, TaskTypePushBatch, req)
	return err
}

func handlePushBatchTask(ctx context.Context, payload []byte) error {
	var req AdminPushSendRequest
	if err := asynq.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("push batch: unmarshal: %w", err)
	}
	return runAdminPushBatch(ctx, req)
}

// runAdminPushBatch 逐人发送。重跑安全：DedupKey = push_batch:<batchId>:<uid>，
// 已入队的人在重跑时得到 ErrPushDuplicate 被跳过。返回 error 让 asynq 重试整批：
// 被中断（任务超时 / 进程退出）或有用户撞上非预期错误（入队失败会释放 key）时，重跑只补发漏掉的人。
func runAdminPushBatch(ctx context.Context, req AdminPushSendRequest) error {
	users, err := adminPushAudience(&req)
	if err != nil {
		return fmt.Errorf("push batch %s: load audience: %w", req.BatchID, err)
	}
	brand := Brand(req.Brand)
	counts := map[string]int{}
	for _, u := range users {
		if err := ctx.Err(); err != nil {
			log.Warnf(ctx, "[PUSH-BATCH] %s interrupted after %v: %v", req.BatchID, counts, err)
			return fmt.Errorf("push batch %s interrupted: %w", req.BatchID, err)
		}
		c := pickAdminCopy(req.Copy, u.Language, brand)
		err := PushToUser(ctx, u.ID, PushRequest{
			Category: PushCategoryMarketing,
			DedupKey: fmt.Sprintf("push_batch:%s:%d", req.BatchID, u.ID),
			Title:    c.Title, Body: c.Body, Route: req.Route, BatchID: req.BatchID,
		})
		switch {
		case err == nil:
			counts["queued"]++
		case errors.Is(err, ErrPushNotConsented):
			counts["not_consented"]++
		case errors.Is(err, ErrPushNoActiveToken):
			counts["no_token"]++
		case errors.Is(err, ErrPushDuplicate):
			counts["duplicate"]++
		case errors.Is(err, ErrPushChannelUnavailable):
			log.Errorf(ctx, "[PUSH-BATCH] %s aborted: %v", req.BatchID, err)
			return nil
		default:
			counts["error"]++
			log.Warnf(ctx, "[PUSH-BATCH] %s user=%d: %v", req.BatchID, u.ID, err)
		}
	}
	log.Infof(ctx, "[PUSH-BATCH] %s done: requested=%d matched=%d %v", req.BatchID, len(req.UserIDs), len(users), counts)
	if counts["error"] > 0 {
		return fmt.Errorf("push batch %s: %d user(s) failed, retrying the rest", req.BatchID, counts["error"])
	}
	return nil
}

// api_admin_push_batch_stats GET /app/push/batches/:batchId
func api_admin_push_batch_stats(c *gin.Context) {
	batchID := c.Param("batchId")
	type row struct {
		Status PushSendStatus
		N      int64
	}
	var rows []row
	if err := db.Get().Model(&PushSendLog{}).Select("status, COUNT(*) AS n").
		Where("batch_id = ?", batchID).Group("status").Scan(&rows).Error; err != nil {
		log.Errorf(c, "push batch stats %s: %v", batchID, err)
		Error(c, ErrorSystemError, "failed to load batch stats")
		return
	}
	stats := AdminPushBatchStats{BatchID: batchID}
	for _, r := range rows {
		stats.Total += r.N
		switch r.Status {
		case PushSendStatusQueued:
			stats.Queued = r.N
		case PushSendStatusSent:
			stats.Sent = r.N
		case PushSendStatusFailed:
			stats.Failed = r.N
		case PushSendStatusNoToken:
			stats.NoToken = r.N
		}
	}
	Success(c, &stats)
}
