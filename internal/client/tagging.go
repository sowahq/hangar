package client

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func (c *Client) PutBucketTagging(name string, tags []bucket.Tag) ([]bucket.Tag, error) {
	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/buckets/%s/tagging", name), tags)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out []bucket.Tag
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

func (c *Client) GetBucketTagging(name string) ([]bucket.Tag, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/admin/buckets/%s/tagging", name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out []bucket.Tag
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

func (c *Client) DeleteBucketTagging(name string) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("/admin/buckets/%s/tagging", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.handleErrorResponse(resp)
}
