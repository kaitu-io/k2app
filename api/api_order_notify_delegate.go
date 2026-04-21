package center

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// DataNotifyDelegate is the response shape for POST /api/user/orders/:uuid/notify-delegate.
type DataNotifyDelegate struct {
	DelegateEmail string `json:"delegateEmail"`
}

// api_order_notify_delegate sends the order's pay URL to the current user's
// delegate payer via email.
//
// Preconditions:
//   - URL param :uuid is a valid order owned by the caller
//   - Caller has a delegate_id set (not NULL)
//   - Order is unpaid
//   - Order's Meta.payUrl is populated (set at order creation)
func api_order_notify_delegate(c *gin.Context) {
	user := ReqUser(c)
	if user == nil {
		Error(c, ErrorNotLogin, "not logged in")
		return
	}
	if user.DelegateID == nil || *user.DelegateID == 0 {
		Error(c, ErrorInvalidArgument, "no delegate set")
		return
	}

	orderUUID := c.Param("uuid")
	if orderUUID == "" {
		Error(c, ErrorInvalidArgument, "missing order uuid")
		return
	}

	var order Order
	err := db.Get().Where("uuid = ?", orderUUID).First(&order).Error
	if err == gorm.ErrRecordNotFound {
		Error(c, ErrorNotFound, "order not found")
		return
	}
	if err != nil {
		log.Errorf(c, "failed to load order %s: %v", orderUUID, err)
		Error(c, ErrorSystemError, "failed to load order")
		return
	}

	if order.UserID != user.ID {
		Error(c, ErrorForbidden, "order does not belong to you")
		return
	}
	if order.IsPaid != nil && *order.IsPaid {
		Error(c, ErrorInvalidArgument, "order already paid")
		return
	}

	payUrl := order.GetPayUrl()
	if payUrl == "" {
		log.Errorf(c, "order %s has no payUrl in meta", orderUUID)
		Error(c, ErrorSystemError, "order has no pay url")
		return
	}

	plan, err := order.GetPlan()
	if err != nil || plan == nil {
		log.Errorf(c, "order %s has no plan in meta: %v", orderUUID, err)
		Error(c, ErrorSystemError, "order has no plan")
		return
	}

	inviterEmail, err := getUserEmail(c, user.ID)
	if err != nil || inviterEmail == "" {
		log.Errorf(c, "failed to load inviter email for user %d: %v", user.ID, err)
		Error(c, ErrorSystemError, "failed to load inviter email")
		return
	}

	delegateEmail, err := getUserEmail(c, *user.DelegateID)
	if err != nil || delegateEmail == "" {
		log.Errorf(c, "failed to load delegate email for user %d: %v", *user.DelegateID, err)
		Error(c, ErrorSystemError, "failed to load delegate email")
		return
	}

	amount := fmt.Sprintf("$%.2f", float64(order.PayAmount)/100.0)

	meta := DelegatePayInviteMeta{
		InviterEmail: inviterEmail,
		PlanName:     plan.Label,
		Amount:       amount,
		PayUrl:       payUrl,
	}

	// Synchronous send — surface success/failure to the user
	if err := emailTo(c, delegateEmail, delegatePayInviteTemplate, meta); err != nil {
		log.Errorf(c, "failed to send delegate invite email to %s: %v", delegateEmail, err)
		Error(c, ErrorSystemError, "failed to send invite email")
		return
	}

	log.Infof(c, "delegate invite email sent: order=%s user=%d delegate=%s", order.UUID, user.ID, delegateEmail)

	Success(c, &DataNotifyDelegate{DelegateEmail: delegateEmail})
}
