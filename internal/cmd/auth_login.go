package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/authflow"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

func newAuthCommand(app *appContext) *cobra.Command {
	auth := &cobra.Command{Use: "auth", Short: "管理 Integration 凭证"}
	auth.AddCommand(
		newAuthLoginCommand(app),
		newAuthImportTokenCommand(app),
		newAuthStatusCommand(app),
		newAuthLogoutCommand(app),
	)
	return auth
}

func newAuthLoginCommand(app *appContext) *cobra.Command {
	var scopes []string
	var noOpen bool
	var site string
	c := &cobra.Command{
		Use:   "login",
		Short: "Device Flow 登录并将凭证存入系统凭证库",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			if err := resolved.RequireAPIBaseURL(); err != nil {
				return err
			}
			store, err := app.newStore()
			if err != nil {
				return err
			}
			client := app.newAPIClient(resolved, nil)

			result, err := authflow.Login(cmd.Context(), client, store, app.printer, authflow.LoginOptions{
				Profile:      resolved.ProfileName,
				Scopes:       scopes,
				Site:         site,
				NoOpen:       noOpen,
				PollInterval: app.loginPollInterval,
				OpenBrowser:  app.openBrowser,
			})
			if err != nil {
				return err
			}

			data := map[string]any{
				"profile":              result.Profile,
				"credentialType":       cred.TypeDeviceFlow,
				"sessionId":            result.SessionID,
				"scopes":               result.Scopes,
				"accessTokenExpiresAt": result.AccessTokenExpiresAt.Format(time.RFC3339),
				"sessionExpiresAt":     result.SessionExpiresAt.Format(time.RFC3339),
			}
			return app.printer.Success("auth.login", data, app.meta, [][2]string{
				{"Profile", result.Profile},
				{"Credential type", cred.TypeDeviceFlow},
				{"Session ID", result.SessionID},
				{"Scopes", joinScopes(result.Scopes)},
				{"Access token expires", result.AccessTokenExpiresAt.Format(time.RFC3339)},
				{"Session expires", result.SessionExpiresAt.Format(time.RFC3339)},
			})
		},
	}
	c.Flags().StringArrayVar(&scopes, "scope", nil, "申请的 Integration Scope，可重复（默认与网页令牌一致：profile.read/task.create/task.read/task.cancel/asset.read/asset.write）")
	c.Flags().BoolVar(&noOpen, "no-open", false, "只打印授权 URL，不自动打开浏览器")
	c.Flags().StringVar(&site, "site", "", "账号所属站点的用户端域名（合作伙伴站点用户必填，如 acme.example.com）；不填则在官方站授权")
	return c
}

func joinScopes(scopes []string) string {
	out := ""
	for i, s := range scopes {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
