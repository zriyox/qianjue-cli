package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const validVideoRequest = `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"一只橙色马克杯在转","durationSeconds":5}]}`

func TestVideoCreateBatchHappyPath(t *testing.T) {
	var posts atomic.Int32
	var sentKey, sentPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		posts.Add(1)
		sentPath = r.URL.Path
		sentKey = r.Header.Get(api.IdempotencyKeyHeader)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, validVideoRequest), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, "/integration/video-tasks", sentPath)
	assert.Contains(t, sentKey, "qjcli-video-")

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "video.create", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, sentKey, data["idempotencyKey"])
	assert.Contains(t, stdout.String(), "9001")

	log, err := idem.LoadLog(app.getenv, "default", sentKey)
	require.NoError(t, err)
	assert.Equal(t, idem.OperationVideoCreate, log.Operation)
	assert.Equal(t, idem.StateSucceeded, log.State)
	require.NotNil(t, log.TaskID)
	assert.Equal(t, "42", *log.TaskID)
}

func TestVideoSubResourcePaths(t *testing.T) {
	cases := map[string]string{
		"edit":            "/integration/video-tasks/edit",
		"upscale":         "/integration/video-tasks/upscale",
		"gesture-replica": "/integration/video-tasks/gesture-replica",
	}
	for sub, wantPath := range cases {
		t.Run(sub, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"taskId":51,"status":"PENDING"}}`)
			}))
			defer srv.Close()

			app, stdout, _ := imageApp(t, srv.URL)
			exit := run(app, []string{"video", sub, "--request", writeRequestFile(t, validVideoRequest), "--output", "json"})
			require.Equal(t, 0, exit, "stdout: %s", stdout.String())
			assert.Equal(t, wantPath, gotPath)
		})
	}
}

func TestVideoCreateWithWaitReachesTerminal(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/video-tasks":
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/tasks/video/42":
			status := "PROCESSING"
			if gets.Add(1) >= 2 {
				status = "COMPLETED"
			}
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"taskId":42,"domain":"VIDEO","status":"%s","media":{"resultMediaList":["https://cdn/v.mp4"]}}}`, status)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	app.waitInterval = func(int) time.Duration { return time.Millisecond }
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, validVideoRequest), "--wait", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Contains(t, stdout.String(), "COMPLETED")
}

func TestVideoCreateSameKeyDifferentJSONRejectedLocally(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"taskIds":[42],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, _, _ := imageApp(t, srv.URL)
	require.Equal(t, 0, run(app, []string{"video", "create", "--request", writeRequestFile(t, validVideoRequest),
		"--idempotency-key", "vconflict", "--output", "json"}))

	other := `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"完全不同"}]}`
	app2, stdout2, _ := imageApp(t, srv.URL)
	app2.getenv = app.getenv
	exit := run(app2, []string{"video", "create", "--request", writeRequestFile(t, other),
		"--idempotency-key", "vconflict", "--output", "json"})
	assert.Equal(t, clierr.ExitIdempotencyConflict, exit)
	assert.Equal(t, "IDEMPOTENCY_CONFLICT", decodeEnvelope(t, stdout2).Error.Kind)
	assert.Equal(t, int32(1), posts.Load(), "本地冲突不得发 HTTP")
}

func TestVideoCreateTransportThenRecoveredByReplay(t *testing.T) {
	// 首个 POST 断连（未知结果）→ 状态查询 404 → 重放 POST 成功。
	var posts atomic.Int32
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/video-tasks":
			keys = append(keys, r.Header.Get(api.IdempotencyKeyHeader))
			if posts.Add(1) == 1 {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"taskIds":[42],"status":"PENDING"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/video-tasks/idempotency":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, validVideoRequest),
		"--idempotency-key", "vreplay", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	require.Equal(t, int32(2), posts.Load())
	assert.Equal(t, []string{"vreplay", "vreplay"}, keys, "恢复必须复用原 Key")

	log, err := idem.LoadLog(app.getenv, "default", "vreplay")
	require.NoError(t, err)
	assert.Equal(t, idem.StateSucceeded, log.State)
	assert.Equal(t, "create", log.Kind)
}

func TestVideoResumeSucceededFetchesUnifiedTask(t *testing.T) {
	statusBody := `{"code":200,"message":"操作成功","data":{"requestId":7,"status":"SUCCEEDED","resourceType":"VIDEO_BATCH","resourceId":"42"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/integration/video-tasks/idempotency":
			fmt.Fprint(w, statusBody)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/tasks/video/42":
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"taskId":42,"domain":"VIDEO","status":"COMPLETED","media":{"resultMediaList":["https://cdn/v.mp4"]}}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	// seed a local video log
	log, err := idem.NewRequestLog("default", srv.URL, "vresume", []byte(validVideoRequest), time.Now())
	require.NoError(t, err)
	log.Operation = idem.OperationVideoCreate
	log.Kind = "create"
	require.NoError(t, idem.WriteLog(app.getenv, log))

	exit := run(app, []string{"video", "resume", "--idempotency-key", "vresume", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "video.resume", env.Command)
	assert.Equal(t, true, env.Data.(map[string]any)["historical"])
	assert.Contains(t, stdout.String(), "COMPLETED")
}

func TestVideoResumeRejectsNonVideoLog(t *testing.T) {
	app, stdout, _ := imageApp(t, "http://localhost:1")
	log, err := idem.NewRequestLog("default", "http://localhost:1", "imgkey", []byte(`{"type":"TXT2IMG"}`), time.Now())
	require.NoError(t, err)
	require.NoError(t, idem.WriteLog(app.getenv, log)) // operation defaults to IMAGE_CREATE

	exit := run(app, []string{"video", "resume", "--idempotency-key", "imgkey", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Contains(t, decodeEnvelope(t, stdout).Error.Message, "不是视频创建请求")
}

func TestVideoCreateForbiddenFieldRejected(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":1,"taskIds":[1]}}`)
	}))
	defer srv.Close()

	bad := `{"sourceType":"VIDEO_TASK","accessToken":"leak","items":[]}`
	app, _, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, bad), "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Equal(t, int32(0), posts.Load())
}
