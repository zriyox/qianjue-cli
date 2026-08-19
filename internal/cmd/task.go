package cmd

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/task"
)

// newTaskCommand builds the domain-agnostic task command group. get/wait/list
// read the unified projection (UnifiedTaskSummaryView/DetailView), so one set
// of commands covers image/video and every future domain — new capabilities
// inherit read/wait for free (docs/09 backbone).
func newTaskCommand(app *appContext) *cobra.Command {
	t := &cobra.Command{Use: "task", Short: "统一任务查询（跨图片/视频等域）"}
	t.AddCommand(
		newTaskGetCommand(app),
		newTaskListCommand(app),
		newTaskWaitCommand(app),
		newTaskCancelCommand(app),
	)
	return t
}

// validateDomain keeps the domain path segment non-empty; the backend is the
// authority on which domains are supported and returns a clear 400 otherwise.
func validateDomain(arg string) (string, error) {
	if arg == "" {
		return "", clierr.Usage("任务域不能为空（如 image、video、image-chat）")
	}
	return arg, nil
}

// unifiedTableRows renders the human summary from a raw unified task view.
func unifiedTableRows(raw json.RawMessage) [][2]string {
	var probe struct {
		TaskID   json.Number `json:"taskId"`
		Domain   string      `json:"domain"`
		TaskType string      `json:"taskType"`
		Status   string      `json:"status"`
		Progress *int        `json:"progress"`
		Message  string      `json:"message"`
		Media    struct {
			ResultMediaList []string `json:"resultMediaList"`
		} `json:"media"`
	}
	_ = json.Unmarshal(raw, &probe)
	rows := [][2]string{
		{"Task ID", probe.TaskID.String()},
		{"Domain", probe.Domain},
		{"Status", probe.Status},
		{"Type", probe.TaskType},
		{"Result count", strconv.Itoa(len(probe.Media.ResultMediaList))},
	}
	if probe.Message != "" {
		rows = append(rows, [2]string{"Message", probe.Message})
	}
	return rows
}

func newTaskGetCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <domain> <taskId>",
		Short: "查询任务详情（domain 如 image、video、image-chat）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, err := validateDomain(args[0])
			if err != nil {
				return err
			}
			taskID, err := validateTaskID(args[1])
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
			raw, _, err := client.GetUnifiedTask(cmd.Context(), domain, taskID)
			if err != nil {
				return err
			}
			return app.printer.Success("task.get", raw, app.meta, unifiedTableRows(raw))
		},
	}
}

func newTaskListCommand(app *appContext) *cobra.Command {
	var status string
	var page, size int
	c := &cobra.Command{
		Use:   "list <domain>",
		Short: "分页查询任务列表（domain 如 image、video、image-chat）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, err := validateDomain(args[0])
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
			raw, err := client.ListUnifiedTasks(cmd.Context(), domain, status, page, size)
			if err != nil {
				return err
			}
			return app.printer.Success("task.list", raw, app.meta, unifiedListTableRows(raw))
		},
	}
	c.Flags().StringVar(&status, "status", "", "按统一状态过滤（PENDING/PROCESSING/COMPLETED/FAILED/CANCELLED）")
	c.Flags().IntVar(&page, "page", 0, "页码，从 1 起")
	c.Flags().IntVar(&size, "size", 0, "每页条数")
	return c
}

func unifiedListTableRows(raw json.RawMessage) [][2]string {
	var probe struct {
		Total   json.Number `json:"total"`
		PageNum json.Number `json:"pageNum"`
		Records []struct {
			TaskID   json.Number `json:"taskId"`
			Status   string      `json:"status"`
			TaskType string      `json:"taskType"`
		} `json:"records"`
	}
	_ = json.Unmarshal(raw, &probe)
	rows := [][2]string{
		{"Total", probe.Total.String()},
		{"Page", probe.PageNum.String()},
	}
	for _, r := range probe.Records {
		rows = append(rows, [2]string{r.TaskID.String(), r.Status + " " + r.TaskType})
	}
	return rows
}

func newTaskCancelCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <domain> <taskId>",
		Short: "取消任务（失败不自动重试；domain 如 image、video、image-chat）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, err := validateDomain(args[0])
			if err != nil {
				return err
			}
			taskID, err := validateTaskID(args[1])
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
			msg, err := client.CancelUnifiedTask(cmd.Context(), domain, taskID)
			if err != nil {
				return err
			}
			data := map[string]any{"domain": domain, "taskId": taskID, "message": msg, "cancelled": true}
			return app.printer.Success("task.cancel", data, app.meta, [][2]string{
				{"Domain", domain},
				{"Task ID", taskID},
				{"Cancelled", "true"},
				{"Message", msg},
			})
		},
	}
}

func newTaskWaitCommand(app *appContext) *cobra.Command {
	var waitTimeout string
	c := &cobra.Command{
		Use:   "wait <domain> <taskId>",
		Short: "等待任务终态（domain 如 image、video、image-chat；Ctrl-C 只停本地等待）",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain, err := validateDomain(args[0])
			if err != nil {
				return err
			}
			taskID, err := validateTaskID(args[1])
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
			fetch := task.Fetch(func(fctx context.Context) (json.RawMessage, string, error) {
				return client.GetUnifiedTask(fctx, domain, taskID)
			})
			raw, err := task.WaitFetch(cmd.Context(), fetch, domain+"/"+taskID, timeout, app.printer, app.waitInterval)
			if err != nil {
				return err
			}
			return app.printer.Success("task.wait", raw, app.meta, unifiedTableRows(raw))
		},
	}
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	return c
}
