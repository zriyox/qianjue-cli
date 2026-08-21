package cmd

import (
	"github.com/spf13/cobra"
)

// newCatalogCommand exposes the server-side capability catalog. Without it the
// only way to know which models, ratios and resolutions exist is to hardcode
// them, which silently rots whenever the platform changes.
func newCatalogCommand(app *appContext) *cobra.Command {
	catalogCmd := &cobra.Command{Use: "catalog", Short: "查询平台能力目录（提交前确认参数，别硬编码）"}
	catalogCmd.AddCommand(newCatalogModelsCommand(app), newCatalogVideoModelsCommand(app))
	return catalogCmd
}

func newCatalogModelsCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "列出可用图片模型及其可选比例、分辨率、画布尺寸",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			raw, err := client.GetImageModelCatalog(cmd.Context())
			if err != nil {
				return err
			}
			return app.printer.Success("catalog.models", raw, app.meta, nil)
		},
	}
}

func newCatalogVideoModelsCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "video-models",
		Short: "列出可用视频模型及其可选时长、比例、输入图上限",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			raw, err := client.GetVideoModelCatalog(cmd.Context())
			if err != nil {
				return err
			}
			return app.printer.Success("catalog.video-models", raw, app.meta, nil)
		},
	}
}
