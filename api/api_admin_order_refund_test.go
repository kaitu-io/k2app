package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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
