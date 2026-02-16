package center

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_refund_order 管理员退款订单
func api_admin_refund_order(c *gin.Context) {
	log.Infof(c, "admin request to refund order")

	// 获取订单ID
	orderIDStr := c.Param("id")
	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "无效的订单ID")
		return
	}

	// 解析请求参数
	var req RefundOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid refund order request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 执行退款流程
	if err := ProcessOrderRefund(c, orderID, req.Reason); err != nil {
		log.Errorf(c, "退款订单失败: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}

// api_admin_trigger_order_cashback 管理员手动触发订单返现
func api_admin_trigger_order_cashback(c *gin.Context) {
	log.Infof(c, "admin request to trigger order cashback")

	// 获取订单ID
	orderIDStr := c.Param("id")
	orderID, err := strconv.ParseUint(orderIDStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "无效的订单ID")
		return
	}

	// 执行返现流程
	if err := ProcessOrderCashback(c, orderID); err != nil {
		log.Errorf(c, "触发订单返现失败: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	SuccessEmpty(c)
}
