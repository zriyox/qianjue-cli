package cmd

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/authflow"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/cred"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// maxTokenBytes bounds the stdin read; real PATs are well under this.
const maxTokenBytes = 4096

// normalizePastedToken trims whitespace and a leading UTF-8 BOM. Windows
// PowerShell 5.1 on a UTF-8 console prepends EF BB BF to text piped into a
// native program, so a correct qj_pat_ token arrived as "\uFEFFqj_pat_..." and
// was rejected as not starting with qj_pat_ (reproduced 2026-10-06).
func normalizePastedToken(raw string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "\uFEFF"))
}

func newAuthImportTokenCommand(app *appContext) *cobra.Command {
	var tokenType string
	var fromStdin bool
	c := &cobra.Command{
		Use:   "import-token",
		Short: "导入 Personal Access Token（终端里按提示粘贴、不回显；脚本里用 --stdin 管道传入）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tokenType != "pat" {
				return clierr.Usage("--type 只支持 pat（收到 %q）", tokenType)
			}
			// 令牌永远不进命令参数（会留在 shell 历史与进程列表里）：
			// 脚本用 --stdin 管道传入；人在终端里运行时由 CLI 提示粘贴，输入不回显。
			// 网页引导曾让用户「先复制令牌、再复制命令 | qianjue ... --stdin」，复制命令那一下
			// 就把剪贴板里的令牌覆盖了 —— 所以终端场景改为先运行命令、看到提示再复制粘贴令牌。
			interactive := !fromStdin && app.stdinIsTerminal != nil && app.stdinIsTerminal() && app.readSecret != nil
			if !fromStdin && !interactive {
				return clierr.Usage("请在终端中运行 qianjue auth import-token --type pat 后按提示粘贴令牌；脚本中请通过管道传入并加 --stdin")
			}
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}

			var raw string
			if interactive {
				secret, rerr := app.readSecret("粘贴令牌后回车（输入不会显示）: ")
				if rerr != nil {
					return clierr.Usage("读取输入失败: %v", rerr)
				}
				raw = secret
			} else {
				data, rerr := io.ReadAll(io.LimitReader(app.stdin, maxTokenBytes+1))
				if rerr != nil {
					return clierr.Usage("读取 stdin 失败: %v", rerr)
				}
				raw = string(data)
			}
			if len(raw) > maxTokenBytes {
				return clierr.Usage("输入内容超过 %d 字节，不是合法 PAT", maxTokenBytes)
			}
			token := normalizePastedToken(raw)
			switch {
			case token == "" && interactive:
				return clierr.Usage("没有收到令牌：请粘贴 qj_pat_ 开头的令牌后回车")
			case token == "":
				return clierr.Usage("stdin 为空：请把 PAT 通过管道写入 stdin")
			case strings.HasPrefix(token, "qj_rt_"):
				return clierr.Usage("qj_rt_ 是 Refresh Token，不能作为 PAT 导入")
			case strings.HasPrefix(token, "qj_at_"):
				return clierr.Usage("qj_at_ 是 Device Flow Access Token，缺少 Refresh Token 与过期元数据，不做持久化；请使用 qianjue auth login")
			case !strings.HasPrefix(token, "qj_pat_"):
				return clierr.Usage("PAT 必须以 qj_pat_ 开头")
			}

			store, err := app.newStore()
			if err != nil {
				return err
			}
			// 绑定当前 API 地址：之后只发往这里，被改掉的 --api-base-url 带不走它
			rec := &cred.Record{CredentialType: cred.TypePAT, AccessToken: token, APIBaseURL: resolved.APIBaseURL}
			if err := store.Set(cred.PATAccount(resolved.ProfileName), rec); err != nil {
				return clierr.AsCLIError(err)
			}

			// 同 Profile 下的 Device Flow 记录必须清掉，否则这次导入不会生效：
			// authflow.ResolveTokenSource 刻意让 Device Flow 优先于 PAT（契约见
			// TestResolveTokenSourcePrecedence），于是请求继续用旧 Device Flow token，
			// 而本命令已经报了 imported=true —— 旧会话失效时用户会陷入死循环：
			// 导入新 PAT、CLI 报成功、请求照旧 401，且 auth status 也只显示 DEVICE_FLOW。
			// 暂存记录一并清，否则下一次刷新会把 Device Flow 记录写回来。
			// 反方向刻意不对称：auth login 不删 PAT —— Device Flow 随时能重新 login 拿回，
			// 而 PAT 只在签发时可见一次，替用户销毁不可恢复的凭证是更大的错。
			deviceFlowCleared := false
			for _, account := range []string{
				cred.DeviceFlowAccount(resolved.ProfileName),
				cred.DeviceFlowStagingAccount(resolved.ProfileName),
			} {
				if _, gerr := store.Get(account); gerr != nil {
					continue
				}
				if derr := store.Delete(account); derr != nil {
					return clierr.AsCLIError(derr)
				}
				deviceFlowCleared = true
			}

			data := map[string]any{
				"profile":           resolved.ProfileName,
				"credentialType":    cred.TypePAT,
				"imported":          true,
				"deviceFlowCleared": deviceFlowCleared,
			}
			rows := [][2]string{
				{"Profile", resolved.ProfileName},
				{"Credential type", cred.TypePAT},
				{"Imported", "true"},
			}
			if deviceFlowCleared {
				rows = append(rows, [2]string{"Device Flow 凭证", "已清除（PAT 生效）"})
			}
			return app.printer.Success("auth.import-token", data, app.meta, rows)
		},
	}
	c.Flags().StringVar(&tokenType, "type", "", "凭证类型，目前只支持 pat")
	c.Flags().BoolVar(&fromStdin, "stdin", false, "从 stdin 管道读取 Token（脚本用；在终端里可省略，运行后按提示粘贴）")
	_ = c.MarkFlagRequired("type")
	return c
}

func newAuthStatusCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "显示当前凭证状态（只读本地元数据，不调用后端）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			now := time.Now()

			if env := strings.TrimSpace(app.getenv("QIANJUE_TOKEN")); env != "" {
				tokenType := "unknown"
				if strings.HasPrefix(env, "qj_at_") {
					tokenType = cred.TypeDeviceFlow
				} else if strings.HasPrefix(env, "qj_pat_") {
					tokenType = cred.TypePAT
				}
				data := map[string]any{
					"profile":          resolved.ProfileName,
					"credentialSource": "environment",
					"credentialType":   tokenType,
				}
				return app.printer.Success("auth.status", data, app.meta, [][2]string{
					{"Profile", resolved.ProfileName},
					{"Source", "environment (QIANJUE_TOKEN)"},
					{"Credential type", tokenType},
				})
			}

			store, err := app.newStore()
			if err != nil {
				return err
			}
			if rec, gerr := store.Get(cred.DeviceFlowAccount(resolved.ProfileName)); gerr == nil {
				data := map[string]any{
					"profile":              resolved.ProfileName,
					"credentialSource":     "keyring",
					"credentialType":       cred.TypeDeviceFlow,
					"sessionId":            rec.SessionID,
					"scopes":               rec.Scopes,
					"accessTokenExpiresAt": rec.AccessTokenExpiresAt.Format(time.RFC3339),
					"sessionExpiresAt":     rec.SessionExpiresAt.Format(time.RFC3339),
					"accessTokenExpired":   !rec.AccessTokenExpiresAt.After(now),
					"sessionExpired":       !rec.SessionExpiresAt.After(now),
				}
				return app.printer.Success("auth.status", data, app.meta, [][2]string{
					{"Profile", resolved.ProfileName},
					{"Source", "keyring"},
					{"Credential type", cred.TypeDeviceFlow},
					{"Session ID", rec.SessionID},
					{"Scopes", joinScopes(rec.Scopes)},
					{"Access token expires", rec.AccessTokenExpiresAt.Format(time.RFC3339)},
					{"Session expires", rec.SessionExpiresAt.Format(time.RFC3339)},
				})
			} else if gerr != cred.ErrNotFound {
				return clierr.AsCLIError(gerr)
			}
			if _, gerr := store.Get(cred.PATAccount(resolved.ProfileName)); gerr == nil {
				data := map[string]any{
					"profile":          resolved.ProfileName,
					"credentialSource": "keyring",
					"credentialType":   cred.TypePAT,
				}
				return app.printer.Success("auth.status", data, app.meta, [][2]string{
					{"Profile", resolved.ProfileName},
					{"Source", "keyring"},
					{"Credential type", cred.TypePAT},
				})
			} else if gerr != cred.ErrNotFound {
				return clierr.AsCLIError(gerr)
			}

			data := map[string]any{
				"profile":          resolved.ProfileName,
				"credentialSource": "none",
			}
			return app.printer.Success("auth.status", data, app.meta, [][2]string{
				{"Profile", resolved.ProfileName},
				{"Source", "none"},
			})
		},
	}
}

func newAuthLogoutCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "吊销当前 Profile 的 Device Flow 服务端会话并删除本地凭证（PAT 只删本地，到网页端管理）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			store, err := app.newStore()
			if err != nil {
				return err
			}

			// 先吊销、后删本地：吊销要用本地凭证。失败也照常删本地，但如实报告
			remoteRevoked, remoteErr := app.revokeDeviceFlowSession(cmd.Context(), resolved, store)

			removed := false
			for _, account := range []string{
				cred.DeviceFlowAccount(resolved.ProfileName),
				cred.DeviceFlowStagingAccount(resolved.ProfileName),
				cred.PATAccount(resolved.ProfileName),
			} {
				if _, gerr := store.Get(account); gerr == nil {
					if derr := store.Delete(account); derr != nil {
						return clierr.AsCLIError(derr)
					}
					removed = true
				} else if gerr != cred.ErrNotFound {
					return clierr.AsCLIError(gerr)
				}
			}

			data := map[string]any{
				"profile":                resolved.ProfileName,
				"localCredentialRemoved": removed,
				"remoteSessionRevoked":   remoteRevoked,
			}
			rows := [][2]string{
				{"Profile", resolved.ProfileName},
				{"Local credential removed", boolString(removed)},
				{"Remote session revoked", boolString(remoteRevoked)},
			}
			if remoteErr != "" {
				data["remoteRevokeError"] = remoteErr
				rows = append(rows, [2]string{"Remote revoke error", remoteErr})
				app.printer.Progressf("服务端会话未吊销（%s）；本地凭证已删除，服务端会话将在到期后自然失效", remoteErr)
			}
			return app.printer.Success("auth.logout", data, app.meta, rows)
		},
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// revokeDeviceFlowSession revokes the profile's Device Flow session on the
// server (auth-device-flow.md §9.1). It reports false with a reason instead of
// failing: logout must still remove the local credential. PATs are not revoked
// here — the same PAT may still be in use on a server or in CI.
func (app *appContext) revokeDeviceFlowSession(ctx context.Context, resolved *config.Resolved, store cred.Store) (bool, string) {
	rec, err := store.Get(cred.DeviceFlowAccount(resolved.ProfileName))
	if err != nil || rec.CredentialType != cred.TypeDeviceFlow || rec.AccessToken == "" {
		return false, ""
	}
	// 吊销请求同样带着凭证：签发地址与当前地址不同就不发
	if err := authflow.RequireBoundTo(rec, resolved.APIBaseURL); err != nil {
		return false, "凭证签发地址与当前 API 根地址不同，未发送吊销请求"
	}
	traceID := api.NewTraceID()
	refreshClient := api.NewClient(resolved.APIBaseURL, resolved.HTTPTimeout, nil, traceID, nil)
	tokens := authflow.NewDeviceFlowTokenSource(store, app.getenv, resolved.ProfileName, refreshClient)
	client := api.NewClient(resolved.APIBaseURL, resolved.HTTPTimeout, tokens, traceID, nil)
	if _, err := client.RevokeCurrentSession(ctx); err != nil {
		return false, output.Redact(clierr.AsCLIError(err).Message)
	}
	return true, ""
}
