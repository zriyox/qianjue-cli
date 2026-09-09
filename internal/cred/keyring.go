package cred

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/99designs/keyring"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// keychainTimeout bounds every OS credential-store call.
//
// Why this is not optional: on macOS, reading an item whose ACL does not list
// the current binary makes Security.framework pop a GUI authorization dialog and
// block inside SecItemCopyMatching until somebody clicks it. That happens
// routinely — a rebuilt binary at a new path is a different application as far
// as the keychain is concerned. Nobody clicks when this CLI is driven by an AI
// agent, by CI, or from a pipe, so the process hangs forever instead of failing.
// A bounded wait turns that into an actionable LOCAL_STORAGE error (exit 14)
// that tells the caller to use QIANJUE_TOKEN.
//
// The timeout does NOT degrade to plaintext storage — an unavailable store still
// fails hard, per cli-contract.md §26. The abandoned goroutine stays parked in
// the C call until the process exits, which is fine for a short-lived CLI.
const keychainTimeout = 10 * time.Second

// withTimeout runs fn and gives up after keychainTimeout.
func withTimeout[T any](what string, fn func() (T, error)) (T, error) {
	return withTimeoutFor(keychainTimeout, what, fn)
}

// withTimeoutFor is withTimeout with an injectable budget (tests use a short one).
func withTimeoutFor[T any](budget time.Duration, what string, fn func() (T, error)) (T, error) {
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{value: v, err: err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-time.After(budget):
		var zero T
		return zero, clierr.LocalStorage(
			"系统凭证库%s超时（%s）；macOS 钥匙串可能正在等待授权点击，"+
				"而当前不是交互式终端。改用环境变量 QIANJUE_TOKEN 提供凭证，"+
				"或在交互式终端里重新执行一次并允许钥匙串访问",
			what, budget)
	}
}

// systemBackends limits keyring to real OS credential stores. The file and
// keyctl/pass backends are intentionally excluded: an unavailable system store
// must fail, not silently degrade to plaintext (cli-contract.md §26).
var systemBackends = []keyring.BackendType{
	keyring.KeychainBackend,
	keyring.WinCredBackend,
	keyring.SecretServiceBackend,
}

type systemStore struct {
	kr keyring.Keyring
}

// NewSystemStore opens the OS credential store or fails with LOCAL_STORAGE.
func NewSystemStore() (Store, error) {
	kr, err := keyring.Open(keyring.Config{
		ServiceName:              ServiceName,
		AllowedBackends:          systemBackends,
		KeychainTrustApplication: true,
		KeychainName:             "login",
	})
	if err != nil {
		return nil, clierr.LocalStorage("系统凭证库不可用（%v）；CLI 不会退化为明文存储，请修复系统凭证库后重试", err)
	}
	return &systemStore{kr: kr}, nil
}

func (s *systemStore) Get(account string) (*Record, error) {
	item, err := withTimeout("读取", func() (keyring.Item, error) { return s.kr.Get(account) })
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		if clierr.AsCLIError(err) != nil {
			return nil, err
		}
		return nil, clierr.LocalStorage("读取系统凭证库失败: %v", err)
	}
	var r Record
	if err := json.Unmarshal(item.Data, &r); err != nil {
		return nil, clierr.LocalStorage("凭证记录损坏（account=%s）：%v；请执行 qianjue auth logout 后重新登录", account, err)
	}
	return &r, nil
}

func (s *systemStore) Set(account string, r *Record) error {
	if err := r.Validate(); err != nil {
		return clierr.LocalStorage("拒绝写入非法凭证记录: %v", err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		return clierr.LocalStorage("序列化凭证记录失败: %v", err)
	}
	_, err = withTimeout("写入", func() (struct{}, error) {
		return struct{}{}, s.kr.Set(keyring.Item{
			Key:   account,
			Data:  data,
			Label: "qianjue CLI (" + account + ")",
		})
	})
	if err != nil {
		if clierr.AsCLIError(err) != nil {
			return err
		}
		return clierr.LocalStorage("写入系统凭证库失败: %v", err)
	}
	return nil
}

func (s *systemStore) Delete(account string) error {
	_, err := withTimeout("删除", func() (struct{}, error) { return struct{}{}, s.kr.Remove(account) })
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		if clierr.AsCLIError(err) != nil {
			return err
		}
		return clierr.LocalStorage("删除系统凭证库记录失败: %v", err)
	}
	return nil
}
