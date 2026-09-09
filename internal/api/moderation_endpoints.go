package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// ModerationHold mirrors the backend VisionModerationHoldDTO. It is only ever
// populated while a task sits in PENDING with its credits still held.
//
// ReviewStatus semantics differ between channels, and the empty case is the one
// that matters here: tasks submitted through this CLI start with NO review
// status, meaning the record has not entered the platform review queue yet and
// the user must explicitly run `qianjue moderation submit-review`. Web-submitted
// tasks are queued automatically and never show the empty state.
type ModerationHold struct {
	RecordID     string `json:"recordId"`
	TaskDomain   string `json:"taskDomain"`
	TaskID       string `json:"taskId"`
	Reason       string `json:"reason"`
	Score        *int   `json:"score,omitempty"`
	ReviewStatus string `json:"reviewStatus,omitempty"`
	ReviewNote   string `json:"reviewNote,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	CreatedAt    string `json:"createdAt,omitempty"`

	FlaggedImageURLs []string `json:"flaggedImageUrls,omitempty"`
	FlaggedImages    []struct {
		Index  int    `json:"index"`
		URL    string `json:"url"`
		Reason string `json:"reason"`
		Score  *int   `json:"score,omitempty"`
	} `json:"flaggedImages,omitempty"`
}

// Review status values as returned by the backend.
const (
	// ReviewNotSubmitted is the empty ReviewStatus: this CLI's records start here
	// and stay until the user explicitly asks for a human review.
	ReviewNotSubmitted = ""
	ReviewPending      = "PENDING_REVIEW"
	ReviewApproved     = "APPROVED"
	ReviewRejected     = "REJECTED"
)

// ListModerationHolds calls GET /integration/moderation/holds.
//
// An AI agent's session often ends long before a human review completes, so the
// user needs a way to enumerate unresolved holds in a brand new session and pick
// up where things stopped. The web UI gets this pushed over SSE instead.
func (c *Client) ListModerationHolds(ctx context.Context, limit int) ([]ModerationHold, json.RawMessage, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/integration/moderation/holds"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	res, err := c.do(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, nil, err
	}
	if err := res.Err(); err != nil {
		return nil, nil, err
	}
	var holds []ModerationHold
	if err := json.Unmarshal(res.Data, &holds); err != nil {
		return nil, nil, clierr.Transport(err)
	}
	return holds, res.Data, nil
}

// GetModerationHold calls GET /integration/moderation/holds/{recordId}.
func (c *Client) GetModerationHold(ctx context.Context, recordID string) (*ModerationHold, json.RawMessage, error) {
	path := "/integration/moderation/holds/" + url.PathEscape(recordID)
	res, err := c.do(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, nil, err
	}
	if err := res.Err(); err != nil {
		return nil, nil, err
	}
	var hold ModerationHold
	if err := json.Unmarshal(res.Data, &hold); err != nil {
		return nil, nil, clierr.Transport(err)
	}
	return &hold, res.Data, nil
}

// SubmitModerationReview calls POST /integration/moderation/holds/{id}/submit-review.
// It only enqueues the record for human review; it does NOT release the task.
func (c *Client) SubmitModerationReview(ctx context.Context, recordID string) error {
	return c.postModerationDecision(ctx, recordID, "submit-review")
}

// ConfirmModerationHold calls POST /integration/moderation/holds/{id}/confirm,
// releasing the task once the platform has approved the record.
func (c *Client) ConfirmModerationHold(ctx context.Context, recordID string) error {
	return c.postModerationDecision(ctx, recordID, "confirm")
}

// CancelModerationHold calls POST /integration/moderation/holds/{id}/cancel,
// failing the task and refunding the held credits.
func (c *Client) CancelModerationHold(ctx context.Context, recordID string) error {
	return c.postModerationDecision(ctx, recordID, "cancel")
}

func (c *Client) postModerationDecision(ctx context.Context, recordID, action string) error {
	path := "/integration/moderation/holds/" + url.PathEscape(recordID) + "/" + action
	res, err := c.do(ctx, http.MethodPost, path, nil, nil, true)
	if err != nil {
		return err
	}
	return res.Err()
}
