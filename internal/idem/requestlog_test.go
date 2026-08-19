package idem

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
)

func stateEnv(t *testing.T) config.Getenv {
	dir := t.TempDir()
	return func(k string) string {
		if k == "XDG_STATE_HOME" {
			return filepath.Join(dir, "state")
		}
		return ""
	}
}

const testKey = "qjcli-image-47044bb9-b834-4604-9aef-56f4f774cab2"

func newTestLog(t *testing.T) *RequestLog {
	t.Helper()
	l, err := NewRequestLog("local", "http://localhost:7777/api/v1", testKey,
		[]byte(contractRequestJSON), time.Now())
	require.NoError(t, err)
	return l
}

func TestNewRequestLogFields(t *testing.T) {
	l := newTestLog(t)
	assert.Equal(t, "1", l.SchemaVersion)
	assert.Equal(t, OperationImageCreate, l.Operation)
	assert.Equal(t, contractDigest, l.RequestDigest)
	assert.Equal(t, StateSubmitting, l.State)
	assert.Equal(t, 1, l.AttemptCount)
	assert.Nil(t, l.RequestID)
	assert.Nil(t, l.TaskID)
}

func TestLogPathIsHashedAndProfileScoped(t *testing.T) {
	getenv := stateEnv(t)
	p, err := LogPath(getenv, "local", testKey)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`requests/local/[0-9a-f]{64}\.json$`), p)

	p2, err := LogPath(getenv, "prod", testKey)
	require.NoError(t, err)
	assert.NotEqual(t, p, p2)
}

func TestWriteLoadRoundTripWithPermissions(t *testing.T) {
	getenv := stateEnv(t)
	l := newTestLog(t)
	require.NoError(t, WriteLog(getenv, l))

	path, _ := LogPath(getenv, "local", testKey)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	loaded, err := LoadLog(getenv, "local", testKey)
	require.NoError(t, err)
	assert.Equal(t, l.RequestDigest, loaded.RequestDigest)
	assert.Equal(t, l.IdempotencyKey, loaded.IdempotencyKey)
	assert.JSONEq(t, string(l.RequestJSON), string(loaded.RequestJSON))
	require.NoError(t, loaded.VerifyIntegrity())
}

func TestLoadLogMissingIsNotFound(t *testing.T) {
	_, err := LoadLog(stateEnv(t), "local", "ghost-key")
	require.Error(t, err)
	assert.Equal(t, clierr.ExitNotFound, clierr.AsCLIError(err).ExitCode)
}

func TestWriteLogRejectsForbiddenFields(t *testing.T) {
	getenv := stateEnv(t)
	l, err := NewRequestLog("local", "http://localhost:7777/api/v1", "k",
		[]byte(`{"type":"TXT2IMG","accessToken":"leak"}`), time.Now())
	require.NoError(t, err)
	err = WriteLog(getenv, l)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
	_, lerr := LoadLog(getenv, "local", "k")
	assert.Error(t, lerr, "被拒请求不得落盘")
}

func TestVerifyIntegrityDetectsTampering(t *testing.T) {
	getenv := stateEnv(t)
	l := newTestLog(t)
	require.NoError(t, WriteLog(getenv, l))

	loaded, err := LoadLog(getenv, "local", testKey)
	require.NoError(t, err)
	loaded.RequestJSON = []byte(`{"type":"TXT2IMG","prompt":"tampered"}`)
	err = loaded.VerifyIntegrity()
	require.Error(t, err)
	assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
}

func TestUpdateStatePersists(t *testing.T) {
	getenv := stateEnv(t)
	l := newTestLog(t)
	require.NoError(t, WriteLog(getenv, l))

	l.SetState(StateResultUnknown)
	l.MarkAttempt(time.Now())
	taskID := "2045019196159766531"
	l.TaskID = &taskID
	require.NoError(t, WriteLog(getenv, l))

	loaded, err := LoadLog(getenv, "local", testKey)
	require.NoError(t, err)
	assert.Equal(t, StateResultUnknown, loaded.State)
	assert.Equal(t, 2, loaded.AttemptCount)
	require.NotNil(t, loaded.TaskID)
	assert.Equal(t, taskID, *loaded.TaskID)
}

func TestLoadLogRejectsLoosePermissions(t *testing.T) {
	getenv := stateEnv(t)
	l := newTestLog(t)
	require.NoError(t, WriteLog(getenv, l))
	path, _ := LogPath(getenv, "local", testKey)
	require.NoError(t, os.Chmod(path, 0o644))

	_, err := LoadLog(getenv, "local", testKey)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitLocalStorage, clierr.AsCLIError(err).ExitCode)
}
