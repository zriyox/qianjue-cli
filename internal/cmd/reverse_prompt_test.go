package cmd

import (
	"encoding/json"
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

func TestReversePromptCreateFromVideoURL(t *testing.T) {
	var sentKey, gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentKey, gotPath = r.Header.Get(api.IdempotencyKeyHeader), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"result":{"sessionId":9001,"status":"PROCESSING"},"historical":false}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"reverse-prompt", "create", "--video-url", "https://cdn/a.mp4", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	assert.Equal(t, "/integration/video-reverse-prompt/sessions", gotPath)
	// 只给 --video-url 时，CLI 自己组请求体，用户不必写 JSON 文件
	assert.Equal(t, "https://cdn/a.mp4", body["videoUrl"])
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "reverse-prompt.create", env.Command)
	key := env.Data.(map[string]any)["idempotencyKey"].(string)
	assert.Equal(t, key, sentKey)
	assert.Contains(t, stdout.String(), "9001")

	log, err := idem.LoadLog(app.getenv, "default", key)
	require.NoError(t, err)
	assert.Equal(t, idem.OperationReversePromptCreate, log.Operation)
	assert.Equal(t, idem.StateSucceeded, log.State)
}

func TestReversePromptCreateRejectsBothInputs(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	exit := run(app, []string{"reverse-prompt", "create",
		"--video-url", "https://cdn/a.mp4", "--request", writeRequestFile(t, `{"videoUrl":"x"}`), "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
}

func TestReversePromptCreateRequiresInput(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	assert.Equal(t, clierr.ExitUsage, run(app, []string{"reverse-prompt", "create", "--output", "json"}))
}

func TestReversePromptGetReturnsStructuredScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/integration/video-reverse-prompt/sessions/9001", r.URL.Path)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"sessionId":9001,"status":"SUCCESS","structuredContent":"{\"shots\":[]}"}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	require.Equal(t, 0, run(app, []string{"reverse-prompt", "get", "9001", "--output", "json"}))
	assert.Equal(t, "reverse-prompt.get", decodeEnvelope(t, stdout).Command)
	assert.Contains(t, stdout.String(), "structuredContent")
}

func TestReversePromptGetRejectsBadID(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	assert.Equal(t, clierr.ExitUsage, run(app, []string{"reverse-prompt", "get", "abc", "--output", "json"}))
}
