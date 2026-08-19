package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVideoTaskPaths(t *testing.T) {
	cases := []struct {
		kind VideoKind
		path string
	}{
		{VideoKindCreate, "/integration/video-tasks"},
		{VideoKindEdit, "/integration/video-tasks/edit"},
		{VideoKindUpscale, "/integration/video-tasks/upscale"},
		{VideoKindGestureReplica, "/integration/video-tasks/gesture-replica"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			var gotPath, gotKey string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				gotPath = r.URL.Path
				gotKey = r.Header.Get(IdempotencyKeyHeader)
				fmt.Fprint(w, ok(`{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}`))
			}))
			defer srv.Close()

			c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
			raw, err := c.CreateVideoTask(context.Background(), tc.kind, "vkey-1", []byte(`{"sourceType":"VIDEO_TASK"}`))
			require.NoError(t, err)
			assert.Equal(t, tc.path, gotPath)
			assert.Equal(t, "vkey-1", gotKey)
			assert.Contains(t, string(raw), "9001", "int64 batchId 透传")
		})
	}
}

func TestCreateVideoTaskBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"code":2020,"message":"结果不确定","data":{"requestId":7,"status":"RECOVERY_REQUIRED"}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	_, err := c.CreateVideoTask(context.Background(), VideoKindCreate, "k", []byte(`{}`))
	require.Error(t, err)
}

func TestGetVideoIdempotencyStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/integration/video-tasks/idempotency", r.URL.Path)
		assert.Equal(t, "vkey-1", r.URL.Query().Get("key"))
		fmt.Fprint(w, ok(`{"requestId":7,"status":"SUCCEEDED","resourceType":"VIDEO_BATCH","resourceId":"9001"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	out, err := c.GetVideoIdempotencyStatus(context.Background(), "vkey-1")
	require.NoError(t, err)
	assert.Equal(t, IdemSucceeded, out.Status)
	require.NotNil(t, out.ResourceID)
	assert.Equal(t, "9001", *out.ResourceID)
}

func TestVideoResultTaskID(t *testing.T) {
	assert.Equal(t, "42", VideoResultTaskID([]byte(`{"taskId":42}`)))
	assert.Equal(t, "7", VideoResultTaskID([]byte(`{"batchId":9001,"taskIds":[7,8,9]}`)))
	assert.Equal(t, "2089609747366940674", VideoResultTaskID([]byte(`{"taskId":2089609747366940674}`)), "int64 不丢精度")
	assert.Equal(t, "", VideoResultTaskID([]byte(`{"batchId":9001}`)))
	assert.Equal(t, "", VideoResultTaskID([]byte(`not json`)))
}
