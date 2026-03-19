package center

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wordgate/qtoolkit/chatwoot"
)

func TestShouldProcessEvent_Filters(t *testing.T) {
	base := chatwoot.Event{
		EventType:      "message_created",
		MessageType:    "incoming",
		Content:        "hello",
		ConversationID: 1,
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
		{"open status", func(e *chatwoot.Event) { e.Conversation.Status = "open" }, false},
		{"resolved status", func(e *chatwoot.Event) { e.Conversation.Status = "resolved" }, false},
		{"empty content", func(e *chatwoot.Event) { e.Content = "" }, false},
		{"whitespace content", func(e *chatwoot.Event) { e.Content = "  \n  " }, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := base
			tt.modify(&e)
			assert.Equal(t, tt.want, shouldProcessChatwootEvent(e))
		})
	}
}

func TestBuildFastGPTParts_TextOnly(t *testing.T) {
	event := chatwoot.Event{Content: "hello"}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 1)
}

func TestBuildFastGPTParts_WithImage(t *testing.T) {
	event := chatwoot.Event{
		Content: "check this",
		Attachments: []chatwoot.Attachment{
			{FileType: "image", DataURL: "https://example.com/img.png"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 2)
}

func TestBuildFastGPTParts_WithFile(t *testing.T) {
	event := chatwoot.Event{
		Content: "see attachment",
		Attachments: []chatwoot.Attachment{
			{FileType: "file", DataURL: "https://example.com/doc.pdf"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 2)
}

func TestBuildFastGPTParts_IgnoresAudioVideo(t *testing.T) {
	event := chatwoot.Event{
		Content: "voice msg",
		Attachments: []chatwoot.Attachment{
			{FileType: "audio", DataURL: "https://example.com/audio.mp3"},
			{FileType: "video", DataURL: "https://example.com/video.mp4"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 1)
}

func TestBuildFastGPTParts_EmptyContentWithUnsupportedAttachment(t *testing.T) {
	event := chatwoot.Event{
		Content: "",
		Attachments: []chatwoot.Attachment{
			{FileType: "audio", DataURL: "https://example.com/audio.mp3"},
		},
	}
	parts := buildFastGPTParts(event)
	assert.Len(t, parts, 1)
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
