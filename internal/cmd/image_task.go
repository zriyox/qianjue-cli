package cmd

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/task"
)

// validateTaskID keeps task ids numeric before they hit the URL path.
func validateTaskID(arg string) (string, error) {
	if _, err := strconv.ParseUint(arg, 10, 64); err != nil {
		return "", clierr.Usage("任务 ID 必须是正整数（收到 %q）", arg)
	}
	return arg, nil
}

// resolveWaitTimeout applies the command-level --wait-timeout on top of the
// env/profile-resolved value.
func resolveWaitTimeout(resolved *config.Resolved, flagVal string) (time.Duration, error) {
	if flagVal == "" {
		return resolved.TaskWaitTimeout, nil
	}
	d, err := time.ParseDuration(flagVal)
	if err != nil || d <= 0 {
		return 0, clierr.Usage("无效的 --wait-timeout 值 %q（示例：10m）", flagVal)
	}
	return d, nil
}

// taskTableRows extracts the human-readable summary from a raw task document.
func taskTableRows(raw json.RawMessage) [][2]string {
	var probe struct {
		ID              json.Number `json:"id"`
		Status          string      `json:"status"`
		TaskType        string      `json:"taskType"`
		ModelCode       string      `json:"modelCode"`
		ResultImageUrls []string    `json:"resultImageUrls"`
		ErrorMessage    string      `json:"errorMessage"`
		CancelReason    string      `json:"cancelReason"`
	}
	_ = json.Unmarshal(raw, &probe)
	rows := [][2]string{
		{"Task ID", probe.ID.String()},
		{"Status", probe.Status},
		{"Type", probe.TaskType},
		{"Model", probe.ModelCode},
		{"Result count", strconv.Itoa(len(probe.ResultImageUrls))},
	}
	if probe.ErrorMessage != "" {
		rows = append(rows, [2]string{"Error", probe.ErrorMessage})
	}
	if probe.CancelReason != "" {
		rows = append(rows, [2]string{"Cancel reason", probe.CancelReason})
	}
	return rows
}

func newImageGetCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <taskId>",
		Short: "查询图片任务",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := validateTaskID(args[0])
			if err != nil {
				return err
			}
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
			raw, _, err := client.GetImageTask(cmd.Context(), taskID)
			if err != nil {
				return err
			}
			return app.printer.Success("image.get", raw, app.meta, taskTableRows(raw))
		},
	}
}

func newImageWaitCommand(app *appContext) *cobra.Command {
	var waitTimeout string
	c := &cobra.Command{
		Use:   "wait <taskId>",
		Short: "等待图片任务终态（Ctrl-C 只停止本地等待，不取消服务端任务）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := validateTaskID(args[0])
			if err != nil {
				return err
			}
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			if err := resolved.RequireAPIBaseURL(); err != nil {
				return err
			}
			timeout, err := resolveWaitTimeout(resolved, waitTimeout)
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			raw, err := task.Wait(cmd.Context(), client, taskID, timeout, app.printer, app.waitInterval)
			if err != nil {
				return err
			}
			return app.printer.Success("image.wait", raw, app.meta, taskTableRows(raw))
		},
	}
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	return c
}

func newImageCancelCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <taskId>",
		Short: "取消 PENDING 图片任务（失败不自动重试）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := validateTaskID(args[0])
			if err != nil {
				return err
			}
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
			msg, err := client.CancelImageTask(cmd.Context(), taskID)
			if err != nil {
				return err
			}
			data := map[string]any{"taskId": taskID, "message": msg, "cancelled": true}
			return app.printer.Success("image.cancel", data, app.meta, [][2]string{
				{"Task ID", taskID},
				{"Cancelled", "true"},
				{"Message", msg},
			})
		},
	}
}
