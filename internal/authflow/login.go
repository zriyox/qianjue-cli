// Package authflow orchestrates Device Flow login and credential lifecycle
// per docs/integration/auth-device-flow.md and cli-contract.md §9/§11.
package authflow

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// DefaultScopes are requested when the user passes no --scope flags
// (cli-contract.md §9).
var DefaultScopes = []string{"task.create", "task.read", "task.cancel"}

const defaultPollInterval = 2 * time.Second

// LoginOptions controls one Device Flow login.
type LoginOptions struct {
	Profile      string
	Scopes       []string
	NoOpen       bool
	PollInterval time.Duration          // 0 → 2s
	OpenBrowser  func(url string) error // nil → OpenBrowser
}

// LoginResult is the token-free summary reported to the user.
type LoginResult struct {
	Profile              string
	SessionID            string
	Scopes               []string
	AccessTokenExpiresAt time.Time
	SessionExpiresAt     time.Time
}

// Login runs the full Device Flow: create session, print/open the
// authorization URL, poll every 2s, and persist credentials to the system
// store BEFORE reporting success. Tokens never reach stdout or the result.
func Login(ctx context.Context, client *api.Client, store cred.Store, p *output.Printer, opts LoginOptions) (*LoginResult, error) {
	scopes := opts.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	openBrowser := opts.OpenBrowser
	if openBrowser == nil {
		openBrowser = OpenBrowser
	}

	hostname, _ := os.Hostname()
	created, err := client.CreateDeviceAuth(ctx, api.DeviceAuthCreateRequest{
		ClientID:   "qianjue-cli/" + runtime.GOOS,
		ClientName: "Qianjue Integration CLI",
		DeviceName: hostname,
		Scopes:     scopes,
	})
	if err != nil {
		return nil, err
	}

	p.Progressf("请在浏览器中确认授权: %s", created.AuthorizationURL)
	if created.ExpiresAt != nil && !created.ExpiresAt.IsZero() {
		p.Progressf("授权链接 %s 过期（约 %s 后）", created.ExpiresAt.Format(time.RFC3339),
			time.Until(created.ExpiresAt.Time).Round(time.Second))
	}
	if opts.NoOpen {
		p.Progressf("已按 --no-open 跳过自动打开浏览器")
	} else if err := openBrowser(created.AuthorizationURL); err != nil {
		p.Progressf("自动打开浏览器失败（%v），请手动访问上面的链接", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, clierr.Interrupted()
		case <-time.After(interval):
		}

		poll, err := client.PollDeviceAuth(ctx, created.DeviceSessionID, created.DeviceCode)
		if err != nil {
			ce := clierr.AsCLIError(err)
			// 2016：轮询与签发/过期标记的 CAS 竞争，服务端语义是“请重新轮询”。
			if ce.Code == 2016 {
				p.Progressf("轮询遇并发更新，继续重试")
				continue
			}
			if ce.Code == 2015 {
				return nil, clierr.New(clierr.KindAuth, "Device Flow 会话已过期，请重新执行 qianjue auth login")
			}
			return nil, err
		}

		if poll.CredentialsIssuedNow {
			return persistIssuedCredentials(store, opts.Profile, created, poll)
		}

		switch poll.Status {
		case api.SessionPending, api.SessionAuthorized:
			p.Progressf("等待授权中（%s）", poll.Status)
		case api.SessionActive:
			// 凭证已被其他轮询请求领取，明文不可再次获取（auth-device-flow.md §7.3）。
			return nil, clierr.New(clierr.KindAuth,
				"凭证已签发但未能取得明文（可能被其他进程领取），请重新执行 qianjue auth login")
		case api.SessionDenied:
			return nil, clierr.New(clierr.KindAuth, "授权已被拒绝")
		case api.SessionExpired:
			return nil, clierr.New(clierr.KindAuth, "Device Flow 会话已过期（5 分钟内未完成授权），请重新执行 qianjue auth login")
		case api.SessionRevoked:
			return nil, clierr.New(clierr.KindAuth, "Device Flow 会话已被撤销，请重新执行 qianjue auth login")
		default:
			return nil, clierr.New(clierr.KindAuth, fmt.Sprintf("未知 Device Flow 状态 %q，登录终止", poll.Status))
		}
	}
}

// persistIssuedCredentials writes the one-time credentials to the store before
// any success output. A persistence failure is unrecoverable for this session
// because the backend never returns the plaintext again.
func persistIssuedCredentials(store cred.Store, profile string, created *api.DeviceAuthCreateResult, poll *api.DeviceAuthPollResult) (*LoginResult, error) {
	if poll.AccessToken == "" || poll.RefreshToken == "" {
		return nil, clierr.New(clierr.KindAuth,
			"签发响应缺少凭证明文，请重新执行 qianjue auth login")
	}
	rec := &cred.Record{
		CredentialType: cred.TypeDeviceFlow,
		AccessToken:    poll.AccessToken,
		RefreshToken:   poll.RefreshToken,
		SessionID:      poll.DeviceSessionID,
		Scopes:         created.Scopes,
	}
	if poll.AccessTokenExpiresAt != nil {
		rec.AccessTokenExpiresAt = poll.AccessTokenExpiresAt.Time
	}
	if poll.ExpiresAt != nil {
		// 签发后 expiresAt 即 Refresh Session 的 30 天过期时间。
		rec.SessionExpiresAt = poll.ExpiresAt.Time
	}
	if err := store.Set(cred.DeviceFlowAccount(profile), rec); err != nil {
		return nil, clierr.LocalStorage(
			"凭证写入系统凭证库失败（%v）；服务端不会再次返回明文，请修复凭证库后重新执行 qianjue auth login", err)
	}
	return &LoginResult{
		Profile:              profile,
		SessionID:            poll.DeviceSessionID,
		Scopes:               created.Scopes,
		AccessTokenExpiresAt: rec.AccessTokenExpiresAt,
		SessionExpiresAt:     rec.SessionExpiresAt,
	}, nil
}
