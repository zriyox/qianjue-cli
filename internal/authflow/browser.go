package authflow

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens the authorization URL with the platform default browser.
// Failures are non-fatal for login: the URL is always printed to stderr.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("不支持自动打开浏览器的平台: %s", runtime.GOOS)
	}
}
