package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// CreateTvcAdsTask calls POST /integration/tvc-ads-tasks with the
// Idempotency-Key header, sending rawJSON verbatim.
func (c *Client) CreateTvcAdsTask(ctx context.Context, idemKey string, rawJSON []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/tvc-ads-tasks",
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetTvcAdsTask calls GET /integration/tvc-ads-tasks/{taskId}.
//
// TVC has no unified-query provider, so `task get video <id>` cannot see it —
// polling a marketing-video task must come through here. Returns the raw task
// plus its status so the shared wait loop can drive it.
func (c *Client) GetTvcAdsTask(ctx context.Context, taskID string) (json.RawMessage, string, error) {
	path := "/integration/tvc-ads-tasks/" + url.PathEscape(taskID)
	res, err := c.do(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, "", err
	}
	if err := res.Err(); err != nil {
		return nil, "", err
	}
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(res.Data, &probe); err != nil {
		return nil, "", clierr.Transport(err)
	}
	return res.Data, probe.Status, nil
}
