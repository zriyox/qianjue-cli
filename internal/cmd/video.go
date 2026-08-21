package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
	"github.com/zriyox/qianjue-cli/internal/task"
)

// autoVideoKeyPrefix is the fixed prefix for generated video keys.
const autoVideoKeyPrefix = "qjcli-video-"

func newVideoCommand(app *appContext) *cobra.Command {
	video := &cobra.Command{Use: "video", Short: "Integration 视频任务"}
	video.AddCommand(
		newVideoCreateCommand(app, api.VideoKindCreate, "create", "创建视频任务（普通/参考视频，批量）"),
		newVideoCreateCommand(app, api.VideoKindEdit, "edit", "创建视频编辑任务"),
		newVideoCreateCommand(app, api.VideoKindUpscale, "upscale", "创建视频高清放大任务"),
		newVideoCreateCommand(app, api.VideoKindGestureReplica, "gesture-replica", "创建手势舞一键复刻任务"),
		newVideoResumeCommand(app),
		newVideoRequestStatusCommand(app),
	)
	return video
}

func newVideoCreateCommand(app *appContext, kind api.VideoKind, use, short string) *cobra.Command {
	var requestPath, idemKey, waitTimeout string
	var wait bool
	c := &cobra.Command{
		Use:   use,
		Short: short,
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
			if err := idem.CheckRequestObject(rawRequest); err != nil {
				return err
			}
			if err := idem.CheckForbiddenFields(rawRequest); err != nil {
				return err
			}

			key := idemKey
			if key == "" {
				key = autoVideoKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}

			log, err := prepareVideoRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest, kind)
			if err != nil {
				return err
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			raw, err := client.CreateVideoTask(cmd.Context(), kind, key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					app.printer.Progressf("创建结果未知（%v），进入恢复流程", err)
					outcome, rerr := idem.Recover(cmd.Context(), idem.NewVideoRecoverClient(client, kind),
						app.getenv, log, app.printer, idem.RecoverOptions{PollTimeout: resolved.TaskWaitTimeout})
					if rerr != nil {
						return rerr
					}
					raw = outcome.Result
				} else {
					return handleVideoCreateFailure(app, log, err)
				}
			}
			log.SetState(idem.StateSucceeded)
			if taskID := api.VideoResultTaskID(raw); taskID != "" {
				log.TaskID = &taskID
			}
			if werr := idem.WriteLog(app.getenv, log); werr != nil {
				app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
			}

			if wait {
				timeout, terr := resolveWaitTimeout(resolved, waitTimeout)
				if terr != nil {
					return terr
				}
				taskID := api.VideoResultTaskID(raw)
				if taskID == "" {
					return clierr.New(clierr.KindServer, "视频创建响应缺少任务 ID，无法等待")
				}
				fetch := task.Fetch(func(fctx context.Context) (json.RawMessage, string, error) {
					return client.GetUnifiedTask(fctx, "video", taskID)
				})
				finalRaw, werr := task.WaitFetch(cmd.Context(), fetch, "video/"+taskID, timeout, app.printer, app.waitInterval)
				if werr != nil {
					return werr
				}
				return printVideoResult(app, "video."+use, finalRaw, key, true)
			}
			return printVideoResult(app, "video."+use, raw, key, false)
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	c.Flags().BoolVar(&wait, "wait", false, "创建后等待任务终态")
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	_ = c.MarkFlagRequired("request")
	return c
}

func newVideoResumeCommand(app *appContext) *cobra.Command {
	var idemKey, waitTimeout string
	var wait, allowOriginChange bool
	c := &cobra.Command{
		Use:   "resume",
		Short: "用本地请求日志按原 Key/原 JSON 恢复未知结果的视频创建请求",
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
			if log.Operation != idem.OperationVideoCreate {
				return clierr.Usage("该 Idempotency-Key 的本地日志不是视频创建请求（operation=%s）", log.Operation)
			}
			if err := log.VerifyIntegrity(); err != nil {
				return err
			}
			if log.APIBaseURL != resolved.APIBaseURL && !allowOriginChange {
				return clierr.Usage(
					"本地日志的 API 根地址（%s）与当前 Profile（%s）不一致；确认无误后加 --allow-api-origin-change",
					log.APIBaseURL, resolved.APIBaseURL)
			}
			kind := api.VideoKind(log.Kind)
			if kind == "" {
				kind = api.VideoKindCreate
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			outcome, err := idem.Recover(cmd.Context(), idem.NewVideoRecoverClient(client, kind),
				app.getenv, log, app.printer, idem.RecoverOptions{PollTimeout: resolved.TaskWaitTimeout})
			if err != nil {
				return err
			}
			raw := outcome.Result

			if wait {
				timeout, terr := resolveWaitTimeout(resolved, waitTimeout)
				if terr != nil {
					return terr
				}
				taskID := api.VideoResultTaskID(raw)
				if taskID == "" {
					return clierr.New(clierr.KindServer, "恢复结果缺少任务 ID，无法等待")
				}
				fetch := task.Fetch(func(fctx context.Context) (json.RawMessage, string, error) {
					return client.GetUnifiedTask(fctx, "video", taskID)
				})
				finalRaw, werr := task.WaitFetch(cmd.Context(), fetch, "video/"+taskID, timeout, app.printer, app.waitInterval)
				if werr != nil {
					return werr
				}
				raw = finalRaw
			}
			return printVideoResult(app, "video.resume", raw, idemKey, outcome.Historical)
		},
	}
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "要恢复的 Idempotency-Key（必填）")
	c.Flags().BoolVar(&wait, "wait", false, "恢复后等待任务终态")
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	c.Flags().BoolVar(&allowOriginChange, "allow-api-origin-change", false, "允许日志中的 API 根地址与当前 Profile 不一致")
	_ = c.MarkFlagRequired("idempotency-key")
	return c
}

// prepareVideoRequestLog persists the SUBMITTING record (operation VIDEO_CREATE)
// before any HTTP traffic. An existing log for the same key must carry the same
// digest, else it is rejected locally (exit 6), mirroring server-side 2105.
func prepareVideoRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte, kind api.VideoKind) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		log.Operation = idem.OperationVideoCreate
		log.Kind = string(kind)
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

// handleVideoCreateFailure persists the local state transition for a failed
// video create (transport-unknown is handled by the caller via idem.Recover).
func handleVideoCreateFailure(app *appContext, log *idem.RequestLog, err error) error {
	ce := clierr.AsCLIError(err)
	switch {
	case ce.Code == 2107:
		log.SetState(idem.StateRecoveryRequired)
		captureRequestID(log, ce.Details)
		app.printer.Progressf("结果不确定（RECOVERY_REQUIRED）；记录 requestId 并按 docs/integration/admin-recovery-runbook.md 处理，不要更换 Key")
	case ce.Code == 2106:
		log.SetState(idem.StateResultUnknown)
		captureRequestID(log, ce.Details)
		app.printer.Progressf("原请求仍在处理中；稍后以原 Key 查询，不要更换 Key")
	case ce.Code == 2105 || ce.Code == 2108 || ce.Kind == clierr.KindInvalidRequest || ce.Kind == clierr.KindInsufficientCredit:
		log.SetState(idem.StateFailed)
	default:
		return err
	}
	if werr := idem.WriteLog(app.getenv, log); werr != nil {
		app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
	}
	return err
}

// printVideoResult renders the video create envelope: backend result passed
// through verbatim, plus the CLI-added idempotencyKey.
func printVideoResult(app *appContext, command string, result json.RawMessage, key string, historical bool) error {
	data := map[string]any{
		"result":         result,
		"historical":     historical,
		"idempotencyKey": key,
	}
	meta := app.meta
	if meta == nil {
		meta = map[string]any{}
	}
	meta["httpStatus"] = 200

	var probe struct {
		BatchID json.Number   `json:"batchId"`
		TaskID  json.Number   `json:"taskId"`
		TaskIDs []json.Number `json:"taskIds"`
		Status  string        `json:"status"`
	}
	_ = json.Unmarshal(result, &probe)
	rows := [][2]string{
		{"Idempotency-Key", key},
		{"Status", probe.Status},
	}
	if probe.BatchID.String() != "" {
		rows = append(rows, [2]string{"Batch ID", probe.BatchID.String()})
	}
	if probe.TaskID.String() != "" {
		rows = append(rows, [2]string{"Task ID", probe.TaskID.String()})
	}
	if len(probe.TaskIDs) > 0 {
		rows = append(rows, [2]string{"Task count", fmt.Sprintf("%d", len(probe.TaskIDs))})
	}
	return app.printer.Success(command, data, meta, rows)
}
