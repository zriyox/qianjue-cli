package idem

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/output"
)

const recTask = `{"id":2045019196159766531,"taskType":"TXT2IMG","status":"PENDING"}`

// script serves a sequence of responses for the idempotency-status endpoint
// plus fixed handlers for create/get.
type script struct {
	statusResponses []string // 每次 GET idempotency 依次弹出，"404" 表示 404
	createResponse  string   // POST 响应；"DROP" 表示断连
	posts           int
	statusCalls     int
}

func (s *script) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/idempotency":
			i := s.statusCalls
			if i >= len(s.statusResponses) {
				i = len(s.statusResponses) - 1
			}
			s.statusCalls++
			body := s.statusResponses[i]
			if body == "404" {
				w.WriteHeader(404)
				fmt.Fprint(w, `{"code":404,"message":"Integration 幂等请求不存在","data":null}`)
				return
			}
			fmt.Fprint(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/integration/image-tasks":
			s.posts++
			if s.createResponse == "DROP" {
				hj, _ := w.(http.Hijacker)
				conn, _, _ := hj.Hijack()
				conn.Close()
				return
			}
			fmt.Fprint(w, s.createResponse)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/2045019196159766531":
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":%s}`, recTask)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func statusBody(status string, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"code":200,"message":"操作成功","data":{"requestId":2045019196159766532,"status":"%s","attemptNo":1,"resourceType":null,"resourceId":null,"errorCode":null,"errorMessage":null,"retryable":null,"retryAfterAt":null,"completedAt":null%s}}`, status, extra)
}

type recoverEnv struct {
	getenv config.Getenv
	log    *RequestLog
	p      *output.Printer
	stderr *bytes.Buffer
}

func newRecoverEnv(t *testing.T) *recoverEnv {
	getenv := stateEnv(t)
	l, err := NewRequestLog("local", "http://localhost:7777/api/v1", testKey, []byte(contractRequestJSON), time.Now())
	require.NoError(t, err)
	require.NoError(t, WriteLog(getenv, l))
	var stderr bytes.Buffer
	return &recoverEnv{
		getenv: getenv,
		log:    l,
		p:      output.NewPrinter(&bytes.Buffer{}, &stderr, output.FormatJSON, false, true),
		stderr: &stderr,
	}
}

type staticToken struct{}

func (staticToken) Token(ctx context.Context) (string, error) { return "qj_pat_r", nil }
func (staticToken) HandleAuthError(ctx context.Context, code int) (bool, error) {
	return false, nil
}

func fastOpts() RecoverOptions {
	return RecoverOptions{
		PollTimeout:  2 * time.Second,
		PollInterval: func(int) time.Duration { return time.Millisecond },
	}
}

func runRecover(t *testing.T, srvURL string, env *recoverEnv, opts RecoverOptions) (*RecoverOutcome, error) {
	client := api.NewClient(srvURL, 5*time.Second, staticToken{}, api.NewTraceID(), nil)
	return Recover(context.Background(), NewImageRecoverClient(client), env.getenv, env.log, env.p, opts)
}

func TestRecover404ReplaysOriginalOnce(t *testing.T) {
	s := &script{
		statusResponses: []string{"404"},
		createResponse:  fmt.Sprintf(`{"code":200,"message":"操作成功","data":{"task":%s,"historical":false}}`, recTask),
	}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	outcome, err := runRecover(t, srv.URL, env, fastOpts())
	require.NoError(t, err)
	require.NotNil(t, outcome.Result)
	assert.False(t, outcome.Historical)
	assert.Equal(t, 1, s.posts)

	loaded, err := LoadLog(env.getenv, "local", testKey)
	require.NoError(t, err)
	assert.Equal(t, StateSucceeded, loaded.State)
	require.NotNil(t, loaded.TaskID)
	assert.Equal(t, "2045019196159766531", *loaded.TaskID)
}

func TestRecoverProcessingThenSucceededFetchesTask(t *testing.T) {
	succeeded := statusBody("SUCCEEDED", `"resourceType":"DRAW_TASK"`)
	// resourceId 覆盖上面的 null
	succeeded = bytes.NewBufferString(succeeded).String()
	succeeded = replaceOnce(succeeded, `"resourceId":null`, `"resourceId":"2045019196159766531"`)

	s := &script{statusResponses: []string{
		statusBody("PROCESSING", ""),
		statusBody("PROCESSING", ""),
		succeeded,
	}}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	outcome, err := runRecover(t, srv.URL, env, fastOpts())
	require.NoError(t, err)
	require.NotNil(t, outcome.Result)
	assert.True(t, outcome.Historical)
	assert.Contains(t, string(outcome.Result), "2045019196159766531")
	assert.Equal(t, 0, s.posts, "SUCCEEDED 恢复不得重复创建")
	assert.Equal(t, 3, s.statusCalls)

	loaded, _ := LoadLog(env.getenv, "local", testKey)
	assert.Equal(t, StateSucceeded, loaded.State)
}

func TestRecoverFailedFinalStops(t *testing.T) {
	s := &script{statusResponses: []string{statusBody("FAILED_FINAL", `"errorCode2":"x"`)}}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	_, err := runRecover(t, srv.URL, env, fastOpts())
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.ExitFinalFailure, ce.ExitCode)
	assert.Equal(t, 0, s.posts, "FAILED_FINAL 禁止自动重建")

	loaded, _ := LoadLog(env.getenv, "local", testKey)
	assert.Equal(t, StateFailed, loaded.State)
}

func TestRecoverRecoveryRequiredStopsWithRunbook(t *testing.T) {
	s := &script{statusResponses: []string{statusBody("RECOVERY_REQUIRED", "")}}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	_, err := runRecover(t, srv.URL, env, fastOpts())
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.ExitRecoveryRequired, ce.ExitCode)
	assert.Contains(t, ce.Message, "2045019196159766532", "必须输出 requestId")
	assert.Contains(t, ce.Message, RunbookPath)
	assert.Equal(t, 0, s.posts)

	loaded, _ := LoadLog(env.getenv, "local", testKey)
	assert.Equal(t, StateRecoveryRequired, loaded.State)
	require.NotNil(t, loaded.RequestID)
	assert.Equal(t, int64(2045019196159766532), *loaded.RequestID)
}

func TestRecoverFailedRetryableNullRetryAfterReplaysImmediately(t *testing.T) {
	s := &script{
		statusResponses: []string{statusBody("FAILED_RETRYABLE", `"retryable2":true`)},
		createResponse:  fmt.Sprintf(`{"code":200,"message":"操作成功","data":{"task":%s,"historical":false}}`, recTask),
	}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	outcome, err := runRecover(t, srv.URL, env, fastOpts())
	require.NoError(t, err)
	require.NotNil(t, outcome.Result)
	assert.False(t, outcome.Historical)
	assert.Equal(t, 1, s.posts)
}

func TestRecoverReplayTransportBoundedToOneReplay(t *testing.T) {
	s := &script{
		statusResponses: []string{"404"},
		createResponse:  "DROP",
	}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	_, err := runRecover(t, srv.URL, env, fastOpts())
	require.Error(t, err)
	assert.Equal(t, clierr.KindTransport, clierr.AsCLIError(err).Kind)
	// net/http 对带 Idempotency-Key 的 POST 在连接断开时会自动原样重试一次
	// （同 Key 同 Body，契约安全），所以底层 POST 计数可能是 2；
	// Recover 层面的重放只发生一次：第二次 404 直接终止，状态查询恰好两次。
	assert.LessOrEqual(t, s.posts, 2)
	assert.Equal(t, 2, s.statusCalls, "重放一次后第二个 404 必须终止，不得循环")
}

func TestRecoverPollTimeoutIsWaitTimeout(t *testing.T) {
	s := &script{statusResponses: []string{statusBody("PROCESSING", "")}}
	srv := s.server(t)
	defer srv.Close()

	env := newRecoverEnv(t)
	_, err := runRecover(t, srv.URL, env, RecoverOptions{
		PollTimeout:  50 * time.Millisecond,
		PollInterval: func(int) time.Duration { return 30 * time.Millisecond },
	})
	require.Error(t, err)
	assert.Equal(t, clierr.ExitInProgressOrWaitTimeout, clierr.AsCLIError(err).ExitCode)
	assert.Contains(t, clierr.AsCLIError(err).Message, "image resume")
}

func replaceOnce(s, old, new string) string {
	i := bytes.Index([]byte(s), []byte(old))
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}
