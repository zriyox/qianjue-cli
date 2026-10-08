package authflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

	// cached keeps the record in memory for the life of this process so repeated
	// requests reuse the token instead of re-reading the OS credential store.
	// On macOS every read of an item whose ACL does not list the current binary
	// (an unsigned executable, freshly replaced by an update) blocks inside
	// Security.framework until somebody clicks the authorization dialog: a
	// per-request read turns one command into N dialogs — `task wait` polls, so
	// a 10-minute wait asked for permission dozens of times — and, with nobody
	// to click, dies on the keychain timeout instead.
	//
	// The store is consulted again only when the cached token enters the
	// T-5min refresh window (cli-contract.md §11 trigger 1) or the server
	// rejects it with 401/2002 (§11 trigger 2, through HandleAuthError), so
	// cross-process rotation is still picked up by the next refresh.
	mu     sync.Mutex
	cached *cred.Record
}

func NewDeviceFlowTokenSource(store cred.Store, getenv config.Getenv, profile string, refreshClient *api.Client) *DeviceFlowTokenSource {
	return &DeviceFlowTokenSource{store: store, getenv: getenv, profile: profile, refreshClient: refreshClient, now: time.Now}
}

func (d *DeviceFlowTokenSource) Token(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if rec := d.cached; rec != nil && isFresh(rec, d.clock()) {
		return rec.AccessToken, nil
	}
	rec, err := EnsureFreshToken(ctx, d.refreshClient, d.store, d.getenv, d.profile, d.now, false)
	if err != nil {
		return "", err
	}
	d.cached = rec
	return rec.AccessToken, nil
}

func (d *DeviceFlowTokenSource) HandleAuthError(ctx context.Context, code int) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rec, err := EnsureFreshToken(ctx, d.refreshClient, d.store, d.getenv, d.profile, d.now, true)
	if err != nil {
		return false, err
	}
	// 轮换后的凭证进缓存：重放请求与后续请求都不必再回读凭证库。
	d.cached = rec
	return true, nil
}

// clock keeps the injectable time source optional, matching EnsureFreshToken.
func (d *DeviceFlowTokenSource) clock() time.Time {
	if d.now == nil {
		return time.Now()
	}
	return d.now()
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
	// The binding is checked here, before any token (including the refresh
	// token a later rotation would send) can reach refreshClient's host.
	if rec, err := store.Get(cred.DeviceFlowAccount(profile)); err == nil {
		if rec.CredentialType == cred.TypeDeviceFlow && rec.RefreshToken != "" {
			if err := RequireBoundTo(rec, refreshClient.BaseURL()); err != nil {
				return nil, err
			}
			return NewDeviceFlowTokenSource(store, getenv, profile, refreshClient), nil
		}
	} else if err != cred.ErrNotFound {
		return nil, clierr.AsCLIError(err)
	}
	if rec, err := store.Get(cred.PATAccount(profile)); err == nil {
		if err := RequireBoundTo(rec, refreshClient.BaseURL()); err != nil {
			return nil, err
		}
		return NewStaticTokenSource(rec.AccessToken), nil
	} else if err != cred.ErrNotFound {
		return nil, clierr.AsCLIError(err)
	}
	return nil, clierr.New(clierr.KindAuth,
		"当前 Profile 没有可用凭证：先执行 qianjue auth login，或通过 stdin 导入 PAT（qianjue auth import-token --type pat --stdin），或设置 QIANJUE_TOKEN")
}

// RequireBoundTo refuses to use a credential against an API root other than
// the one it was issued for. Records from older CLIs carry no binding and are
// accepted; they gain one on the next login or PAT import.
func RequireBoundTo(rec *cred.Record, apiBaseURL string) error {
	if rec == nil || rec.APIBaseURL == "" || config.SameAPIBaseURL(rec.APIBaseURL, apiBaseURL) {
		return nil
	}
	return clierr.New(clierr.KindAuth, fmt.Sprintf(
		"当前 Profile 的凭证签发自 %s，而当前 API 根地址是 %s；为防止凭证被发往其它服务器，已拒绝使用。"+
			"若确实要切换地址，请为新地址单独建 Profile 并重新登录", rec.APIBaseURL, apiBaseURL))
}
