package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

func parseNodeOperationID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid operation id")
		return 0, false
	}
	return id, true
}

// adminActorTag 解析当前操作管理员的身份标签,写入 NodeOperation.CreatedBy 做审计。
// 复用 SubmitApproval 的同一惯例(logic_approval.go):ReqUser 取认证用户,getAdminEmail
// 取邮箱(best-effort),拿不到邮箱回退 UUID,完全无认证上下文回退 "admin"。
// 不新引上下文管线 —— 这是代码库既有的"当前管理员是谁"的单一来源。
func adminActorTag(c *gin.Context) string {
	actor := ReqUser(c)
	if actor == nil {
		return "admin"
	}
	if email := getAdminEmail(c.Request.Context(), actor.ID); email != "" {
		return "admin:" + email
	}
	if actor.UUID != "" {
		return "admin:" + actor.UUID
	}
	return "admin"
}

// adminListNodeOperations 列出运维任务,可按 action/status 过滤;最新优先。
func adminListNodeOperations(c *gin.Context) {
	query := db.Get().Model(&NodeOperation{})
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}
	if action := c.Query("action"); action != "" {
		query = query.Where("action = ?", action)
	}
	pagination := PaginationFromRequest(c)
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count node operations: %v", err)
		Error(c, ErrorSystemError, "failed to count node operations")
		return
	}
	var ops []NodeOperation
	if err := query.Order("created_at DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&ops).Error; err != nil {
		log.Errorf(c, "failed to list node operations: %v", err)
		Error(c, ErrorSystemError, "failed to list node operations")
		return
	}
	ListWithData(c, ops, pagination)
}

type AdminCreateNodeOperationRequest struct {
	SubID  uint64         `json:"subId"`
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
}

// adminCreateNodeOperation 手动派一条运维任务(change_ip / ad-hoc stop / destroy)。
// 拒绝 provision(只能订单触发,防绕过付款建机)。open 已存在则返 conflict。
func adminCreateNodeOperation(c *gin.Context) {
	var body AdminCreateNodeOperationRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	switch body.Action {
	case NodeOpChangeIP, NodeOpStop, NodeOpDestroy:
		// allowed
	default:
		Error(c, ErrorInvalidArgument, "action must be change_ip, stop or destroy (provision is order-triggered)")
		return
	}
	var sub PrivateNodeSubscription
	if err := db.Get().First(&sub, body.SubID).Error; err != nil {
		Error(c, ErrorNotFound, "subscription not found")
		return
	}
	if sub.CloudInstanceID == nil {
		Error(c, ErrorInvalidOperation, "subscription has no cloud instance to operate on")
		return
	}
	createdBy := adminActorTag(c)
	created, err := createNodeOperationChecked(c.Request.Context(), body.SubID, sub.CloudInstanceID, body.Action, createdBy, body.Params)
	if err != nil {
		log.Errorf(c, "create node operation: %v", err)
		Error(c, ErrorSystemError, "failed to create operation")
		return
	}
	if created == nil {
		Error(c, ErrorConflict, "an open operation of this action already exists for the subscription")
		return
	}
	Success(c, created)
}

type AdminClaimNodeOperationRequest struct {
	Holder       string `json:"holder"`
	LeaseSeconds int    `json:"leaseSeconds"`
}

// adminClaimNodeOperation 原子认领一条 queued 任务。provision 任务额外返回节点 identity。
func adminClaimNodeOperation(c *gin.Context) {
	id, ok := parseNodeOperationID(c)
	if !ok {
		return
	}
	var body AdminClaimNodeOperationRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	leaseSeconds := body.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 600
	}
	// holder:agent 自报身份(如 agent-xyz);人工 admin 面板留空 → 服务端用真实 admin 身份,
	// 让多操作员队列的"认领人"列可信(与 createdBy 同源,不可伪造)。
	holder := body.Holder
	if holder == "" {
		holder = adminActorTag(c)
	}
	now := time.Now().Unix()
	res := db.Get().Model(&NodeOperation{}).
		Where("id = ? AND status = ?", id, NodeOpQueued).
		Updates(map[string]any{
			"status": NodeOpClaimed, "holder": holder,
			"leased_at": now, "lease_deadline": now + int64(leaseSeconds),
		})
	if res.Error != nil {
		log.Errorf(c, "failed to claim operation %d: %v", id, res.Error)
		Error(c, ErrorSystemError, "failed to claim operation")
		return
	}
	if res.RowsAffected == 0 {
		Error(c, ErrorConflict, "operation not claimable (already claimed or not found)")
		return
	}
	var op NodeOperation
	if err := db.Get().First(&op, id).Error; err != nil {
		log.Errorf(c, "failed to reload claimed operation %d: %v", id, err)
		Error(c, ErrorSystemError, "failed to reload operation")
		return
	}
	resp := gin.H{"operation": op}
	if op.Action == NodeOpProvision {
		var sub PrivateNodeSubscription
		if err := db.Get().Select("provision_claim_token").First(&sub, op.SubID).Error; err != nil {
			log.Errorf(c, "failed to load sub %d for claim token: %v", op.SubID, err)
			Error(c, ErrorSystemError, "failed to load subscription identity")
			return
		}
		resp["identity"] = gin.H{
			"claimToken": sub.ProvisionClaimToken,
			"centerUrl":  centerCallbackURL(),
			"domain":     "",
		}
	}
	log.Infof(c, "node operation %d (%s) claimed by %q", id, op.Action, holder)
	Success(c, &resp)
}

type AdminUpdateNodeOperationRequest struct {
	Status string         `json:"status"`
	Result map[string]any `json:"result"`
	Error  string         `json:"error"`
}

// adminUpdateNodeOperation 上报进度/完成/失败/取消。
// provision 的 done 只能由节点自注册产生 → 此处拒绝(防 op/sub 状态分裂)。
func adminUpdateNodeOperation(c *gin.Context) {
	id, ok := parseNodeOperationID(c)
	if !ok {
		return
	}
	var body AdminUpdateNodeOperationRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	switch body.Status {
	case NodeOpInProgress, NodeOpDone, NodeOpFailed, NodeOpCanceled:
		// allowed
	default:
		Error(c, ErrorInvalidArgument, "status must be in_progress, done, failed or canceled")
		return
	}
	var op NodeOperation
	if err := db.Get().Select("id", "action").First(&op, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "operation not found")
			return
		}
		log.Errorf(c, "load operation %d: %v", id, err)
		Error(c, ErrorSystemError, "failed to load operation")
		return
	}
	if op.Action == NodeOpProvision && body.Status == NodeOpDone {
		Error(c, ErrorInvalidOperation, "provision completion is set by node self-registration, not by update")
		return
	}
	updates := map[string]any{"status": body.Status, "last_error": body.Error}
	if body.Result != nil {
		updates["result"] = mustJSON(body.Result)
	}
	if body.Status == NodeOpDone || body.Status == NodeOpFailed || body.Status == NodeOpCanceled {
		updates["completed_at"] = time.Now().Unix()
	}
	if err := db.Get().Model(&NodeOperation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		log.Errorf(c, "update operation %d: %v", id, err)
		Error(c, ErrorSystemError, "failed to update operation")
		return
	}
	log.Infof(c, "node operation %d updated status=%s", id, body.Status)
	SuccessEmpty(c)
}
