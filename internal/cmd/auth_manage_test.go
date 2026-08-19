package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/cred"
)

func withStore(app *appContext, store cred.Store) *appContext {
	app.newStore = func() (cred.Store, error) { return store, nil }
	return app
}

func TestImportTokenHappyPath(t *testing.T) {
	store := cred.NewMemoryStore()
	app, stdout, stderr := xdgApp(t, nil)
	withStore(app, store)
	app.stdin = bytes.NewReader([]byte("qj_pat_secret123\n"))

	exit := run(app, []string{"auth", "import-token", "--type", "pat", "--stdin", "--output", "json"})
	require.Equal(t, 0, exit, "stderr: %s", stderr.String())

	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "auth.import-token", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, "PAT", data["credentialType"])
	assert.Equal(t, true, data["imported"])
	assert.NotContains(t, stdout.String(), "qj_pat_secret123", "不得回显 PAT")
	assert.NotContains(t, stderr.String(), "qj_pat_secret123")

	rec, err := store.Get(cred.PATAccount("default"))
	require.NoError(t, err)
	assert.Equal(t, "qj_pat_secret123", rec.AccessToken)
}

func TestImportTokenRejectsWrongInputs(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{"refresh token", []string{"auth", "import-token", "--type", "pat", "--stdin"}, "qj_rt_x"},
		{"access token", []string{"auth", "import-token", "--type", "pat", "--stdin"}, "qj_at_x"},
		{"garbage", []string{"auth", "import-token", "--type", "pat", "--stdin"}, "hello"},
		{"empty stdin", []string{"auth", "import-token", "--type", "pat", "--stdin"}, ""},
		{"missing --stdin", []string{"auth", "import-token", "--type", "pat"}, "qj_pat_x"},
		{"wrong type", []string{"auth", "import-token", "--type", "device", "--stdin"}, "qj_pat_x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := cred.NewMemoryStore()
			app, _, _ := xdgApp(t, nil)
			withStore(app, store)
			app.stdin = bytes.NewReader([]byte(tc.stdin))
			exit := run(app, append(tc.args, "--output", "json"))
			assert.Equal(t, clierr.ExitUsage, exit)
			assert.Empty(t, store.Accounts(), "拒绝时不得写入任何记录")
		})
	}
}

func TestAuthStatusDeviceFlow(t *testing.T) {
	store := cred.NewMemoryStore()
	require.NoError(t, store.Set(cred.DeviceFlowAccount("default"), &cred.Record{
		CredentialType:       cred.TypeDeviceFlow,
		AccessToken:          "qj_at_secret",
		AccessTokenExpiresAt: time.Now().Add(-time.Minute), // 已过期
		RefreshToken:         "qj_rt_secret",
		SessionExpiresAt:     time.Now().Add(20 * 24 * time.Hour),
		SessionID:            "qj_ds_s1",
		Scopes:               []string{"task.read"},
	}))
	app, stdout, _ := xdgApp(t, nil)
	withStore(app, store)

	exit := run(app, []string{"auth", "status", "--output", "json"})
	require.Equal(t, 0, exit)
	env := decodeEnvelope(t, stdout)
	data := env.Data.(map[string]any)
	assert.Equal(t, "keyring", data["credentialSource"])
	assert.Equal(t, "DEVICE_FLOW", data["credentialType"])
	assert.Equal(t, "qj_ds_s1", data["sessionId"])
	assert.Equal(t, true, data["accessTokenExpired"])
	assert.Equal(t, false, data["sessionExpired"])
	assert.NotContains(t, stdout.String(), "qj_at_secret")
	assert.NotContains(t, stdout.String(), "qj_rt_secret")
}

func TestAuthStatusEnvironmentToken(t *testing.T) {
	app, stdout, _ := xdgApp(t, map[string]string{"QIANJUE_TOKEN": "qj_pat_envtoken"})
	app.newStore = func() (cred.Store, error) { t.Fatal("env token 时不应打开凭证库"); return nil, nil }

	exit := run(app, []string{"auth", "status", "--output", "json"})
	require.Equal(t, 0, exit)
	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Equal(t, "environment", data["credentialSource"])
	assert.Equal(t, "PAT", data["credentialType"])
	assert.NotContains(t, stdout.String(), "qj_pat_envtoken")
}

func TestAuthStatusNone(t *testing.T) {
	app, stdout, _ := xdgApp(t, nil)
	withStore(app, cred.NewMemoryStore())
	exit := run(app, []string{"auth", "status", "--output", "json"})
	require.Equal(t, 0, exit)
	assert.Equal(t, "none", decodeEnvelope(t, stdout).Data.(map[string]any)["credentialSource"])
}

func TestAuthLogoutRemovesAllAccountsAndNeverClaimsRemoteRevoke(t *testing.T) {
	store := cred.NewMemoryStore()
	require.NoError(t, store.Set(cred.DeviceFlowAccount("default"), &cred.Record{
		CredentialType: cred.TypeDeviceFlow, AccessToken: "qj_at_1", RefreshToken: "qj_rt_1",
	}))
	require.NoError(t, store.Set(cred.PATAccount("default"), &cred.Record{
		CredentialType: cred.TypePAT, AccessToken: "qj_pat_1",
	}))

	app, stdout, _ := xdgApp(t, nil)
	withStore(app, store)
	exit := run(app, []string{"auth", "logout", "--output", "json"})
	require.Equal(t, 0, exit)
	data := decodeEnvelope(t, stdout).Data.(map[string]any)
	assert.Equal(t, true, data["localCredentialRemoved"])
	assert.Equal(t, false, data["remoteSessionRevoked"], "不得谎报远程撤销")
	assert.Empty(t, store.Accounts())

	// 再次 logout：没有可删的记录
	app2, stdout2, _ := xdgApp(t, nil)
	withStore(app2, store)
	exit = run(app2, []string{"auth", "logout", "--output", "json"})
	require.Equal(t, 0, exit)
	assert.Equal(t, false, decodeEnvelope(t, stdout2).Data.(map[string]any)["localCredentialRemoved"])
}
