package center

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

// parseProvisionJobID parses the :id path param as uint64.
func parseProvisionJobID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid job id")
		return 0, false
	}
	return id, true
}

// adminListProvisionJobs lists NodeOperation rows for the external AI agent to
// poll the provisioning queue. Optional ?status= filter; newest first.
func adminListProvisionJobs(c *gin.Context) {
	query := db.Get().Model(&NodeOperation{})
	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status)
	}

	pagination := PaginationFromRequest(c)
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count provision jobs: %v", err)
		Error(c, ErrorSystemError, "failed to count provision jobs")
		return
	}

	var jobs []NodeOperation
	if err := query.Order("created_at DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&jobs).Error; err != nil {
		log.Errorf(c, "failed to list provision jobs: %v", err)
		Error(c, ErrorSystemError, "failed to list provision jobs")
		return
	}
	ListWithData(c, jobs, pagination)
}

// AdminClaimProvisionJobRequest is the agent's lease-claim body.
type AdminClaimProvisionJobRequest struct {
	Holder       string `json:"holder"`
	LeaseSeconds int    `json:"leaseSeconds"`
}

// adminClaimProvisionJob atomically leases a queued job to the calling agent.
// The claim response also carries the node IDENTITY (claimToken/centerUrl/domain)
// the agent injects into the VPS so the node can self-register on activation.
func adminClaimProvisionJob(c *gin.Context) {
	id, ok := parseProvisionJobID(c)
	if !ok {
		return
	}

	var body AdminClaimProvisionJobRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	leaseSeconds := body.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = 600
	}

	now := time.Now().Unix()
	deadline := now + int64(leaseSeconds)

	// Atomic claim: only a still-queued row flips to claimed. RowsAffected is
	// reliable here because status + holder are real value changes.
	res := db.Get().Model(&NodeOperation{}).
		Where("id = ? AND status = ?", id, NodeOpQueued).
		Updates(map[string]any{
			"status":         NodeOpClaimed,
			"holder":         body.Holder,
			"leased_at":      now,
			"lease_deadline": deadline,
		})
	if res.Error != nil {
		log.Errorf(c, "failed to claim provision job %d: %v", id, res.Error)
		Error(c, ErrorSystemError, "failed to claim job")
		return
	}
	if res.RowsAffected == 0 {
		Error(c, ErrorConflict, "job not claimable (already claimed or not found)")
		return
	}

	var job NodeOperation
	if err := db.Get().First(&job, id).Error; err != nil {
		log.Errorf(c, "failed to reload claimed job %d: %v", id, err)
		Error(c, ErrorSystemError, "failed to reload job")
		return
	}

	var sub PrivateNodeSubscription
	if err := db.Get().Select("provision_claim_token").First(&sub, job.SubID).Error; err != nil {
		log.Errorf(c, "failed to load sub %d for claim token: %v", job.SubID, err)
		Error(c, ErrorSystemError, "failed to load subscription identity")
		return
	}

	// domain 现存于 Params JSON（provision 动作快照），从中解出以保持响应不变。
	var params ProvisionParams
	_ = json.Unmarshal([]byte(job.Params), &params)

	log.Infof(c, "provision job %d claimed by %q (lease %ds)", id, body.Holder, leaseSeconds)
	Success(c, &gin.H{
		"job": job,
		"identity": gin.H{
			"claimToken": sub.ProvisionClaimToken,
			"centerUrl":  centerCallbackURL(),
			"domain":     params.Domain,
		},
	})
}

// AdminReportProvisionJobRequest is the agent's progress/result report body.
type AdminReportProvisionJobRequest struct {
	Status     string `json:"status"`
	InstanceID string `json:"instanceId"`
	IPv4       string `json:"ipv4"`
	Error      string `json:"error"`
}

// adminReportProvisionJob records agent progress/result onto a job. Only the
// provided columns are touched; last_error is always set (may clear to empty).
func adminReportProvisionJob(c *gin.Context) {
	id, ok := parseProvisionJobID(c)
	if !ok {
		return
	}

	var body AdminReportProvisionJobRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// agent 只能上报进行中/失败。succeeded 是权威终态，只能由节点自注册路径产生
	// （该路径同时激活订阅）；允许 agent 直接报 succeeded 会让 job=succeeded 而 sub
	// 仍停在 provisioning，制造状态分裂。
	switch body.Status {
	case NodeOpInProgress, NodeOpFailed:
		// allowed
	default:
		Error(c, ErrorInvalidArgument, "status must be in_progress or failed")
		return
	}

	updates := map[string]any{
		"status":     body.Status,
		"last_error": body.Error,
	}
	// instance_id/ipv4 现并入 Result JSON（动作专属结果）。仅在 agent 上报时写入。
	if body.InstanceID != "" || body.IPv4 != "" {
		updates["result"] = mustJSON(map[string]any{
			"instanceId": body.InstanceID,
			"ipv4":       body.IPv4,
		})
	}

	res := db.Get().Model(&NodeOperation{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		log.Errorf(c, "failed to report provision job %d: %v", id, res.Error)
		Error(c, ErrorSystemError, "failed to update job")
		return
	}
	if res.RowsAffected == 0 {
		// No row changed: either job missing or values identical. Disambiguate.
		var exists NodeOperation
		if err := db.Get().Select("id").First(&exists, id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				Error(c, ErrorNotFound, "job not found")
				return
			}
			log.Errorf(c, "failed to verify provision job %d: %v", id, err)
			Error(c, ErrorSystemError, "failed to verify job")
			return
		}
	}

	log.Infof(c, "provision job %d reported status=%s", id, body.Status)
	SuccessEmpty(c)
}
