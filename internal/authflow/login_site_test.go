package authflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// siteTestServer answers device-auth create with the given authorization URL and
// records the create body; polling returns an issued credential immediately.
func siteTestServer(t *testing.T, authorizationURL string, createBody *map[string]any, polls *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/device-auth":
			raw, _ := io.ReadAll(r.Body)
			require.NoError(t, json.Unmarshal(raw, createBody))
			fmt.Fprintf(w, `{"code":200,"message":"ok","data":{"deviceSessionId":"qj_ds_1","deviceCode":"qj_dc_1","authorizationUrl":%q,"expiresAt":"2026-08-17 18:05:00","scopes":["task.read"]}}`, authorizationURL)
		case r.Method == http.MethodGet && r.URL.Path == "/integration/device-auth/qj_ds_1":
			polls.Add(1)
			fmt.Fprint(w, pollResp("ACTIVE", true))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func runSiteLogin(t *testing.T, srvURL, site string, opened *[]string) (*LoginResult, error) {
	p := output.NewPrinter(&bytes.Buffer{}, &bytes.Buffer{}, output.FormatJSON, false, true)
	client := api.NewClient(srvURL, 5*time.Second, nil, api.NewTraceID(), nil)
	return Login(context.Background(), client, cred.NewMemoryStore(), p, LoginOptions{
		Profile:      "acme",
		Site:         site,
		PollInterval: time.Millisecond,
		OpenBrowser: func(url string) error {
			*opened = append(*opened, url)
			return nil
		},
	})
}

func TestLoginSendsSiteAndAcceptsPageOnThatSite(t *testing.T) {
	var body map[string]any
	var polls atomic.Int32
	srv := siteTestServer(t, "https://acme.example.com/integration/authorize?deviceSessionId=qj_ds_1", &body, &polls)
	defer srv.Close()

	var opened []string
	res, err := runSiteLogin(t, srv.URL, "acme.example.com", &opened)
	require.NoError(t, err)
	assert.Equal(t, "qj_ds_1", res.SessionID)
	assert.Equal(t, "acme.example.com", body["site"])
	assert.Equal(t, []string{"https://acme.example.com/integration/authorize?deviceSessionId=qj_ds_1"}, opened)
}

// 服务端不认识 site（旧版本）或下发了别的站点：绝不能打开链接让用户在错误站点登录——
// 合作伙伴用户在官方站登录会以同手机号的官方账号授权，任务与扣费全部落到官方站。
func TestLoginRefusesPageOnAnotherSite(t *testing.T) {
	var body map[string]any
	var polls atomic.Int32
	srv := siteTestServer(t, "https://aiqianjue.com/integration/authorize?deviceSessionId=qj_ds_1", &body, &polls)
	defer srv.Close()

	var opened []string
	_, err := runSiteLogin(t, srv.URL, "acme.example.com", &opened)
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.KindServer, ce.Kind)
	assert.Contains(t, ce.Message, "acme.example.com")
	assert.Empty(t, opened, "must not open a page on the wrong site")
	assert.Zero(t, polls.Load(), "must not poll for credentials issued on the wrong site")
}

// 只比主机名：声明值可带协议 / 大写 / 路径，授权页可带端口（本地联调 http://{domain}:3000）。
func TestLoginSiteComparesHostOnly(t *testing.T) {
	var body map[string]any
	var polls atomic.Int32
	srv := siteTestServer(t, "http://acme.localtest.me:3000/integration/authorize?deviceSessionId=qj_ds_1", &body, &polls)
	defer srv.Close()

	var opened []string
	_, err := runSiteLogin(t, srv.URL, "https://ACME.localtest.me/", &opened)
	require.NoError(t, err)
	assert.Len(t, opened, 1)
}

// 不声明站点时请求体里不带 site，旧服务端完全无感，行为与原来一致。
func TestLoginWithoutSiteOmitsField(t *testing.T) {
	var body map[string]any
	var polls atomic.Int32
	srv := siteTestServer(t, "https://aiqianjue.com/integration/authorize?deviceSessionId=qj_ds_1", &body, &polls)
	defer srv.Close()

	var opened []string
	_, err := runSiteLogin(t, srv.URL, "", &opened)
	require.NoError(t, err)
	_, present := body["site"]
	assert.False(t, present)
	assert.Len(t, opened, 1)
}
