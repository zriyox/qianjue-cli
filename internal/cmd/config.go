package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
)

func newConfigCommand(app *appContext) *cobra.Command {
	configCmd := &cobra.Command{Use: "config", Short: "管理 CLI 配置"}
	profileCmd := &cobra.Command{Use: "profile", Short: "管理 API Profile"}
	profileCmd.AddCommand(
		newProfileCreateCommand(app),
		newProfileUseCommand(app),
		newProfileListCommand(app),
	)
	configCmd.AddCommand(profileCmd, newConfigShowCommand(app), newConfigPathCommand(app))
	return configCmd
}

func newProfileCreateCommand(app *appContext) *cobra.Command {
	var apiBaseURL string
	c := &cobra.Command{
		Use:   "create <name>",
		Short: "创建或更新一个 Profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := config.ValidateProfileName(name); err != nil {
				return err
			}
			if err := config.ValidateBaseURL(apiBaseURL); err != nil {
				return err
			}
			f, err := config.Load(app.getenv)
			if err != nil {
				return err
			}
			prof := f.Profiles[name]
			prof.APIBaseURL = apiBaseURL
			f.Profiles[name] = prof
			if f.CurrentProfile == "" {
				f.CurrentProfile = name
			}
			if err := config.Save(app.getenv, f); err != nil {
				return err
			}
			data := map[string]any{
				"profile":        name,
				"apiBaseUrl":     apiBaseURL,
				"currentProfile": f.CurrentProfile,
			}
			return app.printer.Success("config.profile.create", data, app.meta, [][2]string{
				{"Profile", name},
				{"API base URL", apiBaseURL},
				{"Current profile", f.CurrentProfile},
			})
		},
	}
	c.Flags().StringVar(&apiBaseURL, "api-base-url", "", "该 Profile 的 API 根地址（必填）")
	_ = c.MarkFlagRequired("api-base-url")
	return c
}

func newProfileUseCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "切换当前 Profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			f, err := config.Load(app.getenv)
			if err != nil {
				return err
			}
			if _, ok := f.Profiles[name]; !ok {
				return clierr.New(clierr.KindNotFound, fmt.Sprintf("Profile %q 不存在，先执行 qianjue config profile create %s --api-base-url '...'", name, name))
			}
			f.CurrentProfile = name
			if err := config.Save(app.getenv, f); err != nil {
				return err
			}
			data := map[string]any{"currentProfile": name}
			return app.printer.Success("config.profile.use", data, app.meta, [][2]string{
				{"Current profile", name},
			})
		},
	}
}

func newProfileListCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出全部 Profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := config.Load(app.getenv)
			if err != nil {
				return err
			}
			type profileRow struct {
				Name       string `json:"name"`
				APIBaseURL string `json:"apiBaseUrl"`
				Current    bool   `json:"current"`
			}
			rows := make([]profileRow, 0, len(f.Profiles))
			tableRows := make([][2]string, 0, len(f.Profiles))
			for _, name := range config.SortedProfileNames(f) {
				current := name == f.CurrentProfile
				rows = append(rows, profileRow{Name: name, APIBaseURL: f.Profiles[name].APIBaseURL, Current: current})
				marker := " "
				if current {
					marker = "*"
				}
				tableRows = append(tableRows, [2]string{marker + " " + name, f.Profiles[name].APIBaseURL})
			}
			data := map[string]any{"currentProfile": f.CurrentProfile, "profiles": rows}
			return app.printer.Success("config.profile.list", data, app.meta, tableRows)
		},
	}
}

func newConfigShowCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "显示当前生效配置（不含任何凭证）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			data := map[string]any{
				"profile":         resolved.ProfileName,
				"apiBaseUrl":      resolved.APIBaseURL,
				"output":          string(resolved.Output),
				"httpTimeout":     resolved.HTTPTimeout.String(),
				"taskWaitTimeout": resolved.TaskWaitTimeout.String(),
			}
			return app.printer.Success("config.show", data, app.meta, resolved.TableRows())
		},
	}
}

func newConfigPathCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "显示配置与状态目录路径",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := config.ConfigPath(app.getenv)
			if err != nil {
				return err
			}
			stateDir, err := config.StateDir(app.getenv)
			if err != nil {
				return err
			}
			data := map[string]any{"configPath": configPath, "stateDir": stateDir}
			return app.printer.Success("config.path", data, app.meta, [][2]string{
				{"Config", configPath},
				{"State dir", stateDir},
			})
		},
	}
}
