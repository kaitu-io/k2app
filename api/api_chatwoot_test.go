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

func TestShouldProcessEvent_Filters(t *testing.T) {
	// Set ai_inbox_id for inbox filter tests
	viper.Set("chatwoot.ai_inbox_id", 5)
	t.Cleanup(func() { viper.Set("chatwoot.ai_inbox_id", 0) })

	base := chatwoot.Event{
		EventType:      "message_created",
		MessageType:    "incoming",
		Content:        "hello",
		ConversationID: 1,
		InboxID:        5,
		Sender:         chatwoot.Sender{Type: "contact", Name: "Test"},
		Conversation:   chatwoot.Conversation{Status: "pending"},
	}

	tests := []struct {
		name   string
		modify func(e *chatwoot.Event)
		want   bool
	}{
		{"valid event passes", func(e *chatwoot.Event) {}, true},
		{"wrong event type", func(e *chatwoot.Event) { e.EventType = "conversation_created" }, false},
		{"outgoing message", func(e *chatwoot.Event) { e.MessageType = "outgoing" }, false},
		{"agent sender", func(e *chatwoot.Event) { e.Sender.Type = "user" }, false},
		{"has assignee", func(e *chatwoot.Event) { e.AssigneeID = 7 }, false},
		{"empty content", func(e *chatwoot.Event) { e.Content = "" }, false},
		{"whitespace content", func(e *chatwoot.Event) { e.Content = "  \n  " }, false},
		{"wrong inbox", func(e *chatwoot.Event) { e.InboxID = 99 }, false},
		{"no inbox filter", func(e *chatwoot.Event) { viper.Set("chatwoot.ai_inbox_id", 0); e.InboxID = 999 }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := base
			tt.modify(&e)
			assert.Equal(t, tt.want, shouldProcessChatwootEvent(e))
		})
	}
}

func TestBuildAskOpts_NoHistory(t *testing.T) {
	event := chatwoot.Event{Content: "hello"}
	opts := buildAskOpts(nil, event)
	assert.Len(t, opts, 0)
}

func TestBuildAskOpts_WithHistory(t *testing.T) {
	history := []chatwoot.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello!"},
		{Role: "user", Content: "how to install?"},
	}
	event := chatwoot.Event{Content: "how to install?"}
	opts := buildAskOpts(history, event)
	assert.Len(t, opts, 1) // WithHistory only
}

func TestBuildAskOpts_WithImageAttachment(t *testing.T) {
	event := chatwoot.Event{
		Content: "what is this",
		Attachments: []chatwoot.Attachment{
			{FileType: "image", DataURL: "https://example.com/img.png"},
		},
	}
	opts := buildAskOpts(nil, event)
	assert.Len(t, opts, 1) // WithImage only
}

func TestBuildAskOpts_HistoryAndImage(t *testing.T) {
	history := []chatwoot.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello!"},
	}
	event := chatwoot.Event{
		Content: "check this",
		Attachments: []chatwoot.Attachment{
			{FileType: "image", DataURL: "https://example.com/img.png"},
			{FileType: "image", DataURL: "https://example.com/img2.png"},
		},
	}
	opts := buildAskOpts(history, event)
	assert.Len(t, opts, 3) // WithHistory + 2x WithImage
}

func TestBuildAskOpts_IgnoresNonImageAttachments(t *testing.T) {
	event := chatwoot.Event{
		Content: "voice msg",
		Attachments: []chatwoot.Attachment{
			{FileType: "audio", DataURL: "https://example.com/audio.mp3"},
			{FileType: "video", DataURL: "https://example.com/video.mp4"},
			{FileType: "file", DataURL: "https://example.com/doc.pdf"},
		},
	}
	opts := buildAskOpts(nil, event)
	assert.Len(t, opts, 0) // no image attachments
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
