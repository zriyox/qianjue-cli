package moderation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/zriyox/qianjue-cli/internal/api"
)

func TestNextStepsPerReviewStatus(t *testing.T) {
	cases := []struct {
		status  string
		want    []string
		without []string
	}{
		{api.ReviewNotSubmitted,
			[]string{"submit-review 41207", "confirm 41207", "cancel 41207", ContactSupport, "本人操作"},
			[]string{"审核中"}},
		{api.ReviewPending,
			[]string{"status 41207", "confirm 41207", "cancel 41207", ContactSupport},
			[]string{"submit-review"}},
		{api.ReviewApproved,
			[]string{"confirm 41207", "cancel 41207"},
			[]string{"submit-review"}},
		{api.ReviewRejected,
			[]string{"平台已拒绝", "（素材侵权）", "积分已退回", ContactSupport},
			[]string{"confirm", "submit-review", "cancel"}},
	}
	for _, tc := range cases {
		got := NextSteps("41207", tc.status, "素材侵权")
		for _, w := range tc.want {
			assert.Contains(t, got, w, "reviewStatus=%q", tc.status)
		}
		for _, w := range tc.without {
			assert.NotContains(t, got, w, "reviewStatus=%q", tc.status)
		}
	}
}

func TestSummaryKeepsFrozenCreditWording(t *testing.T) {
	got := Summary("draw/8812", "图2：疑似含受限内容", "2026-09-10T14:22:00")
	assert.Equal(t, "任务 draw/8812 被内容审核拦截（原因：图2：疑似含受限内容）。任务仍保留，积分已冻结未扣除；"+
		"2026-09-10T14:22:00 前未处理将自动取消并退回积分", got)

	bare := Summary("draw/8812", "", "")
	assert.Equal(t, "任务 draw/8812 被内容审核拦截。任务仍保留，积分已冻结未扣除", bare)
}
