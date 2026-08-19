package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const detailImageRequest = `{"requestId":"r-1","generationMode":"AUTO_PACKAGE","productImageUrls":["https://cdn/x.png"],"mainImageCount":1,"detailImageCount":2}`

func TestDetailImageCreateSendsIdempotencyKeyAndLogsFirst(t *testing.T) {
	var sentKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentKey, gotPath = r.Header.Get(api.IdempotencyKeyHeader), r.URL.Path
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"result":{"messageId":"msg-1","items":[{"itemId":"i1","status":"PENDING"}]},"historical":false}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"detail-image", "create", "--request", writeRequestFile(t, detailImageRequest), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	assert.Equal(t, "/integration/detail-image-tasks", gotPath)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "detail-image.create", env.Command)
	key := env.Data.(map[string]any)["idempotencyKey"].(string)
	assert.Contains(t, key, "qjcli-detail-image-")
	assert.Equal(t, key, sentKey, "输出的 Key 必须就是发送的 Key")
	assert.Contains(t, stdout.String(), "msg-1")

	// 本地日志必须先写、且创建成功后置为 SUCCEEDED
	log, err := idem.LoadLog(app.getenv, "default", key)
	require.NoError(t, err)
	assert.Equal(t, idem.OperationDetailImageCreate, log.Operation)
	assert.Equal(t, idem.StateSucceeded, log.State)
}

// 结果未知时不得自动重发；必须引导用户用同一个 Key 安全重放
func TestDetailImageCreateUnknownResultTellsUserToReplaySameKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close() // 模拟连接被切断
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"detail-image", "create", "--request", writeRequestFile(t, detailImageRequest), "--output", "json"})

	assert.Equal(t, clierr.ExitTransport, exit)
	msg := decodeEnvelope(t, stdout).Error.Message
	assert.Contains(t, msg, "不要换新 Key")
	assert.Contains(t, msg, "--idempotency-key")
}

func TestDetailImageCreateRejectsSameKeyDifferentBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"result":{"messageId":"msg-1"},"historical":false}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	require.Equal(t, 0, run(app, []string{"detail-image", "create",
		"--request", writeRequestFile(t, detailImageRequest), "--idempotency-key", "fixed-key", "--output", "json"}))

	stdout.Reset()
	exit := run(app, []string{"detail-image", "create",
		"--request", writeRequestFile(t, `{"requestId":"r-2","generationMode":"MAIN_IMAGE"}`),
		"--idempotency-key", "fixed-key", "--output", "json"})
	assert.Equal(t, clierr.ExitIdempotencyConflict, exit)
}

func TestDetailImageListPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/integration/detail-image-tasks", r.URL.Path)
		assert.Equal(t, "20", r.URL.Query().Get("limit"))
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":[{"messageId":"msg-1","items":[{"itemId":"i1","status":"SUCCESS","resultImageUrl":"https://cdn/a.png"}]}]}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	require.Equal(t, 0, run(app, []string{"detail-image", "list", "--output", "json"}))
	assert.Equal(t, "detail-image.list", decodeEnvelope(t, stdout).Command)
	assert.Contains(t, stdout.String(), "resultImageUrl")
}
