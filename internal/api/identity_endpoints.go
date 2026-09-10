package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// IdentityStartResult is what POST /integration/identity/verify/start returns:
// a Tencent face-verification page URL plus the id used to poll the outcome.
type IdentityStartResult struct {
	URL            string      `json:"url"`
	VerificationID json.Number `json:"verificationId"`
}

// IdentityStatus is one poll of the verification outcome. Tencent gives us no
// server callback, so the state is only ever discovered by asking.
type IdentityStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// GetIdentitySummary calls GET /integration/identity/summary.
func (c *Client) GetIdentitySummary(ctx context.Context) (json.RawMessage, error) {
	return c.identityGet(ctx, "/integration/identity/summary")
}

// SendIdentityCode calls POST /integration/identity/verify/code/send.
func (c *Client) SendIdentityCode(ctx context.Context) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodPost, "/integration/identity/verify/code/send", nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// StartIdentityVerify calls POST /integration/identity/verify/start.
//
// The caller sends their own name, ID number and SMS code; none of it is
// persisted by this CLI — it goes straight into the request body and is dropped.
func (c *Client) StartIdentityVerify(ctx context.Context, realName, idCard, smsCode string) (*IdentityStartResult, error) {
	body, err := json.Marshal(map[string]string{
		"realName": realName, "idCard": idCard, "smsCode": smsCode,
	})
	if err != nil {
		return nil, err
	}
	res, err := c.do(ctx, http.MethodPost, "/integration/identity/verify/start", nil, body, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	var out IdentityStartResult
	if err := json.Unmarshal(res.Data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetIdentityStatus calls GET /integration/identity/verify/status/{id}.
func (c *Client) GetIdentityStatus(ctx context.Context, verificationID string) (*IdentityStatus, json.RawMessage, error) {
	raw, err := c.identityGet(ctx, "/integration/identity/verify/status/"+url.PathEscape(verificationID))
	if err != nil {
		return nil, nil, err
	}
	var out IdentityStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

func (c *Client) identityGet(ctx context.Context, path string) (json.RawMessage, error) {
	res, err := c.do(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, err
	}
	if err := res.Err(); err != nil {
		return nil, err
	}
	return res.Data, nil
}
