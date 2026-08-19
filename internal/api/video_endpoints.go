package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// VideoKind identifies a video create sub-resource. Each maps to a distinct
// backend endpoint and a distinct capability/idempotency scope.
type VideoKind string

const (
	VideoKindCreate         VideoKind = "create"          // batch: normal / reference video
	VideoKindEdit           VideoKind = "edit"            // video editing
	VideoKindUpscale        VideoKind = "upscale"         // video upscale
	VideoKindGestureReplica VideoKind = "gesture-replica" // gesture dance replica
)

// videoPath returns the sub-resource path for a video kind. The batch kind uses
// the collection root; the others append their sub-resource segment.
func videoPath(kind VideoKind) string {
	if kind == VideoKindCreate {
		return "/integration/video-tasks"
	}
	return "/integration/video-tasks/" + string(kind)
}

// CreateVideoTask calls POST /integration/video-tasks[/<kind>] with the
// Idempotency-Key header, sending rawJSON verbatim and returning the raw create
// result (int64-safe passthrough; batch results carry batchId + taskIds, single
// results carry taskId).
func (c *Client) CreateVideoTask(ctx context.Context, kind VideoKind, idemKey string, rawJSON []byte) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, videoPath(kind),
		map[string]string{IdempotencyKeyHeader: idemKey}, rawJSON, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetVideoIdempotencyStatus calls GET /integration/video-tasks/idempotency?key=
// and returns the desensitized idempotency status (same shape as image).
func (c *Client) GetVideoIdempotencyStatus(ctx context.Context, key string) (*RequestStatus, error) {
	res, err := c.do(ctx, http.MethodGet,
		"/integration/video-tasks/idempotency?key="+url.QueryEscape(key), nil, nil, true)
	if err != nil {
		return nil, err
	}
	var out RequestStatus
	if err := decodeData(res, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VideoResultTaskID extracts the primary task id to wait on from a raw video
// create result: taskId for single-task kinds, else the first of taskIds for a
// batch result.
func VideoResultTaskID(raw json.RawMessage) string {
	var probe struct {
		TaskID  *json.Number  `json:"taskId"`
		TaskIDs []json.Number `json:"taskIds"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return ""
	}
	if probe.TaskID != nil && probe.TaskID.String() != "" {
		return probe.TaskID.String()
	}
	if len(probe.TaskIDs) > 0 {
		return probe.TaskIDs[0].String()
	}
	return ""
}
