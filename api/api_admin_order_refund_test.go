package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// TestAdmin_RefundOrder_InvalidReason: reason too short (1 char, fails min=2) → ErrorInvalidArgument.
// Validation fires before any DB access; no mock setup required.
func TestAdmin_RefundOrder_InvalidReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)

	body, _ := json.Marshal(map[string]string{"reason": "a"}) // 1 char, fails min=2
	req := httptest.NewRequest(http.MethodPost, "/app/orders/ord-test-1/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code) // HTTP 200, error in body code field
	require.True(t, strings.Contains(w.Body.String(), "min"),
		"body should mention min validation: %s", w.Body.String())
}

// TestAdmin_RefundOrder_OrderNotFound: UUID lookup returns no rows → ErrorNotFound + "订单不存在".
// Uses the same getDB swap pattern as logic_order_refund_test.go.
func TestAdmin_RefundOrder_OrderNotFound(t *testing.T) {
	m := SetupMockDB(t)

	// Swap getDB to point at mock; restore in Cleanup
	orig := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = orig })

	// GORM Preload("User").Where(&Order{UUID:"ord-missing"}).First(&order) produces:
	// SELECT * FROM `orders` WHERE `orders`.`uuid` = ? ORDER BY `orders`.`id` LIMIT ?
	// returning no rows → ErrRecordNotFound → handler returns ErrorNotFound
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE`).
		WithArgs("ord-missing", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)

	body, _ := json.Marshal(map[string]string{"reason": "valid test reason"})
	req := httptest.NewRequest(http.MethodPost, "/app/orders/ord-missing/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "订单不存在")
	m.ExpectationsWereMet(t)
}

// TestAdmin_RefundOrder_OrderNotPaid: order exists but IsPaid=false → ErrorInvalidOperation + "订单未支付".
func TestAdmin_RefundOrder_OrderNotPaid(t *testing.T) {
	m := SetupMockDB(t)

	origGetDB := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = origGetDB })

	isPaid := false
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE`).
		WithArgs("ord-unpaid", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "is_paid"}).
			AddRow(uint64(1), "ord-unpaid", uint64(100), &isPaid))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)

	body, _ := json.Marshal(map[string]string{"reason": "valid reason"})
	req := httptest.NewRequest(http.MethodPost, "/app/orders/ord-unpaid/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "订单未支付")
	m.ExpectationsWereMet(t)
}

// TestAdmin_RefundOrder_AlreadyRefunded: order exists, IsPaid=true, IsRefunded=true → ErrorConflict + "订单已退款".
func TestAdmin_RefundOrder_AlreadyRefunded(t *testing.T) {
	m := SetupMockDB(t)

	origGetDB := getDB
	getDB = func() *gorm.DB { return m.DB }
	t.Cleanup(func() { getDB = origGetDB })

	isPaid := true
	isRefunded := true
	m.Mock.ExpectQuery(`SELECT \* FROM .orders. WHERE`).
		WithArgs("ord-refunded", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "uuid", "user_id", "is_paid", "is_refunded"}).
			AddRow(uint64(1), "ord-refunded", uint64(100), &isPaid, &isRefunded))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)

	body, _ := json.Marshal(map[string]string{"reason": "valid reason"})
	req := httptest.NewRequest(http.MethodPost, "/app/orders/ord-refunded/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "订单已退款")
	m.ExpectationsWereMet(t)
}

// injectSuperAdmin returns a middleware that sets the authContext for a superadmin user.
func injectSuperAdmin(userID uint64, uuid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authContext", &authContext{
			UserID: userID,
			User:   &User{ID: userID, UUID: uuid, IsAdmin: BoolPtr(true)},
		})
		c.Next()
	}
}

// injectRegularAdmin returns a middleware that sets the authContext for a non-superadmin user.
func injectRegularAdmin(userID uint64, uuid string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authContext", &authContext{
			UserID: userID,
			User:   &User{ID: userID, UUID: uuid, IsAdmin: BoolPtr(false)},
		})
		c.Next()
	}
}

// TestAdmin_RefundOrder_Success_SuperAdmin: superadmin happy path through handler.
// Requires a real DB (config.yml) because SubmitApproval uses db.Get() for the
// AdminApproval INSERT — qtoolkit db.Get() is a sync.Once global with no Set().
// Uses skipIfNoConfig to skip in CI without config.
func TestAdmin_RefundOrder_Success_SuperAdmin(t *testing.T) {
	skipIfNoConfig(t)

	// Ensure order_refund callback is registered (normally done by InitWorker).
	RegisterApprovalCallback("order_refund", executeApprovalOrderRefund)

	now := time.Now()

	// Seed: user + paid unfunded order + VipPurchase history
	user := User{
		UUID:             "usr-handler-superadmin-" + now.Format("20060102150405"),
		ExpiredAt:        now.Add(365 * 24 * time.Hour).Unix(),
		IsFirstOrderDone: BoolPtr(true),
	}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	isPaid := true
	paidAt := now.Add(-5 * 24 * time.Hour)
	order := Order{
		UUID:      "ord-handler-superadmin-" + now.Format("20060102150405"),
		Title:     "Test Handler SuperAdmin",
		UserID:    user.ID,
		PayAmount: 4900,
		IsPaid:    &isPaid,
		PaidAt:    &paidAt,
	}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	history := UserProHistory{
		UserID:      user.ID,
		Type:        VipPurchase,
		ReferenceID: order.ID,
		Days:        365,
		Reason:      "订单支付 - " + order.UUID,
	}
	require.NoError(t, db.Get().Create(&history).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&history) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectSuperAdmin(999, "superadmin-test-uuid"))
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)

	body, _ := json.Marshal(map[string]string{"reason": "superadmin 直通退款测试"})
	req := httptest.NewRequest(http.MethodPost, "/app/orders/"+order.UUID+"/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	// Superadmin path: executed synchronously → SuccessEmpty → code=0
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(0), resp["code"], "superadmin should get code=0 (executed), got body: %s", w.Body.String())

	// Verify AdminApproval row was inserted with status=executed
	var approval AdminApproval
	require.NoError(t, db.Get().Where("requestor_uuid = ? AND action = ?", "superadmin-test-uuid", "order_refund").
		First(&approval).Error)
	require.Equal(t, "executed", approval.Status)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&approval) })

	// Verify order is marked refunded
	var refreshed Order
	require.NoError(t, db.Get().First(&refreshed, order.ID).Error)
	require.NotNil(t, refreshed.IsRefunded)
	require.True(t, *refreshed.IsRefunded, "order must be refunded")

	// Cleanup wallet + wallet_changes created by ProcessOrderRefund
	t.Cleanup(func() {
		var wallet Wallet
		if db.Get().Where(&Wallet{UserID: user.ID}).First(&wallet).Error == nil {
			db.Get().Unscoped().Where("wallet_id = ?", wallet.ID).Delete(&WalletChange{})
			db.Get().Unscoped().Delete(&wallet)
		}
		db.Get().Unscoped().Where("user_id = ? AND type = ?", user.ID, VipRefund).Delete(&UserProHistory{})
	})
}

// TestAdmin_RefundOrder_Success_RegularAdmin: non-superadmin creates a pending approval.
// SubmitApproval returns (approvalID, false, nil) → handler returns PendingApproval (code=202).
// Requires config.yml for the same db.Get() reason as superadmin test.
func TestAdmin_RefundOrder_Success_RegularAdmin(t *testing.T) {
	skipIfNoConfig(t)

	// Ensure order_refund callback is registered.
	RegisterApprovalCallback("order_refund", executeApprovalOrderRefund)

	now := time.Now()

	// Seed: paid, unfunded order
	user := User{UUID: "usr-handler-regadmin-" + now.Format("20060102150405")}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	isPaid := true
	paidAt := now.Add(-3 * 24 * time.Hour)
	order := Order{
		UUID:      "ord-handler-regadmin-" + now.Format("20060102150405"),
		Title:     "Test Handler RegAdmin",
		UserID:    user.ID,
		PayAmount: 1900,
		IsPaid:    &isPaid,
		PaidAt:    &paidAt,
	}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(injectRegularAdmin(500, "regular-admin-uuid"))
	r.POST("/app/orders/:uuid/refund", api_admin_refund_order)

	body, _ := json.Marshal(map[string]string{"reason": "regular admin 待审批退款测试"})
	req := httptest.NewRequest(http.MethodPost, "/app/orders/"+order.UUID+"/refund", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	// Regular admin path: pending → PendingApproval → code=202
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(202), resp["code"], "regular admin should get code=202 (pending), got body: %s", w.Body.String())

	// Verify AdminApproval row exists with status=pending
	var approval AdminApproval
	require.NoError(t, db.Get().Where("requestor_uuid = ? AND action = ?", "regular-admin-uuid", "order_refund").
		First(&approval).Error)
	require.Equal(t, "pending", approval.Status)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&approval) })

	// Verify approvalId is returned in body data
	data, ok := resp["data"].(map[string]any)
	require.True(t, ok, "data should be an object")
	approvalID, ok := data["approvalId"]
	require.True(t, ok, "data.approvalId should be present")
	require.NotZero(t, approvalID, "approvalId must be non-zero")

	// Order must NOT be refunded (callback not executed)
	var refreshed Order
	require.NoError(t, db.Get().First(&refreshed, order.ID).Error)
	require.True(t, refreshed.IsRefunded == nil || !*refreshed.IsRefunded,
		"order must NOT be refunded when approval is pending")
}

// TestAdmin_RefundOrder_ApprovalReValidation: exercises executeApprovalOrderRefund's
// re-check — order is marked refunded in the DB before the callback runs, simulating
// a race between handler pre-check and callback execution.
// Tests the defense-in-depth re-validation in executeApprovalOrderRefund directly.
// Requires config.yml because the callback uses db.Get() to re-read the order.
func TestAdmin_RefundOrder_ApprovalReValidation(t *testing.T) {
	skipIfNoConfig(t)

	now := time.Now()

	// Seed: user + order that is paid but will be refunded before callback runs.
	user := User{UUID: "usr-handler-revalidate-" + now.Format("20060102150405")}
	require.NoError(t, db.Get().Create(&user).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&user) })

	isPaid := true
	paidAt := now.Add(-2 * 24 * time.Hour)
	order := Order{
		UUID:      "ord-handler-revalidate-" + now.Format("20060102150405"),
		Title:     "Test ReValidation",
		UserID:    user.ID,
		PayAmount: 2900,
		IsPaid:    &isPaid,
		PaidAt:    &paidAt,
	}
	require.NoError(t, db.Get().Create(&order).Error)
	t.Cleanup(func() { db.Get().Unscoped().Delete(&order) })

	// Simulate race: mark order as refunded in DB while callback hasn't run yet.
	refundedAt := now
	isRefundedTrue := true
	require.NoError(t, db.Get().Model(&order).Updates(map[string]any{
		"is_refunded":   isRefundedTrue,
		"refunded_at":   refundedAt,
		"refund_amount": order.PayAmount,
		"refund_reason": "concurrent refund",
	}).Error)

	// Call executeApprovalOrderRefund directly with the order ID.
	// The callback re-reads from DB and must detect is_refunded=true → return error.
	params, _ := json.Marshal(orderRefundApprovalParams{
		OrderID:    order.ID,
		Reason:     "revalidation test",
		OperatorID: uint64(999),
	})
	err := executeApprovalOrderRefund(t.Context(), params)
	require.Error(t, err, "callback must fail when order is already refunded at execution time")
	require.Contains(t, err.Error(), "已退款", "error must mention 已退款")
}
