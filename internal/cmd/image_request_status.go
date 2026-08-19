package cmd

import (
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
)

func newImageRequestStatusCommand(app *appContext) *cobra.Command {
	var idemKey string
	c := &cobra.Command{
		Use:   "request-status",
		Short: "查询图片创建请求的脱敏幂等状态",
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
			status, err := client.GetIdempotencyStatus(cmd.Context(), idemKey)
			if err != nil {
				return err
			}
			return app.printer.Success("image.request-status", status, app.meta, requestStatusRows(status))
		},
	}
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "要查询的 Idempotency-Key（必填，不自动取最近一次）")
	_ = c.MarkFlagRequired("idempotency-key")
	return c
}

func requestStatusRows(s *api.RequestStatus) [][2]string {
	rows := [][2]string{
		{"Request ID", formatInt64Ptr(s.RequestID)},
		{"Status", s.Status},
		{"Attempt", formatIntPtr(s.AttemptNo)},
		{"Resource type", strPtr(s.ResourceType)},
		{"Resource ID", strPtr(s.ResourceID)},
		{"Error code", strPtr(s.ErrorCode)},
		{"Retryable", boolPtr(s.Retryable)},
		{"Retry after", timePtr(s.RetryAfterAt)},
		{"Completed at", timePtr(s.CompletedAt)},
	}
	return rows
}

func formatInt64Ptr(v *int64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatInt(*v, 10)
}

func formatIntPtr(v *int) string {
	if v == nil {
		return "-"
	}
	return strconv.Itoa(*v)
}

func strPtr(v *string) string {
	if v == nil || *v == "" {
		return "-"
	}
	return *v
}

func boolPtr(v *bool) string {
	if v == nil {
		return "-"
	}
	return boolString(*v)
}

func timePtr(v *api.APITime) string {
	if v == nil || v.IsZero() {
		return "-"
	}
	return v.Format(time.RFC3339)
}
