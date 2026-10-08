package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/buildinfo"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/updatecheck"
)

func newerResult() updatecheck.Result {
	return updatecheck.Result{
		Current:    "v1.0.0",
		Latest:     "v9.9.9",
		Outdated:   true,
		ReleaseURL: "https://github.com/zriyox/qianjue-cli/releases/latest",
	}
}

func TestUpdateHintPrintsWhenANewerReleaseExists(t *testing.T) {
	app, stdout, stderr := xdgApp(t, nil)
	app.updateCheck = func(context.Context) (updatecheck.Result, error) { return newerResult(), nil }

	exit := run(app, []string{"config", "show", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s stderr: %s", stdout.String(), stderr.String())

	assert.Contains(t, stderr.String(), "有新版本 v9.9.9")
	assert.Contains(t, stderr.String(), "当前 v1.0.0")
	// stdout 必须仍是单个干净的 JSON 文档。
	env := decodeEnvelope(t, stdout)
	assert.True(t, env.OK)
}

func TestUpdateHintSilentWhenUpToDateOrFailing(t *testing.T) {
	cases := map[string]func(context.Context) (updatecheck.Result, error){
		"已是最新": func(context.Context) (updatecheck.Result, error) {
			return updatecheck.Result{Current: "v1.0.0", Latest: "v1.0.0"}, nil
		},
		"查询失败": func(context.Context) (updatecheck.Result, error) {
			return updatecheck.Result{}, errors.New("network down")
		},
		"无法确认": func(context.Context) (updatecheck.Result, error) {
			return updatecheck.Result{}, updatecheck.ErrUnavailable
		},
		"没有可用版本号": func(context.Context) (updatecheck.Result, error) {
			return updatecheck.Result{Current: "v1.0.0"}, nil
		},
	}
	for name, seam := range cases {
		t.Run(name, func(t *testing.T) {
			app, stdout, stderr := xdgApp(t, nil)
			app.updateCheck = seam
			exit := run(app, []string{"config", "show", "--output", "json"})
			require.Equal(t, 0, exit, "stdout: %s", stdout.String())
			assert.Empty(t, stderr.String())
		})
	}
}

// 预算到点就放行：查不动的那一轮不提，但绝不报错，也不无限期拖住命令。
func TestUpdateHintNeverBlocksBeyondItsBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	app, stdout, stderr := xdgApp(t, nil)
	app.ctx = ctx
	app.updateHintBudget = 50 * time.Millisecond
	app.updateCheck = func(checkCtx context.Context) (updatecheck.Result, error) {
		<-checkCtx.Done()
		return updatecheck.Result{}, checkCtx.Err()
	}

	started := time.Now()
	exit := run(app, []string{"config", "show", "--output", "json"})
	elapsed := time.Since(started)
	cancel()

	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Less(t, elapsed, 2*time.Second, "预算必须真的生效")
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
	assert.Empty(t, stderr.String(), "超预算时本轮不提，而不是报错")
}

// version 自己负责回答更新问题（--check），后台提示必须让位，避免重复或竞争。
func TestUpdateHintSkipsVersionCommand(t *testing.T) {
	app, stdout, stderr := xdgApp(t, nil)
	app.updateCheck = func(context.Context) (updatecheck.Result, error) { return newerResult(), nil }

	exit := run(app, []string{"version", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Empty(t, stderr.String())
}

// seam 为 nil 时不查任何地址：测试与嵌入方默认离线。
func TestUpdateHintDisabledWithoutSeam(t *testing.T) {
	app, stdout, stderr := xdgApp(t, nil)
	require.Nil(t, app.updateCheck)
	exit := run(app, []string{"config", "show", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestVersionCheckReportsAvailableUpdate(t *testing.T) {
	app, stdout, stderr := xdgApp(t, nil)
	app.updateCheck = func(context.Context) (updatecheck.Result, error) { return newerResult(), nil }

	exit := run(app, []string{"version", "--check", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s stderr: %s", stdout.String(), stderr.String())

	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Equal(t, "v9.9.9", data["latestVersion"])
	assert.Equal(t, true, data["updateAvailable"])
	assert.Equal(t, checkStatusUpdateAvailable, data["checkStatus"])
}

// JSON 模式没有 table 行可看，结论必须落在 data 里，否则 --check 等于没说。
func TestVersionCheckJSONCarriesTheConclusion(t *testing.T) {
	cases := map[string]struct {
		seam       func(context.Context) (updatecheck.Result, error)
		wantStatus string
		wantLatest any
	}{
		"有新版本": {
			seam:       func(context.Context) (updatecheck.Result, error) { return newerResult(), nil },
			wantStatus: checkStatusUpdateAvailable,
			wantLatest: "v9.9.9",
		},
		"已是最新": {
			seam: func(context.Context) (updatecheck.Result, error) {
				return updatecheck.Result{Current: "v1.0.0", Latest: "v1.0.0"}, nil
			},
			wantStatus: checkStatusUpToDate,
			wantLatest: "v1.0.0",
		},
		"无法确认": {
			seam: func(context.Context) (updatecheck.Result, error) {
				return updatecheck.Result{}, updatecheck.ErrUnavailable
			},
			wantStatus: checkStatusUnavailable,
			wantLatest: nil,
		},
		"没有版本号": {
			seam: func(context.Context) (updatecheck.Result, error) {
				return updatecheck.Result{Current: "v1.0.0"}, nil
			},
			wantStatus: checkStatusUnknown,
			wantLatest: nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			app, stdout, _ := xdgApp(t, nil)
			app.updateCheck = tc.seam
			exit := run(app, []string{"version", "--check", "--output", "json"})
			require.Equal(t, 0, exit, "stdout: %s", stdout.String())
			data := decodeEnvelope(t, stdout).Data.(map[string]any)
			assert.Equal(t, tc.wantStatus, data["checkStatus"])
			assert.Equal(t, tc.wantLatest, data["latestVersion"])
		})
	}
}

// 普通 version 的输出形状不能被 --check 的字段污染。
func TestVersionWithoutCheckKeepsItsOriginalShape(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	require.Nil(t, app.updateCheck)
	exit := run(app, []string{"version", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.NotContains(t, data, "checkStatus")
	assert.NotContains(t, data, "latestVersion")
	assert.NotContains(t, data, "updateAvailable")
}

func TestVersionCheckReportsUpToDate(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	app.updateCheck = func(context.Context) (updatecheck.Result, error) {
		return updatecheck.Result{Current: "v1.0.0", Latest: "v1.0.0"}, nil
	}

	exit := run(app, []string{"version", "--check", "--output", "table"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Contains(t, stdout.String(), "已是最新")
	assert.Contains(t, stdout.String(), "v1.0.0")
}

// 显式问了就不能沉默：查不到要给确定答复，但仍算成功。
func TestVersionCheckReportsUnavailableInsteadOfSilence(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	app.updateCheck = func(context.Context) (updatecheck.Result, error) {
		return updatecheck.Result{}, updatecheck.ErrUnavailable
	}

	exit := run(app, []string{"version", "--check", "--output", "table"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())
	assert.Contains(t, stdout.String(), "暂时无法确认")
}

func TestVersionCheckSurfacesUnexpectedErrors(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	app.updateCheck = func(context.Context) (updatecheck.Result, error) {
		return updatecheck.Result{}, errors.New("boom")
	}

	exit := run(app, []string{"version", "--check", "--output", "json"})
	assert.NotEqual(t, 0, exit, "stdout: %s", stdout.String())
}

func TestVersionCheckWithoutSeamFailsLoudly(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	exit := run(app, []string{"version", "--check", "--output", "json"})
	assert.NotEqual(t, 0, exit, "stdout: %s", stdout.String())
	assert.Contains(t, stdout.String(), "未启用更新检查")
}

func TestUpdateCheckDisabledByEnvAndConfig(t *testing.T) {
	t.Run("环境变量", func(t *testing.T) {
		app, _, _ := xdgApp(t, map[string]string{"QIANJUE_NO_UPDATE_CHECK": "1"})
		assert.True(t, updateCheckDisabled(app))
	})
	t.Run("配置关闭", func(t *testing.T) {
		app, _, _ := xdgApp(t, nil)
		writeConfig(t, app, "current_profile = 'dev'\nupdate_check = false\n")
		assert.True(t, updateCheckDisabled(app))
	})
	t.Run("配置显式开启", func(t *testing.T) {
		app, _, _ := xdgApp(t, nil)
		writeConfig(t, app, "current_profile = 'dev'\nupdate_check = true\n")
		assert.False(t, updateCheckDisabled(app))
	})
	t.Run("旧配置缺键", func(t *testing.T) {
		app, _, _ := xdgApp(t, nil)
		writeConfig(t, app, "current_profile = 'dev'\n")
		assert.False(t, updateCheckDisabled(app), "已有配置不能因为缺键被静默改成关闭")
	})
}

func TestReleaseURLPrefersEnvironmentOverride(t *testing.T) {
	app, _, _ := xdgApp(t, nil)
	assert.Equal(t, defaultReleaseURL, releaseURL(app))

	app, _, _ = xdgApp(t, map[string]string{"QIANJUE_RELEASE_URL": "https://mirror.example.com/cli/latest"})
	assert.Equal(t, "https://mirror.example.com/cli/latest", releaseURL(app))
}

// defaultUpdateCheck 是生产装配：验证它在禁用时完全不发请求。
func TestDefaultUpdateCheckStaysOfflineWhenDisabled(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	app, _, _ := xdgApp(t, map[string]string{
		"QIANJUE_NO_UPDATE_CHECK": "1",
		"QIANJUE_RELEASE_URL":     srv.URL,
	})
	result, err := defaultUpdateCheck(app)(context.Background())
	require.NoError(t, err)
	assert.Empty(t, result.Latest)
	assert.Zero(t, hits)
}

// defaultUpdateCheck 是生产装配：验证缓存落在 state 目录且能识别新版本。
func TestDefaultUpdateCheckUsesStateCacheAndReportsNewerRelease(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Location", "https://github.com/zriyox/qianjue-cli/releases/tag/v9.9.9")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	original := buildinfo.Version
	buildinfo.Version = "v1.0.0"
	defer func() { buildinfo.Version = original }()

	app, _, _ := xdgApp(t, map[string]string{"QIANJUE_RELEASE_URL": srv.URL})
	check := defaultUpdateCheck(app)

	first, err := check(context.Background())
	require.NoError(t, err)
	require.True(t, first.Outdated)
	require.Equal(t, 1, hits)

	second, err := check(context.Background())
	require.NoError(t, err)
	assert.True(t, second.Outdated)
	assert.Equal(t, 1, hits, "第二次必须走 state 目录里的缓存")

	stateDir, err := stateDirFor(app)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(stateDir, "update-check.json"))
}

func writeConfig(t *testing.T, app *appContext, content string) {
	t.Helper()
	path := filepath.Join(app.getenv("XDG_CONFIG_HOME"), "qianjue", "config.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func stateDirFor(app *appContext) (string, error) {
	return config.StateDir(app.getenv)
}
