package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeBatchRequest 落一个 N 项的批量请求文件，返回路径。
func writeBatchRequest(t *testing.T, n int) string {
	t.Helper()
	items := make([]string, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, fmt.Sprintf(`{"type":"TXT2IMG","prompt":"第 %d 张","outputCount":1}`, i+1))
	}
	path := filepath.Join(t.TempDir(), "batch.json")
	require.NoError(t, os.WriteFile(path, []byte("["+strings.Join(items, ",")+"]"), 0o600))
	return path
}

// --wait 跑完后，输出里每一项的 Status 必须是等到的终态，而不是提交那一刻的 PENDING。
//
// 复现的真实症状：批量提交 3 项 + --wait，CLI 明明已经轮询到全部 COMPLETED（等待逻辑
// 自己拿到了终态详情），但最终 JSON 的 items[].status 仍是 "PENDING" —— 调用方按这份
// 输出判断会以为任务还没跑完，于是要么白等，要么再查一遍才敢用结果。
// 根因：等待那段把 WaitFetch 的返回值丢掉了（`_, werr := ...`），没回写 results。
func TestImageBatchWaitWritesBackTerminalStatus(t *testing.T) {
	var created int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// 创建：每项一个独立 taskId，返回提交时的 PENDING
			created++
			fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"task":{"id":%d,"status":"PENDING"},"historical":false}}`, 1000+created)
			return
		}
		// 查询：已经是终态
		id := path.Base(r.URL.Path)
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"taskId":"%s","domain":"DRAW","status":"COMPLETED","resultCount":1}}`, id)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{
		"image", "batch",
		"--request", writeBatchRequest(t, 2),
		"--idempotency-key", "batch-writeback",
		"--wait", "--wait-timeout", "30s",
		"--output", "json",
	})
	require.Equal(t, 0, exit)

	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	items, ok := data["items"].([]any)
	require.True(t, ok, "items 必须是数组")
	require.Len(t, items, 2)

	for i, raw := range items {
		item := raw.(map[string]any)
		assert.Equal(t, "COMPLETED", item["status"],
			"第 %d 项：--wait 已经等到终态，输出里就必须是终态而不是提交时的 PENDING", i+1)
	}
}

// 不加 --wait 时保持原样：输出提交那一刻的状态，不额外轮询。
func TestImageBatchWithoutWaitKeepsSubmitStatus(t *testing.T) {
	var queried int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			queried++
		}
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"task":{"id":2002,"status":"PENDING"},"historical":false}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{
		"image", "batch",
		"--request", writeBatchRequest(t, 1),
		"--idempotency-key", "batch-nowait",
		"--output", "json",
	})
	require.Equal(t, 0, exit)

	items := decodeEnvelope(t, stdout).Data.(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "PENDING", items[0].(map[string]any)["status"])
	assert.Zero(t, queried, "没有 --wait 就不该发查询请求")
}
