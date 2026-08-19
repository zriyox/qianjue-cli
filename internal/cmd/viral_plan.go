package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const autoViralPlanKeyPrefix = "qjcli-viral-plan-"

// newViralPlanCommand produces a finished viral script in one call. The web
// product is a multi-turn conversation where the user picks among drafts; that
// interaction has no place on a command line, so this exposes the one-shot path
// and leaves the conversational experience to the web client.
func newViralPlanCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{Use: "viral-plan", Short: "爆款视频策划（上传商品图，一次拿完整脚本）"}
	c.AddCommand(newViralPlanCreateCommand(app))
	return c
}

func newViralPlanCreateCommand(app *appContext) *cobra.Command {
	var requestPath, idemKey string
	c := &cobra.Command{
		Use:   "create",
		Short: "生成爆款脚本（会扣积分）",
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
				key = autoViralPlanKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}

			log, err := prepareViralPlanRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest)
			if err != nil {
				return err
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			raw, err := client.CreateViralPlanScript(cmd.Context(), key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					return clierr.New(clierr.KindTransport,
						"生成结果未知（"+err.Error()+"）。**不要换新 Key 重试**（会重复扣费）；"+
							"用同一个 Key 重放即可安全恢复：qianjue viral-plan create --request <原文件> --idempotency-key "+key)
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
			return app.printer.Success("viral-plan.create",
				map[string]any{"idempotencyKey": key, "result": raw}, app.meta, nil)
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	_ = c.MarkFlagRequired("request")
	return c
}

func prepareViralPlanRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		log.Operation = idem.OperationViralPlanCreate
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
