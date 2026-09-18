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

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

const unifiedTaskJSON = `{"taskId":2089609747366940674,"domain":"DRAW","taskType":"TXT2IMG","status":"COMPLETED","progress":100,"media":{"resultMediaList":["https://cdn/x.png"]},"capability":{"canCancel":false}}`

func TestTaskGetUnified(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":%s}`, unifiedTaskJSON)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"task", "get", "image", "2089609747366940674", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, "/integration/tasks/image/2089609747366940674", gotPath)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "task.get", env.Command)
	assert.Contains(t, stdout.String(), "2089609747366940674")
}

func TestTaskListUnified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/integration/tasks/video", r.URL.Path)
		assert.Equal(t, "PROCESSING", r.URL.Query().Get("status"))
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"pageNum":1,"pageSize":2,"total":7,"records":[{"taskId":1,"domain":"VIDEO","status":"PROCESSING","taskType":"kling-v3-omni"}]}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"task", "list", "video", "--status", "PROCESSING", "--page", "1", "--size", "2", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Equal(t, "task.list", decodeEnvelope(t, stdout).Command)
	assert.Contains(t, stdout.String(), "\"total\": 7")
}

func TestTaskWaitUnifiedReachesTerminal(t *testing.T) {
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/integration/tasks/video/42", r.URL.Path)
		status := "PROCESSING"
		if gets.Add(1) >= 2 {
			status = "COMPLETED"
		}
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"taskId":42,"domain":"VIDEO","status":"%s","media":{"resultMediaList":["https://cdn/v.mp4"]}}}`, status)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	app.waitInterval = func(int) time.Duration { return time.Millisecond }
	exit := run(app, []string{"task", "wait", "video", "42", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Contains(t, stdout.String(), "COMPLETED")
}

func TestTaskGetNotFoundMapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"code":3002,"message":"资源不存在","data":null}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"task", "get", "video", "9", "--output", "json"})
	assert.Equal(t, clierr.ExitNotFound, exit)
	assert.Equal(t, "NOT_FOUND", decodeEnvelope(t, stdout).Error.Kind)
}

func TestTaskGetInvalidArgs(t *testing.T) {
	app, _, _ := imageApp(t, "http://localhost:1")
	assert.Equal(t, clierr.ExitUsage, run(app, []string{"task", "get", "image", "abc", "--output", "json"}))
}

func TestTaskCancelSuccess(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":"任务取消成功"}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"task", "cancel", "video", "42", "--output", "json"})
	require.Equal(t, 0, exit)
	assert.Equal(t, "PUT", gotMethod)
	assert.Equal(t, "/integration/tasks/video/42/cancel", gotPath)
	// 只断言「后端接受了这次请求并回了什么」，不再断言 cancelled 布尔 ——
	// 见 TestTaskCancelMustNotClaimCancelledForAlreadyTerminalTask。
	assert.Equal(t, "任务取消成功", decodeEnvelope(t, stdout).Data.(map[string]any)["message"])
}

// 后端对「已经是终态的任务」是幂等放行：照样返回 code=200 + "任务取消成功"，
// 但任务其实一直是 COMPLETED、根本没被取消（2026-08-18 真实环境确认过的后端行为）。
// CLI 此前把 cancelled 硬编码成 true，于是向用户谎报「已取消」——用户以为省下了积分，
// 实际任务早就跑完扣过费了。这里要求 CLI 转述后端原话而不是自己下结论。
func TestTaskCancelMustNotClaimCancelledForAlreadyTerminalTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 后端幂等放行，文案与真取消时一致，CLI 无法从这里区分
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":"任务取消成功"}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"task", "cancel", "image", "77", "--output", "json"})
	require.Equal(t, 0, exit)

	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Equal(t, "任务取消成功", data["message"], "必须原样转述后端消息")
	assert.NotContains(t, data, "cancelled",
		"不得输出 cancelled 断言：后端对已终态任务也返回成功，CLI 无从判断是否真的取消了，"+
			"硬编码 true 会让调用方以为任务停了、积分省下了")
}

func TestTaskCancelHTTP200Code500Fails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":500,"message":"任务取消失败","data":null}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"task", "cancel", "image", "9", "--output", "json"})
	assert.Equal(t, clierr.ExitServer, exit)
	assert.Equal(t, "SERVER", decodeEnvelope(t, stdout).Error.Kind)
}
