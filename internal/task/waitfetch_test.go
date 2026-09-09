package task

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/output"
)

func fastInterval(int) time.Duration { return time.Millisecond }

func newPrinter() *output.Printer {
	return output.NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, true, true)
}

// scriptedFetch returns each status in turn, then repeats the last.
func scriptedFetch(statuses []string) Fetch {
	i := 0
	return func(ctx context.Context) (json.RawMessage, string, error) {
		s := statuses[i]
		if i < len(statuses)-1 {
			i++
		}
		return json.RawMessage(`{"status":"` + s + `"}`), s, nil
	}
}

func TestWaitFetchReachesCompleted(t *testing.T) {
	raw, err := WaitFetch(context.Background(),
		scriptedFetch([]string{api.TaskPending, api.TaskProcessing, api.TaskCompleted}),
		"video/42", time.Minute, newPrinter(), fastInterval)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "COMPLETED")
}

func TestWaitFetchTerminalExitCodes(t *testing.T) {
	cases := []struct {
		status   string
		wantExit int
	}{
		{api.TaskFailed, clierr.ExitTaskFailed},
		{api.TaskTimeout, clierr.ExitTaskFailed},
		{api.TaskCancelled, clierr.ExitTaskCancelled},
	}
	for _, tc := range cases {
		_, err := WaitFetch(context.Background(), scriptedFetch([]string{tc.status}),
			"image/1", time.Minute, newPrinter(), fastInterval)
		require.Error(t, err, tc.status)
		assert.Equal(t, tc.wantExit, clierr.AsCLIError(err).ExitCode, tc.status)
	}
}

func TestWaitFetchLocalTimeout(t *testing.T) {
	_, err := WaitFetch(context.Background(), scriptedFetch([]string{api.TaskProcessing}),
		"video/9", 30*time.Millisecond, newPrinter(), func(int) time.Duration { return 20 * time.Millisecond })
	require.Error(t, err)
	assert.Equal(t, clierr.ExitInProgressOrWaitTimeout, clierr.AsCLIError(err).ExitCode)
}

func TestWaitFetchInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WaitFetch(ctx, scriptedFetch([]string{api.TaskProcessing}),
		"x", time.Minute, newPrinter(), func(int) time.Duration { return time.Hour })
	require.Error(t, err)
	assert.Equal(t, clierr.ExitInterrupted, clierr.AsCLIError(err).ExitCode)
}

// heldFetch always reports PENDING carrying a moderation hold.
func heldFetch(reviewStatus string) Fetch {
	body := `{"status":"PENDING","moderationHold":{"recordId":"41207",` +
		`"reason":"图2：疑似含受限内容","reviewStatus":"` + reviewStatus + `",` +
		`"expiresAt":"2026-09-10T14:22:00"}}`
	return func(ctx context.Context) (json.RawMessage, string, error) {
		return json.RawMessage(body), api.TaskPending, nil
	}
}

// A held task must break the wait loop immediately. Polling on would burn the
// whole local timeout waiting for a decision only a human can make, and the
// caller would never learn the task was blocked.
func TestWaitFetchStopsImmediatelyOnModerationHold(t *testing.T) {
	start := time.Now()
	_, err := WaitFetch(context.Background(), heldFetch(""),
		"draw/8812", time.Minute, newPrinter(), fastInterval)

	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	require.NotNil(t, ce)
	assert.Equal(t, clierr.KindModerationHold, ce.Kind)
	assert.Equal(t, clierr.ExitModerationHold, ce.ExitCode)
	// 不是 TASK_FAILED：任务还活着、积分还冻着，重试同一提示词只会再次被拦
	assert.NotEqual(t, clierr.ExitTaskFailed, ce.ExitCode)
	assert.Less(t, time.Since(start), 5*time.Second)
	// 详情要带上，AI 才能把原因和记录号转述给用户
	assert.Contains(t, string(ce.Details), "41207")
}

// The message must name the exact next command, so an agent relaying it does not
// have to invent one — and must not tell the user to "just wait" when the record
// has not even been submitted for review yet.
func TestWaitFetchModerationHoldMessageGuidesNextStep(t *testing.T) {
	cases := []struct {
		reviewStatus string
		wantHint     string
	}{
		{"", "submit-review 41207"},
		{"PENDING_REVIEW", "status 41207"},
		{"APPROVED", "confirm 41207"},
		{"REJECTED", "平台已拒绝"},
	}
	for _, tc := range cases {
		t.Run("review="+tc.reviewStatus, func(t *testing.T) {
			_, err := WaitFetch(context.Background(), heldFetch(tc.reviewStatus),
				"draw/8812", time.Minute, newPrinter(), fastInterval)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantHint)
			assert.Contains(t, err.Error(), "积分已冻结未扣除")
		})
	}
}
