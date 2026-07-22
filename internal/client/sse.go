package client

import (
	"encoding/json"
	"fmt"
)

// ListSSEKeys lists the SSE-S3 keyring via API.
func (c *Client) ListSSEKeys() (map[string]any, error) {
	resp, err := c.doRequest("GET", "/admin/sse/keys", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// RotateSSEKey generates a new active SSE-S3 key via API.
func (c *Client) RotateSSEKey() (map[string]any, error) {
	resp, err := c.doRequest("POST", "/admin/sse/keys/rotate", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// ActivateSSEKey sets an existing SSE-S3 key as active via API.
func (c *Client) ActivateSSEKey(id string) (map[string]any, error) {
	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/sse/keys/%s/activate", id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}
