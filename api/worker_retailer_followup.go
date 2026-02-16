package center

import (
	"context"
	"fmt"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
)

// =====================================================================
// Retailer Follow-up Reminder Worker (Slack Notifications)
// =====================================================================

// Task type constant
const (
	TaskTypeRetailerFollowup = "retailer:followup"
)

// handleRetailerFollowupTask processes pending retailer follow-up reminders
func handleRetailerFollowupTask(ctx context.Context, _ []byte) error {
	log.Infof(ctx, "[FOLLOWUP] Starting retailer follow-up reminder task")

	// Get Beijing time for logging
	loc, _ := time.LoadLocation("Asia/Shanghai")
	beijingNow := time.Now().In(loc)
	log.Infof(ctx, "[FOLLOWUP] Beijing time: %s", beijingNow.Format("2006-01-02 15:04:05"))

	now := time.Now()

	// Find notes that are due and not yet notified
	var notes []RetailerNote
	err := db.Get().
		Preload("Retailer.LoginIdentifies").
		Preload("Operator.LoginIdentifies").
		Preload("Assignee.LoginIdentifies").
		Where("follow_up_at IS NOT NULL AND follow_up_at <= ? AND (is_completed IS NULL OR is_completed = false) AND (slack_notified IS NULL OR slack_notified = false)", now).
		Find(&notes).Error

	if err != nil {
		log.Errorf(ctx, "[FOLLOWUP] Failed to query pending follow-ups: %v", err)
		return fmt.Errorf("query pending follow-ups failed: %w", err)
	}

	log.Infof(ctx, "[FOLLOWUP] Found %d pending follow-up reminders", len(notes))

	if len(notes) == 0 {
		return nil
	}

	// Get retailer configs for level info
	retailerIDs := make([]uint64, len(notes))
	for i, n := range notes {
		retailerIDs[i] = n.RetailerID
	}

	var configs []RetailerConfig
	configMap := make(map[uint64]*RetailerConfig)
	db.Get().Where("user_id IN ?", retailerIDs).Find(&configs)
	for i := range configs {
		configMap[configs[i].UserID] = &configs[i]
	}

	var sentCount, failedCount int

	for _, note := range notes {
		// Get assignee email (defaults to operator if no assignee)
		assigneeEmail := getAssigneeEmail(&note)
		if assigneeEmail == "" {
			log.Warnf(ctx, "[FOLLOWUP] No assignee email for note %d, skipping", note.ID)
			failedCount++
			continue
		}

		// Get retailer email
		retailerEmail := ""
		if note.Retailer != nil && len(note.Retailer.LoginIdentifies) > 0 {
			decrypted, err := secretDecryptString(ctx, note.Retailer.LoginIdentifies[0].EncryptedValue)
			if err == nil {
				retailerEmail = decrypted
			}
		}

		// Get retailer level
		levelName := "L1"
		if config, exists := configMap[note.RetailerID]; exists {
			levelName = config.GetLevelInfo().Name
		}

		// Build Slack message
		message := buildFollowupSlackMessage(&note, retailerEmail, levelName, assigneeEmail)

		// Send to #retailer-followup channel (mention the assignee)
		if err := slack.Send("retailer-followup", message); err != nil {
			log.Errorf(ctx, "[FOLLOWUP] Failed to send Slack notification for note %d: %v", note.ID, err)
			failedCount++
			continue
		}

		// Mark as notified
		if err := db.Get().Model(&note).Update("slack_notified", true).Error; err != nil {
			log.Errorf(ctx, "[FOLLOWUP] Failed to mark note %d as notified: %v", note.ID, err)
		}

		sentCount++
		log.Infof(ctx, "[FOLLOWUP] Sent Slack notification for note %d to %s", note.ID, hideEmail(assigneeEmail))
	}

	log.Infof(ctx, "[FOLLOWUP] Task completed: sent=%d, failed=%d", sentCount, failedCount)
	return nil
}

// getAssigneeEmail gets the email of the person who should be notified
// Returns assignee's email if set, otherwise operator's email
func getAssigneeEmail(note *RetailerNote) string {
	// If assignee is set, use assignee's email
	if note.Assignee != nil && len(note.Assignee.LoginIdentifies) > 0 {
		decrypted, err := secretDecryptString(context.Background(), note.Assignee.LoginIdentifies[0].EncryptedValue)
		if err == nil {
			return decrypted
		}
	}

	// Fall back to operator's email
	if note.Operator != nil && len(note.Operator.LoginIdentifies) > 0 {
		decrypted, err := secretDecryptString(context.Background(), note.Operator.LoginIdentifies[0].EncryptedValue)
		if err == nil {
			return decrypted
		}
	}

	return ""
}

// buildFollowupSlackMessage builds the Slack notification message
func buildFollowupSlackMessage(note *RetailerNote, retailerEmail, levelName, assigneeEmail string) string {
	// Truncate content for preview
	contentPreview := note.Content
	if len(contentPreview) > 150 {
		contentPreview = contentPreview[:150] + "..."
	}

	// Calculate how long overdue
	overdueStatus := ""
	if note.FollowUpAt != nil {
		daysOverdue := note.DaysOverdue()
		if daysOverdue > 0 {
			overdueStatus = fmt.Sprintf("⚠️ 逾期 %d 天", daysOverdue)
		} else if daysOverdue == 0 {
			overdueStatus = "📅 今日到期"
		}
	}

	// Build retailer UUID link
	retailerLink := ""
	if note.Retailer != nil {
		retailerLink = fmt.Sprintf("https://manager.kaitu.io/manager/retailers/%s", note.Retailer.UUID)
	}

	message := fmt.Sprintf(`:bell: *分销商跟进提醒*

*分销商:* %s (%s)
*链接:* %s
%s

*沟通内容:*
> %s

*跟进人:* %s

请及时跟进处理。`, retailerEmail, levelName, retailerLink, overdueStatus, contentPreview, assigneeEmail)

	return message
}
