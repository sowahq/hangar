package client

import (
	"encoding/json"
	"fmt"
)

// GetStatus returns server health and status information via API.
func (c *Client) GetStatus() (map[string]any, error) {
	resp, err := c.doRequest("GET", "/status", nil)
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
