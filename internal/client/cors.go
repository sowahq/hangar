package client

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/service/bucket"
)

func (c *Client) PutBucketCORS(name string, cfg *bucket.CORSConfiguration) (*bucket.CORSConfiguration, error) {
	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/buckets/%s/cors", name), cfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out bucket.CORSConfiguration
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) GetBucketCORS(name string) (*bucket.CORSConfiguration, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/admin/buckets/%s/cors", name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}
	var out bucket.CORSConfiguration
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) DeleteBucketCORS(name string) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("/admin/buckets/%s/cors", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.handleErrorResponse(resp)
}
