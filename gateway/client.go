//go:build linux

package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a gateway HTTP API client for use by the k2r CLI.
type Client struct {
	addr   string
	client *http.Client
}

// NewClient creates a Client that talks to the gateway daemon at addr.
func NewClient(addr string) *Client {
	return &Client{
		addr:   addr,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Do sends an action request to the gateway daemon.
func (c *Client) Do(action string, params map[string]any) (*Response, error) {
	req := Request{Action: action, Params: params}
	body, _ := json.Marshal(req)
	resp, err := c.client.Post(
		fmt.Sprintf("http://%s/api/core", c.addr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("gateway daemon not running (tried %s): %w", c.addr, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var r Response
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("bad response: %s", data)
	}
	return &r, nil
}

// Ping checks if the gateway daemon is reachable.
func (c *Client) Ping() error {
	resp, err := c.client.Get(fmt.Sprintf("http://%s/ping", c.addr))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
