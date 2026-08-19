package cmd

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const autoReversePromptKeyPrefix = "qjcli-reverse-prompt-"

// newReversePromptCommand exposes the parse half of talking-head video
// generation: a video goes in, shot breakdown and script come out. Producing the
// video itself stays on `video create` with sourceType VIDEO_REVERSE_PROMPT.
func newReversePromptCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{Use: "reverse-prompt", Short: "把视频解析成分镜与口播脚本（口播视频生成第一步）"}
	c.AddCommand(newReversePromptCreateCommand(app), newReversePromptGetCommand(app))
	return c
}

func newReversePromptCreateCommand(app *appContext) *cobra.Command {
	var requestPath, videoURL, idemKey string
	c := &cobra.Command{
		Use:   "create",
		Short: "创建解析会话（异步；返回 sessionId）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			if err := resolved.RequireAPIBaseURL(); err != nil {
				return err
			}

			var rawRequest []byte
			switch {
			case requestPath != "" && videoURL != "":
				return clierr.Usage("--request 与 --video-url 只能给一个")
			case requestPath != "":
				rawRequest, err = readRequestDocument(app, requestPath)
				if err != nil {
					return err
				}
			case videoURL != "":
				// 只解析一条视频是最常见的用法，不必为它写一个 JSON 文件
				rawRequest, err = json.Marshal(map[string]string{"videoUrl": videoURL})
				if err != nil {
					return err
				}
			default:
				return clierr.Usage("必须给出 --video-url 或 --request")
			}

			if err := idem.CheckRequestObject(rawRequest); err != nil {
				return err
			}
			if err := idem.CheckForbiddenFields(rawRequest); err != nil {
				return err
			}

			key := idemKey
			if key == "" {
				key = autoReversePromptKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}

			log, err := prepareReversePromptRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest)
			if err != nil {
				return err
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			raw, err := client.CreateReversePromptSession(cmd.Context(), key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					return clierr.New(clierr.KindTransport,
						"创建结果未知（"+err.Error()+"）。**不要换新 Key 重试**；"+
							"用同一个 Key 重放即可安全恢复：qianjue reverse-prompt create --idempotency-key "+key)
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
			return app.printer.Success("reverse-prompt.create",
				map[string]any{"idempotencyKey": key, "result": raw}, app.meta, nil)
		},
	}
	c.Flags().StringVar(&videoURL, "video-url", "", "待解析视频的公网地址（简单用法）")
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（需要参考图/音频等完整参数时用）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	return c
}

func newReversePromptGetCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <sessionId>",
		Short: "查询解析会话（状态与结构化脚本）",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || sessionID <= 0 {
				return clierr.Usage("会话 ID 必须是正整数")
			}
			resolved, err := app.resolveConfig()
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			raw, err := client.GetReversePromptSession(cmd.Context(), sessionID)
			if err != nil {
				return err
			}
			return app.printer.Success("reverse-prompt.get", raw, app.meta, nil)
		},
	}
}

func prepareReversePromptRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		log.Operation = idem.OperationReversePromptCreate
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
