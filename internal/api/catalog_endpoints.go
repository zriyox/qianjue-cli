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

// GetVideoModelCatalog calls GET /integration/catalog/video-models and returns
// the raw catalog. Video parameter ranges differ far more per model than image
// ones (duration windows, prompt requirement, input-image limits), so hardcoding
// them in scripts or prompts rots fast.
func (c *Client) GetVideoModelCatalog(ctx context.Context) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodGet, "/integration/catalog/video-models", nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
