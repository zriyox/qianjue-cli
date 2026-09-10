package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
	"github.com/zriyox/qianjue-cli/internal/task"
)

// batchItemKeySuffix builds the per-item Idempotency-Key. Deriving it from the
// batch key plus the item's index (rather than minting a fresh uuid per item)
// is what makes a whole batch safely replayable: re-running the same file with
// the same --idempotency-key returns the existing tasks instead of creating a
// second set and charging twice.
func batchItemKeySuffix(batchKey string, index int) string {
	return batchKey + "-" + strconv.Itoa(index)
}

func newImageBatchCommand(app *appContext) *cobra.Command {
	var requestPath, idemKey, waitTimeout string
	var wait, continueOnError bool
	c := &cobra.Command{
		Use:   "batch",
		Short: "批量提交图片任务（请求文件是 JSON 数组，一项一个任务）",
		Long: "把一个 JSON 数组里的每一项当作一次 image create 提交，逐个落本地幂等日志。\n\n" +
			"每项的 Idempotency-Key 由「批次 Key + 序号」推导，所以整批可以安全重放：\n" +
			"用同一个 --idempotency-key 再跑一次，已建的任务会原样返回，不会重复创建、不会重复扣费。\n" +
			"因此中途失败后直接重跑同一条命令即可续上，不要换新 Key。",
		Args: cobra.NoArgs,
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
			var items []json.RawMessage
			if err := json.Unmarshal(rawRequest, &items); err != nil {
				return clierr.Usage("批量请求文件必须是 JSON 数组（每项一个任务请求）：%v", err)
			}
			if len(items) == 0 {
				return clierr.Usage("批量请求数组为空")
			}
			for i, item := range items {
				if err := idem.CheckRequestObject(item); err != nil {
					return clierr.Usage("第 %d 项不是合法请求对象：%v", i+1, err)
				}
				if err := idem.CheckForbiddenFields(item); err != nil {
					return clierr.Usage("第 %d 项校验失败：%v", i+1, err)
				}
			}

			batchKey := idemKey
			if batchKey == "" {
				batchKey = "qjcli-image-batch-" + api.UUID4()
			} else if len(batchKey) > 100 {
				// 每项还要追加 "-<序号>"，给后缀留余量（服务端上限 128）
				return clierr.Usage("--idempotency-key 最多 100 个字符（每项会追加序号后缀）")
			}

			client, err := app.newAuthedClient(resolved)
			if err != nil {
				return err
			}

			type outcome struct {
				Index      int    `json:"index"`
				Key        string `json:"idempotencyKey"`
				TaskID     string `json:"taskId,omitempty"`
				Status     string `json:"status,omitempty"`
				Historical bool   `json:"historical,omitempty"`
				Error      string `json:"error,omitempty"`
			}
			results := make([]outcome, 0, len(items))
			taskIDs := make([]string, 0, len(items))
			failed := 0

			for i, item := range items {
				key := batchItemKeySuffix(batchKey, i+1)
				app.printer.Progressf("提交第 %d/%d 项（key=%s）", i+1, len(items), key)

				log, lerr := prepareRequestLog(app, resolved.ProfileName, resolved.APIBaseURL, key, item)
				if lerr != nil {
					results = append(results, outcome{Index: i + 1, Key: key, Error: lerr.Error()})
					failed++
					if !continueOnError {
						return summarizeBatch(app, batchKey, results, lerr)
					}
					continue
				}

				res, cerr := client.CreateImageTask(cmd.Context(), key, log.RequestJSON)
				if cerr != nil {
					// 传输类失败结果未知：绝不能换 Key 重来，重跑同一条命令即可安全续上。
					if clierr.AsCLIError(cerr).Kind == clierr.KindTransport {
						hint := clierr.New(clierr.KindTransport, fmt.Sprintf(
							"第 %d 项结果未知（%v）。**不要换新 Key**；重跑同一条命令即可安全续上："+
								"qianjue image batch --request %s --idempotency-key %s",
							i+1, cerr, requestPath, batchKey))
						results = append(results, outcome{Index: i + 1, Key: key, Error: cerr.Error()})
						return summarizeBatch(app, batchKey, results, hint)
					}
					log.SetState(idem.StateFailed)
					_ = idem.WriteLog(app.getenv, log)
					results = append(results, outcome{Index: i + 1, Key: key, Error: cerr.Error()})
					failed++
					if !continueOnError {
						return summarizeBatch(app, batchKey, results, cerr)
					}
					continue
				}

				log.SetState(idem.StateSucceeded)
				if werr := idem.WriteLog(app.getenv, log); werr != nil {
					app.printer.Progressf("警告：更新本地请求日志失败（%v）", werr)
				}
				id, status := imageCreateIdentity(res)
				results = append(results, outcome{
					Index: i + 1, Key: key, TaskID: id, Status: status, Historical: isHistorical(res),
				})
				if id != "" {
					taskIDs = append(taskIDs, id)
				}
			}

			if wait && len(taskIDs) > 0 {
				timeout, terr := resolveWaitTimeout(resolved, waitTimeout)
				if terr != nil {
					return terr
				}
				deadline := time.Now().Add(timeout)
				for _, id := range taskIDs {
					remaining := time.Until(deadline)
					if remaining <= 0 {
						app.printer.Progressf("批次等待超时，剩余任务仍在服务端执行（不会自动取消）")
						break
					}
					fetch := task.Fetch(func(fctx context.Context) (json.RawMessage, string, error) {
						return client.GetUnifiedTask(fctx, "image", id)
					})
					if _, werr := task.WaitFetch(cmd.Context(), fetch, "image/"+id,
						remaining, app.printer, app.waitInterval); werr != nil {
						// 单个任务失败/被拦截不该让整批中断：记下来继续等后面的
						app.printer.Progressf("任务 %s 未成功：%v", id, werr)
						failed++
					}
				}
			}

			rows := [][2]string{
				{"批次 Key", batchKey},
				{"提交", strconv.Itoa(len(results)-failed) + "/" + strconv.Itoa(len(items))},
			}
			if failed > 0 {
				rows = append(rows, [2]string{"失败", strconv.Itoa(failed)})
			}
			rows = append(rows, [2]string{"重放", "qianjue image batch --request " + requestPath +
				" --idempotency-key " + batchKey + "（安全，不会重复创建）"})
			return app.printer.Success("image.batch",
				map[string]any{"idempotencyKey": batchKey, "items": results}, app.meta, rows)
		},
	}
	c.Flags().StringVar(&requestPath, "request", "", "请求 JSON 数组文件路径；- 表示 stdin（必填）")
	c.Flags().StringVar(&idemKey, "idempotency-key", "", "批次 Key；省略时自动生成。重放整批就传同一个")
	c.Flags().BoolVar(&wait, "wait", false, "提交后逐个等待终态")
	c.Flags().StringVar(&waitTimeout, "wait-timeout", "", "整批等待超时（如 30m），覆盖配置")
	c.Flags().BoolVar(&continueOnError, "continue-on-error", false, "单项失败后继续提交剩余项")
	_ = c.MarkFlagRequired("request")
	return c
}

// summarizeBatch prints what already succeeded before returning the fatal error,
// so a half-done batch never leaves the caller guessing which items exist.
func summarizeBatch(app *appContext, batchKey string, results any, cause error) error {
	_ = app.printer.Success("image.batch.partial",
		map[string]any{"idempotencyKey": batchKey, "items": results}, app.meta, nil)
	return cause
}

func isHistorical(res any) bool {
	b, err := json.Marshal(res)
	if err != nil {
		return false
	}
	var probe struct {
		Historical bool `json:"historical"`
	}
	_ = json.Unmarshal(b, &probe)
	return probe.Historical
}

// imageCreateIdentity pulls (taskId, status) out of the create result.
func imageCreateIdentity(res any) (string, string) {
	b, err := json.Marshal(res)
	if err != nil {
		return "", ""
	}
	var probe struct {
		Task struct {
			ID     json.Number `json:"id"`
			Status string      `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		return "", ""
	}
	return strings.TrimSpace(probe.Task.ID.String()), probe.Task.Status
}
