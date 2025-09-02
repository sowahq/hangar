package client

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

// CreateBucket creates a new bucket via API
func (c *Client) CreateBucket(req *bucket.CreateBucketRequest) (*bucket.CreateBucketResponse, error) {
	resp, err := c.doRequest("POST", "/buckets", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result bucket.CreateBucketResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ListBuckets lists all buckets via API
func (c *Client) ListBuckets() (*bucket.ListBucketsResponse, error) {
	resp, err := c.doRequest("GET", "/buckets", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result bucket.ListBucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetBucket gets bucket information via API
func (c *Client) GetBucket(name string) (*bucket.BucketInfo, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/buckets/%s", name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var result bucket.BucketInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteBucket deletes a bucket via API
func (c *Client) DeleteBucket(name string, force bool) error {
	path := fmt.Sprintf("/buckets/%s", name)
	if force {
		path += "?force=true"
	}

	resp, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return c.handleErrorResponse(resp)
}
