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

const autoTvcAdsKeyPrefix = "qjcli-tvc-ads-"

func newTvcAdsCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{
		Use:   "tvc-ads",
		Short: "营销视频生成（TVC 高级广告，产品图一路到成片）",
		Long: "营销视频是聚合任务，分镜与视频分阶段产出。\n" +
			"统一任务接口读不到它，查询与等待用本命令组的 get / wait。",
	}
	c.AddCommand(newTvcAdsCreateCommand(app), newTvcAdsGetCommand(app), newTvcAdsWaitCommand(app))
	return c
}

func newTvcAdsCreateCommand(app *appContext) *cobra.Command {
	var requestPath, idemKey string
	c := &cobra.Command{
		Use:   "create",
		Short: "创建营销视频任务（必填 productImageOosKey）",
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
				key = autoTvcAdsKeyPrefix + api.UUID4()
			} else if len(key) > 128 {
				return clierr.Usage("--idempotency-key 最多 128 个字符")
			}
			log, err := prepareTvcAdsRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, rawRequest)
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			raw, err := client.CreateTvcAdsTask(cmd.Context(), key, log.RequestJSON)
			if err != nil {
				if clierr.AsCLIError(err).Kind == clierr.KindTransport {
					return clierr.New(clierr.KindTransport,
						"创建结果未知（"+err.Error()+"）。**不要换新 Key 重试**；"+
							"用同一个 Key 重放即可安全恢复：qianjue tvc-ads create --request <原文件> --idempotency-key "+key)
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
			return app.printer.Success("tvc-ads.create",
				map[string]any{"idempotencyKey": key, "result": raw}, app.meta, tvcAdsCreateRows(raw))
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "显式 Idempotency-Key；省略时自动生成")
	_ = c.MarkFlagRequired("request")
	return c
}

func newTvcAdsGetCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "get <taskId>",
		Short: "查询营销视频任务（含分镜与视频各阶段状态）",
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
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			raw, _, err := client.GetTvcAdsTask(cmd.Context(), taskID)
			if err != nil {
				return err
			}
			return app.printer.Success("tvc-ads.get", raw, app.meta, tvcAdsRows(raw))
		},
	}
}

func newTvcAdsWaitCommand(app *appContext) *cobra.Command {
	var waitTimeout string
	c := &cobra.Command{
		Use:   "wait <taskId>",
		Short: "等待营销视频任务终态（Ctrl-C 只停本地等待）",
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
			timeout, err := resolveWaitTimeout(resolved, waitTimeout)
			if err != nil {
				return err
			}
			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}
			fetch := task.Fetch(func(fctx context.Context) (json.RawMessage, string, error) {
				return client.GetTvcAdsTask(fctx, taskID)
			})
			raw, err := task.WaitFetch(cmd.Context(), fetch, "tvc-ads/"+taskID, timeout, app.printer, app.waitInterval)
			if err != nil {
				return err
			}
			return app.printer.Success("tvc-ads.wait", raw, app.meta, tvcAdsRows(raw))
		},
	}
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "任务等待超时（如 30m），覆盖配置")
	return c
}

func tvcAdsCreateRows(raw json.RawMessage) [][2]string {
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
	rows = append(rows, [2]string{"下一步", "qianjue tvc-ads wait " + id})
	return rows
}

func tvcAdsRows(raw json.RawMessage) [][2]string {
	var probe struct {
		ID       json.Number `json:"id"`
		Status   string      `json:"status"`
		Stage    string      `json:"stage"`
		VideoURL string      `json:"videoUrl"`
	}
	_ = json.Unmarshal(raw, &probe)
	rows := [][2]string{
		{"Task ID", probe.ID.String()},
		{"Status", probe.Status},
	}
	if probe.Stage != "" {
		rows = append(rows, [2]string{"当前阶段", probe.Stage})
	}
	if probe.VideoURL != "" {
		rows = append(rows, [2]string{"成片", probe.VideoURL})
	}
	return rows
}

func prepareTvcAdsRequestLog(app *appContext, profile, apiBaseURL, key string, rawRequest []byte) (*idem.RequestLog, error) {
	existing, err := idem.LoadLog(app.getenv, profile, key)
	if err != nil {
		if clierr.AsCLIError(err).Kind != clierr.KindNotFound {
			return nil, err
		}
		log, nerr := idem.NewRequestLog(profile, apiBaseURL, key, rawRequest, time.Now())
		if nerr != nil {
			return nil, nerr
		}
		log.Operation = idem.OperationTvcAdsCreate
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
