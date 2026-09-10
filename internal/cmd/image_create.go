package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
	"github.com/zriyox/qianjue-cli/internal/task"
)

// autoKeyPrefix is the fixed prefix for generated keys (cli-contract.md §14).
const autoKeyPrefix = "qjcli-image-"

func newImageCommand(app *appContext) *cobra.Command {
	image := &cobra.Command{Use: "image", Short: "Integration 图片任务"}
	image.AddCommand(
		newImageCreateCommand(app),
		newImageBatchCommand(app),
		newImageGetCommand(app),
		newImageWaitCommand(app),
		newImageCancelCommand(app),
		newImageRequestStatusCommand(app),
		newImageResumeCommand(app),
	)
	return image
}

func newImageCreateCommand(app *appContext) *cobra.Command {
	var requestPath, idemKey, waitTimeout string
	var wait bool
	c := &cobra.Command{
		Use:   "create",
		Short: "创建图片任务（Idempotency-Key 在首个 HTTP 请求前持久化）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			if err := resolved.RequireAPIBaseURL(); err != nil {
				return err
			}

			rawRequest, err := readRequestDocument(app, requestPath)
			if err != nil {
				return err
			}
			if err := idem.CheckRequestShape(rawRequest); err != nil {
				return err
			}
			if err := idem.CheckForbiddenFields(rawRequest); err != nil {
				return err
			}

			key := idemKey
			if key == "" {
				key = autoKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}

			log, err := prepareRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest)
			if err != nil {
				return err
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			result, err := client.CreateImageTask(cmd.Context(), key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					// 未知结果：原 Key/原 JSON 自动恢复（cli-contract.md §16）。
					app.printer.Progressf("创建结果未知（%v），进入恢复流程", err)
					outcome, rerr := idem.Recover(cmd.Context(), idem.NewImageRecoverClient(client), app.getenv, log, app.printer,
						idem.RecoverOptions{PollTimeout: resolved.TaskWaitTimeout})
					if rerr != nil {
						return rerr
					}
					result = recoverOutcomeToResult(outcome)
				} else {
					return handleCreateFailure(app, log, err)
				}
			} else {
				log.SetState(idem.StateSucceeded)
				if taskID := extractTaskID(result.Task); taskID != "" {
					log.TaskID = &taskID
				}
				if werr := idem.WriteLog(app.getenv, log); werr != nil {
					// 任务已创建成功，本地日志写失败只提示，不改变命令结果。
					app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
				}
			}

			if wait {
				timeout, terr := resolveWaitTimeout(resolved, waitTimeout)
				if terr != nil {
					return terr
				}
				taskID := extractTaskID(result.Task)
				if taskID == "" {
					return clierr.New(clierr.KindServer, "创建响应缺少任务 ID，无法等待")
				}
				finalRaw, werr := task.Wait(cmd.Context(), client, taskID, timeout, app.printer, app.waitInterval)
				if werr != nil {
					return werr
				}
				result = &api.ImageTaskCreateResult{Task: finalRaw, Historical: result.Historical}
			}

			return printCreateResult(app, "image.create", result, key)
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	c.Flags().BoolVar(&wait, "wait", false, "创建后等待任务终态")
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	_ = c.MarkFlagRequired("request")
	return c
}

// recoverOutcomeToResult converts a recovery outcome into the image create
// shape; the recovery layer surfaces the raw task document either way.
func recoverOutcomeToResult(outcome *idem.RecoverOutcome) *api.ImageTaskCreateResult {
	return &api.ImageTaskCreateResult{Task: outcome.Result, Historical: outcome.Historical}
}

// readRequestDocument loads the request JSON from a file or stdin.
func readRequestDocument(app *appContext, path string) ([]byte, error) {
	if path == "-" {
		raw, err := io.ReadAll(io.LimitReader(app.stdin, idem.MaxRequestBytes+1))
		if err != nil {
			return nil, clierr.Usage("读取 stdin 请求失败: %v", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, clierr.Usage("读取请求文件失败: %v", err)
	}
	return raw, nil
}

// prepareRequestLog persists the SUBMITTING record before any HTTP traffic.
// An existing log for the same key must carry the same digest — a different
// request document is rejected locally with the idempotency-conflict exit
// code, mirroring the server-side 2105 semantics.
func prepareRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		if werr := idem.WriteLog(app.getenv, log); werr != nil {
			return nil, werr
		}
		return log, nil
	}

	digest, err := idem.Digest(rawRequest)
	if err != nil {
		return nil, err
	}
	if digest != existing.RequestDigest {
		return nil, &clierr.CLIError{
			Kind:     clierr.KindIdempotencyConflict,
			ExitCode: clierr.ExitIdempotencyConflict,
			Message: fmt.Sprintf(
				"本地已存在 Key %s 的请求日志且请求 JSON 不同；同一 Key 必须复用原 JSON（原 digest %s）",
				key, existing.RequestDigest),
		}
	}
	if err := existing.VerifyIntegrity(); err != nil {
		return nil, err
	}
	existing.MarkAttempt(time.Now())
	if werr := idem.WriteLog(app.getenv, existing); werr != nil {
		return nil, werr
	}
	return existing, nil
}

// handleCreateFailure persists the local state transition for a failed
// create, then returns the error for envelope rendering. Transport errors are
// handled by the caller via idem.Recover.
func handleCreateFailure(app *appContext, log *idem.RequestLog, err error) error {
	ce := clierr.AsCLIError(err)
	switch {
	case ce.Code == 2107:
		log.SetState(idem.StateRecoveryRequired)
		captureRequestID(log, ce.Details)
		app.printer.Progressf("结果不确定（RECOVERY_REQUIRED）；记录 requestId 并按 docs/integration/admin-recovery-runbook.md 处理，不要更换 Key")
	case ce.Code == 2106:
		log.SetState(idem.StateResultUnknown)
		captureRequestID(log, ce.Details)
		app.printer.Progressf("原请求仍在处理中；稍后用 qianjue image resume --idempotency-key '%s' 查询，不要更换 Key", log.IdempotencyKey)
	case ce.Code == 2105 || ce.Code == 2108 || ce.Kind == clierr.KindInvalidRequest || ce.Kind == clierr.KindInsufficientCredit:
		log.SetState(idem.StateFailed)
	default:
		// 认证/权限等错误发生在业务执行前，保持现有状态。
		return err
	}
	if werr := idem.WriteLog(app.getenv, log); werr != nil {
		app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
	}
	return err
}

func captureRequestID(log *idem.RequestLog, details json.RawMessage) {
	if len(details) == 0 {
		return
	}
	var probe struct {
		RequestID *int64 `json:"requestId"`
	}
	if json.Unmarshal(details, &probe) == nil && probe.RequestID != nil {
		log.RequestID = probe.RequestID
	}
}

// extractTaskID pulls the task id out of the raw task document without
// losing int64 precision.
func extractTaskID(task json.RawMessage) string {
	var probe struct {
		ID json.Number `json:"id"`
	}
	if json.Unmarshal(task, &probe) != nil {
		return ""
	}
	return strings.TrimSpace(probe.ID.String())
}

// printCreateResult renders the create envelope: backend fields passed
// through, plus the CLI-added idempotencyKey (cli-contract.md §14.1).
func printCreateResult(app *appContext, command string, result *api.ImageTaskCreateResult, key string) error {
	data := map[string]any{
		"task":           result.Task,
		"historical":     result.Historical,
		"idempotencyKey": key,
	}
	meta := app.meta
	if meta == nil {
		meta = map[string]any{}
	}
	meta["httpStatus"] = 200

	taskID := extractTaskID(result.Task)
	var status string
	var probe struct {
		Status   string `json:"status"`
		TaskType string `json:"taskType"`
	}
	_ = json.Unmarshal(result.Task, &probe)
	status = probe.Status
	return app.printer.Success(command, data, meta, [][2]string{
		{"Task ID", taskID},
		{"Status", status},
		{"Type", probe.TaskType},
		{"Historical", boolString(result.Historical)},
		{"Idempotency-Key", key},
	})
}
