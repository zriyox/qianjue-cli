package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

type fakeTokens struct {
	token        string
	refreshed    atomic.Int32
	allowRefresh bool
}

func (f *fakeTokens) Token(ctx context.Context) (string, error) { return f.token, nil }
func (f *fakeTokens) HandleAuthError(ctx context.Context, code int) (bool, error) {
	if !f.allowRefresh {
		return false, nil
	}
	f.refreshed.Add(1)
	f.token = "qj_at_refreshed"
	return true, nil
}

func newTestClient(baseURL string, tokens TokenSource) *Client {
	c := NewClient(baseURL, 5*time.Second, tokens, NewTraceID(), nil)
	c.retryDelay = time.Millisecond
	return c
}

func ok(data string) string {
	return fmt.Sprintf(`{"code":200,"message":"操作成功","data":%s}`, data)
}

func TestTraceIDShape(t *testing.T) {
	id := NewTraceID()
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), id)
	assert.NotEqual(t, id, NewTraceID())

	u := UUID4()
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), u)
}

func TestHeadersAndAuth(t *testing.T) {
	var gotAuth, gotTrace, gotUA, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTrace = r.Header.Get("X-Trace-Id")
		gotUA = r.Header.Get("User-Agent")
		gotKey = r.Header.Get(IdempotencyKeyHeader)
		fmt.Fprint(w, ok(`{"task":{"id":2045019196159766531,"status":"PENDING"},"historical":false}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	out, err := c.CreateImageTask(context.Background(), "key-1", []byte(`{"type":"TXT2IMG"}`))
	require.NoError(t, err)
	assert.Equal(t, "Bearer qj_at_x", gotAuth)
	assert.Equal(t, c.TraceID(), gotTrace)
	assert.Contains(t, gotUA, "qianjue-cli/")
	assert.Equal(t, "key-1", gotKey)
	assert.False(t, out.Historical)
	assert.Contains(t, string(out.Task), "2045019196159766531", "int64 ID 原样透传")
}

func TestBusinessErrorMapping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"code":2020,"message":"图片任务创建结果不确定，请勿更换 Idempotency-Key 重复提交","data":{"requestId":7,"status":"RECOVERY_REQUIRED"}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	_, err := c.CreateImageTask(context.Background(), "k", []byte(`{}`))
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.KindRecoveryRequired, ce.Kind)
	assert.Equal(t, 2020, ce.Code)
	assert.Equal(t, 409, ce.HTTPStatus)
	assert.Contains(t, string(ce.Details), "RECOVERY_REQUIRED")
}

func TestCancelHTTP200Code500IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		fmt.Fprint(w, `{"code":500,"message":"任务取消失败","data":null}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	_, err := c.CancelImageTask(context.Background(), "123")
	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	assert.Equal(t, clierr.KindServer, ce.Kind)
	assert.Equal(t, clierr.ExitServer, ce.ExitCode)
}

func TestAuthRetryOn2002OnlyOnce(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"code":2002,"message":"Integration Access Token 已过期","data":null}`)
			return
		}
		assert.Equal(t, "Bearer qj_at_refreshed", r.Header.Get("Authorization"))
		fmt.Fprint(w, ok(`{"requestId":1,"status":"SUCCEEDED"}`))
	}))
	defer srv.Close()

	tokens := &fakeTokens{token: "qj_at_old", allowRefresh: true}
	c := newTestClient(srv.URL, tokens)
	out, err := c.GetIdempotencyStatus(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, IdemSucceeded, out.Status)
	assert.Equal(t, int32(1), tokens.refreshed.Load())
}

func TestAuthNoRetryWhenSourceCannotRefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"code":2002,"message":"Integration Access Token 已过期","data":null}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_pat_x"})
	_, err := c.GetIdempotencyStatus(context.Background(), "k")
	require.Error(t, err)
	assert.Equal(t, clierr.KindAuth, clierr.AsCLIError(err).Kind)
}

func TestGetRetriesOn5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(502)
			fmt.Fprint(w, "bad gateway")
			return
		}
		fmt.Fprint(w, ok(`{"requestId":1,"status":"PROCESSING"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	out, err := c.GetIdempotencyStatus(context.Background(), "k")
	require.NoError(t, err)
	assert.Equal(t, IdemProcessing, out.Status)
	assert.Equal(t, int32(3), calls.Load())
}

func TestPostDoesNotRetryTransportError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// 断开连接模拟传输失败
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	_, err := c.CreateImageTask(context.Background(), "k", []byte(`{}`))
	require.Error(t, err)
	assert.Equal(t, clierr.KindTransport, clierr.AsCLIError(err).Kind)
	assert.Equal(t, int32(1), calls.Load(), "POST 创建不得在传输层自动重试")
}

func TestUnparseable2xxIsTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>proxy page</html>")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	_, err := c.CreateImageTask(context.Background(), "k", []byte(`{}`))
	require.Error(t, err)
	assert.Equal(t, clierr.KindTransport, clierr.AsCLIError(err).Kind, "2xx 但解析失败=未知结果")
}

func TestDeviceFlowEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/integration/device-auth":
			assert.Empty(t, r.Header.Get("Authorization"), "device-auth 创建是匿名接口")
			fmt.Fprint(w, ok(`{"deviceSessionId":"qj_ds_1","deviceCode":"qj_dc_1","authorizationUrl":"http://localhost:3000/integration/authorize?deviceSessionId=qj_ds_1","expiresAt":"2026-08-17 18:05:00","scopes":["task.create"]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/integration/device-auth/qj_ds_1":
			assert.Equal(t, "qj_dc_1", r.Header.Get(DeviceCodeHeader))
			fmt.Fprint(w, ok(`{"deviceSessionId":"qj_ds_1","status":"ACTIVE","expiresAt":"2026-09-16 18:00:00","authorizedAt":"2026-08-17 18:01:00","accessToken":"qj_at_new","accessTokenExpiresAt":"2026-08-17 20:00:00","refreshToken":"qj_rt_new","credentialsIssuedNow":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/integration/token/refresh":
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "qj_rt_new", body["refreshToken"])
			fmt.Fprint(w, ok(`{"sessionId":"qj_ds_1","accessToken":"qj_at_2","accessTokenExpiresAt":"2026-08-17 22:00:00","refreshToken":"qj_rt_2","refreshTokenExpiresAt":"2026-09-16 18:00:00"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, nil)
	created, err := c.CreateDeviceAuth(context.Background(), DeviceAuthCreateRequest{
		ClientID: "qianjue-cli/darwin", ClientName: "Qianjue Integration CLI", Scopes: []string{"task.create"},
	})
	require.NoError(t, err)
	assert.Equal(t, "qj_ds_1", created.DeviceSessionID)
	require.NotNil(t, created.ExpiresAt)
	assert.Equal(t, 2026, created.ExpiresAt.Year(), "后端 yyyy-MM-dd HH:mm:ss 格式必须可解析")

	poll, err := c.PollDeviceAuth(context.Background(), "qj_ds_1", "qj_dc_1")
	require.NoError(t, err)
	assert.True(t, poll.CredentialsIssuedNow)
	assert.Equal(t, "qj_at_new", poll.AccessToken)

	refreshed, err := c.RefreshToken(context.Background(), "qj_rt_new")
	require.NoError(t, err)
	assert.Equal(t, "qj_at_2", refreshed.AccessToken)
	assert.Equal(t, "qj_rt_2", refreshed.RefreshToken)
}

func TestAPITimeFormats(t *testing.T) {
	var ts APITime
	require.NoError(t, ts.UnmarshalJSON([]byte(`"2026-08-17 20:00:00"`)))
	assert.Equal(t, 20, ts.Hour())
	require.NoError(t, ts.UnmarshalJSON([]byte(`"2026-08-17T20:00:00"`)))
	assert.Equal(t, 20, ts.Hour())
	require.NoError(t, ts.UnmarshalJSON([]byte(`"2026-08-17T20:00:00+08:00"`)))
	require.NoError(t, ts.UnmarshalJSON([]byte(`null`)))
	assert.True(t, ts.IsZero())
	assert.Error(t, ts.UnmarshalJSON([]byte(`"tomorrow"`)))

	out, err := json.Marshal(APITime{Time: time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	assert.Equal(t, `"2026-08-17T20:00:00Z"`, string(out))
}

func TestGetImageTaskExtractsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ok(`{"id":2045019196159766531,"status":"COMPLETED","resultImageUrls":["https://cdn/x.png"]}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL, &fakeTokens{token: "qj_at_x"})
	raw, status, err := c.GetImageTask(context.Background(), "2045019196159766531")
	require.NoError(t, err)
	assert.Equal(t, TaskCompleted, status)
	assert.Contains(t, string(raw), "2045019196159766531")
}
