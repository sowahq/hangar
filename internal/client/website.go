package client

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func (c *Client) PutBucketWebsite(name string, cfg *bucket.WebsiteConfig) (*bucket.WebsiteConfig, error) {
	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/buckets/%s/website", name), cfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out bucket.WebsiteConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) GetBucketWebsite(name string) (*bucket.WebsiteConfig, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/admin/buckets/%s/website", name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out bucket.WebsiteConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) DeleteBucketWebsite(name string) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("/admin/buckets/%s/website", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.handleErrorResponse(resp)
}
