package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const autoDetailImageKeyPrefix = "qjcli-detail-image-"

func newDetailImageCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{Use: "detail-image", Short: "电商详情图生成（一次提交出整套）"}
	c.AddCommand(newDetailImageCreateCommand(app), newDetailImageListCommand(app))
	return c
}

// newDetailImageCreateCommand submits a detail-image request. One submission
// produces a whole set of images, so the result is a messageId plus per-image
// items rather than a single task id.
func newDetailImageCreateCommand(app *appContext) *cobra.Command {
	var requestPath, idemKey string
	c := &cobra.Command{
		Use:   "create",
		Short: "创建详情图任务（主图 / 自动套餐 / 版式复刻）",
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
				key = autoDetailImageKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}

			// 先落本地日志再发 HTTP：结果未知时才有据可查
			log, err := prepareDetailImageRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest)
			if err != nil {
				return err
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			raw, err := client.CreateDetailImageTask(cmd.Context(), key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					// 服务端创建是幂等的：同 Key 同请求体重发会回放历史结果，不会创建第二套图。
					// 这里不自动重发，把决定权留给用户，但明确给出安全的重放命令。
					return clierr.New(clierr.KindTransport,
						"创建结果未知（"+err.Error()+"）。**不要换新 Key 重试**；"+
							"用同一个 Key 重放即可安全恢复：qianjue detail-image create --request <原文件> --idempotency-key "+key)
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

			return app.printer.Success("detail-image.create",
				map[string]any{"idempotencyKey": key, "result": raw}, app.meta, nil)
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	_ = c.MarkFlagRequired("request")
	return c
}

// newDetailImageListCommand polls detail-image results. Each message carries its
// images as items, each with its own status and URL, so this is the read path
// rather than `task get`.
func newDetailImageListCommand(app *appContext) *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "查询最近的详情图任务与每张图的状态、结果地址（轮询用它，最新在前）",
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
			raw, err := client.ListDetailImageTasks(cmd.Context(), limit)
			if err != nil {
				return err
			}
			return app.printer.Success("detail-image.list", raw, app.meta, nil)
		},
	}
	c.Flags().IntVar(&limit, "limit", 20, "返回最近多少条（1~200）")
	return c
}

func prepareDetailImageRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		log.Operation = idem.OperationDetailImageCreate
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
