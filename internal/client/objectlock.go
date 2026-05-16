package client

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func (c *Client) PutBucketObjectLock(name string, cfg *bucket.ObjectLockConfig) (*bucket.ObjectLockConfig, error) {
	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/buckets/%s/object-lock", name), cfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var out bucket.ObjectLockConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) GetBucketObjectLock(name string) (*bucket.ObjectLockConfig, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/admin/buckets/%s/object-lock", name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var out bucket.ObjectLockConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}
