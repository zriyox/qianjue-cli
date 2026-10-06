package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const validKouboRequest = `{"videoUrl":"https://example.com/a.mp4","templateId":"t1","title":"标题","durationSeconds":15}`

// 口播 / 混剪提交与其它视频创建同一套：带 Idempotency-Key、先写本地请求日志；
// 输出保持原来的 data.taskId，只追加 idempotencyKey（已有脚本按 data.taskId 解析）。
func TestVideoStudioSubmitCarriesIdempotencyKeyAndKeepsTaskIDShape(t *testing.T) {
	cases := map[string]struct {
		path    string
		command string
		kind    string
	}{
		"koubo":     {"/integration/video-studio/koubo/tasks", "video-studio.koubo.submit", "koubo"},
		"smart-mix": {"/integration/video-studio/smart-mix/tasks", "video-studio.smart-mix.submit", "smart-mix"},
	}
	for sub, want := range cases {
		t.Run(sub, func(t *testing.T) {
			var sentKey, sentPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				sentPath = r.URL.Path
				sentKey = r.Header.Get(api.IdempotencyKeyHeader)
				fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"taskId":2107301441819250690}}`)
			}))
			defer srv.Close()

			app, stdout, _ := imageApp(t, srv.URL)
			exit := run(app, []string{"video-studio", sub, "submit", "--request", writeRequestFile(t, validKouboRequest), "--output", "json"})
			require.Equal(t, 0, exit, "stdout: %s", stdout.String())
			assert.Equal(t, want.path, sentPath)
			assert.Contains(t, sentKey, "qjcli-video-")

			env := decodeEnvelope(t, stdout)
			assert.Equal(t, want.command, env.Command)
			data := env.Data.(map[string]any)
			assert.Equal(t, sentKey, data["idempotencyKey"])
			// taskId 还在 data 顶层，19 位雪花 ID 原样透传不丢精度
			assert.Contains(t, stdout.String(), `"taskId": 2107301441819250690`)

			log, err := idem.LoadLog(app.getenv, "default", sentKey)
			require.NoError(t, err)
			assert.Equal(t, idem.OperationVideoCreate, log.Operation)
			assert.Equal(t, want.kind, log.Kind)
			assert.Equal(t, idem.StateSucceeded, log.State)
		})
	}
}

// 结果不明（断连）时按原 Key 恢复，重放打回口播接口而不是普通视频接口。
func TestVideoStudioSubmitTransportThenRecoveredToTheStudioEndpoint(t *testing.T) {
	var posts atomic.Int32
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/video-studio/koubo/tasks":
			keys = append(keys, r.Header.Get(api.IdempotencyKeyHeader))
			if posts.Add(1) == 1 {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"taskId":42}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/video-tasks/idempotency":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"video-studio", "koubo", "submit", "--request", writeRequestFile(t, validKouboRequest),
		"--idempotency-key", "kreplay", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, []string{"kreplay", "kreplay"}, keys, "恢复必须复用原 Key")
}
