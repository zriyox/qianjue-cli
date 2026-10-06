package qrterm

import (
	"regexp"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var cellPattern = regexp.MustCompile(`(?:\x1b\[(\d+);(\d+)m)?▀`)

// decode rebuilds the module grid from the drawing, so the test checks what a
// camera would actually see rather than the renderer's internals.
func decode(t *testing.T, drawing string) [][]bool {
	t.Helper()
	var grid [][]bool
	for _, line := range strings.Split(strings.TrimSuffix(drawing, "\n"), "\n") {
		require.True(t, strings.HasSuffix(line, reset), "每行必须以颜色复位结尾，否则会染到后面的输出")
		var top, bottom []bool
		fg, bg := "", ""
		for _, m := range cellPattern.FindAllStringSubmatch(line, -1) {
			if m[1] != "" {
				fg, bg = m[1], m[2]
			}
			top = append(top, fg == darkFG)
			bottom = append(bottom, bg == darkBG)
		}
		grid = append(grid, top, bottom)
	}
	return grid
}

func TestRenderDrawsExactlyTheEncodedModules(t *testing.T) {
	content := "https://faceid.qq.com/api/auth/getBizToken?BizToken=0A1B2C3D-4E5F-6071-8293-A4B5C6D7E8F9&RuleId=2"
	drawing, err := Render(content)
	require.NoError(t, err)

	code, err := qrcode.New(content, qrcode.Low)
	require.NoError(t, err)
	want := code.Bitmap()

	got := decode(t, drawing)
	require.GreaterOrEqual(t, len(got), len(want))
	for y := range want {
		assert.Equal(t, want[y], got[y], "row %d", y)
	}
	// 奇数行时最后半行必须是留白
	for y := len(want); y < len(got); y++ {
		for _, dark := range got[y] {
			assert.False(t, dark, "补齐的半行必须是浅色留白")
		}
	}
}

func TestRenderKeepsQuietZone(t *testing.T) {
	drawing, err := Render("https://example.com")
	require.NoError(t, err)
	grid := decode(t, drawing)
	for _, row := range grid[:4] {
		for _, dark := range row {
			assert.False(t, dark, "上方 4 格静区必须全浅色")
		}
	}
	for _, row := range grid {
		for x := 0; x < 4; x++ {
			assert.False(t, row[x], "左侧 4 格静区必须全浅色")
		}
	}
}

func TestRenderRejectsOversizedContent(t *testing.T) {
	_, err := Render(strings.Repeat("x", 5000))
	assert.Error(t, err, "超出二维码容量要返回错误，由调用方退回只打印链接")
}
