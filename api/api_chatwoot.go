package center

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/chatwoot"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/openai/filesearch"
	"github.com/wordgate/qtoolkit/slack"
)

const transferHumanMarker = "[TRANSFER_HUMAN]"

var chatwootHTTPClient = &http.Client{Timeout: 10 * time.Second}

// shouldProcessChatwootEvent checks all filter conditions.
// Returns true only if the event should be forwarded to AI.
func shouldProcessChatwootEvent(event chatwoot.Event) bool {
	if event.EventType != "message_created" {
		return false
	}
	if event.MessageType != "incoming" {
		return false
	}
	if event.Sender.Type != "contact" {
		return false
	}
	if event.Conversation.Status != "pending" {
		return false
	}
	if strings.TrimSpace(event.Content) == "" {
		return false
	}
	// Only process messages from the configured AI inbox (0 = no filter)
	if inboxID := viper.GetInt("chatwoot.ai_inbox_id"); inboxID > 0 && event.InboxID != inboxID {
		return false
	}
	return true
}

// handleChatwootEvent processes Chatwoot webhook events.
// Called asynchronously by chatwoot.Mount() in a goroutine with 60s context timeout.
// Note: gin.Context is not available here (already released), so we use context.Context.
// Request-scoped tracing is not available — this is inherent to async webhook processing.
func handleChatwootEvent(ctx context.Context, event chatwoot.Event) {
	if !shouldProcessChatwootEvent(event) {
		return
	}

	log.Infof(ctx, "conversation=%d sender=%s content=%q",
		event.ConversationID, event.Sender.Name, truncateString(event.Content, 100))

	// Wait 1s for Chatwoot to persist the current message before fetching history
	time.Sleep(1 * time.Second)

	// Fetch conversation history from Chatwoot (source of truth)
	history, err := chatwoot.GetMessages(ctx, event.ConversationID, 10)
	if err != nil {
		log.Warnf(ctx, "failed to get history: conversation=%d err=%v", event.ConversationID, err)
		// Degrade to single-turn: no history, just current question
		history = nil
	}

	opts := buildAskOpts(history, event)
	result, err := filesearch.Ask(ctx, "crm", event.Content, opts...)
	if err != nil {
		log.Errorf(ctx, "filesearch error: conversation=%d err=%v", event.ConversationID, err)
		chatwoot.Reply(ctx, event.ConversationID, "抱歉，系统暂时无法处理您的消息，请稍后再试。")
		return
	}

	if strings.TrimSpace(result.Content) == "" {
		log.Warnf(ctx, "filesearch returned empty content: conversation=%d", event.ConversationID)
		chatwoot.Reply(ctx, event.ConversationID, "抱歉，系统暂时无法处理您的消息，请稍后再试。")
		return
	}

	if strings.HasSuffix(strings.TrimSpace(result.Content), transferHumanMarker) {
		handleTransferHuman(ctx, event.ConversationID, result.Content, event.Content)
		return
	}

	if err := chatwoot.Reply(ctx, event.ConversationID, result.Content); err != nil {
		log.Errorf(ctx, "reply error: conversation=%d err=%v", event.ConversationID, err)
	}
}

// buildAskOpts converts Chatwoot history + current event into filesearch options.
// History from GetMessages is chronological and includes the current message.
// Images in history are passed via WithHistory (filesearch.Message.Images).
// Images in the current event are passed via WithImage (detail:low is automatic).
func buildAskOpts(history []chatwoot.Message, event chatwoot.Event) []filesearch.Option {
	var opts []filesearch.Option

	// Convert chatwoot history to filesearch messages
	if len(history) > 0 {
		fsHistory := make([]filesearch.Message, len(history))
		for i, m := range history {
			fsHistory[i] = filesearch.Message{
				Role:    m.Role,
				Content: m.Content,
				Images:  m.Images,
			}
		}
		opts = append(opts, filesearch.WithHistory(fsHistory))
	}

	// Current message image attachments
	for _, att := range event.Attachments {
		if att.FileType == "image" {
			opts = append(opts, filesearch.WithImage(att.DataURL))
		}
	}

	return opts
}

// handleTransferHuman handles the human handoff flow:
// 1. Strip marker and reply to user
// 2. Toggle conversation status to "open"
// 3. Send Slack notification with customer's original message
func handleTransferHuman(ctx context.Context, conversationID int, aiReply string, customerMsg string) {
	reply := strings.TrimSpace(strings.ReplaceAll(aiReply, transferHumanMarker, ""))
	if reply != "" {
		if err := chatwoot.Reply(ctx, conversationID, reply); err != nil {
			log.Errorf(ctx, "reply error on transfer: conversation=%d err=%v", conversationID, err)
		}
	}

	if err := toggleConversationStatus(ctx, conversationID); err != nil {
		log.Errorf(ctx, "toggle status error: conversation=%d err=%v", conversationID, err)
	}

	slackMsg := fmt.Sprintf("[Chatwoot] 客户需要人工客服 — 会话 #%d\n客户消息: %s",
		conversationID, truncateString(customerMsg, 200))
	if err := slack.Send("support", slackMsg); err != nil {
		log.Errorf(ctx, "slack notify error: %v", err)
	}

	log.Infof(ctx, "transferred to human: conversation=%d", conversationID)
}

// toggleConversationStatus calls Chatwoot REST API to toggle conversation status (pending ↔ open).
// This API is not wrapped by qtoolkit/chatwoot, so we call it directly.
func toggleConversationStatus(ctx context.Context, conversationID int) error {
	cfg := getChatwootConfig()
	url := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/toggle_status",
		cfg.BaseURL, cfg.AccountID, conversationID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("api_access_token", cfg.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := chatwootHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return nil
}

// getChatwootConfig reads chatwoot config from viper.
// Used for APIs not wrapped by qtoolkit/chatwoot (e.g., toggle_status).
func getChatwootConfig() chatwoot.Config {
	return chatwoot.Config{
		APIToken:  viper.GetString("chatwoot.api_token"),
		BaseURL:   strings.TrimRight(viper.GetString("chatwoot.base_url"), "/"),
		AccountID: viper.GetInt("chatwoot.account_id"),
	}
}
