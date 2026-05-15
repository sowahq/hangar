package client

import (
	"encoding/json"
	"fmt"
)

type CreateS3KeyRequest struct {
	Permissions []string `json:"permissions"`
	Buckets     []string `json:"buckets,omitempty"`
}

type S3KeyResponse struct {
	AccessKeyID string   `json:"access_key_id"`
	SecretKey   string   `json:"secret_key,omitempty"`
	Permissions []string `json:"permissions"`
	Buckets     []string `json:"buckets"`
	CreatedAt   int64    `json:"created_at"`
}

type ListS3KeysResponse struct {
	Keys  []S3KeyResponse `json:"keys"`
	Count int             `json:"count"`
}

func (c *Client) CreateS3Key(req *CreateS3KeyRequest) (*S3KeyResponse, error) {
	resp, err := c.doRequest("POST", "/admin/s3keys", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result S3KeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) ListS3Keys() (*ListS3KeysResponse, error) {
	resp, err := c.doRequest("GET", "/admin/s3keys", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result ListS3KeysResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) DeleteS3Key(id string) error {
	resp, err := c.doRequest("DELETE", "/admin/s3keys/"+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.handleErrorResponse(resp)
}
