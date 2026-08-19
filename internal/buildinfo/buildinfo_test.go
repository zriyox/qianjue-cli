package buildinfo

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func reset(t *testing.T) {
	t.Helper()
	v, c, d := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = v, c, d })
}

func stub(version string, settings ...debug.BuildSetting) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main:     debug.Module{Version: version},
			Settings: settings,
		}, true
	}
}

// `go install module@v0.1.0` 场景：没有 ldflags，版本与提交来自 Go 工具链记录。
func TestFillsFromToolchainWhenNotStamped(t *testing.T) {
	reset(t)
	Version, Commit, BuildDate = "dev", "unknown", "unknown"

	fillFromBuildInfo(stub("v0.1.0",
		debug.BuildSetting{Key: "vcs.revision", Value: "92b20a0abcdef1234567890"},
		debug.BuildSetting{Key: "vcs.time", Value: "2026-08-18T22:00:00Z"},
	))

	assert.Equal(t, "v0.1.0", Version)
	assert.Equal(t, "92b20a0", Commit, "commit 截断为 7 位")
	assert.Equal(t, "2026-08-18T22:00:00Z", BuildDate)
}

// Release 二进制：ldflags 已注入，工具链信息不得覆盖它。
func TestLdflagsStampWins(t *testing.T) {
	reset(t)
	Version, Commit, BuildDate = "v1.2.3", "deadbee", "2026-01-01"

	fillFromBuildInfo(stub("v0.1.0",
		debug.BuildSetting{Key: "vcs.revision", Value: "0000000aaaaaaaa"},
		debug.BuildSetting{Key: "vcs.time", Value: "2020-01-01T00:00:00Z"},
	))

	assert.Equal(t, "v1.2.3", Version)
	assert.Equal(t, "deadbee", Commit)
	assert.Equal(t, "2026-01-01", BuildDate)
}

// 本地 `go run` / 无 VCS 信息：不得凭空编造版本号。
func TestNeverFabricatesWhenToolchainSaysDevel(t *testing.T) {
	reset(t)
	Version, Commit, BuildDate = "dev", "unknown", "unknown"

	fillFromBuildInfo(stub("(devel)"))

	assert.Equal(t, "dev", Version)
	assert.Equal(t, "unknown", Commit)
	assert.Equal(t, "unknown", BuildDate)
}

func TestHandlesMissingBuildInfo(t *testing.T) {
	reset(t)
	Version, Commit, BuildDate = "dev", "unknown", "unknown"

	fillFromBuildInfo(func() (*debug.BuildInfo, bool) { return nil, false })

	assert.Equal(t, "dev", Version)
}
