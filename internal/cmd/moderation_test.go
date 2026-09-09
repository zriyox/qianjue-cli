package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// The human gate is what keeps an AI agent from acknowledging a content-moderation
// risk on the account owner's behalf. Interactive terminal = a person ran it.
func TestRequireHumanAckAllowsInteractiveTerminal(t *testing.T) {
	app := &appContext{isTTY: func() bool { return true }}
	assert.NoError(t, app.requireHumanAck(false, "知悉风险并继续生成"))
}

// Non-interactive (an agent, CI, a pipe) must carry the explicit flag.
func TestRequireHumanAckBlocksNonInteractiveWithoutFlag(t *testing.T) {
	app := &appContext{isTTY: func() bool { return false }}

	err := app.requireHumanAck(false, "申请人工审核")

	require.Error(t, err)
	ce := clierr.AsCLIError(err)
	require.NotNil(t, ce)
	assert.Equal(t, clierr.KindUsage, ce.Kind)
	// 报错必须直接告诉 AI 该怎么办，否则它会自己猜（多半是加上标志继续）
	assert.Contains(t, err.Error(), "请把这条命令原样交给用户执行")
	assert.Contains(t, err.Error(), riskAckFlag)
}

func TestRequireHumanAckPassesWithExplicitFlag(t *testing.T) {
	app := &appContext{isTTY: func() bool { return false }}
	assert.NoError(t, app.requireHumanAck(true, "知悉风险并继续生成"))
}

// An empty review status is CLI-specific and must never read as "queued" —
// otherwise the user waits for a review nobody ever requested, until the hold
// expires and the credits are refunded.
func TestReviewStatusLabelNeverCallsUnsubmittedQueued(t *testing.T) {
	assert.Equal(t, "未申请人工审核", reviewStatusLabel(api.ReviewNotSubmitted))
	assert.Equal(t, "平台审核中", reviewStatusLabel(api.ReviewPending))
	assert.Equal(t, "平台已通过（可继续生成）", reviewStatusLabel(api.ReviewApproved))
	assert.Equal(t, "平台已拒绝", reviewStatusLabel(api.ReviewRejected))
}

func TestNextActionHintPerReviewStatus(t *testing.T) {
	cases := map[string]string{
		api.ReviewNotSubmitted: "submit-review 41207",
		api.ReviewPending:      "status 41207",
		api.ReviewApproved:     "confirm 41207",
		api.ReviewRejected:     "平台已拒绝",
	}
	for status, want := range cases {
		hold := &api.ModerationHold{RecordID: "41207", ReviewStatus: status}
		assert.Contains(t, nextActionHint(hold), want, "reviewStatus=%q", status)
	}
}

// task get is the only place a CLI user can discover the task is waiting on them
// rather than running, so the hold has to surface in its table output.
func TestUnifiedTableRowsSurfacesModerationHold(t *testing.T) {
	raw := json.RawMessage(`{"taskId":8812,"domain":"DRAW","status":"PENDING",
		"moderationHold":{"recordId":"41207","reason":"图2：疑似含受限内容","reviewStatus":""}}`)

	rows := unifiedTableRows(raw)

	flat := ""
	for _, r := range rows {
		flat += r[0] + "=" + r[1] + ";"
	}
	assert.Contains(t, flat, "41207")
	assert.Contains(t, flat, "图2：疑似含受限内容")
	assert.Contains(t, flat, "未申请人工审核")
	assert.Contains(t, flat, "积分已冻结未扣除")
}

func TestUnifiedTableRowsWithoutHoldStaysUnchanged(t *testing.T) {
	raw := json.RawMessage(`{"taskId":8812,"domain":"DRAW","status":"COMPLETED"}`)
	for _, r := range unifiedTableRows(raw) {
		assert.NotEqual(t, "内容审核", r[0])
	}
}

func TestValidateRecordIDRejectsNonNumeric(t *testing.T) {
	_, err := validateRecordID("41207; rm -rf /")
	require.Error(t, err)
	assert.Equal(t, clierr.KindUsage, clierr.AsCLIError(err).Kind)

	got, err := validateRecordID("  41207 ")
	require.NoError(t, err)
	assert.Equal(t, "41207", got)
}
