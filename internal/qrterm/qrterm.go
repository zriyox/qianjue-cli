// Package qrterm draws a QR code in a terminal so a phone can scan it straight
// off the screen.
//
// Each character cell packs two QR rows with the upper-half block "▀": the
// foreground paints the top module and the background paints the bottom one.
// Both colors are set explicitly, so the code is always dark-on-light no matter
// the terminal theme — a plain "█" would come out inverted on dark themes, and
// WeChat does not reliably read inverted codes.
package qrterm

import (
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	darkFG  = "30"
	lightFG = "97"
	darkBG  = "40"
	lightBG = "107"
	reset   = "\x1b[0m"
)

// Render encodes content and returns the drawing, one terminal line per two QR
// rows, each line ending in an ANSI reset and a newline. The library's default
// 4-module quiet zone is kept: scanners need it and the terminal edge is not a
// substitute.
//
// Low error correction keeps the symbol as small as possible — the drawing is
// read off a screen, not printed on something that can get scratched.
func Render(content string) (string, error) {
	code, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", err
	}
	return renderBitmap(code.Bitmap()), nil
}

func renderBitmap(bitmap [][]bool) string {
	var b strings.Builder
	for y := 0; y < len(bitmap); y += 2 {
		lastFG, lastBG := "", ""
		for x := range bitmap[y] {
			fg := lightFG
			if bitmap[y][x] {
				fg = darkFG
			}
			bg := lightBG
			// an odd row count leaves the last line's bottom half as quiet zone
			if y+1 < len(bitmap) && bitmap[y+1][x] {
				bg = darkBG
			}
			if fg != lastFG || bg != lastBG {
				b.WriteString("\x1b[" + fg + ";" + bg + "m")
				lastFG, lastBG = fg, bg
			}
			b.WriteString("▀")
		}
		b.WriteString(reset + "\n")
	}
	return b.String()
}
