package cmd

import (
	"encoding/json"
	"fmt"
	"io"
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

// The provider only ever sees items[].inputImageUrl plus
// items[].seedanceConfig.referenceImages, so a document that lists every
// material in items[].inputImages must be shaped the same way the web client
// shapes it before the body leaves the CLI.
func TestVideoCreateNormalizesSeedanceInputImages(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, stdout, stderr := imageApp(t, srv.URL)
	request := `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"转场","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"},{"url":"https://x/3.png"}]}]}`
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, request), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	var sent map[string]any
	require.NoError(t, json.Unmarshal(body, &sent), "body: %s", body)
	items, ok := sent["items"].([]any)
	require.True(t, ok, "body: %s", body)
	item := items[0].(map[string]any)
	assert.Equal(t, "https://x/1.png", item["inputImageUrl"])
	config, ok := item["seedanceConfig"].(map[string]any)
	require.True(t, ok, "body: %s", body)
	references := config["referenceImages"].([]any)
	require.Len(t, references, 2)
	assert.Equal(t, "https://x/2.png", references[0].(map[string]any)["url"])
	assert.Equal(t, "https://x/3.png", references[1].(map[string]any)["url"])

	// The rewrite is disclosed on stderr so the JSON envelope on stdout stays a
	// single clean document.
	assert.Contains(t, stderr.String(), "referenceImages")
	assert.Contains(t, stdout.String(), "9001")

	// The local request log must carry the normalized document, otherwise a
	// replay with the same key would be rejected as a digest conflict.
	env := decodeEnvelope(t, stdout)
	key := env.Data.(map[string]any)["idempotencyKey"].(string)
	log, err := idem.LoadLog(app.getenv, "default", key)
	require.NoError(t, err)
	assert.Contains(t, string(log.RequestJSON), "referenceImages")
}

// kling-v3-omni takes the same material list but a different provider field:
// the extra images must reach omniConfig.imageList, the field
// KlingOmniVideoModeAdapter.addImageList actually reads.
func TestVideoCreateNormalizesKlingOmniInputImages(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9002,"totalCount":1,"taskIds":[43],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, stdout, stderr := imageApp(t, srv.URL)
	request := `{"sourceType":"VIDEO_TASK","modelCode":"kling-v3-omni","items":[{"prompt":"镜头","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"},{"url":"https://x/3.png"}]}]}`
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, request), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	var sent map[string]any
	require.NoError(t, json.Unmarshal(body, &sent), "body: %s", body)
	item := sent["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "https://x/1.png", item["inputImageUrl"])
	assert.NotContains(t, item, "seedanceConfig")
	config, ok := item["omniConfig"].(map[string]any)
	require.True(t, ok, "body: %s", body)
	imageList := config["imageList"].([]any)
	require.Len(t, imageList, 2)
	assert.Equal(t, "https://x/2.png", imageList[0].(map[string]any)["imageUrl"])
	assert.Equal(t, "https://x/3.png", imageList[1].(map[string]any)["imageUrl"])

	// The disclosure names the field that actually reaches the provider.
	assert.Contains(t, stderr.String(), "omniConfig.imageList")
	assert.Contains(t, stdout.String(), "9002")

	env := decodeEnvelope(t, stdout)
	key := env.Data.(map[string]any)["idempotencyKey"].(string)
	log, err := idem.LoadLog(app.getenv, "default", key)
	require.NoError(t, err)
	assert.Contains(t, string(log.RequestJSON), "imageList")
}

// Sibling kinds derive their images server-side from inputImages and reject
// seedanceConfig, so the normalizer must not touch their documents.
func TestVideoCreateNormalizationSkipsSiblingKinds(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"taskId":51,"status":"PENDING"}}`)
	}))
	defer srv.Close()

	request := `{"modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`
	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"video", "gesture-replica", "--request", writeRequestFile(t, request), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, request, string(body))
}

// A request that already carries the provider-facing fields is sent verbatim:
// no reordering, no reformatting, no duplicate references.
func TestVideoCreateNormalizationLeavesReadyDocumentUntouched(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	request := `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"inputImageUrl":"https://x/1.png","seedanceConfig":{"referenceImages":[{"url":"https://x/2.png"}]},"inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`
	app, stdout, stderr := imageApp(t, srv.URL)
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, request), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, request, string(body))
	assert.NotContains(t, stderr.String(), "已按 Web 端规则")
}

// Re-running the same --request file with the same key is the documented
// "结果未知时用同一个 --idempotency-key 重放" recovery, so a log archived
// before the body rewrite must still replay — and must replay its archived
// bytes, which is the shape the server already fingerprinted.
func TestVideoCreateReplaysLogArchivedBeforeNormalization(t *testing.T) {
	var body []byte
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, stdout, stderr := imageApp(t, srv.URL)
	request := `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"转场","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`
	key := "qjcli-video-archived"

	// Mimic a CLI that ran before the rewrite existed: the log holds the
	// untouched document, so its digest is the pre-rewrite one.
	archived, err := idem.NewRequestLog("default", srv.URL, key, []byte(request), time.Now())
	require.NoError(t, err)
	archived.Operation = idem.OperationVideoCreate
	archived.Kind = string(api.VideoKindCreate)
	require.NoError(t, idem.WriteLog(app.getenv, archived))

	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, request), "--idempotency-key", key, "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s stderr: %s", stdout.String(), stderr.String())
	assert.Equal(t, int32(1), posts.Load())
	assert.Contains(t, stderr.String(), "按归档原文重放")

	// The archived bytes are what the server already fingerprinted: the replay
	// must carry the untouched document, not the rewritten one.
	var sent, want map[string]any
	require.NoError(t, json.Unmarshal(body, &sent), "body: %s", body)
	require.NoError(t, json.Unmarshal([]byte(request), &want))
	assert.Equal(t, want, sent, "body: %s", body)
	item := sent["items"].([]any)[0].(map[string]any)
	assert.NotContains(t, item, "seedanceConfig")
	assert.NotContains(t, item, "inputImageUrl")
}

// The archived-replay tolerance is model-agnostic, so the omni rewrite must
// compose with it too: a log written before the omni mapping existed still
// replays its archived bytes instead of turning into an exit-6 conflict.
func TestVideoCreateReplaysOmniLogArchivedBeforeNormalization(t *testing.T) {
	var body []byte
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		body, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9003,"totalCount":1,"taskIds":[44],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, stdout, stderr := imageApp(t, srv.URL)
	request := `{"sourceType":"VIDEO_TASK","modelCode":"kling-v3-omni","items":[{"prompt":"镜头","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`
	key := "qjcli-video-omni-archived"

	archived, err := idem.NewRequestLog("default", srv.URL, key, []byte(request), time.Now())
	require.NoError(t, err)
	archived.Operation = idem.OperationVideoCreate
	archived.Kind = string(api.VideoKindCreate)
	require.NoError(t, idem.WriteLog(app.getenv, archived))

	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, request), "--idempotency-key", key, "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s stderr: %s", stdout.String(), stderr.String())
	assert.Equal(t, int32(1), posts.Load())
	assert.Contains(t, stderr.String(), "按归档原文重放")

	var sent, want map[string]any
	require.NoError(t, json.Unmarshal(body, &sent), "body: %s", body)
	require.NoError(t, json.Unmarshal([]byte(request), &want))
	assert.Equal(t, want, sent, "body: %s", body)
	item := sent["items"].([]any)[0].(map[string]any)
	assert.NotContains(t, item, "omniConfig")
	assert.NotContains(t, item, "inputImageUrl")
}

// The archived-document tolerance must not weaken the conflict guard: a
// different document under the same key is still exit 6.
func TestVideoCreateStillRejectsDifferentDocumentUnderSameKey(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"batchId":9001,"totalCount":1,"taskIds":[42],"status":"PENDING"}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	key := "qjcli-video-conflict"
	archived := `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"原始","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/2.png"}]}]}`
	log, err := idem.NewRequestLog("default", srv.URL, key, []byte(archived), time.Now())
	require.NoError(t, err)
	log.Operation = idem.OperationVideoCreate
	log.Kind = string(api.VideoKindCreate)
	require.NoError(t, idem.WriteLog(app.getenv, log))

	different := `{"sourceType":"VIDEO_TASK","modelCode":"SEEDANCE_2_0_MINI","items":[{"prompt":"改了","inputImages":[{"url":"https://x/1.png"},{"url":"https://x/9.png"}]}]}`
	exit := run(app, []string{"video", "create", "--request", writeRequestFile(t, different), "--idempotency-key", key, "--output", "json"})
	assert.Equal(t, clierr.ExitIdempotencyConflict, exit, "stdout: %s", stdout.String())
	assert.Equal(t, int32(0), posts.Load())
}
