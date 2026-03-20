package center

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/wordgate/qtoolkit/chatwoot"
)

func TestIsAIInbox(t *testing.T) {
	viper.Set("chatwoot.ai_inbox_id", 5)
	t.Cleanup(func() { viper.Set("chatwoot.ai_inbox_id", 0) })

	tests := []struct {
		name    string
		inboxID int
		cfgID   int
		want    bool
	}{
		{"matching inbox", 5, 5, true},
		{"wrong inbox", 99, 5, false},
		{"no filter (0)", 999, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Set("chatwoot.ai_inbox_id", tt.cfgID)
			assert.Equal(t, tt.want, isAIInbox(chatwoot.Event{InboxID: tt.inboxID}))
		})
	}
}

func TestHandleMessageCreated_Filters(t *testing.T) {
	base := chatwoot.Event{
		EventType:      "message_created",
		MessageType:    "incoming",
		Content:        "hello",
		ConversationID: 1,
		InboxID:        5,
		Sender:         chatwoot.Sender{Type: "contact", Name: "Test"},
	}

	// handleMessageCreated should silently return for these cases (no panic, no side effects)
	t.Run("outgoing skipped", func(t *testing.T) {
		e := base
		e.MessageType = "outgoing"
		handleMessageCreated(context.Background(), e) // should not panic
	})
	t.Run("agent sender skipped", func(t *testing.T) {
		e := base
		e.Sender.Type = "user"
		handleMessageCreated(context.Background(), e)
	})
	t.Run("assigned skipped", func(t *testing.T) {
		e := base
		e.AssigneeID = 7
		handleMessageCreated(context.Background(), e)
	})
	t.Run("empty content skipped", func(t *testing.T) {
		e := base
		e.Content = ""
		handleMessageCreated(context.Background(), e)
	})
}

// Handoff marker detection tests
func TestDetectTransferMarker(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"marker at end", "正在转接人工客服[TRANSFER_HUMAN]", true},
		{"marker at end with trailing space", "正在转接[TRANSFER_HUMAN]  ", true},
		{"marker at end with newline", "正在转接[TRANSFER_HUMAN]\n", true},
		{"no marker", "普通回复内容", false},
		{"marker in middle", "请[TRANSFER_HUMAN]稍等", false},
		{"marker at start", "[TRANSFER_HUMAN]请稍等", false},
		{"empty content", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.HasSuffix(strings.TrimSpace(tt.content), transferHumanMarker)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStripTransferMarker(t *testing.T) {
	input := "我理解您的需求，正在为您转接人工客服，请稍等。[TRANSFER_HUMAN]"
	result := strings.TrimSpace(strings.ReplaceAll(input, transferHumanMarker, ""))
	assert.Equal(t, "我理解您的需求，正在为您转接人工客服，请稍等。", result)
}

func TestAssignConversation_Success(t *testing.T) {
	var receivedPath string
	var receivedToken string
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedToken = r.Header.Get("api_access_token")
		b, _ := io.ReadAll(r.Body)
		receivedBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	viper.Set("chatwoot.api_token", "test-token")
	viper.Set("chatwoot.base_url", server.URL)
	viper.Set("chatwoot.account_id", 42)
	viper.Set("chatwoot.handoff_assignee_id", 7)
	t.Cleanup(func() {
		viper.Set("chatwoot.api_token", "")
		viper.Set("chatwoot.base_url", "")
		viper.Set("chatwoot.account_id", 0)
		viper.Set("chatwoot.handoff_assignee_id", 0)
	})

	err := assignConversation(context.Background(), 123)
	assert.NoError(t, err)
	assert.Equal(t, "/api/v1/accounts/42/conversations/123/assignments", receivedPath)
	assert.Equal(t, "test-token", receivedToken)
	assert.Contains(t, receivedBody, `"assignee_id":7`)
}

func TestAssignConversation_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	viper.Set("chatwoot.api_token", "test-token")
	viper.Set("chatwoot.base_url", server.URL)
	viper.Set("chatwoot.account_id", 1)
	viper.Set("chatwoot.handoff_assignee_id", 7)
	t.Cleanup(func() {
		viper.Set("chatwoot.api_token", "")
		viper.Set("chatwoot.base_url", "")
		viper.Set("chatwoot.account_id", 0)
	})

	err := assignConversation(context.Background(), 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
