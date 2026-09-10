package clierr

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExitCodesAreStable locks the numbers of cli-contract.md §23; renumbering
// any of them is a contract break.
func TestExitCodesAreStable(t *testing.T) {
	assert.Equal(t, 0, ExitOK)
	assert.Equal(t, 2, ExitUsage)
	assert.Equal(t, 3, ExitAuth)
	assert.Equal(t, 4, ExitForbidden)
	assert.Equal(t, 5, ExitNotFound)
	assert.Equal(t, 6, ExitIdempotencyConflict)
	assert.Equal(t, 7, ExitInProgressOrWaitTimeout)
	assert.Equal(t, 8, ExitRecoveryRequired)
	assert.Equal(t, 9, ExitFinalFailure)
	assert.Equal(t, 10, ExitTaskFailed)
	assert.Equal(t, 11, ExitTaskCancelled)
	assert.Equal(t, 12, ExitTransport)
	assert.Equal(t, 13, ExitServer)
	assert.Equal(t, 14, ExitLocalStorage)
	assert.Equal(t, 15, ExitModerationHold)
	assert.Equal(t, 16, ExitIdentityRequired)
	assert.Equal(t, 130, ExitInterrupted)
}

// TestFromAPIContractTable covers the full business-code table of
// cli-contract.md §24.
func TestFromAPIContractTable(t *testing.T) {
	cases := []struct {
		name       string
		httpStatus int
		code       int
		wantKind   Kind
		wantExit   int
	}{
		{"401", 401, 401, KindAuth, 3},
		{"2001", 401, 2001, KindAuth, 3},
		{"2002", 401, 2002, KindAuth, 3},
		{"2102", 410, 2102, KindAuth, 3},
		{"2103", 409, 2103, KindAuth, 3},
		{"2104", 403, 2104, KindForbidden, 4},
		{"2105", 409, 2105, KindIdempotencyConflict, 6},
		{"2106", 409, 2106, KindInProgress, 7},
		{"2107", 409, 2107, KindRecoveryRequired, 8},
		{"2108", 409, 2108, KindFinalFailure, 9},
		{"3007", 400, 3007, KindInsufficientCredit, 9},
		{"400", 400, 400, KindInvalidRequest, 2},
		{"4001", 400, 4001, KindInvalidRequest, 2},
		{"4002", 400, 4002, KindInvalidRequest, 2},
		{"404", 404, 404, KindNotFound, 5},
		{"2101", 404, 2101, KindNotFound, 5},
		{"3002", 404, 3002, KindNotFound, 5},
		{"500", 500, 500, KindServer, 13},
		{"501", 500, 501, KindServer, 13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := FromAPI(tc.httpStatus, tc.code, "m", nil)
			assert.Equal(t, tc.wantKind, e.Kind)
			assert.Equal(t, tc.wantExit, e.ExitCode)
			assert.Equal(t, tc.code, e.Code)
			assert.Equal(t, tc.httpStatus, e.HTTPStatus)
		})
	}
}

// TestFromAPIHTTPFallback covers unmapped business codes falling back to HTTP
// status semantics (e.g. filter-wrapped 1002/1004 arrive as HTTP 401).
func TestFromAPIHTTPFallback(t *testing.T) {
	cases := []struct {
		httpStatus int
		code       int
		wantKind   Kind
		wantExit   int
	}{
		{401, 1002, KindAuth, 3},
		{401, 1004, KindAuth, 3},
		{403, 9999, KindForbidden, 4},
		{404, 9999, KindNotFound, 5},
		{409, 9999, KindInvalidRequest, 2},
		{502, 0, KindServer, 13},
	}
	for _, tc := range cases {
		e := FromAPI(tc.httpStatus, tc.code, "m", nil)
		assert.Equal(t, tc.wantKind, e.Kind, "code=%d http=%d", tc.code, tc.httpStatus)
		assert.Equal(t, tc.wantExit, e.ExitCode)
	}
}

func TestFromAPIKeepsDetails(t *testing.T) {
	data := json.RawMessage(`{"requestId":2045019196159766532,"status":"RECOVERY_REQUIRED"}`)
	e := FromAPI(409, 2107, "m", data)
	assert.JSONEq(t, string(data), string(e.Details))
}

func TestAsCLIError(t *testing.T) {
	ce := Usage("bad flag")
	assert.Same(t, ce, AsCLIError(ce))
	assert.Equal(t, ExitUsage, ce.ExitCode)

	wrapped := AsCLIError(assert.AnError)
	assert.Equal(t, KindServer, wrapped.Kind)
	assert.Equal(t, ExitServer, wrapped.ExitCode)
	assert.Nil(t, AsCLIError(nil))
}

// 2016 是实名闸的业务码。它此前不在映射表里，被实名拦住的账号只会拿到一个
// 泛化的 HTTP 兜底错误，用户分不清是"要实名"还是"坏了"。现在它有专属 kind
// 和退出码，且错误信息里必须带上补救办法。
func TestIdentityRequiredCodeCarriesRemedy(t *testing.T) {
	e := FromAPI(403, 2016, "请先完成实名认证", nil)

	assert.Equal(t, KindIdentityRequired, e.Kind)
	assert.Equal(t, ExitIdentityRequired, e.ExitCode)
	assert.Contains(t, e.Message, "qianjue identity verify")
	// AI 代跑时最危险的是它自己去填身份证，文案必须挡住这一点
	assert.Contains(t, e.Message, "不要代填")
}

// 2014/2015 是实名流程内部的状态（已认证、发起过频），不该升级成 IDENTITY_REQUIRED
// 而把调用方引到"再去实名一次"。
func TestOtherIdentityCodesAreNotEscalated(t *testing.T) {
	for _, code := range []int{2014, 2015} {
		e := FromAPI(400, code, "x", nil)
		assert.Equal(t, KindInvalidRequest, e.Kind, "code=%d", code)
		assert.NotContains(t, e.Message, "qianjue identity verify", "code=%d", code)
	}
}
