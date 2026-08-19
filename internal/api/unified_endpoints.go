package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// GetUnifiedTask calls GET /integration/tasks/{domain}/{taskId} and returns the
// raw unified task view (int64-safe) plus the extracted status string. The
// unified view归一 image/video/imagechat into one shape, so a single CLI read
// path covers every domain.
func (c *Client) GetUnifiedTask(ctx context.Context, domain, taskID string) (json.RawMessage, string, error) {
	path := "/integration/tasks/" + url.PathEscape(domain) + "/" + url.PathEscape(taskID)
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

// ListUnifiedTasks calls GET /integration/tasks/{domain} with optional status
// filter and paging, returning the raw PageResponse (records int64-safe).
func (c *Client) ListUnifiedTasks(ctx context.Context, domain, status string, page, size int) (json.RawMessage, error) {
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	path := "/integration/tasks/" + url.PathEscape(domain)
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
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

// CancelUnifiedTask calls PUT /integration/tasks/{domain}/{taskId}/cancel and
// returns the message. The backend reports failure as HTTP 200 + code 500
// ("任务取消失败"), which Result.Err maps to a SERVER error — callers must not
// look at HTTP alone.
func (c *Client) CancelUnifiedTask(ctx context.Context, domain, taskID string) (string, error) {
	path := "/integration/tasks/" + url.PathEscape(domain) + "/" + url.PathEscape(taskID) + "/cancel"
	res, err := c.do(ctx, http.MethodPut, path, nil, nil, true)
	if err != nil {
		return "", err
	}
	var msg string
	if err := decodeData(res, &msg); err != nil {
		return "", err
	}
	return msg, nil
}
