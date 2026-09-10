package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
)

func newIdentityCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{
		Use:   "identity",
		Short: "实名认证（提交任务被实名闸拦住时用）",
		Long: "平台按后台策略要求实名后才能提交任务。实名要填身份证号并刷脸，是账号所有者本人的行为：\n" +
			"  qianjue identity status    查当前是否已实名、是否被强制要求\n" +
			"  qianjue identity verify    走完整流程（短信码 → 发起 → 手机扫码刷脸 → 轮询结果）\n\n" +
			"如果你是 AI 助手：把 verify 这条命令交给用户自己执行，不要代填身份信息。",
	}
	c.AddCommand(newIdentityStatusCommand(app), newIdentityVerifyCommand(app))
	return c
}

func newIdentityStatusCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "查询实名状态（姓名与证件号一律脱敏）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.stylingClientOrAuthed()
			if err != nil {
				return err
			}
			raw, err := client.GetIdentitySummary(cmd.Context())
			if err != nil {
				return err
			}
			return app.printer.Success("identity.status", raw, app.meta, identityStatusRows(raw))
		},
	}
}

func newIdentityVerifyCommand(app *appContext) *cobra.Command {
	var realName, idCard, smsCode string
	var pollTimeout string
	c := &cobra.Command{
		Use:   "verify",
		Short: "完成实名认证（交互式：终端出扫码链接，手机微信扫码刷脸）",
		Long: "流程与网页端一致：发短信码 → 提交姓名/身份证/短信码 → 拿到腾讯刷脸页地址 →\n" +
			"用手机微信扫码完成人脸核身 → 本地轮询结果。\n\n" +
			"身份信息只随本次请求发往平台，CLI 不落盘、不写日志、不回显。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 实名要填身份证并刷脸，必须是本人在自己终端操作 —— 与 moderation confirm 同一条红线
			if err := app.requireHumanAck(false, "实名认证"); err != nil {
				return err
			}
			client, err := app.stylingClientOrAuthed()
			if err != nil {
				return err
			}

			if smsCode == "" {
				if _, err := client.SendIdentityCode(cmd.Context()); err != nil {
					return err
				}
				app.printer.Progressf("验证码已发送到账号绑定的手机号")
			}

			reader := bufio.NewReader(app.stdin)
			if realName, err = promptIfEmpty(app, reader, realName, "真实姓名"); err != nil {
				return err
			}
			if idCard, err = promptIfEmpty(app, reader, idCard, "身份证号"); err != nil {
				return err
			}
			if smsCode, err = promptIfEmpty(app, reader, smsCode, "短信验证码"); err != nil {
				return err
			}

			started, err := client.StartIdentityVerify(cmd.Context(), realName, idCard, smsCode)
			if err != nil {
				return err
			}
			if started.URL == "" {
				return clierr.New(clierr.KindServer, "发起认证成功但未返回刷脸页地址，请稍后重试")
			}

			fmt.Fprintln(app.stderr)
			fmt.Fprintln(app.stderr, "用手机微信「扫一扫」打开下面的链接完成人脸核身：")
			fmt.Fprintln(app.stderr, "  "+started.URL)
			fmt.Fprintln(app.stderr, "（在 PC 浏览器直接打开会调用电脑摄像头，体验差；建议用手机扫）")
			fmt.Fprintln(app.stderr)

			timeout := 10 * time.Minute
			if pollTimeout != "" {
				d, perr := time.ParseDuration(pollTimeout)
				if perr != nil {
					return clierr.Usage("--poll-timeout 格式错误：%v", perr)
				}
				timeout = d
			}
			raw, err := pollIdentity(cmd.Context(), app, client, started.VerificationID.String(), timeout)
			if err != nil {
				return err
			}
			return app.printer.Success("identity.verify", raw, app.meta, identityStatusRows(raw))
		},
	}
	c.Flags().StringVar(&realName, "real-name", "", "真实姓名；省略时交互式询问")
	c.Flags().StringVar(&idCard, "id-card", "", "身份证号；省略时交互式询问（建议省略，避免留在 shell 历史里）")
	c.Flags().StringVar(&smsCode, "sms-code", "", "短信验证码；省略时自动发送并询问")
	c.Flags().StringVar(&pollTimeout, "poll-timeout", "", "等待刷脸完成的上限（默认 10m）")
	return c
}

// pollIdentity waits for the face-verification outcome. Tencent has no server
// callback, so the state is only ever learned by asking — same 3s cadence the
// web dialog uses.
func pollIdentity(ctx context.Context, app *appContext, client *api.Client, verificationID string,
	timeout time.Duration) (json.RawMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, raw, err := client.GetIdentityStatus(ctx, verificationID)
		if err != nil {
			return nil, err
		}
		switch strings.ToUpper(st.Status) {
		case "VERIFIED":
			app.printer.Progressf("实名认证已通过")
			return raw, nil
		case "FAILED", "EXPIRED":
			e := clierr.New(clierr.KindIdentityRequired,
				fmt.Sprintf("实名认证未通过（%s）%s；可重新运行 qianjue identity verify 再试一次",
					st.Status, suffixIfPresent(st.Message)))
			e.Details = raw
			return nil, e
		default:
			app.printer.Progressf("等待手机端完成人脸核身…")
		}
		if time.Now().After(deadline) {
			e := clierr.New(clierr.KindWaitTimeout,
				"等待实名认证超时；认证本身仍可在手机上继续完成，之后用 qianjue identity status 查看结果")
			e.Details = raw
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, clierr.Interrupted()
		case <-time.After(3 * time.Second):
		}
	}
}

func suffixIfPresent(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return ""
	}
	return "：" + msg
}

// promptIfEmpty asks for a value on stderr so `--output json` stdout stays clean.
func promptIfEmpty(app *appContext, reader *bufio.Reader, current, label string) (string, error) {
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current), nil
	}
	fmt.Fprintf(app.stderr, "%s: ", label)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", clierr.Usage("读取%s失败：%v", label, err)
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return "", clierr.Usage("%s不能为空", label)
	}
	return value, nil
}

func identityStatusRows(raw json.RawMessage) [][2]string {
	// 字段名对齐 summary 接口的真实返回（实测确认）
	var probe struct {
		Verified       *bool  `json:"verified"`
		VerifyRequired *bool  `json:"verifyRequired"`
		ForceEnabled   *bool  `json:"forceEnabled"`
		UsageCount     *int   `json:"usageCount"`
		UsageThreshold *int   `json:"usageThreshold"`
		Status         string `json:"status"`
		RealName       string `json:"realName"`
		IDCard         string `json:"idCard"`
	}
	_ = json.Unmarshal(raw, &probe)
	rows := [][2]string{}
	if probe.Verified != nil {
		rows = append(rows, [2]string{"已实名", boolText(*probe.Verified)})
	}
	if probe.VerifyRequired != nil {
		rows = append(rows, [2]string{"当前是否被要求实名", boolText(*probe.VerifyRequired)})
	}
	if probe.UsageCount != nil && probe.UsageThreshold != nil {
		rows = append(rows, [2]string{"提交次数 / 阈值",
			fmt.Sprintf("%d / %d", *probe.UsageCount, *probe.UsageThreshold)})
	}
	if probe.ForceEnabled != nil {
		rows = append(rows, [2]string{"平台强制开关", boolText(*probe.ForceEnabled)})
	}
	if probe.Status != "" {
		rows = append(rows, [2]string{"本次认证状态", probe.Status})
	}
	if probe.RealName != "" {
		rows = append(rows, [2]string{"姓名", probe.RealName})
	}
	if probe.IDCard != "" {
		rows = append(rows, [2]string{"证件号", probe.IDCard})
	}
	if len(rows) == 0 {
		rows = append(rows, [2]string{"结果", "见 JSON 输出"})
	}
	return rows
}

func boolText(b bool) string {
	if b {
		return "是"
	}
	return "否"
}
