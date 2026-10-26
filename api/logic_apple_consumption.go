package center

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/wordgate/qtoolkit/appstore"
	"github.com/wordgate/qtoolkit/log"
)

// Apple 退款 Consumption 应答（spec 2026-10-08 §5；退款倾向 2026-10-26 David 定：两品牌一律
// GRANT_PRORATED，与用户协议「按剩余比例退款」一致，不算用量、不分品牌）。
//
// 用户发起退款时 Apple 发 CONSUMPTION_REQUEST；只有用户明确同意过（users.apple_consumption_consent
// = true）才调用 Send Consumption Information v2，否则按 Apple 要求不应答。

const (
	appleRefundPreferenceProrated = "GRANT_PRORATED"
	appleDeliveryStatusDelivered  = "DELIVERED"
)

// appleConsumptionRequest 是 Apple ConsumptionRequest（v2）的请求体。必填：customerConsented /
// deliveryStatus / sampleContentProvided。**不发 consumptionPercentage**：自动续订订阅选
// GRANT_PRORATED 时带它 Apple 返回 400，比例由 Apple 按剩余时间自己算。
type appleConsumptionRequest struct {
	CustomerConsented     bool   `json:"customerConsented"`
	DeliveryStatus        string `json:"deliveryStatus"`
	RefundPreference      string `json:"refundPreference,omitempty"`
	SampleContentProvided bool   `json:"sampleContentProvided"`
}

// appleConsumptionBaseURL 按 sandbox 选主机（var 供测试替换）。
var appleConsumptionBaseURL = map[bool]string{
	false: "https://api.storekit.apple.com",
	true:  "https://api.storekit-sandbox.apple.com",
}

var appleConsumptionHTTPClient = &http.Client{Timeout: 10 * time.Second}

// respondAppleConsumption 对一笔 CONSUMPTION_REQUEST 应答。没同意 → 不调用 Apple，返回 nil。
// 返回 error 表示发送失败（网络 / 非 2xx），调用方应让 Apple 重投。
func respondAppleConsumption(ctx context.Context, user *User, sub *Subscription, txnID, bundleID string, sandbox bool) error {
	if user.AppleConsumptionConsent == nil || !*user.AppleConsumptionConsent {
		log.Infof(ctx, "[APPLE-CONSUMPTION] skip no-consent user=%d txn=%s", user.ID, txnID)
		return nil
	}

	body, err := json.Marshal(appleConsumptionRequest{
		CustomerConsented: true,
		DeliveryStatus:    appleDeliveryStatusDelivered,
		RefundPreference:  appleRefundPreferenceProrated,
		// 试用链转正后只清 is_trial，trial_ends_at 保留——它非 0 即这条链给过免费试用。
		SampleContentProvided: sub.TrialEndsAt != 0,
	})
	if err != nil {
		return fmt.Errorf("marshal consumption request: %w", err)
	}

	token, err := appstore.GenerateJwtToken(bundleID)
	if err != nil {
		return fmt.Errorf("generate App Store Server API token: %w", err)
	}

	url := fmt.Sprintf("%s/inApps/v2/transactions/consumption/%s", appleConsumptionBaseURL[sandbox], txnID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build consumption request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := appleConsumptionHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send consumption info txn=%s: %w", txnID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("send consumption info txn=%s: Apple returned %d: %s", txnID, resp.StatusCode, snippet)
	}
	log.Infof(ctx, "[APPLE-CONSUMPTION] sent user=%d txn=%s sandbox=%v status=%d sample=%v",
		user.ID, txnID, sandbox, resp.StatusCode, sub.TrialEndsAt != 0)
	return nil
}
