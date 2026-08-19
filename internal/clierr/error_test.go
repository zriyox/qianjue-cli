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
		{"2015", 410, 2015, KindAuth, 3},
		{"2016", 409, 2016, KindAuth, 3},
		{"2017", 403, 2017, KindForbidden, 4},
		{"2018", 409, 2018, KindIdempotencyConflict, 6},
		{"2019", 409, 2019, KindInProgress, 7},
		{"2020", 409, 2020, KindRecoveryRequired, 8},
		{"2021", 409, 2021, KindFinalFailure, 9},
		{"3007", 400, 3007, KindInsufficientCredit, 9},
		{"400", 400, 400, KindInvalidRequest, 2},
		{"4001", 400, 4001, KindInvalidRequest, 2},
		{"4002", 400, 4002, KindInvalidRequest, 2},
		{"404", 404, 404, KindNotFound, 5},
		{"2014", 404, 2014, KindNotFound, 5},
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
	e := FromAPI(409, 2020, "m", data)
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
