package authflow

import (
	"context"
	"strings"
	"time"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

// StaticTokenSource serves a fixed token with no refresh capability
// (QIANJUE_TOKEN or an imported PAT).
type StaticTokenSource struct {
	token string
}

func NewStaticTokenSource(token string) *StaticTokenSource {
	return &StaticTokenSource{token: token}
}

func (s *StaticTokenSource) Token(ctx context.Context) (string, error) { return s.token, nil }

func (s *StaticTokenSource) HandleAuthError(ctx context.Context, code int) (bool, error) {
	return false, nil
}

// DeviceFlowTokenSource serves the keyring Device Flow token with proactive
// T-5min refresh and one-shot 2002 recovery. It uses a dedicated anonymous
// client for the refresh endpoint (the refresh request itself carries no
// bearer token).
type DeviceFlowTokenSource struct {
	store         cred.Store
	getenv        config.Getenv
	profile       string
	refreshClient *api.Client
	now           func() time.Time
}

func NewDeviceFlowTokenSource(store cred.Store, getenv config.Getenv, profile string, refreshClient *api.Client) *DeviceFlowTokenSource {
	return &DeviceFlowTokenSource{store: store, getenv: getenv, profile: profile, refreshClient: refreshClient, now: time.Now}
}

func (d *DeviceFlowTokenSource) Token(ctx context.Context) (string, error) {
	rec, err := EnsureFreshToken(ctx, d.refreshClient, d.store, d.getenv, d.profile, d.now, false)
	if err != nil {
		return "", err
	}
	return rec.AccessToken, nil
}

func (d *DeviceFlowTokenSource) HandleAuthError(ctx context.Context, code int) (bool, error) {
	if _, err := EnsureFreshToken(ctx, d.refreshClient, d.store, d.getenv, d.profile, d.now, true); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveTokenSource applies the credential precedence of cli-contract.md §8:
// QIANJUE_TOKEN > keyring Device Flow record > keyring PAT record > AUTH error.
// newStore is invoked lazily so the system credential store is only opened
// when the env token is absent.
func ResolveTokenSource(getenv config.Getenv, newStore func() (cred.Store, error), profile string, refreshClient *api.Client) (api.TokenSource, error) {
	if env := strings.TrimSpace(getenv("QIANJUE_TOKEN")); env != "" {
		if !strings.HasPrefix(env, "qj_at_") && !strings.HasPrefix(env, "qj_pat_") {
			return nil, clierr.New(clierr.KindAuth,
				"QIANJUE_TOKEN 必须是 qj_at_ 或 qj_pat_ 开头的 Token（qj_rt_/qj_dc_ 不能用于业务调用）")
		}
		return NewStaticTokenSource(env), nil
	}
	store, err := newStore()
	if err != nil {
		return nil, err
	}
	if rec, err := store.Get(cred.DeviceFlowAccount(profile)); err == nil {
		if rec.CredentialType == cred.TypeDeviceFlow && rec.RefreshToken != "" {
			return NewDeviceFlowTokenSource(store, getenv, profile, refreshClient), nil
		}
	} else if err != cred.ErrNotFound {
		return nil, clierr.AsCLIError(err)
	}
	if rec, err := store.Get(cred.PATAccount(profile)); err == nil {
		return NewStaticTokenSource(rec.AccessToken), nil
	} else if err != cred.ErrNotFound {
		return nil, clierr.AsCLIError(err)
	}
	return nil, clierr.New(clierr.KindAuth,
		"当前 Profile 没有可用凭证：先执行 qianjue auth login，或通过 stdin 导入 PAT（qianjue auth import-token --type pat --stdin），或设置 QIANJUE_TOKEN")
}
