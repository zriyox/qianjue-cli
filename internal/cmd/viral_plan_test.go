package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

const viralPlanRequest = `{"businessType":"VIRAL_PLAN","initialQuery":"夏季男士POLO衫，主打透气","files":[{"type":"image","url":"https://cdn/p.png"}]}`

func TestViralPlanCreateReturnsFinishedScript(t *testing.T) {
	var sentKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentKey, gotPath = r.Header.Get(api.IdempotencyKeyHeader), r.URL.Path
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"result":{"conversationId":77,"messageId":"m-1","answer":"{\"scripts\":[{\"title\":\"t\"}]}","chargedCredits":50},"historical":false}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"viral-plan", "create", "--request", writeRequestFile(t, viralPlanRequest), "--output", "json"})
	require.Equal(t, 0, exit, "stdout: %s", stdout.String())

	assert.Equal(t, "/integration/viral-plan/scripts", gotPath)
	env := decodeEnvelope(t, stdout)
	assert.Equal(t, "viral-plan.create", env.Command)
	key := env.Data.(map[string]any)["idempotencyKey"].(string)
	assert.Equal(t, key, sentKey)
	// answer 就是完整稿子；chargedCredits 让调用方知道花了多少
	assert.Contains(t, stdout.String(), "scripts")
	assert.Contains(t, stdout.String(), "chargedCredits")

	log, err := idem.LoadLog(app.getenv, "default", key)
	require.NoError(t, err)
	assert.Equal(t, idem.OperationViralPlanCreate, log.Operation)
	assert.Equal(t, idem.StateSucceeded, log.State)
}

// 策划一轮 50 积分，未知结果时重发新 Key 等于白扣一次
func TestViralPlanCreateUnknownResultWarnsAboutDoubleCharge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	exit := run(app, []string{"viral-plan", "create", "--request", writeRequestFile(t, viralPlanRequest), "--output", "json"})

	assert.Equal(t, clierr.ExitTransport, exit)
	msg := decodeEnvelope(t, stdout).Error.Message
	assert.Contains(t, msg, "重复扣费")
	assert.Contains(t, msg, "--idempotency-key")
}

func TestViralPlanCreateRejectsSameKeyDifferentBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":200,"message":"操作成功","data":{"result":{"conversationId":77},"historical":false}}`)
	}))
	defer srv.Close()

	app, stdout, _ := imageApp(t, srv.URL)
	require.Equal(t, 0, run(app, []string{"viral-plan", "create",
		"--request", writeRequestFile(t, viralPlanRequest), "--idempotency-key", "vp-key", "--output", "json"}))
	stdout.Reset()
	exit := run(app, []string{"viral-plan", "create",
		"--request", writeRequestFile(t, `{"businessType":"VIRAL_PLAN","initialQuery":"别的"}`),
		"--idempotency-key", "vp-key", "--output", "json"})
	assert.Equal(t, clierr.ExitIdempotencyConflict, exit)
}
