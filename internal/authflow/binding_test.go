package authflow

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

const boundAPI = "https://api.aiqianjue.com/api/v1"

// 凭证只发给签发它的 API 地址：--api-base-url / QIANJUE_API_BASE_URL 被改成别的 https 主机
// （提示注入、仓库里的 .envrc）时，Access / Refresh Token 绝不能被带过去。
func TestResolveTokenSourceRefusesCredentialBoundToAnotherAPI(t *testing.T) {
	for _, rec := range []*cred.Record{
		{CredentialType: cred.TypeDeviceFlow, AccessToken: "qj_at_d", RefreshToken: "qj_rt_d",
			AccessTokenExpiresAt: time.Now().Add(time.Hour), APIBaseURL: boundAPI},
		{CredentialType: cred.TypePAT, AccessToken: "qj_pat_k", APIBaseURL: boundAPI},
	} {
		store := cred.NewMemoryStore()
		account := cred.PATAccount("local")
		if rec.CredentialType == cred.TypeDeviceFlow {
			account = cred.DeviceFlowAccount("local")
		}
		require.NoError(t, store.Set(account, rec))
		client := api.NewClient("https://evil.example.com/api/v1", time.Second, nil, api.NewTraceID(), nil)

		_, err := ResolveTokenSource(func(string) string { return "" },
			func() (cred.Store, error) { return store, nil }, "local", client)

		require.Error(t, err, rec.CredentialType)
		ce := clierr.AsCLIError(err)
		assert.Equal(t, clierr.KindAuth, ce.Kind)
		assert.Contains(t, ce.Message, boundAPI)
		assert.Contains(t, ce.Message, "evil.example.com")
	}
}

func TestResolveTokenSourceAcceptsSameAPIIgnoringHostCaseAndTrailingSlash(t *testing.T) {
	store := cred.NewMemoryStore()
	require.NoError(t, store.Set(cred.PATAccount("local"),
		&cred.Record{CredentialType: cred.TypePAT, AccessToken: "qj_pat_k", APIBaseURL: "https://API.aiqianjue.com/api/v1/"}))
	client := api.NewClient(boundAPI, time.Second, nil, api.NewTraceID(), nil)

	ts, err := ResolveTokenSource(func(string) string { return "" },
		func() (cred.Store, error) { return store, nil }, "local", client)
	require.NoError(t, err)
	tk, _ := ts.Token(context.Background())
	assert.Equal(t, "qj_pat_k", tk)
}

// 轮换 Refresh Token 时新记录必须保留绑定，否则第一次刷新之后保护就消失了。
func TestRefreshKeepsTheAPIBinding(t *testing.T) {
	var calls atomic.Int32
	srv := refreshServer(t, &calls)
	defer srv.Close()

	store := cred.NewMemoryStore()
	rec := expiringRecord()
	rec.APIBaseURL = srv.URL
	require.NoError(t, store.Set(cred.DeviceFlowAccount("local"), rec))
	client := api.NewClient(srv.URL, 5*time.Second, nil, api.NewTraceID(), nil)

	_, err := EnsureFreshToken(context.Background(), client, store, lockEnv(t), "local", nil, false)
	require.NoError(t, err)

	persisted, err := store.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, srv.URL, persisted.APIBaseURL)
}

// Device Flow 登录写入的凭证绑定登录时的 API 地址。
func TestLoginBindsCredentialToAPIBaseURL(t *testing.T) {
	srv := loginTestServer(t, []string{pollResp("ACTIVE", true)})
	defer srv.Close()

	store := cred.NewMemoryStore()
	_, err, _ := runLogin(t, srv.URL, store)
	require.NoError(t, err)

	rec, err := store.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, srv.URL, rec.APIBaseURL)
}
