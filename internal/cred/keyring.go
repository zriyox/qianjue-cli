package cred

import (
	"encoding/json"
	"errors"

	"github.com/99designs/keyring"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

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
	item, err := s.kr.Get(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
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
	err = s.kr.Set(keyring.Item{
		Key:   account,
		Data:  data,
		Label: "qianjue CLI (" + account + ")",
	})
	if err != nil {
		return clierr.LocalStorage("写入系统凭证库失败: %v", err)
	}
	return nil
}

func (s *systemStore) Delete(account string) error {
	err := s.kr.Remove(account)
	if errors.Is(err, keyring.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return clierr.LocalStorage("删除系统凭证库记录失败: %v", err)
	}
	return nil
}
