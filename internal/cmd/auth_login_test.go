package cmd

import (
	"encoding/json"
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
	"github.com/zriyox/qianjue-cli/internal/config"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

// loginServer serves a create + N-poll device flow.
func loginServer(t *testing.T) *httptest.Server {
	var polls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/device-auth":
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_9","deviceCode":"qj_dc_9","authorizationUrl":"http://localhost:3000/integration/authorize?deviceSessionId=qj_ds_9","expiresAt":"2026-08-17 18:05:00","scopes":["task.create","task.read","task.cancel"]}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/device-auth/qj_ds_9":
			if polls.Add(1) == 1 {
				fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_9","status":"PENDING","expiresAt":"2026-08-17 18:05:00","authorizedAt":null,"accessToken":null,"accessTokenExpiresAt":null,"refreshToken":null,"credentialsIssuedNow":false}}`)
				return
			}
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_9","status":"ACTIVE","expiresAt":"2026-09-16 18:00:00","authorizedAt":"2026-08-17 18:01:00","accessToken":"qj_at_cmdtest","accessTokenExpiresAt":"2026-08-17 20:00:00","refreshToken":"qj_rt_cmdtest","credentialsIssuedNow":true}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestAuthLoginCommandEndToEnd(t *testing.T) {
	srv := loginServer(t)
	defer srv.Close()

	store := cred.NewMemoryStore()
	app, stdout, stderr := xdgApp(t, map[string]string{"QIANJUE_API_BASE_URL": srv.URL})
	app.newStore = func() (cred.Store, error) { return store, nil }
	app.openBrowser = func(url string) error { t.Fatal("--no-open 时不得打开浏览器"); return nil }
	app.loginPollInterval = time.Millisecond

	exit := run(app, []string{"auth", "login", "--no-open", "--output", "json"})
	require.Equal(t, 0, exit, "stderr: %s", stderr.String())

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "auth.login", env.Command)
	assert.True(t, env.OK)
	data := env.Data.(map[string]any)
	assert.Equal(t, "DEVICE_FLOW", data["credentialType"])
	assert.Equal(t, "qj_ds_9", data["sessionId"])
	assert.Len(t, data["scopes"], 3)
	assert.NotEmpty(t, env.Meta["traceId"])
	assert.Equal(t, "default", env.Meta["profile"])

	// stdout/stderr 均无 token 明文
	assert.NotContains(t, stdout.String(), "qj_at_cmdtest")
	assert.NotContains(t, stdout.String(), "qj_rt_cmdtest")
	assert.NotContains(t, stderr.String(), "qj_at_cmdtest")

	rec, err := store.Get(cred.DeviceFlowAccount("default"))
	require.NoError(t, err)
	assert.Equal(t, "qj_at_cmdtest", rec.AccessToken)
}

// 未配置 API 地址时不再报 USAGE：CLI 回落到生产端点（config.DefaultAPIBaseURL），
// 终端用户因此零配置可用。这里只断言解析结果，不发任何 HTTP —— 测试必须离线。
func TestConfigDefaultsToProductionEndpoint(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	exit := run(app, []string{"config", "show", "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Equal(t, config.DefaultAPIBaseURL, data["apiBaseUrl"],
		"没有任何配置时必须回落到生产端点")
}

func TestAuthLoginStoreUnavailableFailsSafely(t *testing.T) {
	srv := loginServer(t)
	defer srv.Close()

	app, stdout, _ := xdgApp(t, map[string]string{"QIANJUE_API_BASE_URL": srv.URL})
	app.newStore = func() (cred.Store, error) {
		return nil, clierr.LocalStorage("系统凭证库不可用")
	}
	exit := run(app, []string{"auth", "login", "--no-open", "--output", "json"})
	assert.Equal(t, clierr.ExitLocalStorage, exit)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "LOCAL_STORAGE", env.Error.Kind)
}

func TestAuthLoginCustomScopesSentToServer(t *testing.T) {
	var gotScopes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/integration/device-auth" {
			var body api.DeviceAuthCreateRequest
			require.NoError(t, jsonDecode(r, &body))
			gotScopes = body.Scopes
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_9","deviceCode":"qj_dc_9","authorizationUrl":"http://x/integration/authorize","expiresAt":"2026-08-17 18:05:00","scopes":["task.read"]}}`)
			return
		}
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_9","status":"ACTIVE","expiresAt":"2026-09-16 18:00:00","authorizedAt":"2026-08-17 18:01:00","accessToken":"qj_at_s","accessTokenExpiresAt":"2026-08-17 20:00:00","refreshToken":"qj_rt_s","credentialsIssuedNow":true}}`)
	}))
	defer srv.Close()

	app, _, _ := xdgApp(t, map[string]string{"QIANJUE_API_BASE_URL": srv.URL})
	app.newStore = func() (cred.Store, error) { return cred.NewMemoryStore(), nil }
	app.loginPollInterval = time.Millisecond

	exit := run(app, []string{"auth", "login", "--no-open", "--scope", "task.read", "--output", "json"})
	require.Equal(t, 0, exit)
	assert.Equal(t, []string{"task.read"}, gotScopes)
}

func jsonDecode(r *http.Request, out any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}
