package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

// secretPattern finds any unmasked credential in CLI output.
var secretPattern = regexp.MustCompile(`qj_(at|rt|pat|dc)_(?:[A-Za-z0-9_-]*[A-Za-z0-9_-])`)

func assertNoSecrets(t *testing.T, label, s string) {
	t.Helper()
	for _, m := range secretPattern.FindAllString(s, -1) {
		if m == "qj_at_***" || m == "qj_rt_***" || m == "qj_pat_***" || m == "qj_dc_***" {
			continue
		}
		t.Errorf("%s 泄露凭证样式内容: %s", label, m)
	}
}

// e2eBackend is a full mock of the Integration endpoints used by the CLI.
type e2eBackend struct {
	t          *testing.T
	polls      atomic.Int32
	taskGets   atomic.Int32
	cancelled  atomic.Bool
	accessTok  string
	refreshTok string
	deviceCode string
}

func (b *e2eBackend) handler(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/integration/device-auth":
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_e2e","deviceCode":"%s","authorizationUrl":"http://localhost:3000/integration/authorize?deviceSessionId=qj_ds_e2e","expiresAt":"%s","scopes":["task.create","task.read","task.cancel"]}}`,
			b.deviceCode, future(5*time.Minute))
	case r.Method == http.MethodGet && r.URL.Path == "/integration/device-auth/qj_ds_e2e":
		require.Equal(b.t, b.deviceCode, r.Header.Get(api.DeviceCodeHeader))
		if b.polls.Add(1) == 1 {
			fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_e2e","status":"PENDING","expiresAt":null,"authorizedAt":null,"accessToken":null,"accessTokenExpiresAt":null,"refreshToken":null,"credentialsIssuedNow":false}}`)
			return
		}
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"deviceSessionId":"qj_ds_e2e","status":"ACTIVE","expiresAt":"%s","authorizedAt":"%s","accessToken":"%s","accessTokenExpiresAt":"%s","refreshToken":"%s","credentialsIssuedNow":true}}`,
			future(30*24*time.Hour), now(), b.accessTok, future(2*time.Hour), b.refreshTok)
	case r.URL.Path == "/integration/image-tasks" && r.Method == http.MethodPost:
		require.Equal(b.t, "Bearer "+b.accessTok, auth)
		require.NotEmpty(b.t, r.Header.Get(api.IdempotencyKeyHeader))
		require.NotEmpty(b.t, r.Header.Get("X-Trace-Id"))
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"task":{"id":2045019196159766531,"taskType":"TXT2IMG","modelCode":"GPT_IMAGE_2","status":"PENDING"},"historical":false}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/idempotency":
		require.Equal(b.t, "Bearer "+b.accessTok, auth)
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"requestId":2045019196159766532,"status":"SUCCEEDED","attemptNo":1,"resourceType":"DRAW_TASK","resourceId":"2045019196159766531","errorCode":null,"errorMessage":null,"retryable":false,"retryAfterAt":null,"completedAt":"%s"}}`, now())
	case r.Method == http.MethodGet && r.URL.Path == "/integration/image-tasks/2045019196159766531":
		status := "PROCESSING"
		if b.taskGets.Add(1) >= 2 {
			status = "COMPLETED"
		}
		fmt.Fprintf(w, `{"code":200,"message":"操作成功","data":{"id":2045019196159766531,"taskType":"TXT2IMG","modelCode":"GPT_IMAGE_2","status":"%s","resultImageUrls":["https://cdn/e2e.png"]}}`, status)
	case r.Method == http.MethodPut && r.URL.Path == "/integration/image-tasks/2045019196159766531/cancel":
		b.cancelled.Store(true)
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":"任务取消成功"}`)
	default:
		b.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}
}

func now() string { return time.Now().Format("2006-01-02 15:04:05") }
func future(d time.Duration) string {
	return time.Now().Add(d).Format("2006-01-02 15:04:05")
}

// e2eRun executes one CLI invocation against shared env/store, collecting all
// output for the secret sweep.
type e2eSession struct {
	t      *testing.T
	getenv func(string) string
	store  *cred.MemoryStore
	allOut *bytes.Buffer
}

func (s *e2eSession) exec(args ...string) (int, string) {
	var stdout, stderr bytes.Buffer
	app := &appContext{
		flags:             &globalFlags{},
		stdout:            &stdout,
		stderr:            &stderr,
		stdin:             bytes.NewReader(nil),
		getenv:            s.getenv,
		isTTY:             func() bool { return false },
		newStore:          func() (cred.Store, error) { return s.store, nil },
		openBrowser:       func(string) error { return nil },
		loginPollInterval: time.Millisecond,
		waitInterval:      func(int) time.Duration { return time.Millisecond },
	}
	exit := run(app, args)
	s.allOut.WriteString(stdout.String())
	s.allOut.WriteString(stderr.String())
	assertNoSecrets(s.t, fmt.Sprintf("%v stdout", args), stdout.String())
	assertNoSecrets(s.t, fmt.Sprintf("%v stderr", args), stderr.String())
	return exit, stdout.String()
}

// TestEndToEndLifecycle drives the full acceptance flow in-process:
// profile → login → create --wait → request-status → get → cancel → logout,
// asserting stable exit codes and zero credential leakage everywhere.
func TestEndToEndLifecycle(t *testing.T) {
	backend := &e2eBackend{
		t:          t,
		accessTok:  "qj_at_E2ESECRETACCESS",
		refreshTok: "qj_rt_E2ESECRETREFRESH",
		deviceCode: "qj_dc_E2ESECRETDEVICE",
	}
	srv := httptest.NewServer(http.HandlerFunc(backend.handler))
	defer srv.Close()

	dir := t.TempDir()
	env := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(dir, "config"),
		"XDG_STATE_HOME":  filepath.Join(dir, "state"),
	}
	s := &e2eSession{
		t:      t,
		getenv: func(k string) string { return env[k] },
		store:  cred.NewMemoryStore(),
		allOut: &bytes.Buffer{},
	}

	// 1. profile
	exit, _ := s.exec("config", "profile", "create", "local", "--api-base-url", srv.URL, "--output", "json")
	require.Equal(t, 0, exit)
	exit, _ = s.exec("config", "profile", "use", "local", "--output", "json")
	require.Equal(t, 0, exit)

	// 2. login（device flow：PENDING → 签发）
	exit, out := s.exec("auth", "login", "--no-open", "--output", "json")
	require.Equal(t, 0, exit, out)
	rec, err := s.store.Get(cred.DeviceFlowAccount("local"))
	require.NoError(t, err)
	assert.Equal(t, backend.accessTok, rec.AccessToken)

	// 3. status（本地）
	exit, out = s.exec("auth", "status", "--output", "json")
	require.Equal(t, 0, exit)
	assert.Contains(t, out, "DEVICE_FLOW")

	// 4. create --wait → COMPLETED
	reqFile := filepath.Join(dir, "req.json")
	writeFile(t, reqFile, validRequest)
	exit, out = s.exec("image", "create", "--request", reqFile, "--idempotency-key", "e2e-key",
		"--wait", "--output", "json")
	require.Equal(t, 0, exit, out)
	assert.Contains(t, out, `"COMPLETED"`)
	assert.Contains(t, out, "2045019196159766531")

	// 5. request-status
	exit, out = s.exec("image", "request-status", "--idempotency-key", "e2e-key", "--output", "json")
	require.Equal(t, 0, exit)
	assert.Contains(t, out, "SUCCEEDED")

	// 6. get / cancel
	exit, _ = s.exec("image", "get", "2045019196159766531", "--output", "json")
	require.Equal(t, 0, exit)
	exit, _ = s.exec("image", "cancel", "2045019196159766531", "--output", "json")
	require.Equal(t, 0, exit)
	assert.True(t, backend.cancelled.Load())

	// 7. logout：只删本地
	exit, out = s.exec("auth", "logout", "--output", "json")
	require.Equal(t, 0, exit)
	assert.Contains(t, out, `"remoteSessionRevoked": false`)
	_, err = s.store.Get(cred.DeviceFlowAccount("local"))
	assert.ErrorIs(t, err, cred.ErrNotFound)

	// 全程输出兜底扫描（双保险；每步 exec 已各自断言过）
	assertNoSecrets(t, "全程输出", s.allOut.String())
}

// TestServerErrorMessageWithTokenIsRedacted covers redaction of hostile or
// buggy server messages that embed credentials.
func TestServerErrorMessageWithTokenIsRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"code":2001,"message":"invalid token qj_at_LEAKEDVALUE in request","data":null}`)
	}))
	defer srv.Close()

	app, stdout, stderr := imageApp(t, srv.URL)
	exit := run(app, []string{"image", "get", taskIDArg, "--output", "json"})
	assert.Equal(t, clierr.ExitAuth, exit)
	assert.NotContains(t, stdout.String(), "qj_at_LEAKEDVALUE")
	assert.Contains(t, stdout.String(), "qj_at_***")
	assert.NotContains(t, stderr.String(), "qj_at_LEAKEDVALUE")
}

// TestProfileFlagSelectsNamespace verifies --profile switches both config and
// credential namespaces.
func TestProfileFlagSelectsNamespace(t *testing.T) {
	store := cred.NewMemoryStore()
	require.NoError(t, store.Set(cred.PATAccount("beta"), &cred.Record{
		CredentialType: cred.TypePAT, AccessToken: "qj_pat_beta",
	}))
	app, stdout, _ := xdgApp(t, nil)
	withStore(app, store)

	exit := run(app, []string{"auth", "status", "--profile", "beta", "--output", "json"})
	require.Equal(t, 0, exit)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "beta", env.Meta["profile"])
	assert.Equal(t, "PAT", env.Data.(map[string]any)["credentialType"])
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
