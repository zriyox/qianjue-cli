package authflow

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
	"github.com/zriyox/qianjue-cli/internal/output"
)

const createResp = `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_1","deviceCode":"qj_dc_1","authorizationUrl":"http://localhost:3000/integration/authorize?deviceSessionId=qj_ds_1","expiresAt":"2026-08-17 18:05:00","scopes":["task.create","task.read","task.cancel"]}}`

func pollResp(status string, issued bool) string {
	tokens := `"accessToken":null,"accessTokenExpiresAt":null,"refreshToken":null`
	if issued {
		tokens = `"accessToken":"qj_at_new","accessTokenExpiresAt":"2026-08-17 20:00:00","refreshToken":"qj_rt_new"`
	}
	return fmt.Sprintf(`{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_1","status":"%s","expiresAt":"2026-09-16 18:00:00","authorizedAt":null,%s,"credentialsIssuedNow":%v}}`, status, tokens, issued)
}

func loginTestServer(t *testing.T, pollBodies []string) *httptest.Server {
	var pollCount atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/device-auth":
			fmt.Fprint(w, createResp)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/device-auth/qj_ds_1":
			assert.Equal(t, "qj_dc_1", r.Header.Get(api.DeviceCodeHeader))
			i := int(pollCount.Add(1)) - 1
			if i >= len(pollBodies) {
				i = len(pollBodies) - 1
			}
			body := pollBodies[i]
			if len(body) > 4 && body[:4] == "HTTP" {
				var status int
				var rest string
				fmt.Sscanf(body, "HTTP%d %s", &status, &rest)
				w.WriteHeader(status)
				fmt.Fprint(w, rest)
				return
			}
			fmt.Fprint(w, body)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func runLogin(t *testing.T, srvURL string, store cred.Store) (*LoginResult, error, *bytes.Buffer) {
	var stderr bytes.Buffer
	p := output.NewPrinter(&bytes.Buffer{}, &stderr, output.FormatJSON, false, true)
	client := api.NewClient(srvURL, 5*time.Second, nil, api.NewTraceID(), nil)
	res, err := Login(context.Background(), client, store, p, LoginOptions{
		Profile:      "local",
		PollInterval: time.Millisecond,
		NoOpen:       true,
	})
	return res, err, &stderr
}

func TestLoginHappyPathPersistsBeforeSuccess(t *testing.T) {
	srv := loginTestServer(t, []string{
		pollResp("PENDING", false),
		pollResp("AUTHORIZED", false),
		pollResp("ACTIVE", true),
	})
	defer srv.Close()

	store := cred.NewMemoryStore()
	res, err, stderr := runLogin(t, srv.URL, store)
	require.NoError(t, err)
	assert.Equal(t, "qj_ds_1", res.SessionID)
	assert.Equal(t, []string{"task.create", "task.read", "task.cancel"}, res.Scopes)
	assert.Equal(t, 20, res.AccessTokenExpiresAt.Hour())
	assert.Equal(t, time.September, res.SessionExpiresAt.Month())

	rec, err := store.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, "qj_at_new", rec.AccessToken)
	assert.Equal(t, "qj_rt_new", rec.RefreshToken)
	assert.Equal(t, cred.TypeDeviceFlow, rec.CredentialType)

	// stderr 打了授权 URL，但绝不含 token/device code
	assert.Contains(t, stderr.String(), "integration/authorize")
	assert.NotContains(t, stderr.String(), "qj_at_new")
	assert.NotContains(t, stderr.String(), "qj_dc_1")
}

func TestLoginActiveWithoutTokenFailsSafely(t *testing.T) {
	srv := loginTestServer(t, []string{pollResp("ACTIVE", false)})
	defer srv.Close()

	store := cred.NewMemoryStore()
	_, err, _ := runLogin(t, srv.URL, store)
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.KindAuth, ce.Kind)
	assert.Contains(t, ce.Message, "重新执行 qianjue auth login")
	assert.Empty(t, store.Accounts(), "不得写入无凭证记录")
}

func TestLoginTerminalStates(t *testing.T) {
	for _, status := range []string{"DENIED", "EXPIRED", "REVOKED"} {
		srv := loginTestServer(t, []string{pollResp(status, false)})
		store := cred.NewMemoryStore()
		_, err, _ := runLogin(t, srv.URL, store)
		srv.Close()
		require.Error(t, err, status)
		assert.Equal(t, clierr.ExitAuth, clierr.AsCLIError(err).ExitCode, status)
	}
}

func TestLogin2016ConflictKeepsPolling(t *testing.T) {
	srv := loginTestServer(t, []string{
		`HTTP409 {"code":2103,"message":"IntegrationDeviceSession状态冲突","data":null}`,
		pollResp("ACTIVE", true),
	})
	defer srv.Close()

	store := cred.NewMemoryStore()
	res, err, _ := runLogin(t, srv.URL, store)
	require.NoError(t, err, "2103 应继续轮询而不是失败")
	assert.Equal(t, "qj_ds_1", res.SessionID)
}

func TestLoginStorePersistFailureIsLocalStorage(t *testing.T) {
	srv := loginTestServer(t, []string{pollResp("ACTIVE", true)})
	defer srv.Close()

	store := cred.NewMemoryStore()
	store.FailWith = assert.AnError
	_, err, _ := runLogin(t, srv.URL, store)
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.ExitLocalStorage, ce.ExitCode)
	assert.Contains(t, ce.Message, "重新执行 qianjue auth login")
}

func TestLoginInterrupted(t *testing.T) {
	srv := loginTestServer(t, []string{pollResp("PENDING", false)})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := output.NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, true, true)
	client := api.NewClient(srv.URL, time.Second, nil, api.NewTraceID(), nil)
	_, err := Login(ctx, client, cred.NewMemoryStore(), p, LoginOptions{
		Profile: "local", PollInterval: time.Millisecond, NoOpen: true,
	})
	require.Error(t, err)
	assert.Equal(t, clierr.ExitInterrupted, clierr.AsCLIError(err).ExitCode)
}
