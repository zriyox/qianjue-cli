package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/buildinfo"
	"github.com/zriyox/qianjue-cli/internal/updatecheck"
)

type versionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	// 仅在 --check 时填充，且用 omitempty 保证普通 version 的输出形状不变。
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable,omitempty"`
	// CheckStatus 让 JSON 也不隐瞒检查结论（table 行只在 table 模式下可见）：
	// UPDATE_AVAILABLE / UP_TO_DATE / UNAVAILABLE / UNKNOWN。
	CheckStatus string `json:"checkStatus,omitempty"`
}

// version check 结论。
const (
	checkStatusUpdateAvailable = "UPDATE_AVAILABLE"
	checkStatusUpToDate        = "UP_TO_DATE"
	checkStatusUnavailable     = "UNAVAILABLE"
	checkStatusUnknown         = "UNKNOWN"
)

func newVersionCommand(app *appContext) *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "version",
		Short: "输出 CLI 版本信息",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data := versionData{
				Version:   buildinfo.Version,
				Commit:    buildinfo.Commit,
				BuildDate: buildinfo.BuildDate,
			}
			rows := [][2]string{
				{"Version", data.Version},
				{"Commit", data.Commit},
				{"Build date", data.BuildDate},
			}
			if !check {
				return app.printer.Success("version", data, map[string]any{}, rows)
			}

			// --check 是同步的：用户显式问了，就要给一个确定答复，不能用
			// 后台提示那种「查不到就沉默」的行为糊过去。
			result, err := runUpdateCheckNow(app)
			if err != nil && !errors.Is(err, updatecheck.ErrUnavailable) {
				return err
			}
			data.LatestVersion = result.Latest
			data.UpdateAvailable = result.Outdated
			switch {
			case errors.Is(err, updatecheck.ErrUnavailable):
				data.CheckStatus = checkStatusUnavailable
				rows = append(rows, [2]string{"Update check", "暂时无法确认（网络不可用或最近一次检查失败）"})
			case result.Latest == "":
				data.CheckStatus = checkStatusUnknown
				rows = append(rows, [2]string{"Update check", "发布地址没有可用的版本号"})
			case result.Outdated:
				data.CheckStatus = checkStatusUpdateAvailable
				rows = append(rows, [2]string{"Update check", "有新版本 " + result.Latest})
				rows = append(rows, [2]string{"Release", result.ReleaseURL})
			default:
				data.CheckStatus = checkStatusUpToDate
				rows = append(rows, [2]string{"Update check", "已是最新（最新发布 " + result.Latest + "）"})
			}
			return app.printer.Success("version", data, map[string]any{}, rows)
		},
	}
	c.Flags().BoolVar(&check, "check", false, "同时查询已发布的最新版本")
	return c
}
