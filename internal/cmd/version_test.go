package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// testApp builds an appContext with captured writers, a fake env, and a fixed
// TTY answer.
func testApp(env map[string]string, isTTY bool) (*appContext, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	app := &appContext{
		flags:  &globalFlags{},
		stdout: &stdout,
		stderr: &stderr,
		stdin:  bytes.NewReader(nil),
		getenv: func(k string) string { return env[k] },
		isTTY:  func() bool { return isTTY },
	}
	return app, &stdout, &stderr
}

func TestVersionJSON(t *testing.T) {
	app, stdout, stderr := testApp(nil, false)
	exit := run(app, []string{"version", "--output", "json"})
	require.Equal(t, 0, exit)

	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var env output.Envelope
	require.NoError(t, dec.Decode(&env))
	var extra any
	require.Error(t, dec.Decode(&extra), "stdout 只允许一个 JSON Document")

	assert.Equal(t, "1", env.SchemaVersion)
	assert.Equal(t, "version", env.Command)
	assert.True(t, env.OK)
	data, ok := env.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, data, "version")
	assert.Contains(t, data, "commit")
	assert.Contains(t, data, "buildDate")
	assert.Empty(t, stderr.String())
}

func TestVersionNonTTYDefaultsToJSON(t *testing.T) {
	app, stdout, _ := testApp(nil, false)
	exit := run(app, []string{"version"})
	require.Equal(t, 0, exit)
	assert.True(t, json.Valid(stdout.Bytes()), "非 TTY 默认输出 JSON")
}

func TestVersionTTYDefaultsToTable(t *testing.T) {
	app, stdout, _ := testApp(nil, true)
	exit := run(app, []string{"version"})
	require.Equal(t, 0, exit)
	assert.False(t, json.Valid(bytes.TrimSpace(stdout.Bytes())), "TTY 默认输出 table")
	assert.Contains(t, stdout.String(), "Version")
}

func TestVersionEnvOutputOverride(t *testing.T) {
	app, stdout, _ := testApp(map[string]string{"QIANJUE_OUTPUT": "json"}, true)
	exit := run(app, []string{"version"})
	require.Equal(t, 0, exit)
	assert.True(t, json.Valid(stdout.Bytes()), "QIANJUE_OUTPUT 覆盖 TTY 默认")
}

func TestInvalidOutputValueIsUsageError(t *testing.T) {
	app, stdout, stderr := testApp(nil, false)
	exit := run(app, []string{"version", "--output", "yaml"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "--output")
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	app, stdout, stderr := testApp(nil, false)
	exit := run(app, []string{"nonexistent"})
	assert.Equal(t, clierr.ExitUsage, exit)
	assert.Empty(t, stdout.String(), "usage 错误不得写 stdout")
	assert.NotEmpty(t, stderr.String())
}

func TestCommandPathDotted(t *testing.T) {
	app, _, _ := testApp(nil, false)
	root := newRootCommand(app)
	version, _, err := root.Find([]string{"version"})
	require.NoError(t, err)
	assert.Equal(t, "version", commandPath(version))
	assert.Equal(t, "qianjue", commandPath(root))
}
