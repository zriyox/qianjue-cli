package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

func TestImageRequestStatusPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/integration/image-tasks/idempotency", r.URL.Path)
		require.Equal(t, "my-key", r.URL.Query().Get("key"))
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"requestId":2045019196159766532,"status":"SUCCEEDED","attemptNo":1,"resourceType":"DRAW_TASK","resourceId":"2045019196159766531","errorCode":null,"errorMessage":null,"retryable":false,"retryAfterAt":null,"completedAt":"2026-08-17 18:03:00"}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "request-status", "--idempotency-key", "my-key", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "image.request-status", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, "SUCCEEDED", data["status"])
	assert.Equal(t, "DRAW_TASK", data["resourceType"])
	assert.Equal(t, "2045019196159766531", data["resourceId"])
	assert.Contains(t, stdout.String(), "2045019196159766532")
}

func TestImageRequestStatusNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "request-status", "--idempotency-key", "ghost", "--output", "json"})
	assert.Equal(t, clierr.ExitNotFound, exit)
	assert.Equal(t, "NOT_FOUND", decodeEnvelope(t, stdout).Error.Kind)
}

func TestImageRequestStatusKeyRequired(t *testing.T) {
	app, stdout, _ := imageApp(t, "http://localhost:1")
	exit := run(app, []string{"image", "request-status", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "USAGE", env.Error.Kind)
	assert.Contains(t, env.Error.Message, "idempotency-key")
}
