package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// GetImageModelCatalog calls GET /integration/catalog/image-models and returns
// the catalog verbatim, so new capability fields reach the caller without a CLI
// release.
func (c *Client) GetImageModelCatalog(ctx context.Context) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodGet, "/integration/catalog/image-models", nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
