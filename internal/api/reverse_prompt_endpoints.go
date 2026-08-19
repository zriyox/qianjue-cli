package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
)

// CreateReversePromptSession calls POST /integration/video-reverse-prompt/sessions
// with the Idempotency-Key header, sending rawJSON verbatim.
func (c *Client) CreateReversePromptSession(ctx context.Context, idemKey string, rawJSON []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/video-reverse-prompt/sessions",
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetReversePromptSession fetches one parse session, including its structured
// script once the asynchronous analysis finishes.
func (c *Client) GetReversePromptSession(ctx context.Context, sessionID int64) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodGet,
		"/integration/video-reverse-prompt/sessions/"+strconv.FormatInt(sessionID, 10), nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
