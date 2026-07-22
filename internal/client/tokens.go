package client

import (
	"encoding/json"
	"fmt"
)

type CreateTokenRequest struct {
	Permissions []string `json:"permissions"`
}

type TokenResponse struct {
	Token       string   `json:"token,omitempty"`
	ID          string   `json:"id"`
	Bucket      string   `json:"bucket"`
	Permissions []string `json:"permissions"`
	CreatedAt   int64    `json:"created_at,omitempty"`
}

type ListTokensResponse struct {
	Tokens []TokenResponse `json:"tokens"`
	Count  int             `json:"count"`
}

// CreateToken issues a new access token for a bucket via API.
func (c *Client) CreateToken(bucketName string, perms []string) (*TokenResponse, error) {
	req := &CreateTokenRequest{Permissions: perms}

	resp, err := c.doRequest("POST", fmt.Sprintf("/admin/buckets/%s/tokens", bucketName), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ListTokens lists all tokens for a bucket via API.
func (c *Client) ListTokens(bucketName string) (*ListTokensResponse, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/admin/buckets/%s/tokens", bucketName), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result ListTokensResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteToken revokes a token from a bucket via API.
func (c *Client) DeleteToken(bucketName, id string) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("/admin/buckets/%s/tokens/%s", bucketName, id), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return c.handleErrorResponse(resp)
}
