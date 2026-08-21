package cmd

import (
	"github.com/spf13/cobra"
)

// newVideoRequestStatusCommand 暴露 GET /integration/video-tasks/idempotency。
//
// API 层的 GetVideoIdempotencyStatus 早就实现了（video resume 内部在用），
// 但一直没挂成用户命令，导致视频侧只能 resume、没法先只读地看一眼幂等状态，
// 与图片侧的 `image request-status` 不对称。响应结构与图片侧完全一致。
func newVideoRequestStatusCommand(app *appContext) *cobra.Command {
	var idemKey string
	c := &cobra.Command{
		Use:   "request-status",
		Short: "查询视频创建请求的脱敏幂等状态",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			if err := resolved.RequireAPIBaseURL(); err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			status, err := client.GetVideoIdempotencyStatus(cmd.Context(), idemKey)
			if err != nil {
				return err
			}
			return app.printer.Success("video.request-status", status, app.meta, requestStatusRows(status))
		},
	}
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "要查询的 Idempotency-Key（必填，不自动取最近一次）")
	_ = c.MarkFlagRequired("idempotency-key")
	return c
}
