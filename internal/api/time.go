package api

import (
	"fmt"
	"strings"
	"time"
)

// backendTimeLayout is the real backend JSON time format: the server
// serializes every LocalDateTime as "yyyy-MM-dd HH:mm:ss" without a timezone
// (WebConfig.java:39,57-58). RFC3339 is accepted as a defensive fallback.
const backendTimeLayout = "2006-01-02 15:04:05"

// APITime parses backend timestamps and renders RFC3339 in CLI output.
// Backend times carry no zone; they are interpreted in the CLI's local zone,
// which matches dev setups. The T-5min refresh window plus the 2002-triggered
// retry covers moderate clock/zone skew.
type APITime struct {
	time.Time
}

func (t *APITime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		t.Time = time.Time{}
		return nil
	}
	if parsed, err := time.ParseInLocation(backendTimeLayout, s, time.Local); err == nil {
		t.Time = parsed
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339, s); err == nil {
		t.Time = parsed
		return nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); err == nil {
		t.Time = parsed
		return nil
	}
	return fmt.Errorf("无法解析时间 %q", s)
}

func (t APITime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return []byte(`"` + t.Format(time.RFC3339) + `"`), nil
}
