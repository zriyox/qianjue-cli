package cmd

import (
	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
	"github.com/zriyox/qianjue-cli/internal/task"
)

func newImageResumeCommand(app *appContext) *cobra.Command {
	var idemKey, waitTimeout string
	var wait, allowOriginChange bool
	c := &cobra.Command{
		Use:   "resume",
		Short: "用本地请求日志按原 Key/原 JSON 恢复未知结果的创建请求",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			if err := resolved.RequireAPIBaseURL(); err != nil {
				return err
			}

			log, err := idem.LoadLog(app.getenv, resolved.ProfileName, idemKey)
			if err != nil {
				return err
			}
			if err := log.VerifyIntegrity(); err != nil {
				return err
			}
			if log.APIBaseURL != resolved.APIBaseURL && !allowOriginChange {
				return clierr.Usage(
					"本地日志的 API 根地址（%s）与当前 Profile（%s）不一致；确认无误后加 --allow-api-origin-change",
					log.APIBaseURL, resolved.APIBaseURL)
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			outcome, err := idem.Recover(cmd.Context(), idem.NewImageRecoverClient(client), app.getenv, log, app.printer,
				idem.RecoverOptions{PollTimeout: resolved.TaskWaitTimeout})
			if err != nil {
				return err
			}
			result := recoverOutcomeToResult(outcome)

			if wait {
				timeout, terr := resolveWaitTimeout(resolved, waitTimeout)
				if terr != nil {
					return terr
				}
				taskID := extractTaskID(result.Task)
				if taskID == "" {
					return clierr.New(clierr.KindServer, "恢复结果缺少任务 ID，无法等待")
				}
				finalRaw, werr := task.Wait(cmd.Context(), client, taskID, timeout, app.printer, app.waitInterval)
				if werr != nil {
					return werr
				}
				result = &api.ImageTaskCreateResult{Task: finalRaw, Historical: result.Historical}
			}

			return printCreateResult(app, "image.resume", result, idemKey)
		},
	}
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "要恢复的 Idempotency-Key（必填）")
	c.Flags().BoolVar(&wait, "wait", false, "恢复后等待任务终态")
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	c.Flags().BoolVar(&allowOriginChange, "allow-api-origin-change", false, "允许日志中的 API 根地址与当前 Profile 不一致")
	_ = c.MarkFlagRequired("idempotency-key")
	return c
}
