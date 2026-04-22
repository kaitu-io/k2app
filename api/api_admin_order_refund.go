package center

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wordgate/qtoolkit/log"
)

// api_admin_refund_order 管理员发起订单退款（走 SubmitApproval）
func api_admin_refund_order(c *gin.Context) {
	orderUUID := c.Param("uuid")
	log.Infof(c, "admin request to refund order uuid=%s", orderUUID)

	var req RefundOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid refund request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// 预校验订单
	var order Order
	if err := getDB().Preload("User").Where(&Order{UUID: orderUUID}).First(&order).Error; err != nil {
		log.Warnf(c, "order not found: uuid=%s err=%v", orderUUID, err)
		Error(c, ErrorNotFound, "订单不存在")
		return
	}
	if order.IsPaid == nil || !*order.IsPaid {
		Error(c, ErrorInvalidOperation, "订单未支付，无法退款")
		return
	}
	if order.IsRefunded != nil && *order.IsRefunded {
		Error(c, ErrorConflict, "订单已退款")
		return
	}

	operatorID := ReqUserID(c)
	userIdent := ""
	if order.User != nil {
		userIdent = order.User.UUID
	}
	summary := fmt.Sprintf("退款订单 %s（¥%.2f，用户 %s，原因：%s）",
		order.UUID, float64(order.PayAmount)/100.0, userIdent, req.Reason)

	approvalID, executed, err := SubmitApproval(c, "order_refund", orderRefundApprovalParams{
		OrderID:    order.ID,
		Reason:     req.Reason,
		OperatorID: operatorID,
	}, summary)
	if err != nil {
		log.Errorf(c, "submit order_refund approval failed: %v", err)
		Error(c, ErrorSystemError, err.Error())
		return
	}

	if !executed {
		PendingApproval(c, approvalID)
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
