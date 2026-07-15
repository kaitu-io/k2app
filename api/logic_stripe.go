package center

import (
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
)

// stripeSubStatus 把 Stripe subscription.status 映射到本仓库 Subscription.Status 词表
// （active|grace|billing_retry|expired|revoked）。纯函数。
// 返回 "" 表示"不改"（未知/不适用状态）。绝不返回 "revoked"——revoked 专属退款语义，
// 由人工/后续 admin 流程落地，webhook 状态同步永不触碰（对标 deriveVerifiedStatus 的 terminal 规则）。
func stripeSubStatus(s stripe.SubscriptionStatus) string {
	switch s {
	case stripe.SubscriptionStatusActive, stripe.SubscriptionStatusTrialing:
		return "active"
	case stripe.SubscriptionStatusPastDue:
		return "billing_retry"
	case stripe.SubscriptionStatusCanceled, stripe.SubscriptionStatusUnpaid,
		stripe.SubscriptionStatusIncompleteExpired, stripe.SubscriptionStatusPaused:
		return "expired"
	default:
		return ""
	}
}

// stripeInvoiceFacts 是入账所需字段的规范载体。extractStripeInvoiceFacts 是从
// stripe.Invoice 提取它的唯一适配点——隔离 SDK/API 版本形态差异（basil 把
// subscription 挪进 invoice.parent.subscription_details）。升级 stripe-go 只改这里。
type stripeInvoiceFacts struct {
	InvoiceID      string
	SubscriptionID string
	CustomerID     string
	UserUUID       string // subscription_data.metadata.user_uuid（checkout 创建时烘焙）
	PlanPID        string // subscription_data.metadata.plan_pid
	PeriodStart    int64  // unix 秒（invoice line period；basil 后订阅级 period 已删）
	PeriodEnd      int64  // unix 秒
	Livemode       bool
}

func extractStripeInvoiceFacts(inv *stripe.Invoice) (*stripeInvoiceFacts, error) {
	if inv == nil || inv.ID == "" {
		return nil, fmt.Errorf("nil or empty invoice")
	}
	f := &stripeInvoiceFacts{InvoiceID: inv.ID, Livemode: inv.Livemode}
	if inv.Customer != nil {
		f.CustomerID = inv.Customer.ID
	}
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
		sd := inv.Parent.SubscriptionDetails
		if sd.Subscription != nil {
			f.SubscriptionID = sd.Subscription.ID
		}
		f.UserUUID = sd.Metadata["user_uuid"]
		f.PlanPID = sd.Metadata["plan_pid"]
	}
	if f.SubscriptionID == "" {
		return nil, fmt.Errorf("invoice %s has no parent subscription (not a subscription invoice)", inv.ID)
	}
	if inv.Lines != nil {
		for _, line := range inv.Lines.Data {
			if line == nil || line.Period == nil {
				continue
			}
			if line.Period.End > f.PeriodEnd {
				f.PeriodEnd = line.Period.End
				f.PeriodStart = line.Period.Start
			}
		}
	}
	if f.PeriodEnd == 0 {
		return nil, fmt.Errorf("invoice %s has no line period", inv.ID)
	}
	return f, nil
}
