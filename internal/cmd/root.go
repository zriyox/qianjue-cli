// Package cmd wires the cobra command tree. Commands only parse arguments,
// assemble dependencies, and print results; business logic lives in the
// internal service packages.
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/authflow"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/cred"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// globalFlags carries the raw values of the global flags (cli-contract.md §5).
type globalFlags struct {
	profile     string
	apiBaseURL  string
	outputFmt   string
	quiet       bool
	noColor     bool
	httpTimeout string
	trace       bool
}

// appContext is the per-invocation dependency container handed to commands.
type appContext struct {
	flags   *globalFlags
	stdout  io.Writer
	stderr  io.Writer
	stdin   io.Reader
	getenv  func(string) string
	isTTY   func() bool
	printer *output.Printer
	meta    map[string]any // envelope meta for the running command (profile/apiBaseUrl/traceId/...)

	// injectable seams (overridden by tests)
	ctx               context.Context
	newStore          func() (cred.Store, error)
	openBrowser       func(url string) error
	loginPollInterval time.Duration
	waitInterval      func(attempt int) time.Duration // nil → backoff.Interval
	homeDir           func() (string, error)          // nil → os.UserHomeDir (cross-platform)
}

// newAPIClient builds the per-command HTTP client bound to one trace id and
// records the trace id in the envelope meta (cli-contract.md §27).
func (app *appContext) newAPIClient(resolved *config.Resolved, tokens api.TokenSource) *api.Client {
	traceID := api.NewTraceID()
	if app.meta == nil {
		app.meta = map[string]any{}
	}
	app.meta["traceId"] = traceID
	var tracef func(string, ...any)
	if resolved.Trace {
		tracef = app.printer.Progressf
	}
	return api.NewClient(resolved.APIBaseURL, resolved.HTTPTimeout, tokens, traceID, tracef)
}

// resolveOutputFormat implements the fixed precedence flag > QIANJUE_OUTPUT >
// profile output > TTY default (table on TTY, json otherwise). The profile
// value comes from a lenient config load so that local-only commands (e.g.
// version) still work when the config file is broken; strict validation
// happens in resolveConfig for commands that actually need configuration.
func resolveOutputFormat(flagVal string, getenv func(string) string, profileOutput string, isTTY bool) (output.Format, error) {
	if flagVal != "" {
		return output.ParseFormat(flagVal)
	}
	if env := getenv("QIANJUE_OUTPUT"); env != "" {
		return output.ParseFormat(env)
	}
	if profileOutput != "" {
		return output.ParseFormat(profileOutput)
	}
	if isTTY {
		return output.FormatTable, nil
	}
	return output.FormatJSON, nil
}

// overrides converts the raw global flags into config precedence input.
func (app *appContext) overrides() config.Overrides {
	return config.Overrides{
		Profile:     app.flags.profile,
		APIBaseURL:  app.flags.apiBaseURL,
		Output:      app.flags.outputFmt,
		HTTPTimeout: app.flags.httpTimeout,
		Quiet:       app.flags.quiet,
		NoColor:     app.flags.noColor,
		Trace:       app.flags.trace,
	}
}

// newAuthedClient builds the bearer-authenticated client for image commands.
// The anonymous refresh client shares the same trace id so refresh, create
// and status queries correlate under one trace (cli-contract.md §27).
func (app *appContext) newAuthedClient(resolved *config.Resolved) (*api.Client, error) {
	traceID := api.NewTraceID()
	if app.meta == nil {
		app.meta = map[string]any{}
	}
	app.meta["traceId"] = traceID
	var tracef func(string, ...any)
	if resolved.Trace {
		tracef = app.printer.Progressf
	}
	refreshClient := api.NewClient(resolved.APIBaseURL, resolved.HTTPTimeout, nil, traceID, tracef)
	tokens, err := authflow.ResolveTokenSource(app.getenv, app.newStore, resolved.ProfileName, refreshClient)
	if err != nil {
		return nil, err
	}
	return api.NewClient(resolved.APIBaseURL, resolved.HTTPTimeout, tokens, traceID, tracef), nil
}

// resolveConfig performs the strict configuration resolution used by commands
// that depend on config, and fills the envelope meta.
func (app *appContext) resolveConfig() (*config.Resolved, error) {
	f, err := config.Load(app.getenv)
	if err != nil {
		return nil, err
	}
	resolved, err := config.Resolve(f, app.overrides(), app.getenv, app.isTTY())
	if err != nil {
		return nil, err
	}
	app.meta = map[string]any{"profile": resolved.ProfileName}
	if resolved.APIBaseURL != "" {
		app.meta["apiBaseUrl"] = resolved.APIBaseURL
	}
	return resolved, nil
}

func stdoutIsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// commandPath converts cobra's "qianjue image create" into the envelope
// command name "image.create" (cli-contract.md §21).
func commandPath(c *cobra.Command) string {
	path := c.CommandPath()
	if i := strings.Index(path, " "); i >= 0 {
		return strings.ReplaceAll(path[i+1:], " ", ".")
	}
	return path
}

// newRootCommand builds the full command tree against the given app context.
func newRootCommand(app *appContext) *cobra.Command {
	root := &cobra.Command{
		Use:           "qianjue",
		Short:         "千谲 Integration CLI",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			profileOutput := ""
			if f, err := config.Load(app.getenv); err == nil {
				name := config.ResolveProfileName(f, app.flags.profile, app.getenv)
				profileOutput = f.Profiles[name].Output
			}
			format, err := resolveOutputFormat(app.flags.outputFmt, app.getenv, profileOutput, app.isTTY())
			if err != nil {
				return err
			}
			noColor := app.flags.noColor || app.getenv("NO_COLOR") != ""
			app.printer = output.NewPrinter(app.stdout, app.stderr, format, app.flags.quiet, noColor)
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&app.flags.profile, "profile", "", "选择 API 与凭证命名空间")
	pf.StringVar(&app.flags.apiBaseURL, "api-base-url", "", "单次覆盖 API 根地址")
	pf.StringVar(&app.flags.outputFmt, "output", "", "输出格式：table 或 json")
	pf.BoolVar(&app.flags.quiet, "quiet", false, "禁止非必要 stderr 进度输出")
	pf.BoolVar(&app.flags.noColor, "no-color", false, "禁止 ANSI 色彩")
	pf.StringVar(&app.flags.httpTimeout, "http-timeout", "", "单次 HTTP 请求超时（如 30s）")
	pf.BoolVar(&app.flags.trace, "trace", false, "输出请求阶段与 Trace ID")

	root.AddCommand(newVersionCommand(app))
	root.AddCommand(newConfigCommand(app))
	root.AddCommand(newAuthCommand(app))
	root.AddCommand(newImageCommand(app))
	root.AddCommand(newTaskCommand(app))
	root.AddCommand(newVideoCommand(app))
	root.AddCommand(newAssetCommand(app))
	root.AddCommand(newCatalogCommand(app))
	root.AddCommand(newDetailImageCommand(app))
	root.AddCommand(newReversePromptCommand(app))
	root.AddCommand(newViralPlanCommand(app))
	root.AddCommand(newSkillCommand(app))
	return root
}

// run executes the CLI against the given context and returns the exit code.
// Split from Execute so tests can inject writers/env/TTY.
func run(app *appContext, args []string) int {
	app.applyDefaults()
	root := newRootCommand(app)
	root.SetArgs(args)
	root.SetOut(app.stderr) // cobra help/usage 文本不得混入 stdout JSON
	root.SetErr(app.stderr)
	if app.ctx != nil {
		root.SetContext(app.ctx)
	}

	executed, err := root.ExecuteC()
	if err == nil {
		return clierr.ExitOK
	}
	// 命令实现只返回 *CLIError；其余错误都来自 cobra 的参数解析
	// （未知命令、非法 flag、缺必填 flag），归 USAGE。
	ce, ok := err.(*clierr.CLIError)
	if !ok {
		ce = clierr.Usage("%s", err.Error())
	}
	if app.printer == nil {
		// 解析错误可能发生在 printer 装配前，只能走 stderr。
		fmt.Fprintln(app.stderr, "Error:", output.Redact(ce.Error()))
		return clierr.ExitUsage
	}
	return app.printer.Failure(commandPath(executed), ce, app.meta)
}

// Execute runs the CLI with OS-level defaults and returns the process exit
// code. Ctrl-C cancels the local context only; it never cancels server-side
// tasks (cli-contract.md §19).
func Execute(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app := &appContext{
		flags:  &globalFlags{},
		stdout: os.Stdout,
		stderr: os.Stderr,
		stdin:  os.Stdin,
		getenv: os.Getenv,
		isTTY:  stdoutIsTTY,
		ctx:    ctx,
	}
	return run(app, args)
}

// applyDefaults fills the injectable seams not overridden by tests.
func (app *appContext) applyDefaults() {
	if app.newStore == nil {
		app.newStore = cred.NewSystemStore
	}
	if app.openBrowser == nil {
		app.openBrowser = authflow.OpenBrowser
	}
	if app.loginPollInterval == 0 {
		app.loginPollInterval = 2 * time.Second
	}
	if app.homeDir == nil {
		app.homeDir = os.UserHomeDir
	}
}
