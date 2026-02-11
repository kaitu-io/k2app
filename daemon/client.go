package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is the IPC client used by CLI commands to communicate with the daemon.
type Client struct {
	addr   string
	client *http.Client
}

// NewClient creates a new daemon IPC client.
func NewClient(addr string) *Client {
	return &Client{
		addr: addr,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Ping checks if the daemon is reachable.
func (c *Client) Ping() error {
	resp, err := c.client.Get(fmt.Sprintf("http://%s/ping", c.addr))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Do sends an action request to the daemon and returns the response.
func (c *Client) Do(action string, params map[string]any) (*Response, error) {
	req := Request{
		Action: action,
		Params: params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpResp, err := c.client.Post(
		fmt.Sprintf("http://%s/api/core", c.addr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	var resp Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}
