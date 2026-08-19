package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

func taskBody(status, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"code":200,"message":"操作成功","data":{"id":2045019196159766531,"taskType":"TXT2IMG","modelCode":"GPT_IMAGE_2","status":"%s","resultImageUrls":null%s}}`, status, extra)
}

// taskServer serves GET task with a scripted status sequence and records
// cancel calls.
type taskServer struct {
	statuses []string
	gets     atomic.Int32
	cancels  atomic.Int32
	extra    string
}

func (s *taskServer) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/2045019196159766531":
			i := int(s.gets.Add(1)) - 1
			if i >= len(s.statuses) {
				i = len(s.statuses) - 1
			}
			fmt.Fprint(w, taskBody(s.statuses[i], s.extra))
		case r.Method == http.MethodPut && r.URL.Path == "/integration/image-tasks/2045019196159766531/cancel":
			s.cancels.Add(1)
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":"任务取消成功"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func fastWaitApp(t *testing.T, srvURL string) (*appContext, func() string) {
	app, stdout, _ := imageApp(t, srvURL)
	app.waitInterval = func(int) time.Duration { return time.Millisecond }
	return app, func() string { return stdout.String() }
}

const taskIDArg = "2045019196159766531"

func TestImageGetPassthrough(t *testing.T) {
	s := &taskServer{statuses: []string{"COMPLETED"}, extra: `"resultImageUrls":["https://cdn/x.png"]`}
	srv := s.server(t)
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "get", taskIDArg, "--output", "json"})
	require.Equal(t, 0, exit)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "image.get", env.Command)
	assert.Contains(t, stdout.String(), "2045019196159766531")
}

func TestImageGetNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"code":3002,"message":"资源不存在","data":null}`)
	}))
	defer srv.Close()

	app, _, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "get", taskIDArg, "--output", "json"})
	assert.Equal(t, clierr.ExitNotFound, exit)
}

func TestImageGetInvalidID(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	exit := run(app, []string{"image", "get", "abc", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
}

func TestImageWaitSixStates(t *testing.T) {
	cases := []struct {
		name     string
		statuses []string
		extra    string
		wantExit int
	}{
		{"pending→processing→completed", []string{"PENDING", "PROCESSING", "COMPLETED"}, "", 0},
		{"failed", []string{"PENDING", "FAILED"}, `"errorMessage":"provider error"`, clierr.ExitTaskFailed},
		{"timeout", []string{"TIMEOUT"}, "", clierr.ExitTaskFailed},
		{"cancelled", []string{"CANCELLED"}, `"cancelReason":"用户取消"`, clierr.ExitTaskCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &taskServer{statuses: tc.statuses, extra: tc.extra}
			srv := s.server(t)
			defer srv.Close()

			app, _ := fastWaitApp(t, srv.URL)
			exit := run(app, []string{"image", "wait", taskIDArg, "--output", "json"})
			assert.Equal(t, tc.wantExit, exit)
			assert.Equal(t, int32(0), s.cancels.Load(), "wait 终态不得触发取消")
		})
	}
}

func TestImageWaitLocalTimeoutDoesNotCancel(t *testing.T) {
	s := &taskServer{statuses: []string{"PROCESSING"}}
	srv := s.server(t)
	defer srv.Close()

	app, getStdout := fastWaitApp(t, srv.URL)
	exit := run(app, []string{"image", "wait", taskIDArg, "--wait-timeout", "30ms", "--output", "json"})
	assert.Equal(t, clierr.ExitInProgressOrWaitTimeout, exit)
	assert.Equal(t, int32(0), s.cancels.Load(), "本地超时绝不调用取消接口")
	assert.Contains(t, getStdout(), "WAIT_TIMEOUT")
}

func TestImageWaitInterruptedDoesNotCancel(t *testing.T) {
	s := &taskServer{statuses: []string{"PROCESSING"}}
	srv := s.server(t)
	defer srv.Close()

	app, _ := fastWaitApp(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	app.ctx = ctx
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	app.waitInterval = func(int) time.Duration { return time.Hour } // 卡在等待，靠 ctx 打断
	exit := run(app, []string{"image", "wait", taskIDArg, "--output", "json"})
	assert.Equal(t, clierr.ExitInterrupted, exit)
	assert.Equal(t, int32(0), s.cancels.Load(), "Ctrl-C 不取消服务端任务")
}

func TestImageCancelSuccess(t *testing.T) {
	s := &taskServer{statuses: []string{"PENDING"}}
	srv := s.server(t)
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "cancel", taskIDArg, "--output", "json"})
	require.Equal(t, 0, exit)
	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Equal(t, true, data["cancelled"])
	assert.Equal(t, int32(1), s.cancels.Load())
}

func TestImageCancelHTTP200Code500FailsNoRetry(t *testing.T) {
	var cancels atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancels.Add(1)
		fmt.Fprint(w, `{"code":500,"message":"任务取消失败","data":null}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "cancel", taskIDArg, "--output", "json"})
	assert.Equal(t, clierr.ExitServer, exit)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "SERVER", env.Error.Kind)
	assert.EqualValues(t, 500, env.Error.Code)
	assert.EqualValues(t, 200, env.Error.HTTPStatus, "HTTP 200 + code 500 必须按失败处理")
	assert.Equal(t, int32(1), cancels.Load(), "取消失败不得自动重试")
}

func TestImageCreateWithWaitReachesTerminal(t *testing.T) {
	var posts atomic.Int32
	s := &taskServer{statuses: []string{"PENDING", "PROCESSING", "COMPLETED"}, extra: `"resultImageUrls":["https://cdn/x.png"]`}
	base := s.server(t)
	defer base.Close()
	// 复合服务器：create POST + 任务查询转发
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/integration/image-tasks" {
			posts.Add(1)
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"task":%s,"historical":false}}`, testTaskJSON)
			return
		}
		resp, err := http.Get(base.URL + r.URL.Path)
		require.NoError(t, err)
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = fmt.Fprint(w, readAll(t, resp))
	}))
	defer srv.Close()

	app, getStdout := fastWaitApp(t, srv.URL)
	exit := run(app, []string{"image", "create", "--request", writeRequestFile(t, validRequest),
		"--wait", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", getStdout())
	assert.Equal(t, int32(1), posts.Load())
	assert.Contains(t, getStdout(), `"COMPLETED"`, "--wait 输出终态任务")
	assert.Contains(t, getStdout(), "idempotencyKey")
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	buf := make([]byte, 64*1024)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}
