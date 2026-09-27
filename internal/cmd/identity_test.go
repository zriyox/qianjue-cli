package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

const faceURL = "https://faceid.qq.com/h5?BizToken=0A1B2C3D-4E5F-6071-8293-A4B5C6D7E8F9"

func faceEntryOutput(canDraw bool, noColor bool, env map[string]string) string {
	var stderr bytes.Buffer
	app := &appContext{
		flags:     &globalFlags{noColor: noColor},
		stderr:    &stderr,
		getenv:    func(k string) string { return env[k] },
		canDrawQR: func() bool { return canDraw },
	}
	printFaceVerifyEntry(app, faceURL)
	return stderr.String()
}

// 交互终端：画二维码，链接照样打印作兜底
func TestFaceVerifyEntryDrawsQRInTerminal(t *testing.T) {
	out := faceEntryOutput(true, false, nil)
	assert.Contains(t, out, "▀", "交互终端必须画出二维码")
	assert.Contains(t, out, "扫描下面的二维码")
	assert.Contains(t, out, faceURL, "二维码之外必须保留链接兜底")
}

// 非终端（AI 助手驱动 / 重定向）、--no-color、NO_COLOR：一律只给链接，不输出字符画
func TestFaceVerifyEntryFallsBackToLinkOnly(t *testing.T) {
	cases := map[string]string{
		"not a terminal": faceEntryOutput(false, false, nil),
		"--no-color":     faceEntryOutput(true, true, nil),
		"NO_COLOR":       faceEntryOutput(true, false, map[string]string{"NO_COLOR": "1"}),
	}
	for name, out := range cases {
		assert.NotContains(t, out, "▀", name)
		assert.NotContains(t, out, "\x1b[", "%s: 不得输出转义序列", name)
		assert.NotContains(t, out, "二维码", "%s: 没画二维码就不能让用户扫码", name)
		assert.Contains(t, out, faceURL, name)
	}
}
