// Package task implements the client-side task wait loop (cli-contract.md §19).
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/backoff"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// Fetch returns one task snapshot: the raw task document (int64-safe) plus its
// status string. It abstracts the per-domain read call so the wait loop works
// for image (GetImageTask) and the unified read endpoint (GetUnifiedTask) alike.
type Fetch func(ctx context.Context) (json.RawMessage, string, error)

// Wait polls an image task until terminal or timeout (legacy image path).
// Kept for the shipped image commands; delegates to WaitFetch.
func Wait(ctx context.Context, client *api.Client, taskID string, timeout time.Duration, p *output.Printer, interval func(int) time.Duration) (json.RawMessage, error) {
	fetch := func(fctx context.Context) (json.RawMessage, string, error) {
		return client.GetImageTask(fctx, taskID)
	}
	return WaitFetch(ctx, fetch, taskID, timeout, p, interval)
}

// WaitFetch polls via the given Fetch until a terminal state or the local wait
// timeout. Terminal mapping: COMPLETED → success; FAILED/TIMEOUT → TASK_FAILED(10);
// CANCELLED → TASK_CANCELLED(11). A local timeout returns WAIT_TIMEOUT(7) with
// the last snapshot and NEVER cancels the server-side task; the same holds for
// Ctrl-C (INTERRUPTED, 130). label is a human identifier used only in messages.
func WaitFetch(ctx context.Context, fetch Fetch, label string, timeout time.Duration, p *output.Printer, interval func(int) time.Duration) (json.RawMessage, error) {
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	if interval == nil {
		interval = backoff.Interval
	}
	deadline := time.Now().Add(timeout)
	var lastRaw json.RawMessage

	for attempt := 0; ; attempt++ {
		raw, status, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		lastRaw = raw

		switch status {
		case api.TaskCompleted:
			return raw, nil
		case api.TaskFailed, api.TaskTimeout:
			e := clierr.New(clierr.KindTaskFailed, fmt.Sprintf("任务 %s 终态 %s%s", label, status, errorSuffix(raw)))
			e.Details = raw
			return nil, e
		case api.TaskCancelled:
			e := clierr.New(clierr.KindTaskCancelled, fmt.Sprintf("任务 %s 已取消%s", label, cancelSuffix(raw)))
			e.Details = raw
			return nil, e
		case api.TaskPending, api.TaskProcessing:
			p.Progressf("任务 %s 状态 %s，继续等待", label, status)
		default:
			p.Progressf("任务 %s 未知状态 %q，继续等待", label, status)
		}

		if time.Now().After(deadline) {
			e := clierr.New(clierr.KindWaitTimeout, fmt.Sprintf(
				"本地等待超时（%s），任务 %s 仍在服务端执行；不会自动取消，可稍后重新查询", timeout, label))
			e.Details = lastRaw
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, clierr.Interrupted()
		case <-time.After(interval(attempt)):
		}
	}
}

func errorSuffix(raw json.RawMessage) string {
	var probe struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.ErrorMessage != "" {
		return "：" + probe.ErrorMessage
	}
	return ""
}

func cancelSuffix(raw json.RawMessage) string {
	var probe struct {
		CancelReason string `json:"cancelReason"`
	}
	if json.Unmarshal(raw, &probe) == nil && probe.CancelReason != "" {
		return "：" + probe.CancelReason
	}
	return ""
}
