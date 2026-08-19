package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/skill"
)

// skillApp builds a test app whose home directory is redirected to a temp dir,
// so skill commands never touch the real HOME.
func skillApp(t *testing.T) (*appContext, *bytes.Buffer, string) {
	t.Helper()
	home := t.TempDir()
	app, stdout, _ := xdgApp(t, nil)
	app.homeDir = func() (string, error) { return home, nil }
	return app, stdout, home
}

func TestSkillShowPrintsRawMarkdown(t *testing.T) {
	app, stdout, _ := skillApp(t)

	exit := run(app, []string{"skill", "show"})
	require.Equal(t, 0, exit)
	assert.Equal(t, string(skill.Markdown), stdout.String(), "show 必须原样吐 SKILL.md，便于重定向到文件")
}

func TestSkillInstallWritesOnlyExistingDirs(t *testing.T) {
	app, stdout, home := skillApp(t)
	// only Codex exists on this machine
	codexBase := filepath.Join(home, ".codex", "skills")
	require.NoError(t, os.MkdirAll(codexBase, 0o755))

	exit := run(app, []string{"skill", "install", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	got, err := os.ReadFile(filepath.Join(codexBase, skill.Name, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, skill.Markdown, got)

	// the absent Claude directory must NOT be created
	_, err = os.Stat(filepath.Join(home, ".claude"))
	assert.True(t, os.IsNotExist(err), "不得创建用户未使用的助手配置目录")

	env := decodeEnvelope(t, stdout)
	data := env.Data.(map[string]any)
	require.Len(t, data["installed"].([]any), 1)
	require.Len(t, data["skipped"].([]any), 1)
}

// When no assistant skills directory exists the CLI must refuse rather than
// invent one; placing the file is then the AI's (or user's) job.
func TestSkillInstallFailsWhenNoDirExists(t *testing.T) {
	app, stdout, home := skillApp(t)

	exit := run(app, []string{"skill", "install", "--output", "json"})
	assert.Equal(t, clierr.ExitUsage, exit)

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "USAGE", env.Error.Kind)
	assert.Contains(t, env.Error.Message, "--dir")

	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	assert.Empty(t, entries, "失败时不得留下任何目录")
}

func TestSkillInstallExplicitDirIsCreated(t *testing.T) {
	app, stdout, home := skillApp(t)
	custom := filepath.Join(home, "elsewhere", "skills")

	exit := run(app, []string{"skill", "install", "--dir", custom, "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	got, err := os.ReadFile(filepath.Join(custom, skill.Name, "SKILL.md"))
	require.NoError(t, err, "--dir 是显式指令，允许创建")
	assert.Equal(t, skill.Markdown, got)
}

func TestSkillInstallIdempotent(t *testing.T) {
	app, stdout, home := skillApp(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "skills"), 0o755))

	require.Equal(t, 0, run(app, []string{"skill", "install", "--output", "json"}))
	stdout.Reset()
	require.Equal(t, 0, run(app, []string{"skill", "install", "--output", "json"}), "stdout: %s", stdout.String())

	got, err := os.ReadFile(filepath.Join(home, ".codex", "skills", skill.Name, "SKILL.md"))
	require.NoError(t, err)
	assert.Equal(t, skill.Markdown, got)
}

func TestSkillPathReportsExistenceWithoutWriting(t *testing.T) {
	app, stdout, home := skillApp(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex", "skills"), 0o755))

	exit := run(app, []string{"skill", "path", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "skill.path", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, skill.Name, data["name"])

	candidates := data["candidates"].([]any)
	require.Len(t, candidates, 2)
	byHost := map[string]bool{}
	for _, c := range candidates {
		m := c.(map[string]any)
		byHost[m["host"].(string)] = m["exists"].(bool)
	}
	assert.True(t, byHost[".codex"], "已存在的目录必须报告 exists=true")
	assert.False(t, byHost[".claude"], "不存在的目录必须报告 exists=false")

	// path must not create anything
	_, err := os.Stat(filepath.Join(home, ".claude"))
	assert.True(t, os.IsNotExist(err), "path 不得写任何文件")
}
