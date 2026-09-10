package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// CreateImageChatTask calls POST /integration/image-chat-tasks with the
// Idempotency-Key header, sending rawJSON verbatim.
//
// Reading back goes through the unified task endpoints (domain "image-chat"),
// not through here: IMAGE_CHAT already has a unified query projection, and one
// task maps to one result, so a second read shape would only confuse callers.
func (c *Client) CreateImageChatTask(ctx context.Context, idemKey string, rawJSON []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/image-chat-tasks",
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
