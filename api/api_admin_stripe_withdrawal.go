package center

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// 14 天撤回的后台入口（spec 2026-10-07 A 期 §3.6）。/app 组是 AdminRequired，超管请求同步执行；
// 审批行与 order_refund 一样是留痕。客服流程见 docs/customer-service/README.md。

// StripeWithdrawalRequest 发起 / 续跑撤回。
type StripeWithdrawalRequest struct {
	NoticeAt       int64  `json:"noticeAt" binding:"required"` // 用户提出撤回的时刻（unix 秒）
	SubscriptionID string `json:"subscriptionId"`              // 可选：Stripe 订阅 id；缺省取用户最新一条非 revoked 的
	Mode           string `json:"mode" binding:"required"`     // withdrawal | termination（条款 8.3）
	Reason         string `json:"reason"`
	RequestID      string `json:"requestId"` // 可选：显式续跑一个未完成请求
}

// StripeWithdrawalAbandonRequest 作废一个卡住的请求。
type StripeWithdrawalAbandonRequest struct {
	Reason string `json:"reason" binding:"required,min=2"`
}

func adminWithdrawalUser(c *gin.Context) (*User, bool) {
	var u User
	if err := getDB().Where(&User{UUID: c.Param("uuid")}).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
		} else {
			Error(c, ErrorSystemError, "query user failed")
		}
		return nil, false
	}
	if !Brand(u.Brand).Config().AllowsPayment(PayChannelStripe) {
		Error(c, ErrorNotSupported, "this user's brand does not bill through Stripe")
		return nil, false
	}
	return &u, true
}

// api_admin_stripe_withdrawal_quote GET /app/users/:uuid/stripe-withdrawal?notice_at=&mode=&subscription_id=
// 只读预览：计划条目、天数、金额。不写库。
func api_admin_stripe_withdrawal_quote(c *gin.Context) {
	u, ok := adminWithdrawalUser(c)
	if !ok {
		return
	}
	noticeAt, _ := strconv.ParseInt(c.Query("notice_at"), 10, 64)
	mode := c.DefaultQuery("mode", withdrawModeWithdrawal)
	req := &withdrawalRequest{UserID: u.ID, SubscriptionID: c.Query("subscription_id"), NoticeAt: noticeAt, Mode: mode}
	if err := validateWithdrawalRequest(req, time.Now().Unix()); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	plan, err := buildWithdrawalPlan(c, req)
	if err != nil {
		if plan != nil { // 不合格：把判定细节一起给客服
			c.JSON(200, Response[withdrawalPlan]{Code: ErrorInvalidOperation, Message: err.Error(), Data: plan})
			return
		}
		Error(c, ErrorInvalidOperation, err.Error())
		return
	}
	Success(c, plan)
}

type stripeWithdrawalApprovalParams struct {
	UserID         uint64 `json:"userId"`
	SubscriptionID string `json:"subscriptionId,omitempty"`
	NoticeAt       int64  `json:"noticeAt"`
	Mode           string `json:"mode"`
	Reason         string `json:"reason,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
	OperatorID     uint64 `json:"operatorId"`
}

// api_admin_stripe_withdrawal POST /app/users/:uuid/stripe-withdrawal：发起或续跑撤回（走审批）。
func api_admin_stripe_withdrawal(c *gin.Context) {
	u, ok := adminWithdrawalUser(c)
	if !ok {
		return
	}
	var req StripeWithdrawalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	params := stripeWithdrawalApprovalParams{
		UserID: u.ID, SubscriptionID: req.SubscriptionID, NoticeAt: req.NoticeAt, Mode: req.Mode,
		Reason: req.Reason, RequestID: req.RequestID, OperatorID: ReqUserID(c),
	}
	if req.RequestID == "" {
		// 新请求先校验参数，坏参数不建审批单（续跑沿用原请求的参数，不校验）。
		if err := validateWithdrawalRequest(&withdrawalRequest{NoticeAt: req.NoticeAt, Mode: req.Mode}, time.Now().Unix()); err != nil {
			Error(c, ErrorInvalidArgument, err.Error())
			return
		}
	}
	summary := fmt.Sprintf("Stripe 撤回退款：用户 %s，mode=%s，notice_at=%s，原因：%s",
		u.UUID, req.Mode, time.Unix(req.NoticeAt, 0).UTC().Format(time.RFC3339), req.Reason)
	if req.RequestID != "" {
		summary = fmt.Sprintf("Stripe 撤回续跑：用户 %s，请求 %s", u.UUID, req.RequestID)
	}
	approvalID, executed, err := SubmitApproval(c, "stripe_withdrawal", params, summary)
	if err != nil {
		log.Warnf(c, "stripe withdrawal for user %d failed: %v", u.ID, err)
		Error(c, ErrorInvalidOperation, err.Error())
		return
	}
	if !executed {
		PendingApproval(c, approvalID)
		return
	}
	// 回最新请求的各行（金额、退款 id、备注），客服据此回邮件。
	var latest StatutoryRefund
	if err := getDB().Where("user_id = ?", u.ID).Order("id DESC").First(&latest).Error; err != nil {
		SuccessEmpty(c)
		return
	}
	rows, _ := loadWithdrawalRows(latest.RequestID)
	Success(c, &withdrawalResult{RequestID: latest.RequestID, Rows: rows})
}

// api_admin_stripe_withdrawal_abandon POST /app/stripe-withdrawals/:request_id/abandon
func api_admin_stripe_withdrawal_abandon(c *gin.Context) {
	var req StripeWithdrawalAbandonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	rep, err := abandonStripeWithdrawal(c, c.Param("request_id"), ReqUserID(c), req.Reason)
	if err != nil {
		Error(c, ErrorInvalidOperation, err.Error())
		return
	}
	Success(c, rep)
}

func executeApprovalStripeWithdrawal(ctx context.Context, params json.RawMessage) error {
	var p stripeWithdrawalApprovalParams
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("unmarshal params: %w", err)
	}
	_, err := executeStripeWithdrawal(ctx, &withdrawalRequest{
		UserID: p.UserID, SubscriptionID: p.SubscriptionID, NoticeAt: p.NoticeAt, Mode: p.Mode,
		RequestID: p.RequestID, OperatorID: p.OperatorID, Source: "admin", Reason: p.Reason,
	})
	return err
}
