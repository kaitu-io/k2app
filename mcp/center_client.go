package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// centerResponse is the JSON envelope returned by every Center API endpoint.
// The API always returns HTTP 200; non-zero Code indicates an error.
type centerResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// CenterError is returned when the Center API responds with a non-zero code.
type CenterError struct {
	Code    int
	Message string
}

func (e *CenterError) Error() string {
	return fmt.Sprintf("center API error %d: %s", e.Code, e.Message)
}

// RefreshSource is implemented by Session to provide and persist tokens
// during transparent auto-refresh on 401 responses.
type RefreshSource interface {
	GetRefreshToken() string
	UpdateTokens(access, refresh string, issuedAt int64)
}

// CenterClient is an HTTP client for the Kaitu Center API.
type CenterClient struct {
	BaseURL       string
	mu            sync.RWMutex
	token         string
	udid          string
	http          *http.Client
	refreshSource RefreshSource
}

// NewCenterClient creates a new CenterClient targeting baseURL.
func NewCenterClient(baseURL string) *CenterClient {
	return &CenterClient{
		BaseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// SetToken sets the Bearer token used for Authorization headers.
func (c *CenterClient) SetToken(token string) {
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

// Token returns the current Bearer token.
func (c *CenterClient) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// SetUDID sets the device UDID sent in the X-UDID header.
func (c *CenterClient) SetUDID(udid string) {
	c.mu.Lock()
	c.udid = udid
	c.mu.Unlock()
}

// SetRefreshSource registers a RefreshSource used to obtain a refresh token
// and persist new tokens when a 401 triggers automatic re-authentication.
func (c *CenterClient) SetRefreshSource(rs RefreshSource) {
	c.mu.Lock()
	c.refreshSource = rs
	c.mu.Unlock()
}

// Get performs a GET request to path and unmarshals the response Data into result.
// On a 401 CenterError it attempts one transparent token refresh and retries.
func (c *CenterClient) Get(path string, result any) error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("center client get: %w", err)
	}
	err = c.do(req, result)
	if !c.isCode401(err) {
		return err
	}
	if !c.tryRefresh() {
		return err
	}
	req2, err2 := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err2 != nil {
		return fmt.Errorf("center client get retry: %w", err2)
	}
	return c.do(req2, result)
}

// Post performs a POST request to path with body marshalled as JSON,
// and unmarshals the response Data into result.
// On a 401 CenterError it attempts one transparent token refresh and retries
// (body bytes are marshalled once and replayed on retry).
func (c *CenterClient) Post(path string, body any, result any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("center client post marshal: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("center client post: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	err = c.do(req, result)
	if !c.isCode401(err) {
		return err
	}
	if !c.tryRefresh() {
		return err
	}
	req2, err2 := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(b))
	if err2 != nil {
		return fmt.Errorf("center client post retry: %w", err2)
	}
	req2.Header.Set("Content-Type", "application/json")
	return c.do(req2, result)
}

// isCode401 reports whether err is a CenterError with Code == 401.
func (c *CenterClient) isCode401(err error) bool {
	var ce *CenterError
	return errors.As(err, &ce) && ce.Code == 401
}

// refreshResponse is the data field returned by POST /api/auth/refresh.
type refreshResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// tryRefresh attempts to obtain new tokens using the refresh token.
// It updates both the in-memory token and the persistent store.
// Returns true if the refresh succeeded.
func (c *CenterClient) tryRefresh() bool {
	c.mu.RLock()
	rs := c.refreshSource
	c.mu.RUnlock()

	if rs == nil {
		return false
	}
	refreshToken := rs.GetRefreshToken()
	if refreshToken == "" {
		return false
	}

	body := map[string]string{"refreshToken": refreshToken}
	b, err := json.Marshal(body)
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+"/api/auth/refresh", bytes.NewReader(b))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	c.mu.RLock()
	udid := c.udid
	c.mu.RUnlock()
	if udid != "" {
		req.Header.Set("X-UDID", udid)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var envelope centerResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return false
	}
	if envelope.Code != 0 {
		return false
	}

	var result refreshResponse
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return false
	}
	if result.AccessToken == "" {
		return false
	}

	c.SetToken(result.AccessToken)
	rs.UpdateTokens(result.AccessToken, result.RefreshToken, time.Now().Unix())
	return true
}

// do injects auth headers, executes the request, parses the envelope,
// and returns a CenterError for non-zero response codes.
func (c *CenterClient) do(req *http.Request, result any) error {
	c.mu.RLock()
	token := c.token
	udid := c.udid
	c.mu.RUnlock()

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if udid != "" {
		req.Header.Set("X-UDID", udid)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("center client request: %w", err)
	}
	defer resp.Body.Close()

	var envelope centerResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("center client decode: %w", err)
	}

	if envelope.Code != 0 {
		return &CenterError{Code: envelope.Code, Message: envelope.Message}
	}

	if result != nil && envelope.Data != nil {
		if err := json.Unmarshal(envelope.Data, result); err != nil {
			return fmt.Errorf("center client unmarshal data: %w", err)
		}
	}

	return nil
}
