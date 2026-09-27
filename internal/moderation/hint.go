// Package moderation holds the user-facing wording for a task held by content
// moderation. task wait (exit 15), task get and the moderation commands all
// describe the same hold; keeping the text here stops those places drifting
// apart, which they had already started to do.
package moderation

import (
	"strings"

	"github.com/zriyox/qianjue-cli/internal/api"
)

// ContactSupport tells the user where a human can be reached. The CLI cannot
// show the support QR code itself — it is an image configured on the website —
// so this points at the website entry that shows it.
const ContactSupport = "在网页端点右上角「操作咨询」扫码联系客服"

// Summary states what happened. It keeps "积分已冻结未扣除" verbatim: agents and
// users both key on it to know nothing has been charged yet.
func Summary(label, reason, expiresAt string) string {
	var b strings.Builder
	b.WriteString("任务 " + label + " 被内容审核拦截")
	if strings.TrimSpace(reason) != "" {
		b.WriteString("（原因：" + strings.TrimSpace(reason) + "）")
	}
	b.WriteString("。任务仍保留，积分已冻结未扣除")
	if expiresAt != "" {
		b.WriteString("；" + expiresAt + " 前未处理将自动取消并退回积分")
	}
	return b.String()
}

// NextSteps spells out the exact commands for the current review state, so an
// agent relaying it never has to invent one. The not-submitted state must never
// read as "just wait": nothing is queued until the user asks for review, and
// waiting only runs the hold out to its expiry.
func NextSteps(recordID, reviewStatus, reviewNote string) string {
	submit := "`qianjue moderation submit-review " + recordID + "`"
	confirm := "`qianjue moderation confirm " + recordID + "`"
	status := "`qianjue moderation status " + recordID + "`"
	cancel := "不想继续可运行 `qianjue moderation cancel " + recordID + "`，积分立即退回。"

	switch reviewStatus {
	case api.ReviewNotSubmitted:
		return "以下需要你本人操作：① 运行 " + submit + " 申请人工审核；② " + ContactSupport +
			"，说明情况可加快审核；③ 平台审核通过后运行 " + confirm + " 继续生成。" + cancel
	case api.ReviewPending:
		return "平台人工审核中，" + ContactSupport + "可加快处理；用 " + status + " 查看进度，" +
			"审核通过后运行 " + confirm + " 继续生成。" + cancel
	case api.ReviewApproved:
		return "平台已审核通过，运行 " + confirm + " 知悉风险并继续生成。" + cancel
	case api.ReviewRejected:
		note := ""
		if strings.TrimSpace(reviewNote) != "" {
			note = "（" + strings.TrimSpace(reviewNote) + "）"
		}
		return "平台已拒绝" + note + "，任务已按失败处理、积分已退回，无需操作；如有疑问可" + ContactSupport + "。"
	default:
		return "运行 " + status + " 查看最新状态。"
	}
}
