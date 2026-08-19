package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

func decodeSingleDocument(t *testing.T, stdout *bytes.Buffer) Envelope {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var env Envelope
	require.NoError(t, dec.Decode(&env), "stdout must contain a JSON document")
	var extra any
	require.Error(t, dec.Decode(&extra), "stdout must contain exactly one JSON document")
	return env
}

func TestSuccessJSONEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr, FormatJSON, false, true)

	err := p.Success("version", map[string]any{"version": "1.0.0"}, nil, nil)
	require.NoError(t, err)

	env := decodeSingleDocument(t, &stdout)
	assert.Equal(t, "1", env.SchemaVersion)
	assert.Equal(t, "version", env.Command)
	assert.True(t, env.OK)
	assert.Nil(t, env.Error)
	assert.NotNil(t, env.Meta)
	assert.Empty(t, stderr.String())
}

func TestSuccessJSONRawMessagePassthroughKeepsInt64(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, &bytes.Buffer{}, FormatJSON, false, true)

	raw := json.RawMessage(`{"id":2045019196159766531}`)
	require.NoError(t, p.Success("image.get", raw, nil, nil))
	assert.Contains(t, stdout.String(), "2045019196159766531", "int64 ID 不得经 float64 变形")
}

func TestFailureJSONEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr, FormatJSON, false, true)

	apiErr := clierr.FromAPI(409, 2020, "图片任务创建结果不确定，请勿更换 Idempotency-Key 重复提交",
		json.RawMessage(`{"requestId":1,"status":"RECOVERY_REQUIRED"}`))
	exit := p.Failure("image.request-status", apiErr, map[string]any{"profile": "local"})

	assert.Equal(t, clierr.ExitRecoveryRequired, exit)
	env := decodeSingleDocument(t, &stdout)
	assert.False(t, env.OK)
	assert.Nil(t, env.Data)
	require.NotNil(t, env.Error)
	assert.Equal(t, "RECOVERY_REQUIRED", env.Error.Kind)
	assert.Equal(t, 409, env.Error.HTTPStatus)
	assert.Equal(t, 2020, env.Error.Code)
	assert.Equal(t, "local", env.Meta["profile"])
}

func TestFailureTableModeWritesStderrOnly(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr, FormatTable, false, true)

	exit := p.Failure("auth.status", clierr.New(clierr.KindAuth, "缺少凭证"), nil)
	assert.Equal(t, clierr.ExitAuth, exit)
	assert.Empty(t, stdout.String(), "table 模式错误不得写 stdout")
	assert.Contains(t, stderr.String(), "缺少凭证")
}

func TestFailureRedactsTokensInMessage(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, &bytes.Buffer{}, FormatJSON, false, true)

	p.Failure("auth.login", clierr.New(clierr.KindServer, "unexpected token qj_at_AbC-123_xyz in state"), nil)
	assert.NotContains(t, stdout.String(), "qj_at_AbC-123_xyz")
	assert.Contains(t, stdout.String(), "qj_at_***")
}

func TestProgressfQuietAndRedact(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := NewPrinter(&stdout, &stderr, FormatJSON, false, true)
	p.Progressf("polling with qj_dc_secretvalue123")
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "qj_dc_***")
	assert.NotContains(t, stderr.String(), "qj_dc_secretvalue123")

	stderr.Reset()
	q := NewPrinter(&stdout, &stderr, FormatJSON, true, true)
	q.Progressf("should be silent")
	assert.Empty(t, stderr.String())
}

func TestTableAlignment(t *testing.T) {
	var stdout bytes.Buffer
	p := NewPrinter(&stdout, &bytes.Buffer{}, FormatTable, false, true)
	p.Table([][2]string{{"Task ID", "123"}, {"Status", "COMPLETED"}})
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	assert.Len(t, lines, 2)
	assert.True(t, strings.HasPrefix(lines[0], "Task ID  "))
	assert.True(t, strings.HasPrefix(lines[1], "Status   "))
}

func TestParseFormat(t *testing.T) {
	f, err := ParseFormat("json")
	assert.NoError(t, err)
	assert.Equal(t, FormatJSON, f)
	_, err = ParseFormat("yaml")
	assert.Error(t, err)
	assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
}
