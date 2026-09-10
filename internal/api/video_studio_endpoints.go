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

// SubmitKouboTask calls POST /integration/video-studio/koubo/tasks, sending
// rawJSON verbatim. The result is a VIDEO-domain task; poll it with
// `qianjue task wait video <taskId>`.
func (c *Client) SubmitKouboTask(ctx context.Context, rawJSON []byte) (json.RawMessage, error) {
	return c.videoStudioPost(ctx, "/integration/video-studio/koubo/tasks", rawJSON)
}

// ListSmartMixTemplates calls GET /integration/video-studio/smart-mix/templates.
func (c *Client) ListSmartMixTemplates(ctx context.Context) (json.RawMessage, error) {
	return c.videoStudioGet(ctx, "/integration/video-studio/smart-mix/templates")
}

// ListSmartMixVoices calls GET /integration/video-studio/smart-mix/voices.
func (c *Client) ListSmartMixVoices(ctx context.Context) (json.RawMessage, error) {
	return c.videoStudioGet(ctx, "/integration/video-studio/smart-mix/voices")
}

// SubmitSmartMixTask calls POST /integration/video-studio/smart-mix/tasks.
func (c *Client) SubmitSmartMixTask(ctx context.Context, rawJSON []byte) (json.RawMessage, error) {
	return c.videoStudioPost(ctx, "/integration/video-studio/smart-mix/tasks", rawJSON)
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

func (c *Client) videoStudioPost(ctx context.Context, path string, body []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, path, nil, body, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
