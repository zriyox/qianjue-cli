package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

// seedLog writes a local request log for the app's XDG state dir.
func seedLog(t *testing.T, app *appContext, key, apiBaseURL string) *idem.RequestLog {
	t.Helper()
	log, err := idem.NewRequestLog("default", apiBaseURL, key, []byte(validRequest), time.Now())
	require.NoError(t, err)
	require.NoError(t, idem.WriteLog(app.getenv, log))
	return log
}

func TestImageResumeReplaysAfter404(t *testing.T) {
	var posts atomic.Int32
	var postedKeys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/idempotency":
			w.WriteHeader(404)
			fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
		case r.Method == http.MethodPost && r.URL.Path == "/integration/image-tasks":
			posts.Add(1)
			postedKeys = append(postedKeys, r.Header.Get(api.IdempotencyKeyHeader))
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"task":%s,"historical":false}}`, testTaskJSON)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	seedLog(t, app, "resume-key", srv.URL)

	exit := run(app, []string{"image", "resume", "--idempotency-key", "resume-key", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, []string{"resume-key"}, postedKeys, "resume 必须复用原 Key")

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "image.resume", env.Command)
	assert.Equal(t, "resume-key", env.Data.(map[string]any)["idempotencyKey"])

	log, err := idem.LoadLog(app.getenv, "default", "resume-key")
	require.NoError(t, err)
	assert.Equal(t, idem.StateSucceeded, log.State)
}

func TestImageResumeSucceededStatusFetchesTaskWithWait(t *testing.T) {
	statusBody := `{"code":200,"message":"操作成功","data":{"requestId":7,"status":"SUCCEEDED","attemptNo":1,"resourceType":"DRAW_TASK","resourceId":"2045019196159766531","errorCode":null,"errorMessage":null,"retryable":false,"retryAfterAt":null,"completedAt":null}}`
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/idempotency":
			fmt.Fprint(w, statusBody)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/2045019196159766531":
			status := "PROCESSING"
			if gets.Add(1) >= 2 {
				status = "COMPLETED"
			}
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"id":2045019196159766531,"taskType":"TXT2IMG","status":"%s","resultImageUrls":["https://cdn/x.png"]}}`, status)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	app.waitInterval = func(int) time.Duration { return time.Millisecond }
	seedLog(t, app, "resume-wait-key", srv.URL)

	exit := run(app, []string{"image", "resume", "--idempotency-key", "resume-wait-key", "--wait", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	env := decodeEnvelope(t, stdout)
	data := env.Data.(map[string]any)
	assert.Equal(t, true, data["historical"], "SUCCEEDED 回查语义上是历史结果")
	assert.Contains(t, stdout.String(), `"COMPLETED"`)
}

func TestImageResumeWithoutLocalLogIsNotFound(t *testing.T) {
	app, stdout, _ := imageApp(t, "http://localhost:7777/api/v1")
	exit := run(app, []string{"image", "resume", "--idempotency-key", "ghost", "--output", "json"})
	assert.Equal(t, clierr.ExitNotFound, exit)
	assert.Equal(t, "NOT_FOUND", decodeEnvelope(t, stdout).Error.Kind)
}

func TestImageResumeTamperedLogRejected(t *testing.T) {
	app, stdout, _ := imageApp(t, "http://localhost:7777/api/v1")
	log := seedLog(t, app, "tampered-key", "http://localhost:7777/api/v1")

	// 直接篡改落盘文件里的 requestJson
	path, err := idem.LogPath(app.getenv, "default", "tampered-key")
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	tampered := []byte(string(raw))
	tampered = []byte(replaceFirst(string(tampered), "白底马克杯", "被篡改内容"))
	require.NoError(t, os.WriteFile(path, tampered, 0o600))
	_ = log

	exit := run(app, []string{"image", "resume", "--idempotency-key", "tampered-key", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Contains(t, decodeEnvelope(t, stdout).Error.Message, "digest 不一致")
}

func TestImageResumeAPIOriginMismatchNeedsFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	seedLog(t, app, "origin-key", "http://localhost:9999/api/v1") // 与当前 profile 不同

	exit := run(app, []string{"image", "resume", "--idempotency-key", "origin-key", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Contains(t, decodeEnvelope(t, stdout).Error.Message, "--allow-api-origin-change")

	// 带 flag 后放行（404 → 重放，这里只验证不再被 origin 检查拦截）
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"task":%s,"historical":false}}`, testTaskJSON)
			return
		}
		w.WriteHeader(404)
		fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
	}))
	defer srv2.Close()

	app2, stdout2, _ := imageApp(t, srv2.URL)
	app2.getenv = overlayEnv(app.getenv, map[string]string{"QIANJUE_API_BASE_URL": srv2.URL})
	exit = run(app2, []string{"image", "resume", "--idempotency-key", "origin-key",
		"--allow-api-origin-change", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout2.String())
}

func overlayEnv(base func(string) string, overrides map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := overrides[k]; ok {
			return v
		}
		return base(k)
	}
}

func replaceFirst(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
