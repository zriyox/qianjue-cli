package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// Header names fixed by the backend controllers.
const (
	DeviceCodeHeader     = "X-Qianjue-Device-Code"
	IdempotencyKeyHeader = "Idempotency-Key"
)

// CreateDeviceAuth calls POST /integration/device-auth (anonymous).
func (c *Client) CreateDeviceAuth(ctx context.Context, req DeviceAuthCreateRequest) (*DeviceAuthCreateResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, clierr.Usage("序列化 Device Flow 请求失败: %v", err)
	}
	res, err := c.do(ctx, http.MethodPost, "/integration/device-auth", nil, body, false)
	if err != nil {
		return nil, err
	}
	var out DeviceAuthCreateResult
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollDeviceAuth calls GET /integration/device-auth/{id} with the device code
// header (anonymous). A session-expired backend answer may arrive either as
// HTTP 410/2015 or as HTTP 200 with data.status=EXPIRED; both surface to the
// caller: the former as a CLIError, the latter via the Status field.
func (c *Client) PollDeviceAuth(ctx context.Context, deviceSessionID, deviceCode string) (*DeviceAuthPollResult, error) {
	res, err := c.do(ctx, http.MethodGet, "/integration/device-auth/"+url.PathEscape(deviceSessionID),
		map[string]string{DeviceCodeHeader: deviceCode}, nil, false)
	if err != nil {
		return nil, err
	}
	var out DeviceAuthPollResult
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RefreshToken calls POST /integration/token/refresh (anonymous body carries
// the refresh token).
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*TokenRefreshResult, error) {
	body, err := json.Marshal(map[string]string{"refreshToken": refreshToken})
	if err != nil {
		return nil, clierr.Usage("序列化 refresh 请求失败: %v", err)
	}
	res, err := c.do(ctx, http.MethodPost, "/integration/token/refresh", nil, body, false)
	if err != nil {
		return nil, err
	}
	var out TokenRefreshResult
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateImageTask calls POST /integration/image-tasks with the Idempotency-Key
// header. rawJSON is the caller-persisted request document, sent verbatim.
func (c *Client) CreateImageTask(ctx context.Context, idemKey string, rawJSON []byte) (*ImageTaskCreateResult, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/image-tasks",
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	var out ImageTaskCreateResult
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetImageTask calls GET /integration/image-tasks/{taskId} and returns the raw
// task document (int64-safe) plus the extracted status string.
func (c *Client) GetImageTask(ctx context.Context, taskID string) (json.RawMessage, string, error) {
	res, err := c.do(ctx, http.MethodGet, "/integration/image-tasks/"+url.PathEscape(taskID), nil, nil, true)
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

// CancelImageTask calls PUT /integration/image-tasks/{taskId}/cancel. The
// backend reports failure as HTTP 200 + code 500 ("任务取消失败"), which
// Result.Err maps to a SERVER error — callers must not look at HTTP alone.
func (c *Client) CancelImageTask(ctx context.Context, taskID string) (string, error) {
	res, err := c.do(ctx, http.MethodPut, "/integration/image-tasks/"+url.PathEscape(taskID)+"/cancel", nil, nil, true)
	if err != nil {
		return "", err
	}
	var msg string
	if err := decodeData(res, &msg); err != nil {
		return "", err
	}
	return msg, nil
}

// GetIdempotencyStatus calls GET /integration/image-tasks/idempotency?key=...
func (c *Client) GetIdempotencyStatus(ctx context.Context, key string) (*RequestStatus, error) {
	res, err := c.do(ctx, http.MethodGet, "/integration/image-tasks/idempotency?key="+url.QueryEscape(key), nil, nil, true)
	if err != nil {
		return nil, err
	}
	var out RequestStatus
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
