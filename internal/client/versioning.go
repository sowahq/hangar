package client

import (
	"encoding/json"
	"fmt"

	"github.com/sowahq/hangar/internal/service/bucket"
)

// UpdateBucketVersioning enables or suspends versioning on a bucket via API.
func (c *Client) UpdateBucketVersioning(name string, enabled bool) (*bucket.BucketInfo, error) {
	req := map[string]bool{"enabled": enabled}

	resp, err := c.doRequest("PUT", fmt.Sprintf("/admin/buckets/%s/versioning", name), req)
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
