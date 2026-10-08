package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/buildinfo"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/updatecheck"
)

const (
	// defaultUpdateHintBudget is how long a finishing command waits for the
	// background release lookup. It must cover the request timeout so the result
	// is always cached — a lookup killed at process exit would cost the timeout
	// again on the next command and never produce a hint. The once-per-TTL cost
	// is paid here rather than by shortening the request timeout, which would
	// make the check fail on slow routes.
	defaultUpdateHintBudget = updatecheck.DefaultTimeout + 500*time.Millisecond
	// updateHintTTL is how long a confirmed result is trusted.
	updateHintTTL = 24 * time.Hour
	// updateHintFailureTTL is how long a failed lookup suppresses retries.
	updateHintFailureTTL = time.Hour
	// defaultReleaseURL is the published release page. The redirect target
	// carries the newest tag, so no API token and no rate limit is involved.
	defaultReleaseURL = "https://github.com/zriyox/qianjue-cli/releases/latest"
	// releaseURLEnv overrides defaultReleaseURL for installs that are
	// distributed from somewhere other than the public repository.
	releaseURLEnv = "QIANJUE_RELEASE_URL"
	// noUpdateCheckEnv opts out of the hint entirely.
	noUpdateCheckEnv = "QIANJUE_NO_UPDATE_CHECK"
)

// updateHint is the handle for one background release lookup. A nil handle
// means the check is disabled for this invocation.
type updateHint struct {
	done chan struct{}
}

// defaultUpdateCheck builds the production lookup. app.updateCheck stays nil
// unless this is installed, so no test and no embedding can reach the network
// by accident.
func defaultUpdateCheck(app *appContext) func(context.Context) (updatecheck.Result, error) {
	return func(ctx context.Context) (updatecheck.Result, error) {
		if updateCheckDisabled(app) {
			return updatecheck.Result{}, nil
		}
		stateDir, err := config.StateDir(app.getenv)
		if err != nil {
			return updatecheck.Result{}, err
		}
		checker := updatecheck.Checker{
			CurrentVersion:   buildinfo.Version,
			LatestReleaseURL: releaseURL(app),
			CachePath:        filepath.Join(stateDir, "update-check.json"),
			TTL:              updateHintTTL,
			FailureTTL:       updateHintFailureTTL,
		}
		return checker.Run(ctx)
	}
}

// releaseURL prefers the environment override so an install that publishes
// elsewhere can be retargeted without shipping a new binary.
func releaseURL(app *appContext) string {
	if override := strings.TrimSpace(app.getenv(releaseURLEnv)); override != "" {
		return override
	}
	return defaultReleaseURL
}

// updateCheckDisabled reads the two opt-outs: the environment variable, and the
// config.toml `update_check = false` switch. A broken config never disables or
// enables anything by surprise — the hint simply stays on.
func updateCheckDisabled(app *appContext) bool {
	if strings.TrimSpace(app.getenv(noUpdateCheckEnv)) != "" {
		return true
	}
	file, err := config.Load(app.getenv)
	if err != nil || file == nil {
		return false
	}
	return config.UpdateCheckDisabled(file)
}

// updateHintBudget returns the wait budget for this invocation.
func (app *appContext) hintBudget() time.Duration {
	if app.updateHintBudget > 0 {
		return app.updateHintBudget
	}
	return defaultUpdateHintBudget
}

// startUpdateHint launches the lookup in the background, overlapped with the
// command's own work. A lookup that fails or finds nothing stays silent.
func startUpdateHint(cmd *cobra.Command, app *appContext) *updateHint {
	if app.updateCheck == nil || commandPath(cmd) == "version" {
		// `version` owns the update question through --check; letting the hint
		// run there would either duplicate or race that answer.
		return nil
	}
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	hint := &updateHint{done: make(chan struct{})}
	go func() {
		defer close(hint.done)
		result, err := app.updateCheck(ctx)
		if err != nil || !result.Outdated {
			return
		}
		app.printer.Progressf("%s", describeUpdate(result))
	}()
	return hint
}

// wait blocks until the lookup lands or the budget runs out. Waiting is the
// point: a lookup abandoned at process exit never reaches the cache, so the
// next command would pay the timeout again.
func (h *updateHint) wait(budget time.Duration) {
	if h == nil {
		return
	}
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-h.done:
	case <-timer.C:
	}
}

func describeUpdate(result updatecheck.Result) string {
	return fmt.Sprintf(
		"[提示] qianjue 有新版本 %s（当前 %s），升级方式见千谲官方安装页；发布记录 %s",
		result.Latest, result.Current, result.ReleaseURL)
}

// runUpdateCheckNow performs the lookup synchronously for `version --check`.
func runUpdateCheckNow(app *appContext) (updatecheck.Result, error) {
	if app.updateCheck == nil {
		return updatecheck.Result{}, clierr.Usage("当前运行环境未启用更新检查")
	}
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return app.updateCheck(ctx)
}
