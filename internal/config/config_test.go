package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/output"
)

func envMap(m map[string]string) Getenv {
	return func(k string) string { return m[k] }
}

func tmpEnv(t *testing.T) (Getenv, string) {
	t.Helper()
	dir := t.TempDir()
	return envMap(map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
	}), dir
}

func TestLoadMissingFileYieldsEmptyConfig(t *testing.T) {
	getenv, _ := tmpEnv(t)
	f, err := Load(getenv)
	require.NoError(t, err)
	assert.Empty(t, f.CurrentProfile)
	assert.NotNil(t, f.Profiles)
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	getenv, _ := tmpEnv(t)
	f := &File{
		CurrentProfile: "local",
		Profiles: map[string]Profile{
			"local": {APIBaseURL: "http://localhost:7777/api/v1", Output: "table", HTTPTimeout: "30s", TaskWaitTimeout: "10m"},
		},
	}
	require.NoError(t, Save(getenv, f))

	path, err := ConfigPath(getenv)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "config.toml 必须 0600")
	dirInfo, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "配置目录必须 0700")

	loaded, err := Load(getenv)
	require.NoError(t, err)
	assert.Equal(t, "local", loaded.CurrentProfile)
	assert.Equal(t, "http://localhost:7777/api/v1", loaded.Profiles["local"].APIBaseURL)
}

func TestLoadRejectsLooseFilePermissions(t *testing.T) {
	getenv, _ := tmpEnv(t)
	require.NoError(t, Save(getenv, &File{CurrentProfile: "x", Profiles: map[string]Profile{"x": {APIBaseURL: "https://api.example.com"}}}))
	path, _ := ConfigPath(getenv)
	require.NoError(t, os.Chmod(path, 0o644))

	_, err := Load(getenv)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitLocalStorage, clierr.AsCLIError(err).ExitCode)
	assert.Contains(t, err.Error(), "chmod 0600")
}

func TestValidateBaseURL(t *testing.T) {
	require.NoError(t, ValidateBaseURL("https://api.example.com/api/v1"))
	require.NoError(t, ValidateBaseURL("http://localhost:7777/api/v1"))
	require.NoError(t, ValidateBaseURL("http://127.0.0.1:7777/api/v1"))
	require.NoError(t, ValidateBaseURL("http://[::1]:7777/api/v1"))

	for _, bad := range []string{
		"http://10.0.0.5:7777/api/v1",
		"http://api.example.com/api/v1",
		"ftp://localhost/api",
		"not-a-url",
		"",
	} {
		err := ValidateBaseURL(bad)
		require.Error(t, err, bad)
		assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode, bad)
	}
}

func TestResolvePrecedenceMatrix(t *testing.T) {
	f := &File{
		CurrentProfile: "local",
		Profiles: map[string]Profile{
			"local": {APIBaseURL: "http://localhost:7777/api/v1", Output: "table", HTTPTimeout: "5s", TaskWaitTimeout: "1m"},
			"prod":  {APIBaseURL: "https://api.example.com/api/v1"},
		},
	}

	// profile 值兜底
	r, err := Resolve(f, Overrides{}, envMap(nil), false)
	require.NoError(t, err)
	assert.Equal(t, "local", r.ProfileName)
	assert.Equal(t, "http://localhost:7777/api/v1", r.APIBaseURL)
	assert.Equal(t, output.FormatTable, r.Output)
	assert.Equal(t, 5*time.Second, r.HTTPTimeout)
	assert.Equal(t, time.Minute, r.TaskWaitTimeout)

	// env 覆盖 profile
	env := envMap(map[string]string{
		"QIANJUE_PROFILE":           "prod",
		"QIANJUE_OUTPUT":            "json",
		"QIANJUE_HTTP_TIMEOUT":      "7s",
		"QIANJUE_TASK_WAIT_TIMEOUT": "2m",
	})
	r, err = Resolve(f, Overrides{}, env, true)
	require.NoError(t, err)
	assert.Equal(t, "prod", r.ProfileName)
	assert.Equal(t, "https://api.example.com/api/v1", r.APIBaseURL)
	assert.Equal(t, output.FormatJSON, r.Output)
	assert.Equal(t, 7*time.Second, r.HTTPTimeout)
	assert.Equal(t, 2*time.Minute, r.TaskWaitTimeout)

	// flag 覆盖 env
	r, err = Resolve(f, Overrides{
		Profile:     "local",
		APIBaseURL:  "http://127.0.0.1:8888/api/v1",
		Output:      "table",
		HTTPTimeout: "9s",
		WaitTimeout: "3m",
	}, env, false)
	require.NoError(t, err)
	assert.Equal(t, "local", r.ProfileName)
	assert.Equal(t, "http://127.0.0.1:8888/api/v1", r.APIBaseURL)
	assert.Equal(t, output.FormatTable, r.Output)
	assert.Equal(t, 9*time.Second, r.HTTPTimeout)
	assert.Equal(t, 3*time.Minute, r.TaskWaitTimeout)

	// 内置默认
	r, err = Resolve(&File{Profiles: map[string]Profile{}}, Overrides{}, envMap(nil), false)
	require.NoError(t, err)
	assert.Equal(t, DefaultProfileName, r.ProfileName)
	assert.Equal(t, DefaultHTTPTimeout, r.HTTPTimeout)
	assert.Equal(t, DefaultTaskWaitTimeout, r.TaskWaitTimeout)
	assert.Equal(t, output.FormatJSON, r.Output)
	// 未配置任何 API 地址时回落到生产端点，且标记为内置默认，
	// 这样开发者能在 config show 里看出自己没切 dev/test。
	assert.Equal(t, DefaultAPIBaseURL, r.APIBaseURL)
	assert.True(t, r.APIBaseURLIsDefault)
	require.NoError(t, r.RequireAPIBaseURL())
}

// 显式配置必须压过内置生产默认，且不再标记为默认。
func TestResolveExplicitBaseURLOverridesProductionDefault(t *testing.T) {
	r, err := Resolve(&File{Profiles: map[string]Profile{}},
		Overrides{APIBaseURL: "http://localhost:7777/api/v1"}, envMap(nil), false)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:7777/api/v1", r.APIBaseURL)
	assert.False(t, r.APIBaseURLIsDefault)
}

func TestResolveRejectsNonLocalHTTPFromEnv(t *testing.T) {
	env := envMap(map[string]string{"QIANJUE_API_BASE_URL": "http://internal.example.com/api/v1"})
	_, err := Resolve(&File{Profiles: map[string]Profile{}}, Overrides{}, env, false)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
}

func TestResolveTrimsTrailingSlash(t *testing.T) {
	env := envMap(map[string]string{"QIANJUE_API_BASE_URL": "http://localhost:7777/api/v1/"})
	r, err := Resolve(&File{Profiles: map[string]Profile{}}, Overrides{}, env, false)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:7777/api/v1", r.APIBaseURL)
}

func TestResolveInvalidDuration(t *testing.T) {
	_, err := Resolve(&File{Profiles: map[string]Profile{}}, Overrides{HTTPTimeout: "banana"}, envMap(nil), false)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
}

func TestValidateProfileName(t *testing.T) {
	require.NoError(t, ValidateProfileName("local"))
	require.NoError(t, ValidateProfileName("prod-1.eu_west"))
	for _, bad := range []string{"", "a/b", "a b", "..", ".hidden", "名字"} {
		assert.Error(t, ValidateProfileName(bad), bad)
	}
}

func TestAtomicWriteReplacesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	require.NoError(t, AtomicWrite(path, []byte("v1")))
	require.NoError(t, AtomicWrite(path, []byte("v2")))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "v2", string(got))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "不得残留临时文件")
}
