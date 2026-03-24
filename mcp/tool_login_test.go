package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTestApp(t *testing.T, centerURL string) *App {
	t.Helper()
	dir := t.TempDir()
	sess := NewSession(dir)
	center := NewCenterClient(centerURL)
	center.SetUDID(sess.UDID())
	return &App{
		center:  center,
		daemon:  &DaemonClient{Addr: "http://127.0.0.1:1777"},
		session: sess,
	}
}

// textContent extracts the text from the first content item in a CallToolResult.
func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestToolSendCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/code" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["email"] != "test@example.com" {
			t.Errorf("expected email test@example.com, got %v", body["email"])
		}

		data, _ := json.Marshal(sendCodeResponse{
			UserExists:  true,
			IsActivated: true,
		})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	result, _, err := app.toolSendCode(context.Background(), nil, SendCodeInput{
		Email: "test@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["email"] != "test@example.com" {
		t.Errorf("expected email in result, got %v", out)
	}
	if out["message"] != "verification code sent" {
		t.Errorf("expected message 'verification code sent', got %v", out["message"])
	}
}

func TestToolLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["email"] != "test@example.com" {
			t.Errorf("expected email test@example.com, got %v", body["email"])
		}
		if body["verificationCode"] != "123456" {
			t.Errorf("expected verificationCode 123456, got %v", body["verificationCode"])
		}
		if body["remark"] != "k2-mcp" {
			t.Errorf("expected remark k2-mcp, got %v", body["remark"])
		}

		data, _ := json.Marshal(loginResponse{
			AccessToken:  "access-token-123",
			RefreshToken: "refresh-token-456",
			IssuedAt:     1700000000,
		})
		resp := centerResponse{Code: 0, Message: "ok", Data: json.RawMessage(data)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	result, _, err := app.toolLogin(context.Background(), nil, LoginInput{
		Email:            "test@example.com",
		VerificationCode: "123456",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", textContent(t, result))
	}

	// Verify tokens stored in session.
	if !app.session.LoggedIn() {
		t.Error("expected session to be logged in")
	}
	app.session.mu.RLock()
	gotAccess := app.session.AccessToken
	gotEmail := app.session.Email
	app.session.mu.RUnlock()

	if gotAccess != "access-token-123" {
		t.Errorf("expected access token 'access-token-123', got %q", gotAccess)
	}
	if gotEmail != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %q", gotEmail)
	}

	// Verify center token set.
	if app.center.Token() != "access-token-123" {
		t.Errorf("expected center token 'access-token-123', got %q", app.center.Token())
	}

	// Verify result JSON.
	var out map[string]string
	if err := json.Unmarshal([]byte(textContent(t, result)), &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out["email"] != "test@example.com" {
		t.Errorf("expected email in result, got %v", out)
	}

	// Verify session file persisted.
	path := app.session.dir + "/mcp-session.json"
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("session file not saved: %v", statErr)
	}
}

func TestToolLogin_InvalidCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := centerResponse{Code: 400007, Message: "invalid verification code", Data: json.RawMessage("null")}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	app := newTestApp(t, srv.URL)
	result, _, err := app.toolLogin(context.Background(), nil, LoginInput{
		Email:            "bad@example.com",
		VerificationCode: "000000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected IsError=true for invalid verification code")
	}

	// Verify tokens NOT stored.
	if app.session.LoggedIn() {
		t.Error("expected session NOT to be logged in after failed login")
	}
}
