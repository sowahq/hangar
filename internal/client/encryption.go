package client

import (
	"encoding/json"
	"fmt"

	"github.com/sowahq/hangar/internal/service/bucket"
)

func (c *Client) PutBucketEncryption(name string, cfg *bucket.EncryptionConfig) (*bucket.EncryptionConfig, error) {
	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/buckets/%s/encryption", name), cfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var out bucket.EncryptionConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) GetBucketEncryption(name string) (*bucket.EncryptionConfig, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/admin/buckets/%s/encryption", name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.handleErrorResponse(resp); err != nil {
		return nil, err
	}

	var out bucket.EncryptionConfig
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &out, nil
}

func (c *Client) DeleteBucketEncryption(name string) error {
	resp, err := c.doRequest("DELETE", fmt.Sprintf("/admin/buckets/%s/encryption", name), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return c.handleErrorResponse(resp)
}
