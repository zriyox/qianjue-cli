package api

import (
	"context"
	"net/http"
)

// SessionRevokeResult mirrors IntegrationSessionRevokeService.RevokeResult.
type SessionRevokeResult struct {
	SessionID       string `json:"sessionId"`
	Status          string `json:"status"`
	AlreadyInactive bool   `json:"alreadyInactive"`
}

// RevokeCurrentSession revokes the session the bearer token belongs to
// (auth-device-flow.md §9.1). The server only ever revokes the caller's own
// session; repeated calls are idempotent.
func (c *Client) RevokeCurrentSession(ctx context.Context) (*SessionRevokeResult, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/sessions/current/revoke", nil, nil, true)
	if err != nil {
		return nil, err
	}
	var out SessionRevokeResult
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
