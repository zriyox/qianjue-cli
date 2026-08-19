package idem

import (
	"context"
	"encoding/json"

	"github.com/zriyox/qianjue-cli/internal/api"
)

// RecoverClient abstracts the three backend calls the unknown-result recovery
// loop needs, so Recover works for any domain: query idempotency status, replay
// the original create with the original key, and fetch a bound resource.
type RecoverClient interface {
	// Status queries the domain's idempotency status by key.
	Status(ctx context.Context, key string) (*api.RequestStatus, error)
	// Replay re-sends the original create (original key + JSON) and returns the
	// raw result plus the primary task/resource id for the local log.
	Replay(ctx context.Context, key string, rawJSON []byte) (json.RawMessage, string, error)
	// FetchTask fetches the bound resource by id and returns its raw document.
	FetchTask(ctx context.Context, resourceID string) (json.RawMessage, error)
}

// imageRecoverClient binds RecoverClient to the image-task endpoints.
type imageRecoverClient struct{ client *api.Client }

// NewImageRecoverClient adapts an api.Client for image recovery.
func NewImageRecoverClient(client *api.Client) RecoverClient {
	return imageRecoverClient{client: client}
}

func (r imageRecoverClient) Status(ctx context.Context, key string) (*api.RequestStatus, error) {
	return r.client.GetIdempotencyStatus(ctx, key)
}

func (r imageRecoverClient) Replay(ctx context.Context, key string, rawJSON []byte) (json.RawMessage, string, error) {
	result, err := r.client.CreateImageTask(ctx, key, rawJSON)
	if err != nil {
		return nil, "", err
	}
	return result.Task, taskIDFromRaw(result.Task), nil
}

func (r imageRecoverClient) FetchTask(ctx context.Context, resourceID string) (json.RawMessage, error) {
	raw, _, err := r.client.GetImageTask(ctx, resourceID)
	return raw, err
}

// videoRecoverClient binds RecoverClient to the video-task endpoints; replay
// targets one video kind and reads bound tasks through the unified endpoint.
type videoRecoverClient struct {
	client *api.Client
	kind   api.VideoKind
}

// NewVideoRecoverClient adapts an api.Client for video recovery of one kind.
func NewVideoRecoverClient(client *api.Client, kind api.VideoKind) RecoverClient {
	return videoRecoverClient{client: client, kind: kind}
}

func (r videoRecoverClient) Status(ctx context.Context, key string) (*api.RequestStatus, error) {
	return r.client.GetVideoIdempotencyStatus(ctx, key)
}

func (r videoRecoverClient) Replay(ctx context.Context, key string, rawJSON []byte) (json.RawMessage, string, error) {
	raw, err := r.client.CreateVideoTask(ctx, r.kind, key, rawJSON)
	if err != nil {
		return nil, "", err
	}
	return raw, api.VideoResultTaskID(raw), nil
}

func (r videoRecoverClient) FetchTask(ctx context.Context, resourceID string) (json.RawMessage, error) {
	raw, _, err := r.client.GetUnifiedTask(ctx, "video", resourceID)
	return raw, err
}
