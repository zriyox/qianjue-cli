package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// ListKouboTemplates calls GET /integration/video-studio/koubo/templates.
// templateId is mandatory on submit and the catalogue is admin-configurable, so
// callers must read it here instead of hardcoding an id that will rot.
func (c *Client) ListKouboTemplates(ctx context.Context) (json.RawMessage, error) {
	return c.videoStudioGet(ctx, "/integration/video-studio/koubo/templates")
}

// Submitting a koubo / smart-mix task goes through CreateVideoTask with
// VideoKindKoubo / VideoKindSmartMix: the server requires an Idempotency-Key and
// the CLI journals the request first, exactly like the other video creates.

// ListSmartMixTemplates calls GET /integration/video-studio/smart-mix/templates.
func (c *Client) ListSmartMixTemplates(ctx context.Context) (json.RawMessage, error) {
	return c.videoStudioGet(ctx, "/integration/video-studio/smart-mix/templates")
}

// ListSmartMixVoices calls GET /integration/video-studio/smart-mix/voices.
func (c *Client) ListSmartMixVoices(ctx context.Context) (json.RawMessage, error) {
	return c.videoStudioGet(ctx, "/integration/video-studio/smart-mix/voices")
}

func (c *Client) videoStudioGet(ctx context.Context, path string) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
