package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// riskAckFlag is the explicit opt-in required to run a risk-bearing moderation
// decision from a non-interactive terminal.
//
// Why this exists: this CLI is normally driven by an AI agent (Codex, Claude
// Code, WorkBuddy) rather than typed by a person. "Ask for human review" and
// "acknowledge the risk and continue" both carry compliance weight and belong to
// the account owner, not to the agent acting on their behalf. Requiring an
// explicit flag when stdin is not a TTY stops an agent from clicking through by
// default; SKILL.md forbids agents from passing it. It cannot stop a determined
// caller — the PAT can reach the API directly — but it removes the accident.
const riskAckFlag = "i-understand-the-risk"

func newModerationCommand(app *appContext) *cobra.Command {
	m := &cobra.Command{
		Use:   "moderation",
		Short: "处理被内容审核拦截的任务（列出 / 申请人工审核 / 继续 / 取消）",
		Long: "任务命中视觉识别拦截后不会直接失败：任务保持 PENDING、积分保持冻结，等你决定。\n" +
			"CLI 提交的任务需要你显式申请人工审核（submit-review），平台通过后再 confirm 放行；\n" +
			"不想继续就 cancel，积分立即退回。超时未处理会自动按取消收口并退积分。",
	}
	m.AddCommand(newModerationListCommand(app))
	m.AddCommand(newModerationStatusCommand(app))
	m.AddCommand(newModerationSubmitReviewCommand(app))
	m.AddCommand(newModerationConfirmCommand(app))
	m.AddCommand(newModerationCancelCommand(app))
	return m
}

// validateRecordID keeps record ids numeric so a typo never becomes a path segment.
func validateRecordID(arg string) (string, error) {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return "", clierr.New(clierr.KindUsage, "记录 ID 不能为空")
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
		return "", clierr.New(clierr.KindUsage, fmt.Sprintf("记录 ID 必须是数字：%q", arg))
	}
	return trimmed, nil
}

// reviewStatusLabel renders the review state for humans. The empty status is the
// CLI-specific one and must never read as "queued", or the user would just wait
// for a review that was never requested until the hold expires.
func reviewStatusLabel(status string) string {
	switch status {
	case api.ReviewNotSubmitted:
		return "未申请人工审核"
	case api.ReviewPending:
		return "平台审核中"
	case api.ReviewApproved:
		return "平台已通过（可继续生成）"
	case api.ReviewRejected:
		return "平台已拒绝"
	default:
		return status
	}
}

func holdTableRows(h *api.ModerationHold) [][2]string {
	rows := [][2]string{
		{"记录 ID", h.RecordID},
		{"任务", strings.ToLower(h.TaskDomain) + "/" + h.TaskID},
		{"拦截原因", h.Reason},
		{"审核状态", reviewStatusLabel(h.ReviewStatus)},
	}
	if h.ReviewNote != "" {
		rows = append(rows, [2]string{"审核备注", h.ReviewNote})
	}
	if h.ExpiresAt != "" {
		rows = append(rows, [2]string{"决策截止", h.ExpiresAt})
	}
	for _, img := range h.FlaggedImages {
		rows = append(rows, [2]string{fmt.Sprintf("命中素材 %d", img.Index+1), img.URL + "（" + img.Reason + "）"})
	}
	rows = append(rows, [2]string{"下一步", nextActionHint(h)})
	return rows
}

// nextActionHint spells out the single next command. The agent reading this
// output should hand these to the user rather than run them itself.
func nextActionHint(h *api.ModerationHold) string {
	switch h.ReviewStatus {
	case api.ReviewNotSubmitted:
		return "qianjue moderation submit-review " + h.RecordID + "（申请人工审核） 或 cancel " + h.RecordID + "（放弃并退积分）"
	case api.ReviewPending:
		return "等待平台审核，可用 qianjue moderation status " + h.RecordID + " 查询进度"
	case api.ReviewApproved:
		return "qianjue moderation confirm " + h.RecordID + "（知悉风险并继续生成）"
	case api.ReviewRejected:
		return "平台已拒绝，任务已按失败收口并退回积分，无需操作"
	default:
		return "qianjue moderation status " + h.RecordID
	}
}

func newModerationListCommand(app *appContext) *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "列出所有待你决定的拦截记录",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.moderationClient()
			if err != nil {
				return err
			}
			holds, raw, err := client.ListModerationHolds(cmd.Context(), limit)
			if err != nil {
				return err
			}
			rows := make([][2]string, 0, len(holds)+1)
			if len(holds) == 0 {
				rows = append(rows, [2]string{"结果", "当前没有待处理的拦截记录"})
			}
			for _, h := range holds {
				hold := h
				rows = append(rows, [2]string{
					hold.RecordID,
					strings.ToLower(hold.TaskDomain) + "/" + hold.TaskID +
						"  " + reviewStatusLabel(hold.ReviewStatus) + "  " + hold.Reason,
				})
			}
			return app.printer.Success("moderation.list", raw, app.meta, rows)
		},
	}
	c.Flags().IntVar(&limit, "limit", 0, "最多返回条数（默认 20，最大 200）")
	return c
}

func newModerationStatusCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status <recordId>",
		Short: "查询单条拦截记录的审核进度",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordID, err := validateRecordID(args[0])
			if err != nil {
				return err
			}
			client, err := app.moderationClient()
			if err != nil {
				return err
			}
			hold, raw, err := client.GetModerationHold(cmd.Context(), recordID)
			if err != nil {
				return err
			}
			return app.printer.Success("moderation.status", raw, app.meta, holdTableRows(hold))
		},
	}
}

func newModerationSubmitReviewCommand(app *appContext) *cobra.Command {
	var ack bool
	c := &cobra.Command{
		Use:   "submit-review <recordId>",
		Short: "申请人工审核（只入队，不放行任务）",
		Long: "把拦截记录送进平台人工审核队列。只入队、不放行 —— 平台通过后仍需你执行 confirm。\n" +
			"这是需要你本人决定的动作：在非交互式终端下必须显式加 --" + riskAckFlag + "。",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordID, err := validateRecordID(args[0])
			if err != nil {
				return err
			}
			if err := app.requireHumanAck(ack, "申请人工审核"); err != nil {
				return err
			}
			client, err := app.moderationClient()
			if err != nil {
				return err
			}
			if err := client.SubmitModerationReview(cmd.Context(), recordID); err != nil {
				return err
			}
			return app.printer.Success("moderation.submit-review",
				map[string]any{"recordId": recordID, "reviewStatus": api.ReviewPending},
				app.meta,
				[][2]string{
					{"记录 ID", recordID},
					{"结果", "已提交人工审核"},
					{"下一步", "平台通过后执行 qianjue moderation confirm " + recordID + " 继续生成"},
				})
		},
	}
	c.Flags().BoolVar(&ack, riskAckFlag, false, "在非交互式终端下确认这是你本人的决定")
	return c
}

func newModerationConfirmCommand(app *appContext) *cobra.Command {
	var ack bool
	c := &cobra.Command{
		Use:   "confirm <recordId>",
		Short: "知悉风险并继续生成（需平台已审核通过）",
		Long: "确认你已获得素材授权、知悉风险，任务放行继续执行，操作会留痕（含 IP）。\n" +
			"平台未审核通过前无法继续。这是需要你本人决定的动作：\n" +
			"在非交互式终端下必须显式加 --" + riskAckFlag + "。",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordID, err := validateRecordID(args[0])
			if err != nil {
				return err
			}
			if err := app.requireHumanAck(ack, "知悉风险并继续生成"); err != nil {
				return err
			}
			client, err := app.moderationClient()
			if err != nil {
				return err
			}
			if err := client.ConfirmModerationHold(cmd.Context(), recordID); err != nil {
				return err
			}
			return app.printer.Success("moderation.confirm",
				map[string]any{"recordId": recordID},
				app.meta,
				[][2]string{
					{"记录 ID", recordID},
					{"结果", "已确认继续，任务放行回调度"},
				})
		},
	}
	c.Flags().BoolVar(&ack, riskAckFlag, false, "在非交互式终端下确认这是你本人的决定")
	return c
}

func newModerationCancelCommand(app *appContext) *cobra.Command {
	// cancel 不设人类闸：放弃任务是安全方向（退积分、不产出），
	// 让 agent 能自行收拾残局比拦住它更有价值。
	return &cobra.Command{
		Use:   "cancel <recordId>",
		Short: "放弃该任务并退回冻结的积分",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recordID, err := validateRecordID(args[0])
			if err != nil {
				return err
			}
			client, err := app.moderationClient()
			if err != nil {
				return err
			}
			if err := client.CancelModerationHold(cmd.Context(), recordID); err != nil {
				return err
			}
			return app.printer.Success("moderation.cancel",
				map[string]any{"recordId": recordID},
				app.meta,
				[][2]string{
					{"记录 ID", recordID},
					{"结果", "任务已取消，积分已退回"},
				})
		},
	}
}

// moderationClient resolves config and returns an authed client, mirroring what
// every other command does before touching the API.
func (a *appContext) moderationClient() (*api.Client, error) {
	resolved, err := a.resolveConfig()
	if err != nil {
		return nil, err
	}
	if err := resolved.RequireAPIBaseURL(); err != nil {
		return nil, err
	}
	return a.newAuthedClient(resolved)
}

// requireHumanAck blocks risk-bearing decisions when nobody is at the keyboard.
//
// An interactive terminal implies a person ran the command, so it passes. A
// non-interactive one (an AI agent, CI, a pipe) must carry the explicit flag —
// and SKILL.md tells agents to hand the command to the user instead of adding it.
func (a *appContext) requireHumanAck(ack bool, action string) error {
	if ack {
		return nil
	}
	if a.isTTY != nil && a.isTTY() {
		return nil
	}
	return clierr.New(clierr.KindUsage, fmt.Sprintf(
		"「%s」需要你本人确认，当前不是交互式终端。\n"+
			"如果你是 AI 助手：请把这条命令原样交给用户执行，不要自行加 --%s。\n"+
			"如果你确实在脚本里执行本人的决定：加上 --%s。",
		action, riskAckFlag, riskAckFlag))
}

// moderationHoldFromTask extracts the hold embedded in a unified task document.
// Returns nil when the task is not held.
func moderationHoldFromTask(raw json.RawMessage) *api.ModerationHold {
	var probe struct {
		ModerationHold *api.ModerationHold `json:"moderationHold"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	return probe.ModerationHold
}
