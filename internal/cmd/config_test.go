package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// xdgApp builds a test app whose config/state dirs live in a temp dir.
func xdgApp(t *testing.T, extraEnv map[string]string) (*appContext, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	env := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
	}
	for k, v := range extraEnv {
		env[k] = v
	}
	app, stdout, stderr := testApp(env, false)
	return app, stdout, stderr
}

func decodeEnvelope(t *testing.T, stdout *bytes.Buffer) output.Envelope {
	t.Helper()
	var env output.Envelope
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &env), "stdout: %s", stdout.String())
	return env
}

// runSameEnv re-runs the CLI reusing the app's env/writers with fresh buffers.
func runSameEnv(app *appContext, args []string) (*appContext, *bytes.Buffer, int) {
	var stdout, stderr bytes.Buffer
	next := &appContext{
		flags:  &globalFlags{},
		stdout: &stdout,
		stderr: &stderr,
		stdin:  bytes.NewReader(nil),
		getenv: app.getenv,
		isTTY:  app.isTTY,
	}
	exit := run(next, args)
	return next, &stdout, exit
}

func TestProfileCreateUseShowFlow(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)

	exit := run(app, []string{"config", "profile", "create", "local",
		"--api-base-url", "http://localhost:7777/api/v1", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "config.profile.create", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, "local", data["profile"])
	assert.Equal(t, "local", data["currentProfile"], "首个 profile 自动成为 current")

	_, stdout2, exit := runSameEnv(app, []string{"config", "profile", "create", "prod",
		"--api-base-url", "https://api.example.com/api/v1", "--output", "json"})
	require.Equal(t, 0, exit)
	env2 := decodeEnvelope(t, stdout2)
	assert.Equal(t, "local", env2.Data.(map[string]any)["currentProfile"], "已有 current 不被覆盖")

	_, stdout3, exit := runSameEnv(app, []string{"config", "profile", "use", "prod", "--output", "json"})
	require.Equal(t, 0, exit)
	assert.Equal(t, "prod", decodeEnvelope(t, stdout3).Data.(map[string]any)["currentProfile"])

	_, stdout4, exit := runSameEnv(app, []string{"config", "show", "--output", "json"})
	require.Equal(t, 0, exit)
	env4 := decodeEnvelope(t, stdout4)
	show := env4.Data.(map[string]any)
	assert.Equal(t, "prod", show["profile"])
	assert.Equal(t, "https://api.example.com/api/v1", show["apiBaseUrl"])
	assert.Equal(t, "30s", show["httpTimeout"])
	assert.Equal(t, "10m0s", show["taskWaitTimeout"])
	assert.Equal(t, "prod", env4.Meta["profile"])

	_, stdout5, exit := runSameEnv(app, []string{"config", "profile", "list", "--output", "json"})
	require.Equal(t, 0, exit)
	list := decodeEnvelope(t, stdout5).Data.(map[string]any)
	assert.Equal(t, "prod", list["currentProfile"])
	assert.Len(t, list["profiles"], 2)
}

func TestProfileCreateRejectsNonLocalHTTP(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	exit := run(app, []string{"config", "profile", "create", "bad",
		"--api-base-url", "http://internal.example.com/api/v1", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)
	env := decodeEnvelope(t, stdout)
	assert.False(t, env.OK)
	assert.Equal(t, "USAGE", env.Error.Kind)
}

func TestProfileUseUnknownIsNotFound(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	exit := run(app, []string{"config", "profile", "use", "ghost", "--output", "json"})
	assert.Equal(t, clierr.ExitNotFound, exit)
	assert.Equal(t, "NOT_FOUND", decodeEnvelope(t, stdout).Error.Kind)
}

func TestConfigPathCommand(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	exit := run(app, []string{"config", "path", "--output", "json"})
	require.Equal(t, 0, exit)
	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Contains(t, data["configPath"], "qianjue/config.toml")
	assert.Contains(t, data["stateDir"], "qianjue")
}

func TestProfileOutputSettingAffectsDefaultFormat(t *testing.T) {
	app, _, _ := xdgApp(t, nil)
	require.Equal(t, 0, run(app, []string{"config", "profile", "create", "local",
		"--api-base-url", "http://localhost:7777/api/v1", "--output", "json"}))

	// 把 profile 的 output 配成 json
	f, err := config.Load(app.getenv)
	require.NoError(t, err)
	prof := f.Profiles["local"]
	prof.Output = "json"
	f.Profiles["local"] = prof
	require.NoError(t, config.Save(app.getenv, f))

	// isTTY=true 时 profile.output=json 覆盖 TTY 默认 table
	var stdout bytes.Buffer
	next := &appContext{
		flags:  &globalFlags{},
		stdout: &stdout,
		stderr: &bytes.Buffer{},
		stdin:  bytes.NewReader(nil),
		getenv: app.getenv,
		isTTY:  func() bool { return true },
	}
	require.Equal(t, 0, run(next, []string{"version"}))
	assert.True(t, json.Valid(stdout.Bytes()), "profile output=json 应覆盖 TTY 默认 table")
}
