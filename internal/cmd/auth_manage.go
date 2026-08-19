package cmd

import (
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

// maxTokenBytes bounds the stdin read; real PATs are well under this.
const maxTokenBytes = 4096

func newAuthImportTokenCommand(app *appContext) *cobra.Command {
	var tokenType string
	var fromStdin bool
	c := &cobra.Command{
		Use:   "import-token",
		Short: "从 stdin 导入 Personal Access Token（不回显）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tokenType != "pat" {
				return clierr.Usage("--type 只支持 pat（收到 %q）", tokenType)
			}
			if !fromStdin {
				return clierr.Usage("PAT 只能通过 stdin 导入：qianjue auth import-token --type pat --stdin")
			}
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}

			raw, err := io.ReadAll(io.LimitReader(app.stdin, maxTokenBytes+1))
			if err != nil {
				return clierr.Usage("读取 stdin 失败: %v", err)
			}
			if len(raw) > maxTokenBytes {
				return clierr.Usage("stdin 内容超过 %d 字节，不是合法 PAT", maxTokenBytes)
			}
			token := strings.TrimSpace(string(raw))
			switch {
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
			rec := &cred.Record{CredentialType: cred.TypePAT, AccessToken: token}
			if err := store.Set(cred.PATAccount(resolved.ProfileName), rec); err != nil {
				return clierr.AsCLIError(err)
			}

			data := map[string]any{
				"profile":        resolved.ProfileName,
				"credentialType": cred.TypePAT,
				"imported":       true,
			}
			return app.printer.Success("auth.import-token", data, app.meta, [][2]string{
				{"Profile", resolved.ProfileName},
				{"Credential type", cred.TypePAT},
				{"Imported", "true"},
			})
		},
	}
	c.Flags().StringVar(&tokenType, "type", "", "凭证类型，目前只支持 pat")
	c.Flags().BoolVar(&fromStdin, "stdin", false, "从 stdin 读取 Token（必填）")
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
		Short: "删除当前 Profile 的本地凭证（后端无 Device Flow 撤销接口，不声称远程撤销）",
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
				"remoteSessionRevoked":   false,
			}
			return app.printer.Success("auth.logout", data, app.meta, [][2]string{
				{"Profile", resolved.ProfileName},
				{"Local credential removed", boolString(removed)},
				{"Remote session revoked", "false"},
			})
		},
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
