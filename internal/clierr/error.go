// Package clierr defines the CLI error kinds, stable exit codes, and the
// mapping from backend business codes per docs/integration/cli-contract.md
// §23/§24. Exit code numbers are a long-term automation contract and must
// never be renumbered.
package clierr

import (
	"encoding/json"
	"fmt"
)

// Stable exit codes (cli-contract.md §23).
const (
	ExitOK                      = 0
	ExitUsage                   = 2
	ExitAuth                    = 3
	ExitForbidden               = 4
	ExitNotFound                = 5
	ExitIdempotencyConflict     = 6
	ExitInProgressOrWaitTimeout = 7
	ExitRecoveryRequired        = 8
	ExitFinalFailure            = 9
	ExitTaskFailed              = 10
	ExitTaskCancelled           = 11
	ExitTransport               = 12
	ExitServer                  = 13
	ExitLocalStorage            = 14
	// ExitModerationHold marks "blocked by content moderation, waiting on a human
	// decision". It is deliberately NOT ExitTaskFailed: the task is alive, credits
	// are still held, and retrying the same prompt would just be blocked again.
	// AI agents driving this CLI must stop and hand the decision back to the user.
	ExitModerationHold = 15
	// ExitIdentityRequired: the account must finish real-name verification on the
	// website before submitting. It is a personal, ID-document action the owner
	// performs there — an agent cannot do it here — so it gets its own code
	// instead of hiding inside a generic failure.
	ExitIdentityRequired = 16
	ExitInterrupted      = 130
)

// Kind identifies the error category exposed in JSON output (error.kind).
type Kind string

const (
	KindUsage               Kind = "USAGE"
	KindAuth                Kind = "AUTH"
	KindForbidden           Kind = "FORBIDDEN"
	KindNotFound            Kind = "NOT_FOUND"
	KindIdempotencyConflict Kind = "IDEMPOTENCY_CONFLICT"
	KindInProgress          Kind = "IN_PROGRESS"
	KindWaitTimeout         Kind = "WAIT_TIMEOUT"
	KindRecoveryRequired    Kind = "RECOVERY_REQUIRED"
	KindFinalFailure        Kind = "FINAL_FAILURE"
	KindInsufficientCredit  Kind = "INSUFFICIENT_CREDIT"
	KindInvalidRequest      Kind = "INVALID_REQUEST"
	KindTaskFailed          Kind = "TASK_FAILED"
	KindTaskCancelled       Kind = "TASK_CANCELLED"
	KindTransport           Kind = "TRANSPORT"
	KindServer              Kind = "SERVER"
	KindLocalStorage        Kind = "LOCAL_STORAGE"
	// KindModerationHold means the task was held by vision moderation and needs a
	// human decision (submit for review / confirm / cancel). Not a failure.
	KindModerationHold Kind = "MODERATION_HOLD"
	// KindIdentityRequired means the account must finish real-name verification
	// on the website before it can submit anything.
	KindIdentityRequired Kind = "IDENTITY_REQUIRED"
	KindInterrupted      Kind = "INTERRUPTED"
)

// exitByKind is the single source of truth for Kind → exit code.
var exitByKind = map[Kind]int{
	KindUsage:               ExitUsage,
	KindAuth:                ExitAuth,
	KindForbidden:           ExitForbidden,
	KindNotFound:            ExitNotFound,
	KindIdempotencyConflict: ExitIdempotencyConflict,
	KindInProgress:          ExitInProgressOrWaitTimeout,
	KindWaitTimeout:         ExitInProgressOrWaitTimeout,
	KindRecoveryRequired:    ExitRecoveryRequired,
	KindFinalFailure:        ExitFinalFailure,
	KindInsufficientCredit:  ExitFinalFailure,
	KindInvalidRequest:      ExitUsage,
	KindTaskFailed:          ExitTaskFailed,
	KindTaskCancelled:       ExitTaskCancelled,
	KindTransport:           ExitTransport,
	KindServer:              ExitServer,
	KindLocalStorage:        ExitLocalStorage,
	KindModerationHold:      ExitModerationHold,
	KindIdentityRequired:    ExitIdentityRequired,
	KindInterrupted:         ExitInterrupted,
}

// CLIError is the single error type surfaced by commands.
type CLIError struct {
	Kind       Kind
	ExitCode   int
	HTTPStatus int // 0 when the error did not come from an HTTP response
	Code       int // backend business code, 0 when absent
	Message    string
	Details    json.RawMessage // backend error data (e.g. IdempotencyState), passed through verbatim
}

func (e *CLIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("%s (code %d): %s", e.Kind, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// New builds a CLIError for the given kind with the canonical exit code.
func New(kind Kind, message string) *CLIError {
	return &CLIError{Kind: kind, ExitCode: exitByKind[kind], Message: message}
}

func Usage(format string, args ...any) *CLIError {
	return New(KindUsage, fmt.Sprintf(format, args...))
}

func Transport(err error) *CLIError {
	return New(KindTransport, err.Error())
}

func LocalStorage(format string, args ...any) *CLIError {
	return New(KindLocalStorage, fmt.Sprintf(format, args...))
}

func Interrupted() *CLIError {
	return New(KindInterrupted, "interrupted")
}

// kindByCode implements the fixed business-code table of cli-contract.md §24.
//
// 2014/2015/2016 are the real-name verification codes. They used to be absent
// here, so an account blocked by the identity gate only got a generic HTTP
// fallback with no hint about what to do — the user could not tell submission
// was blocked pending verification rather than broken. 2016 (verification
// required) therefore maps to its own kind carrying a link to the website.
var kindByCode = map[int]Kind{
	401:  KindAuth,
	2001: KindAuth,
	2002: KindAuth,
	2102: KindAuth,
	2103: KindAuth,
	2014: KindInvalidRequest, // 已完成实名，无需重复认证
	2015: KindInvalidRequest, // 实名发起过于频繁
	2016: KindIdentityRequired,
	2104: KindForbidden,
	2105: KindIdempotencyConflict,
	2106: KindInProgress,
	2107: KindRecoveryRequired,
	2108: KindFinalFailure,
	3007: KindInsufficientCredit,
	400:  KindInvalidRequest,
	4001: KindInvalidRequest,
	4002: KindInvalidRequest,
	404:  KindNotFound,
	2101: KindNotFound,
	3002: KindNotFound,
	500:  KindServer,
	501:  KindServer,
}

// FromAPI maps an HTTP status + backend business code to a CLIError.
// The business code table wins; unmapped codes fall back to the HTTP status.
func FromAPI(httpStatus, code int, message string, data json.RawMessage) *CLIError {
	kind, ok := kindByCode[code]
	if !ok {
		switch {
		case httpStatus == 401:
			kind = KindAuth
		case httpStatus == 403:
			kind = KindForbidden
		case httpStatus == 404:
			kind = KindNotFound
		case httpStatus >= 400 && httpStatus < 500:
			kind = KindInvalidRequest
		default:
			kind = KindServer
		}
	}
	return &CLIError{
		Kind:       kind,
		ExitCode:   exitByKind[kind],
		HTTPStatus: httpStatus,
		Code:       code,
		Message:    withActionableHint(kind, message),
		Details:    data,
	}
}

// withActionableHint appends "what to do next" to errors whose fix lives
// outside this CLI. The backend message states the fact ("请先完成实名认证")
// but not the remedy, and the remedy here is a personal action on the website
// that neither the CLI nor an agent driving it can perform.
func withActionableHint(kind Kind, message string) string {
	if kind != KindIdentityRequired {
		return message
	}
	if message == "" {
		message = "该账号需要先完成实名认证才能提交任务"
	}
	return message + "。运行 `qianjue identity verify` 按提示完成实名（终端会打印二维码，" +
		"用手机微信扫码做人脸核身），完成后重试即可。" +
		"如果你是 AI 助手：把这条命令交给用户自己执行 —— 实名要填身份证号并刷脸，" +
		"是本人行为，不要代填、不要绕过、不要改用其他账号。"
}

// AsCLIError normalizes any error into a *CLIError; unknown errors become
// SERVER-kind so every failure path still yields a stable exit code.
func AsCLIError(err error) *CLIError {
	if err == nil {
		return nil
	}
	if ce, ok := err.(*CLIError); ok {
		return ce
	}
	return New(KindServer, err.Error())
}
