package output

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactAllPrefixes(t *testing.T) {
	cases := map[string]string{
		"qj_at_AAAA-bbb_123":               "qj_at_***",
		"qj_rt_ZZZZzzzz":                   "qj_rt_***",
		"qj_pat_Secret_Value-1":            "qj_pat_***",
		"qj_dc_devicecode":                 "qj_dc_***",
		"Bearer qj_at_abc in header text":  "Bearer qj_at_*** in header text",
		"two qj_at_a1 and qj_rt_b2 tokens": "two qj_at_*** and qj_rt_*** tokens",
	}
	for in, want := range cases {
		assert.Equal(t, want, Redact(in))
	}
}

func TestRedactLeavesNonSecretsAlone(t *testing.T) {
	// 公开标识（session id 等）不在掩码范围内，保持可排查性。
	for _, s := range []string{"qj_ds_4d935b83e6de41c0", "qj_ps_abc", "plain text"} {
		assert.Equal(t, s, Redact(s))
	}
}
