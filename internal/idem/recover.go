package idem

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/backoff"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// RunbookPath is printed whenever the server reports RECOVERY_REQUIRED.
const RunbookPath = "docs/integration/admin-recovery-runbook.md"

// RecoverOutcome is the successful terminal result of a recovery run. Result
// is the raw document to surface: the replayed create result, or the bound
// task fetched on a SUCCEEDED status (Historical=true in that case).
type RecoverOutcome struct {
	Result     json.RawMessage
	Historical bool
	Status     *api.RequestStatus
}

// RecoverOptions tunes polling for tests.
type RecoverOptions struct {
	PollTimeout  time.Duration                   // 0 → 10m
	PollInterval func(attempt int) time.Duration // nil → backoff.Interval
}

// Recover implements the unknown-result procedure of cli-contract.md §16,
// always with the ORIGINAL key and ORIGINAL JSON, never generating a new key:
//
//  1. mark the local log RESULT_UNKNOWN
//  2. query GET /integration/image-tasks/idempotency
//  3. 404             → replay the original create once
//  4. PROCESSING      → backoff and re-query
//  5. SUCCEEDED       → fetch the bound task
//  6. FAILED_RETRYABLE→ wait until retryAfterAt, then replay original
//  7. FAILED_FINAL    → stop with FINAL_FAILURE, no new key
//  8. RECOVERY_REQUIRED → stop, report requestId + runbook path
func Recover(ctx context.Context, rc RecoverClient, getenv config.Getenv, log *RequestLog, p *output.Printer, opts RecoverOptions) (*RecoverOutcome, error) {
	pollTimeout := opts.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 10 * time.Minute
	}
	interval := opts.PollInterval
	if interval == nil {
		interval = backoff.Interval
	}

	log.SetState(StateResultUnknown)
	if err := WriteLog(getenv, log); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(pollTimeout)
	replayed := false
	var lastStatus *api.RequestStatus

	for attempt := 0; ; attempt++ {
		status, err := rc.Status(ctx, log.IdempotencyKey)
		if err != nil {
			ce := clierr.AsCLIError(err)
			if ce.Kind == clierr.KindNotFound {
				if replayed {
					return nil, clierr.Transport(fmt.Errorf(
						"服务端无该幂等请求且重放一次后仍未确认；请稍后重新执行 qianjue image resume --idempotency-key '%s'", log.IdempotencyKey))
				}
				replayed = true
				outcome, rerr := replayOriginal(ctx, rc, getenv, log, p)
				if rerr != nil {
					if clierr.AsCLIError(rerr).Kind == clierr.KindTransport {
						// 重放又遇未知结果：回到状态查询循环。
						p.Progressf("重放结果未知，继续查询幂等状态")
						goto wait
					}
					return nil, rerr
				}
				return outcome, nil
			}
			return nil, err
		}
		lastStatus = status

		switch status.Status {
		case api.IdemProcessing:
			p.Progressf("幂等请求处理中（attempt %d），退避后重查", attempt+1)
		case api.IdemSucceeded:
			return fetchSucceededTask(ctx, rc, getenv, log, status)
		case api.IdemFailedRetryable:
			if status.RetryAfterAt != nil && !status.RetryAfterAt.IsZero() {
				waitFor := time.Until(status.RetryAfterAt.Time)
				if waitFor > 0 {
					p.Progressf("等待 retryAfterAt（%s）后按原 Key/原 JSON 重试", status.RetryAfterAt.Format(time.RFC3339))
					select {
					case <-ctx.Done():
						return nil, clierr.Interrupted()
					case <-time.After(waitFor):
					}
				}
			}
			outcome, rerr := replayOriginal(ctx, rc, getenv, log, p)
			if rerr != nil {
				return nil, rerr
			}
			return outcome, nil
		case api.IdemFailedFinal:
			log.SetState(StateFailed)
			persistQuietly(getenv, log, p)
			return nil, statusError(clierr.KindFinalFailure, 2021,
				"幂等请求已进入 FAILED_FINAL，不会重新执行创建；请先处理原失败原因，再用新请求和新 Key", status)
		case api.IdemRecoveryRequired:
			log.SetState(StateRecoveryRequired)
			log.RequestID = status.RequestID
			persistQuietly(getenv, log, p)
			msg := fmt.Sprintf("创建结果不确定，请勿更换 Idempotency-Key；requestId=%s，按 %s 人工恢复",
				formatRequestID(status.RequestID), RunbookPath)
			return nil, statusError(clierr.KindRecoveryRequired, 2020, msg, status)
		default:
			return nil, clierr.New(clierr.KindServer, fmt.Sprintf("未知幂等状态 %q", status.Status))
		}

	wait:
		if time.Now().After(deadline) {
			return nil, statusError(clierr.KindWaitTimeout, 0,
				fmt.Sprintf("恢复查询超时；原 Key 仍有效，稍后重新执行 qianjue image resume --idempotency-key '%s'", log.IdempotencyKey), lastStatus)
		}
		select {
		case <-ctx.Done():
			return nil, clierr.Interrupted()
		case <-time.After(interval(attempt)):
		}
	}
}

// replayOriginal re-sends the persisted request with the original key.
func replayOriginal(ctx context.Context, rc RecoverClient, getenv config.Getenv, log *RequestLog, p *output.Printer) (*RecoverOutcome, error) {
	p.Progressf("使用原 Key/原 JSON 重放创建请求")
	log.MarkAttempt(time.Now())
	persistQuietly(getenv, log, p)

	result, taskID, err := rc.Replay(ctx, log.IdempotencyKey, log.RequestJSON)
	if err != nil {
		return nil, err
	}
	log.SetState(StateSucceeded)
	if taskID != "" {
		log.TaskID = &taskID
	}
	persistQuietly(getenv, log, p)
	return &RecoverOutcome{Result: result, Historical: false}, nil
}

// fetchSucceededTask resolves the bound resource for a SUCCEEDED request.
func fetchSucceededTask(ctx context.Context, rc RecoverClient, getenv config.Getenv, log *RequestLog, status *api.RequestStatus) (*RecoverOutcome, error) {
	if status.ResourceID == nil || *status.ResourceID == "" {
		return nil, statusError(clierr.KindRecoveryRequired, 2020,
			"幂等状态为 SUCCEEDED 但未绑定资源，按恢复流程人工确认（"+RunbookPath+"）", status)
	}
	taskRaw, err := rc.FetchTask(ctx, *status.ResourceID)
	if err != nil {
		return nil, err
	}
	log.SetState(StateSucceeded)
	log.TaskID = status.ResourceID
	log.RequestID = status.RequestID
	_ = WriteLog(getenv, log)
	return &RecoverOutcome{Result: taskRaw, Historical: true, Status: status}, nil
}

func statusError(kind clierr.Kind, code int, message string, status *api.RequestStatus) *clierr.CLIError {
	e := clierr.New(kind, message)
	e.Code = code
	if status != nil {
		if data, err := json.Marshal(status); err == nil {
			e.Details = data
		}
	}
	return e
}

func persistQuietly(getenv config.Getenv, log *RequestLog, p *output.Printer) {
	if err := WriteLog(getenv, log); err != nil {
		p.Progressf("警告：更新本地请求日志失败（%v）", err)
	}
}

func taskIDFromRaw(task json.RawMessage) string {
	var probe struct {
		ID json.Number `json:"id"`
	}
	if json.Unmarshal(task, &probe) != nil {
		return ""
	}
	return probe.ID.String()
}

func formatRequestID(id *int64) string {
	if id == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *id)
}
