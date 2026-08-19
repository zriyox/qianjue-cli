package authflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

// refreshWindow: access tokens within 5 minutes of expiry are refreshed
// proactively (cli-contract.md §11).
const refreshWindow = 5 * time.Minute

// lockAcquireTimeout bounds waiting for another process's refresh.
const lockAcquireTimeout = 30 * time.Second

var errReLogin = "请重新执行 qianjue auth login"

// EnsureFreshToken returns a usable Device Flow record, refreshing it through
// POST /integration/token/refresh when needed. A per-profile file lock
// serializes concurrent CLI processes; after acquiring the lock the record is
// re-read so only one process performs the server-side rotation.
//
// force=true (401/2002 recovery) refreshes even when local expiry metadata
// still looks fresh — unless another process already rotated the token, in
// which case the newer record is returned for a retry.
func EnsureFreshToken(ctx context.Context, refreshClient *api.Client, store cred.Store, getenv config.Getenv, profile string, now func() time.Time, force bool) (*cred.Record, error) {
	if now == nil {
		now = time.Now
	}
	rec, err := loadDeviceFlowRecord(store, profile)
	if err != nil {
		return nil, err
	}
	if !force && isFresh(rec, now()) {
		return rec, nil
	}

	unlock, err := acquireRefreshLock(ctx, getenv, profile)
	if err != nil {
		return nil, err
	}
	defer unlock()

	// 拿锁后重读：另一个进程可能已完成轮换。
	latest, err := loadDeviceFlowRecord(store, profile)
	if err != nil {
		return nil, err
	}
	if isFresh(latest, now()) && (!force || latest.AccessToken != rec.AccessToken) {
		return latest, nil
	}
	rec = latest

	if !rec.SessionExpiresAt.IsZero() && !rec.SessionExpiresAt.After(now()) {
		return nil, clierr.New(clierr.KindAuth, "Refresh Session 已过期（30 天），"+errReLogin)
	}

	refreshed, err := refreshClient.RefreshToken(ctx, rec.RefreshToken)
	if err != nil {
		ce := clierr.AsCLIError(err)
		switch ce.Code {
		case 2016:
			// Refresh Token 已被使用：不循环重试（cli-contract.md §11.7）。
			return nil, clierr.New(clierr.KindAuth, "Refresh Token 已被使用，"+errReLogin)
		case 2001, 2002, 2014, 2015:
			return nil, clierr.New(clierr.KindAuth, fmt.Sprintf("Refresh 凭证已失效（code %d），%s", ce.Code, errReLogin))
		}
		return nil, err
	}

	newRec := &cred.Record{
		CredentialType: cred.TypeDeviceFlow,
		AccessToken:    refreshed.AccessToken,
		RefreshToken:   refreshed.RefreshToken,
		SessionID:      refreshed.SessionID,
		Scopes:         rec.Scopes,
	}
	if refreshed.AccessTokenExpiresAt != nil {
		newRec.AccessTokenExpiresAt = refreshed.AccessTokenExpiresAt.Time
	}
	if refreshed.RefreshTokenExpiresAt != nil {
		newRec.SessionExpiresAt = refreshed.RefreshTokenExpiresAt.Time
	}

	if err := commitRotatedRecord(store, profile, newRec); err != nil {
		return nil, err
	}
	return newRec, nil
}

func loadDeviceFlowRecord(store cred.Store, profile string) (*cred.Record, error) {
	rec, err := store.Get(cred.DeviceFlowAccount(profile))
	if err == cred.ErrNotFound {
		return nil, clierr.New(clierr.KindAuth, "当前 Profile 没有 Device Flow 凭证，"+errReLogin)
	}
	if err != nil {
		return nil, clierr.AsCLIError(err)
	}
	if rec.CredentialType != cred.TypeDeviceFlow || rec.RefreshToken == "" {
		return nil, clierr.New(clierr.KindAuth, "凭证记录不支持自动刷新，"+errReLogin)
	}
	return rec, nil
}

func isFresh(rec *cred.Record, at time.Time) bool {
	return !rec.AccessTokenExpiresAt.IsZero() && rec.AccessTokenExpiresAt.After(at.Add(refreshWindow))
}

// commitRotatedRecord performs the staged atomic replacement: write staging →
// verify readback → write primary → drop staging. The server has already
// invalidated the old refresh token at this point, so any persistence failure
// is terminal for this session: report LOCAL_STORAGE and require re-login,
// but never delete the still-readable old record (cli-contract.md §11.5).
func commitRotatedRecord(store cred.Store, profile string, newRec *cred.Record) error {
	staging := cred.DeviceFlowStagingAccount(profile)
	failed := func(stage string, err error) error {
		return clierr.LocalStorage(
			"刷新后的凭证%s失败（%v）；旧 Refresh Token 已被服务端作废，%s", stage, err, errReLogin)
	}
	if err := store.Set(staging, newRec); err != nil {
		return failed("暂存", err)
	}
	verify, err := store.Get(staging)
	if err != nil || verify.AccessToken != newRec.AccessToken || verify.RefreshToken != newRec.RefreshToken {
		if err == nil {
			return failed("暂存校验", clierr.LocalStorage("回读内容不一致"))
		}
		return failed("暂存校验", err)
	}
	if err := store.Set(cred.DeviceFlowAccount(profile), newRec); err != nil {
		return failed("原子替换", err)
	}
	_ = store.Delete(staging) // 清理失败不影响主记录
	return nil
}

// acquireRefreshLock takes the per-profile cross-process refresh lock.
func acquireRefreshLock(ctx context.Context, getenv config.Getenv, profile string) (func(), error) {
	locksDir, err := config.LocksDir(getenv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(locksDir, 0o700); err != nil {
		return nil, clierr.LocalStorage("创建锁目录失败: %v", err)
	}
	fl := flock.New(filepath.Join(locksDir, profile+".refresh.lock"))
	lockCtx, cancel := context.WithTimeout(ctx, lockAcquireTimeout)
	defer cancel()
	ok, err := fl.TryLockContext(lockCtx, 50*time.Millisecond)
	if err != nil || !ok {
		if ctx.Err() != nil {
			return nil, clierr.Interrupted()
		}
		return nil, clierr.LocalStorage("获取 refresh 进程锁超时（另一个 qianjue 进程可能卡住）: %v", err)
	}
	return func() { _ = fl.Unlock() }, nil
}
