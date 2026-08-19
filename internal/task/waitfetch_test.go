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
