package cmd

import (
	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/buildinfo"
)

type versionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

func newVersionCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出 CLI 版本信息",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data := versionData{
				Version:   buildinfo.Version,
				Commit:    buildinfo.Commit,
				BuildDate: buildinfo.BuildDate,
			}
			return app.printer.Success("version", data, map[string]any{}, [][2]string{
				{"Version", data.Version},
				{"Commit", data.Commit},
				{"Build date", data.BuildDate},
			})
		},
	}
}
