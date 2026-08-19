package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// CreateDetailImageTask calls POST /integration/detail-image-tasks with the
// Idempotency-Key header, sending rawJSON verbatim and returning the raw create
// result (messageId plus per-image items).
func (c *Client) CreateDetailImageTask(ctx context.Context, idemKey string, rawJSON []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/detail-image-tasks",
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// ListDetailImageTasks calls GET /integration/detail-image-tasks. Detail-image
// results live on the message's items rather than on a single task, so polling
// goes through here instead of the unified task read.
func (c *Client) ListDetailImageTasks(ctx context.Context, limit int) (json.RawMessage, error) {
	path := "/integration/detail-image-tasks"
	if limit > 0 {
		path += "?limit=" + strconv.Itoa(limit)
	}
	res, err := c.do(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
