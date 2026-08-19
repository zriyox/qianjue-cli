package idem

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// contractRequestJSON is the exact requestJson example of cli-contract.md §15;
// contractDigest is the digest the contract pins for it. This test locks our
// canonicalization against the contract.
const contractRequestJSON = `{
  "type": "TXT2IMG",
  "prompt": "一只简洁白底的橙色陶瓷马克杯，商业棚拍产品图，无文字，无水印",
  "modelCode": "GPT_IMAGE_2",
  "aspectRatio": "1:1",
  "outputResolution": "1K",
  "outputCount": 1
}`

const contractDigest = "c64ef509f3503fd71730531995e9b9182642d1d810b348f04e75cc7f7d489055"

func TestDigestMatchesContractExample(t *testing.T) {
	got, err := Digest([]byte(contractRequestJSON))
	require.NoError(t, err)
	assert.Equal(t, contractDigest, got)
}

func TestDigestIsOrderInsensitiveForObjectKeys(t *testing.T) {
	a := `{"b":1,"a":{"y":2,"x":3}}`
	b := `{"a":{"x":3,"y":2},"b":1}`
	da, err := Digest([]byte(a))
	require.NoError(t, err)
	db, err := Digest([]byte(b))
	require.NoError(t, err)
	assert.Equal(t, da, db)
}

func TestDigestIsArrayOrderSensitive(t *testing.T) {
	da, _ := Digest([]byte(`{"imgs":[{"url":"1"},{"url":"2"}]}`))
	db, _ := Digest([]byte(`{"imgs":[{"url":"2"},{"url":"1"}]}`))
	assert.NotEqual(t, da, db, "数组顺序参与指纹（对齐服务端语义）")
}

func TestCanonicalPreservesNumbersVerbatim(t *testing.T) {
	out, err := CanonicalJSON([]byte(`{"id":2045019196159766531,"f":1.50,"e":1e2}`))
	require.NoError(t, err)
	assert.Contains(t, string(out), "2045019196159766531", "大 int64 不得经 float64 变形")
	assert.Contains(t, string(out), "1.50", "数字字面量保持原样")
	assert.Contains(t, string(out), "1e2")
}

func TestCanonicalCompactAndSorted(t *testing.T) {
	out, err := CanonicalJSON([]byte(`{ "b" : "x" , "a" : [1, 2] }`))
	require.NoError(t, err)
	assert.Equal(t, `{"a":[1,2],"b":"x"}`, string(out))
}

func TestCanonicalRejectsInvalidJSON(t *testing.T) {
	for _, bad := range []string{`{`, `{"a":}`, `{"a":1}{"b":2}`} {
		_, err := CanonicalJSON([]byte(bad))
		require.Error(t, err, bad)
		assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
	}
}

func TestCheckForbiddenFieldsVariants(t *testing.T) {
	blocked := []string{
		`{"type":"TXT2IMG","token":"x"}`,
		`{"type":"TXT2IMG","access_token":"x"}`,
		`{"type":"TXT2IMG","Access-Token":"x"}`,
		`{"type":"TXT2IMG","AccessToken":"x"}`,
		`{"type":"TXT2IMG","nested":{"apiKey":"x"}}`,
		`{"type":"TXT2IMG","list":[{"REFRESH_TOKEN":"x"}]}`,
		`{"type":"TXT2IMG","deviceCode":"x"}`,
		`{"type":"TXT2IMG","credentials":{}}`,
	}
	for _, b := range blocked {
		err := CheckForbiddenFields([]byte(b))
		require.Error(t, err, b)
		assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
	}

	allowed := []string{
		`{"type":"TXT2IMG","prompt":"token of appreciation"}`, // 值不检查，只查字段名
		`{"type":"IMG2IMG","inputImages":[{"type":"image","url":"https://x/1.png"}]}`,
		`{"type":"TXT2IMG","tokenCount":1}`, // 归一化后是 tokencount，不在词表
	}
	for _, a := range allowed {
		assert.NoError(t, CheckForbiddenFields([]byte(a)), a)
	}
}

func TestCheckRequestShape(t *testing.T) {
	require.NoError(t, CheckRequestShape([]byte(`{"type":"TXT2IMG","prompt":"x"}`)))

	cases := []string{
		`[1,2]`,
		`"string"`,
		`{"prompt":"no type"}`,
		`{"type":""}`,
		`{"type":123}`,
		`not json`,
	}
	for _, c := range cases {
		err := CheckRequestShape([]byte(c))
		require.Error(t, err, c)
		assert.Equal(t, clierr.ExitUsage, clierr.AsCLIError(err).ExitCode)
	}

	big := make([]byte, MaxRequestBytes+1)
	err := CheckRequestShape(big)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "上限")
}
