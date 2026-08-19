package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const testTaskJSON = `{"id":2045019196159766531,"taskType":"TXT2IMG","modelCode":"GPT_IMAGE_2","status":"PENDING"}`

func writeRequestFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "req.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

const validRequest = `{"type":"TXT2IMG","prompt":"白底马克杯","modelCode":"GPT_IMAGE_2","aspectRatio":"1:1","outputResolution":"1K","outputCount":1}`

// createServer answers POST /integration/image-tasks. Each call is recorded.
func createServer(t *testing.T, historical bool, calls *atomic.Int32, onRequest func(r *http.Request)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/integration/image-tasks", r.URL.Path)
		calls.Add(1)
		if onRequest != nil {
			onRequest(r)
		}
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"task":%s,"historical":%v}}`, testTaskJSON, historical)
	}))
}

func imageApp(t *testing.T, srvURL string) (*appContext, *bytes.Buffer, *bytes.Buffer) {
	app, stdout, stderr := xdgApp(t, map[string]string{
		"QIANJUE_API_BASE_URL": srvURL,
		"QIANJUE_TOKEN":        "qj_pat_testtoken",
	})
	return app, stdout, stderr
}

func TestImageCreateHappyPathAutoKey(t *testing.T) {
	var calls atomic.Int32
	var sentKey string
	srv := createServer(t, false, &calls, func(r *http.Request) {
		sentKey = r.Header.Get(api.IdempotencyKeyHeader)
		assert.Equal(t, "Bearer qj_pat_testtoken", r.Header.Get("Authorization"))
	})
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "image.create", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, false, data["historical"])
	key, _ := data["idempotencyKey"].(string)
	assert.Contains(t, key, "qjcli-image-")
	assert.Equal(t, key, sentKey, "输出的 Key 必须就是发送的 Key")
	assert.Contains(t, stdout.String(), "2045019196159766531", "task 原样透传")
	assert.EqualValues(t, 200, env.Meta["httpStatus"])

	// 本地日志已更新为 SUCCEEDED 且带 taskId
	log, err := idem.LoadLog(app.getenv, "default", key)
	require.NoError(t, err)
	assert.Equal(t, idem.StateSucceeded, log.State)
	require.NotNil(t, log.TaskID)
	assert.Equal(t, "2045019196159766531", *log.TaskID)
}

func TestImageCreateWritesLogBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	var app *appContext
	key := "qjcli-image-pretest-0000"
	srv := createServer(t, false, &calls, func(r *http.Request) {
		// 服务端收到请求时，本地日志必须已经落盘（SUBMITTING）
		log, err := idem.LoadLog(app.getenv, "default", key)
		require.NoError(t, err, "发请求前必须已持久化本地日志")
		assert.Equal(t, idem.StateSubmitting, log.State)
	})
	defer srv.Close()

	var stdout2 *bytes.Buffer
	app, stdout2, _ = imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", key, "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout2.String())
}

func TestImageCreateHistoricalReplay(t *testing.T) {
	var calls atomic.Int32
	srv := createServer(t, true, &calls, nil)
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", "replay-key", "--output", "json"})
	require.Equal(t, 0, exit)
	assert.Equal(t, true, decodeEnvelope(t, stdout).Data.(map[string]any)["historical"])
}

func TestImageCreateLocalSameKeyDifferentJSONRejected(t *testing.T) {
	var calls atomic.Int32
	srv := createServer(t, false, &calls, nil)
	defer srv.Close()

	app, _, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", "conflict-key", "--output", "json"})
	require.Equal(t, 0, exit)
	require.Equal(t, int32(1), calls.Load())

	// 同 Key 不同 JSON：本地直接拒绝，不发 HTTP
	other := `{"type":"TXT2IMG","prompt":"完全不同的请求"}`
	app2, stdout2, _ := imageApp(t, srv.URL)
	app2.getenv = app.getenv // 同一套 XDG 目录
	exit = run(app2, []string{"image", "create", "--request", writeRequestFile(t, other),
		"--idempotency-key", "conflict-key", "--output", "json"})
	assert.Equal(t, clierr.ExitIdempotencyConflict, exit)
	assert.Equal(t, "IDEMPOTENCY_CONFLICT", decodeEnvelope(t, stdout2).Error.Kind)
	assert.Equal(t, int32(1), calls.Load(), "本地冲突不得发 HTTP")
}

func TestImageCreateRecoveryRequiredMarksLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"code":2020,"message":"图片任务创建结果不确定，请勿更换 Idempotency-Key 重复提交","data":{"requestId":2045019196159766532,"status":"RECOVERY_REQUIRED","attemptNo":1,"resourceType":null,"resourceId":null,"errorCode":"INTEGRATION_EXECUTION_UNCERTAIN","retryAfterAt":null}}`)
	}))
	defer srv.Close()

	app, stdout, stderr := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", "recovery-key", "--output", "json"})
	assert.Equal(t, clierr.ExitRecoveryRequired, exit)

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "RECOVERY_REQUIRED", env.Error.Kind)
	assert.EqualValues(t, 2020, env.Error.Code)
	assert.Contains(t, stderr.String(), "admin-recovery-runbook.md")

	log, err := idem.LoadLog(app.getenv, "default", "recovery-key")
	require.NoError(t, err)
	assert.Equal(t, idem.StateRecoveryRequired, log.State)
	require.NotNil(t, log.RequestID)
	assert.Equal(t, int64(2045019196159766532), *log.RequestID)
}

func TestImageCreateTransportErrorEntersRecovery(t *testing.T) {
	// 服务器对所有请求断连：create 未知结果 → 恢复流程的状态查询也失败 → exit 12
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	app, _, stderr := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", "unknown-key", "--output", "json"})
	assert.Equal(t, clierr.ExitTransport, exit)
	assert.Contains(t, stderr.String(), "恢复流程")

	log, err := idem.LoadLog(app.getenv, "default", "unknown-key")
	require.NoError(t, err)
	assert.Equal(t, idem.StateResultUnknown, log.State)
	assert.JSONEq(t, validRequest, string(log.RequestJSON), "原 JSON 保留用于恢复")
}

func TestImageCreateTransportThenRecoveredByReplay(t *testing.T) {
	// 第一次 POST 断连（未知结果）；随后状态查询 404；重放 POST 成功。
	var posts atomic.Int32
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/image-tasks":
			keys = append(keys, r.Header.Get(api.IdempotencyKeyHeader))
			if posts.Add(1) == 1 {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"task":%s,"historical":false}}`, testTaskJSON)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/idempotency":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", "replay-after-unknown", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	require.Equal(t, int32(2), posts.Load())
	assert.Equal(t, []string{"replay-after-unknown", "replay-after-unknown"}, keys, "恢复必须复用原 Key，绝不生成新 Key")

	env := decodeEnvelope(t, stdout)
	assert.True(t, env.OK)
	assert.Equal(t, "replay-after-unknown", env.Data.(map[string]any)["idempotencyKey"])

	log, err := idem.LoadLog(app.getenv, "default", "replay-after-unknown")
	require.NoError(t, err)
	assert.Equal(t, idem.StateSucceeded, log.State)
}

func TestImageCreate2019HintsResume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"code":2019,"message":"Integration 图片任务创建请求仍在处理中","data":{"requestId":9,"status":"PROCESSING","attemptNo":1,"resourceType":null,"resourceId":null,"errorCode":null,"retryAfterAt":null}}`)
	}))
	defer srv.Close()

	app, _, stderr := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--idempotency-key", "busy-key", "--output", "json"})
	assert.Equal(t, clierr.ExitInProgressOrWaitTimeout, exit)
	assert.Contains(t, stderr.String(), "image resume")
}

func TestImageCreateForbiddenFieldRejectedBeforeAnything(t *testing.T) {
	var calls atomic.Int32
	srv := createServer(t, false, &calls, nil)
	defer srv.Close()

	bad := `{"type":"TXT2IMG","prompt":"x","accessToken":"leak"}`
	app, _, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, bad), "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Equal(t, int32(0), calls.Load())
}

func TestImageCreateFromStdin(t *testing.T) {
	var calls atomic.Int32
	srv := createServer(t, false, &calls, nil)
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	app.stdin = bytes.NewReader([]byte(validRequest))
	exit := run(app, []string{"image", "create", "--request", "-", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, int32(1), calls.Load())
}

func TestImageCreateNoCredentialIsAuthError(t *testing.T) {
	var calls atomic.Int32
	srv := createServer(t, false, &calls, nil)
	defer srv.Close()

	app, stdout, _ := xdgApp(t, map[string]string{"QIANJUE_API_BASE_URL": srv.URL})
	withStore(app, cred.NewMemoryStore())
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest), "--output", "json"})
	assert.Equal(t, clierr.ExitAuth, exit)
	assert.Equal(t, "AUTH", decodeEnvelope(t, stdout).Error.Kind)
	assert.Equal(t, int32(0), calls.Load())
}

func TestImageCreateSameKeySameJSONReusesLogAndReplays(t *testing.T) {
	var calls atomic.Int32
	srv := createServer(t, false, &calls, nil)
	defer srv.Close()

	app, _, _ := imageApp(t, srv.URL)
	reqFile := writeRequestFile(t, validRequest)
	require.Equal(t, 0, run(app, []string{"image", "create", "--request", reqFile,
		"--idempotency-key", "same-key", "--output", "json"}))

	app2, _, _ := imageApp(t, srv.URL)
	app2.getenv = app.getenv
	require.Equal(t, 0, run(app2, []string{"image", "create", "--request", reqFile,
		"--idempotency-key", "same-key", "--output", "json"}))
	assert.Equal(t, int32(2), calls.Load())

	log, err := idem.LoadLog(app.getenv, "default", "same-key")
	require.NoError(t, err)
	assert.Equal(t, 2, log.AttemptCount)
}
