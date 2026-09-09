package cred

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// A blocking OS credential store must not hang the process. On macOS a keychain
// item whose ACL does not list the current binary makes Security.framework wait
// on a GUI dialog inside SecItemCopyMatching; with this CLI driven by an AI
// agent or CI nobody ever clicks it. Verified against the real hang: before the
// timeout `auth status` ran forever, after it exits 14 in ~11s.
func TestWithTimeoutFailsInsteadOfHangingForever(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)

	start := time.Now()
	_, err := withTimeoutFor(50*time.Millisecond, "读取", func() (int, error) {
		<-blocked // never returns within the timeout
		return 0, nil
	})

	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	require.NotNil(t, ce)
	assert.Equal(t, clierr.KindLocalStorage, ce.Kind)
	assert.Equal(t, clierr.ExitLocalStorage, ce.ExitCode)
	// 必须给出可操作的出路，否则调用方（多半是 AI）只会重试并再次挂住
	assert.Contains(t, err.Error(), "QIANJUE_TOKEN")
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestWithTimeoutPassesThroughFastResult(t *testing.T) {
	got, err := withTimeoutFor(time.Second, "读取", func() (int, error) { return 42, nil })
	require.NoError(t, err)
	assert.Equal(t, 42, got)
}
