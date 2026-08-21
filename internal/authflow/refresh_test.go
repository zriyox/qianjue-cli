package authflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

func lockEnv(t *testing.T) config.Getenv {
	dir := t.TempDir()
	return func(k string) string {
		if k == "XDG_STATE_HOME" {
			return filepath.Join(dir, "state")
		}
		return ""
	}
}

func expiringRecord() *cred.Record {
	return &cred.Record{
		CredentialType:       cred.TypeDeviceFlow,
		AccessToken:          "qj_at_old",
		AccessTokenExpiresAt: time.Now().Add(time.Minute), // < 5min 窗口
		RefreshToken:         "qj_rt_old",
		SessionExpiresAt:     time.Now().Add(29 * 24 * time.Hour),
		SessionID:            "qj_ds_1",
		Scopes:               []string{"task.create"},
	}
}

func refreshServer(t *testing.T, calls *atomic.Int32) *httptest.Server {
	var mu sync.Mutex
	validRT := "qj_rt_old"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/integration/token/refresh", r.URL.Path)
		mu.Lock()
		defer mu.Unlock()
		var body map[string]string
		require.NoError(t, jsonDecodeBody(r, &body))
		if body["refreshToken"] != validRT {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"code":2103,"message":"Refresh Token 已被使用，请重新登录","data":null}`)
			return
		}
		n := calls.Add(1)
		validRT = fmt.Sprintf("qj_rt_new%d", n)
		atExp := time.Now().Add(2 * time.Hour).Format("2006-01-02 15:04:05")
		rtExp := time.Now().Add(30 * 24 * time.Hour).Format("2006-01-02 15:04:05")
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"sessionId":"qj_ds_1","accessToken":"qj_at_new%d","accessTokenExpiresAt":"%s","refreshToken":"%s","refreshTokenExpiresAt":"%s"}}`, n, atExp, validRT, rtExp)
	}))
}

func jsonDecodeBody(r *http.Request, out any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func TestEnsureFreshTokenNoRefreshWhenFresh(t *testing.T) {
	store := cred.NewMemoryStore()
	rec := expiringRecord()
	rec.AccessTokenExpiresAt = time.Now().Add(time.Hour)
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))

	// refreshClient 指向必炸的地址：不应发起任何 HTTP
	client := api.NewClient("http://127.0.0.1:1", time.Second, nil, api.NewTraceID(), nil)
	got, err := EnsureFreshToken(context.Background(), client, store, lockEnv(t), "local", nil, false)
	require.NoError(t, err)
	assert.Equal(t, "qj_at_old", got.AccessToken)
}

func TestEnsureFreshTokenRotatesAndPersists(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := cred.NewMemoryStore()
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), expiringRecord()))
	client := api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil)

	got, err := EnsureFreshToken(context.Background(), client, store, lockEnv(t), "local", nil, false)
	require.NoError(t, err)
	assert.Equal(t, "qj_at_new1", got.AccessToken)
	assert.Equal(t, "qj_rt_new1", got.RefreshToken)
	assert.Equal(t, []string{"task.create"}, got.Scopes, "scopes 从旧记录继承")

	persisted, err := store.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, "qj_at_new1", persisted.AccessToken)
	assert.True(t, persisted.SessionExpiresAt.After(time.Now().Add(29*24*time.Hour)), "session 过期=refreshTokenExpiresAt")
	_, err = store.Get(cred.DeviceFlowStagingAccount("local"))
	assert.ErrorIs(t, err, cred.ErrNotFound, "staging 记录必须清理")
	assert.Equal(t, int32(1), calls.Load())
}

func TestConcurrentRefreshOnlyOneServerCall(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := cred.NewMemoryStore()
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), expiringRecord()))
	getenv := lockEnv(t)

	var wg sync.WaitGroup
	tokens := make([]string, 4)
	for i := range 4 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil)
			rec, err := EnsureFreshToken(context.Background(), client, store, getenv, "local", nil, false)
			require.NoError(t, err)
			tokens[i] = rec.AccessToken
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int32(1), calls.Load(), "并发刷新只允许一个真正调后端")
	for _, tk := range tokens {
		assert.Equal(t, "qj_at_new1", tk, "所有进程/协程拿到同一个赢家 token")
	}
}

func TestRefreshTokenReuseIs2016NoRetry(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := cred.NewMemoryStore()
	rec := expiringRecord()
	rec.RefreshToken = "qj_rt_stolen" // 服务端视为已被使用
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))
	client := api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil)

	_, err := EnsureFreshToken(context.Background(), client, store, lockEnv(t), "local", nil, false)
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.KindAuth, ce.Kind)
	assert.Contains(t, ce.Message, "重新")
	assert.Equal(t, int32(0), calls.Load())

	old, gerr := store.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, gerr)
	assert.Equal(t, "qj_at_old", old.AccessToken, "失败时旧记录保留")
}

func TestSessionExpiredLocallyNoHTTP(t *testing.T) {
	store := cred.NewMemoryStore()
	rec := expiringRecord()
	rec.SessionExpiresAt = time.Now().Add(-time.Hour)
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))

	client := api.NewClient("http://127.0.0.1:1", time.Second, nil, api.NewTraceID(), nil)
	_, err := EnsureFreshToken(context.Background(), client, store, lockEnv(t), "local", nil, false)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitAuth, clierr.AsCLIError(err).ExitCode)
}

// failingStore lets individual accounts fail while others succeed.
type failingStore struct {
	cred.Store
	failSet map[string]error
}

func (f *failingStore) Set(account string, r *cred.Record) error {
	if err, ok := f.failSet[account]; ok {
		return err
	}
	return f.Store.Set(account, r)
}

func TestRotationPersistFailureKeepsOldRecordAndReportsLocalStorage(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	inner := cred.NewMemoryStore()
	require.NoError(t, inner.Set(cred.DeviceFlowAccount("local"), expiringRecord()))
	store := &failingStore{Store: inner, failSet: map[string]error{
		cred.DeviceFlowStagingAccount("local"): assert.AnError,
	}}
	client := api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil)

	_, err := EnsureFreshToken(context.Background(), client, store, lockEnv(t), "local", nil, false)
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.ExitLocalStorage, ce.ExitCode)
	assert.Contains(t, ce.Message, "重新执行 qianjue auth login")

	old, gerr := inner.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, gerr)
	assert.Equal(t, "qj_at_old", old.AccessToken, "不得删除仍可读取的旧记录")
}

func TestDeviceFlowTokenSourceHandleAuthErrorForcesRefresh(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := cred.NewMemoryStore()
	rec := expiringRecord()
	rec.AccessTokenExpiresAt = time.Now().Add(time.Hour) // 本地看还新鲜，但服务端已判 2002
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))

	ts := NewDeviceFlowTokenSource(store, lockEnv(t), "local", api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil))
	retried, err := ts.HandleAuthError(context.Background(), 2002)
	require.NoError(t, err)
	assert.True(t, retried)
	assert.Equal(t, int32(1), calls.Load())

	token, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "qj_at_new1", token)
}

func TestResolveTokenSourcePrecedence(t *testing.T) {
	store := cred.NewMemoryStore()
	newStore := func() (cred.Store, error) { return store, nil }
	client := api.NewClient("http://127.0.0.1:1", time.Second, nil, api.NewTraceID(), nil)

	// 1. 无任何凭证 → AUTH
	_, err := ResolveTokenSource(func(string) string { return "" }, newStore, "local", client)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitAuth, clierr.AsCLIError(err).ExitCode)

	// 2. PAT 记录兜底
	require.NoError(t, store.Set(cred.PATAccount("local"), &cred.Record{CredentialType: cred.TypePAT, AccessToken: "qj_pat_k"}))
	ts, err := ResolveTokenSource(func(string) string { return "" }, newStore, "local", client)
	require.NoError(t, err)
	tk, _ := ts.Token(context.Background())
	assert.Equal(t, "qj_pat_k", tk)

	// 3. device-flow 优先于 PAT（A1）
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), &cred.Record{
		CredentialType: cred.TypeDeviceFlow, AccessToken: "qj_at_d",
		AccessTokenExpiresAt: time.Now().Add(time.Hour), RefreshToken: "qj_rt_d",
	}))
	ts, err = ResolveTokenSource(func(string) string { return "" }, newStore, "local", client)
	require.NoError(t, err)
	tk, err = ts.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "qj_at_d", tk)

	// 4. QIANJUE_TOKEN 最优先，且不打开凭证库
	env := func(k string) string {
		if k == "QIANJUE_TOKEN" {
			return "qj_pat_env"
		}
		return ""
	}
	ts, err = ResolveTokenSource(env, func() (cred.Store, error) { t.Fatal("env token 不应打开凭证库"); return nil, nil }, "local", client)
	require.NoError(t, err)
	tk, _ = ts.Token(context.Background())
	assert.Equal(t, "qj_pat_env", tk)

	// 5. 非法前缀拒绝
	badEnv := func(k string) string {
		if k == "QIANJUE_TOKEN" {
			return "qj_rt_notallowed"
		}
		return ""
	}
	_, err = ResolveTokenSource(badEnv, newStore, "local", client)
	require.Error(t, err)
	assert.Equal(t, clierr.ExitAuth, clierr.AsCLIError(err).ExitCode)
}
