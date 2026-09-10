package idem

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
)

// State is the local lifecycle of one create request (cli-contract.md §15/§16).
type State string

const (
	StateSubmitting       State = "SUBMITTING"
	StateResultUnknown    State = "RESULT_UNKNOWN"
	StateSucceeded        State = "SUCCEEDED"
	StateFailed           State = "FAILED"
	StateRecoveryRequired State = "RECOVERY_REQUIRED"
)

// Local request-log operations.
const (
	OperationImageCreate         = "IMAGE_CREATE"
	OperationVideoCreate         = "VIDEO_CREATE"
	OperationDetailImageCreate   = "DETAIL_IMAGE_CREATE"
	OperationReversePromptCreate = "REVERSE_PROMPT_CREATE"
	OperationViralPlanCreate     = "VIRAL_PLAN_CREATE"
	OperationImageChatCreate     = "IMAGE_CHAT_CREATE"
	OperationTvcAdsCreate        = "TVC_ADS_CREATE"
)

// RequestLog is the on-disk pre-flight record, written BEFORE the first HTTP
// attempt. Field names match cli-contract.md §15 exactly. It never contains
// tokens (CheckForbiddenFields runs before every write).
type RequestLog struct {
	SchemaVersion  string          `json:"schemaVersion"`
	Profile        string          `json:"profile"`
	APIBaseURL     string          `json:"apiBaseUrl"`
	Operation      string          `json:"operation"`
	IdempotencyKey string          `json:"idempotencyKey"`
	RequestDigest  string          `json:"requestDigest"`
	RequestJSON    json.RawMessage `json:"requestJson"`
	CreatedAt      time.Time       `json:"createdAt"`
	LastAttemptAt  time.Time       `json:"lastAttemptAt"`
	AttemptCount   int             `json:"attemptCount"`
	RequestID      *int64          `json:"requestId"`
	TaskID         *string         `json:"taskId"`
	State          State           `json:"state"`
	// Kind is the sub-resource discriminator for domains with multiple create
	// endpoints (video: create/edit/upscale/gesture-replica). Empty for image.
	Kind string `json:"kind,omitempty"`
}

// NewRequestLog builds the initial SUBMITTING record.
func NewRequestLog(profile, apiBaseURL, key string, requestJSON []byte, now time.Time) (*RequestLog, error) {
	digest, err := Digest(requestJSON)
	if err != nil {
		return nil, err
	}
	return &RequestLog{
		SchemaVersion:  "1",
		Profile:        profile,
		APIBaseURL:     apiBaseURL,
		Operation:      OperationImageCreate,
		IdempotencyKey: key,
		RequestDigest:  digest,
		RequestJSON:    json.RawMessage(requestJSON),
		CreatedAt:      now,
		LastAttemptAt:  now,
		AttemptCount:   1,
		State:          StateSubmitting,
	}, nil
}

// LogPath returns <state>/requests/<profile>/<sha256(key)>.json. Hashing the
// key keeps arbitrary key content path-safe.
func LogPath(getenv config.Getenv, profile, key string) (string, error) {
	dir, err := config.RequestsDir(getenv, profile)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json"), nil
}

// WriteLog persists the record atomically with 0600 permissions.
func WriteLog(getenv config.Getenv, l *RequestLog) error {
	if err := CheckForbiddenFields(l.RequestJSON); err != nil {
		return err
	}
	path, err := LogPath(getenv, l.Profile, l.IdempotencyKey)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return clierr.LocalStorage("序列化请求日志失败: %v", err)
	}
	return config.AtomicWrite(path, data)
}

// LoadLog reads the record for (profile, key). Missing → NOT_FOUND.
func LoadLog(getenv config.Getenv, profile, key string) (*RequestLog, error) {
	path, err := LogPath(getenv, profile, key)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, clierr.New(clierr.KindNotFound,
			"当前 Profile 没有该 Idempotency-Key 的本地请求日志: "+key)
	}
	if err != nil {
		return nil, clierr.LocalStorage("读取请求日志失败: %v", err)
	}
	if err := config.CheckPerms(path, false); err != nil {
		return nil, err
	}
	var l RequestLog
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, clierr.LocalStorage("请求日志 %s 解析失败: %v", path, err)
	}
	return &l, nil
}

// VerifyIntegrity rejects a log whose stored JSON no longer matches its
// digest (cli-contract.md §18: tampered/corrupted logs must not be replayed).
func (l *RequestLog) VerifyIntegrity() error {
	digest, err := Digest(l.RequestJSON)
	if err != nil {
		return err
	}
	if digest != l.RequestDigest {
		return clierr.Usage("本地请求日志与其 digest 不一致（Key %s），拒绝执行；请勿手工修改请求日志", l.IdempotencyKey)
	}
	return nil
}

// MarkAttempt records one more submit attempt.
func (l *RequestLog) MarkAttempt(now time.Time) {
	l.AttemptCount++
	l.LastAttemptAt = now
}

// SetState transitions the local state.
func (l *RequestLog) SetState(s State) { l.State = s }
