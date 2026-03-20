package center

import (
	"context"
	_ "embed"
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

//go:embed data/system_prompt.md
var systemPrompt string

// isAIInbox checks if the event belongs to the configured AI inbox.
func isAIInbox(event chatwoot.Event) bool {
	inboxID := viper.GetInt("chatwoot.ai_inbox_id")
	return inboxID == 0 || event.InboxID == inboxID
}

// handleChatwootEvent dispatches Chatwoot webhook events by type.
// Called asynchronously by chatwoot.Mount() in a goroutine with 60s context timeout.
func handleChatwootEvent(ctx context.Context, event chatwoot.Event) {
	if !isAIInbox(event) {
		return
	}

	switch event.EventType {
	case "conversation_created":
		handleConversationCreated(ctx, event)
	case "message_created":
		handleMessageCreated(ctx, event)
	case "message_updated":
		handleMessageUpdated(ctx, event)
	}
}

// handleConversationCreated sends a welcome message with interactive buttons.
func handleConversationCreated(ctx context.Context, event chatwoot.Event) {
	if event.AssigneeID > 0 {
		return // human already assigned
	}

	log.Infof(ctx, "new conversation=%d", event.ConversationID)

	if err := chatwoot.SendOptions(ctx, event.ConversationID,
		"您好！请问需要什么帮助？",
		chatwoot.NewOption("📱 安装问题", "install"),
		chatwoot.NewOption("💳 购买/续费", "purchase"),
		chatwoot.NewOption("❓ 使用问题", "usage"),
		chatwoot.NewOption("📹 与客服视频", "video_support"),
	); err != nil {
		log.Errorf(ctx, "send welcome options error: conversation=%d err=%v", event.ConversationID, err)
	}
}

// handleMessageUpdated processes interactive button selections.
func handleMessageUpdated(ctx context.Context, event chatwoot.Event) {
	if len(event.SubmittedValues) == 0 {
		return
	}
	if event.AssigneeID > 0 {
		return
	}

	selected := event.SubmittedValues[0].Value
	log.Infof(ctx, "button selected: conversation=%d value=%s", event.ConversationID, selected)

	switch selected {
	case "video_support":
		handleTransferHuman(ctx, event.ConversationID,
			"好的，正在为您转接人工客服安排视频支持 😊", "")
	case "install":
		askAI(ctx, event.ConversationID, "我需要安装帮助")
	case "purchase":
		askAI(ctx, event.ConversationID, "我想了解购买和续费")
	case "usage":
		askAI(ctx, event.ConversationID, "我有使用问题")
	default:
		askAI(ctx, event.ConversationID, event.SubmittedValues[0].Title)
	}
}

// handleMessageCreated processes incoming customer messages via AI.
func handleMessageCreated(ctx context.Context, event chatwoot.Event) {
	if event.MessageType != "incoming" {
		return
	}
	if event.Sender.Type != "contact" {
		return
	}
	if event.AssigneeID > 0 {
		return
	}
	if strings.TrimSpace(event.Content) == "" {
		return
	}

	log.Infof(ctx, "conversation=%d sender=%s content=%q",
		event.ConversationID, event.Sender.Name, truncateString(event.Content, 100))

	askAI(ctx, event.ConversationID, event.Content)
}

// askAI fetches conversation history from Chatwoot, queries OpenAI filesearch,
// and replies. Transfers to human on error or [TRANSFER_HUMAN] marker.
func askAI(ctx context.Context, conversationID int, question string) {
	// Wait 1s for Chatwoot to persist the current message before fetching history
	time.Sleep(1 * time.Second)

	history, err := chatwoot.GetMessages(ctx, conversationID, 0)
	if err != nil {
		log.Warnf(ctx, "failed to get history: conversation=%d err=%v", conversationID, err)
		history = nil
	}

	var opts []filesearch.Option
	opts = append(opts, filesearch.WithSystemPrompt(systemPrompt))
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

	result, err := filesearch.Ask(ctx, "crm", question, opts...)
	if err != nil || strings.TrimSpace(result.Content) == "" {
		log.Errorf(ctx, "filesearch failed: conversation=%d err=%v", conversationID, err)
		handleTransferHuman(ctx, conversationID, "", question)
		return
	}

	if strings.HasSuffix(strings.TrimSpace(result.Content), transferHumanMarker) {
		handleTransferHuman(ctx, conversationID, result.Content, question)
		return
	}

	if err := chatwoot.Reply(ctx, conversationID, result.Content); err != nil {
		log.Errorf(ctx, "reply error: conversation=%d err=%v", conversationID, err)
	}
}

// handleTransferHuman handles the human handoff flow:
// 1. Strip marker and reply to user
// 2. Assign conversation to a human agent (stops bot from processing)
// 3. Send Slack notification with customer's original message
func handleTransferHuman(ctx context.Context, conversationID int, aiReply string, customerMsg string) {
	reply := strings.TrimSpace(strings.ReplaceAll(aiReply, transferHumanMarker, ""))
	if reply != "" {
		if err := chatwoot.Reply(ctx, conversationID, reply); err != nil {
			log.Errorf(ctx, "reply error on transfer: conversation=%d err=%v", conversationID, err)
		}
	}

	if err := assignConversation(ctx, conversationID); err != nil {
		log.Errorf(ctx, "assign error: conversation=%d err=%v", conversationID, err)
	}

	slackMsg := fmt.Sprintf("[Chatwoot] 客户需要人工客服 — 会话 #%d\n客户消息: %s",
		conversationID, truncateString(customerMsg, 200))
	if err := slack.Send("customer", slackMsg); err != nil {
		log.Errorf(ctx, "slack notify error: %v", err)
	}

	log.Infof(ctx, "transferred to human: conversation=%d", conversationID)
}

// assignConversation assigns a conversation to the configured human agent.
// Chatwoot API: POST /api/v1/accounts/{id}/conversations/{cid}/assignments
// Once assigned, bot stops processing (AssigneeID > 0 filter).
// Human can unassign themselves in Chatwoot to re-engage bot.
func assignConversation(ctx context.Context, conversationID int) error {
	assigneeID := viper.GetInt("chatwoot.handoff_assignee_id")
	if assigneeID == 0 {
		return fmt.Errorf("chatwoot.handoff_assignee_id not configured")
	}

	cfg := getChatwootConfig()
	url := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/assignments",
		cfg.BaseURL, cfg.AccountID, conversationID)

	body := fmt.Sprintf(`{"assignee_id":%d}`, assigneeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
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
