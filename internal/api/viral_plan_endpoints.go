package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// CreateViralPlanScript calls POST /integration/viral-plan/scripts with the
// Idempotency-Key header. The response carries the finished script in `answer`.
func (c *Client) CreateViralPlanScript(ctx context.Context, idemKey string, rawJSON []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/viral-plan/scripts",
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
