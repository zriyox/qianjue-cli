package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
	"github.com/zriyox/qianjue-cli/internal/task"
)

const autoImageChatKeyPrefix = "qjcli-image-chat-"

func newImageChatCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{
		Use:   "image-chat",
		Short: "对话生图（用提示词加参考图直接出图）",
		Long: "对话生图任务。查询与等待走统一任务接口：\n" +
			"  qianjue task get  image-chat <taskId>\n" +
			"  qianjue task wait image-chat <taskId>",
	}
	c.AddCommand(newImageChatCreateCommand(app))
	return c
}

// newImageChatCreateCommand submits one image-chat task. Unlike detail-image
// (one message -> many items) this is a plain single task, so the result is a
// task id and everything downstream reuses the unified task endpoints.
func newImageChatCreateCommand(app *appContext) *cobra.Command {
	var requestPath, idemKey, waitTimeout string
	var wait bool
	c := &cobra.Command{
		Use:   "create",
		Short: "创建对话生图任务",
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
				key = autoImageChatKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}

			// 先落本地日志再发 HTTP：结果未知时才有据可查
			log, err := prepareImageChatRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest)
			if err != nil {
				return err
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			raw, err := client.CreateImageChatTask(cmd.Context(), key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					// 服务端创建是幂等的：同 Key 同请求体重发会回放历史结果，不会重复扣费。
					// 不自动重发，把决定权留给用户，但给出安全的重放命令。
					return clierr.New(clierr.KindTransport,
						"创建结果未知（"+err.Error()+"）。**不要换新 Key 重试**；"+
							"用同一个 Key 重放即可安全恢复：qianjue image-chat create --request <原文件> --idempotency-key "+key)
				}
				log.SetState(idem.StateFailed)
				if werr := idem.WriteLog(app.getenv, log); werr != nil {
					app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
				}
				return err
			}

			log.SetState(idem.StateSucceeded)
			if werr := idem.WriteLog(app.getenv, log); werr != nil {
				app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
			}

			payload := map[string]any{"idempotencyKey": key, "result": raw}
			if !wait {
				return app.printer.Success("image-chat.create", payload, app.meta, imageChatRows(raw))
			}

			taskID := imageChatTaskID(raw)
			if taskID == "" {
				return app.printer.Success("image-chat.create", payload, app.meta, imageChatRows(raw))
			}
			timeout, err := resolveWaitTimeout(resolved, waitTimeout)
			if err != nil {
				return err
			}
			fetch := task.Fetch(func(fctx context.Context) (json.RawMessage, string, error) {
				return client.GetUnifiedTask(fctx, "image-chat", taskID)
			})
			final, err := task.WaitFetch(cmd.Context(), fetch, "image-chat/"+taskID, timeout, app.printer, app.waitInterval)
			if err != nil {
				return err
			}
			return app.printer.Success("image-chat.create", final, app.meta, unifiedTableRows(final))
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	c.Flags().BoolVar(&wait, "wait", false, "创建后等待任务终态")
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 10m），覆盖配置")
	_ = c.MarkFlagRequired("request")
	return c
}

// imageChatTaskID digs the created task id out of the create response so --wait
// can hand it straight to the unified read path.
func imageChatTaskID(raw json.RawMessage) string {
	// 这里的 raw 是 R 的 data，即 ImageChatCreateResult{result, historical}，
	// 任务体就在 result 一层（实测确认；printer 之后还会再包一层 idempotencyKey，别混淆）。
	var probe struct {
		Result struct {
			ID json.Number `json:"id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Result.ID.String()
}

func imageChatRows(raw json.RawMessage) [][2]string {
	var probe struct {
		Historical bool `json:"historical"`
		Result     struct {
			ID     json.Number `json:"id"`
			Status string      `json:"status"`
		} `json:"result"`
	}
	_ = json.Unmarshal(raw, &probe)
	id := probe.Result.ID.String()
	rows := [][2]string{{"Task ID", id}}
	if probe.Result.Status != "" {
		rows = append(rows, [2]string{"Status", probe.Result.Status})
	}
	if probe.Historical {
		rows = append(rows, [2]string{"幂等命中", "是（返回历史结果，未重复创建）"})
	}
	rows = append(rows, [2]string{"下一步", "qianjue task wait image-chat " + id})
	return rows
}

func prepareImageChatRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		log.Operation = idem.OperationImageChatCreate
		if werr := idem.WriteLog(app.getenv, log); werr != nil {
			return nil, werr
		}
		return log, nil
	}

	digest, err := idem.Digest(rawRequest)
	if err != nil {
		return nil, err
	}
	if existing.RequestDigest != digest {
		return nil, clierr.New(clierr.KindIdempotencyConflict,
			"同一个 Idempotency-Key 已用于不同的请求体，换一个 Key 或改回原请求体")
	}
	return existing, nil
}
