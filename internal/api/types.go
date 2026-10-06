package api

import "encoding/json"

// Device Flow session statuses (IntegrationSessionStatus.java).
const (
	SessionPending    = "PENDING"
	SessionAuthorized = "AUTHORIZED"
	SessionActive     = "ACTIVE"
	SessionDenied     = "DENIED"
	SessionExpired    = "EXPIRED"
	SessionRevoked    = "REVOKED"
)

// Idempotency request statuses (IntegrationIdempotencyStatus.java).
const (
	IdemProcessing       = "PROCESSING"
	IdemSucceeded        = "SUCCEEDED"
	IdemFailedRetryable  = "FAILED_RETRYABLE"
	IdemFailedFinal      = "FAILED_FINAL"
	IdemRecoveryRequired = "RECOVERY_REQUIRED"
)

// DrawTask statuses (TaskStatus.java).
const (
	TaskPending    = "PENDING"
	TaskProcessing = "PROCESSING"
	TaskCompleted  = "COMPLETED"
	TaskFailed     = "FAILED"
	TaskCancelled  = "CANCELLED"
	TaskTimeout    = "TIMEOUT"
)

// DeviceAuthCreateRequest is the POST /integration/device-auth body.
type DeviceAuthCreateRequest struct {
	ClientID   string   `json:"clientId"`
	ClientName string   `json:"clientName"`
	DeviceName string   `json:"deviceName,omitempty"`
	Scopes     []string `json:"scopes"`
	// Site is the user-site domain the authorization page must open on.
	// Empty keeps the official site (and is omitted, so older servers see
	// exactly the old request).
	Site string `json:"site,omitempty"`
}

// DeviceAuthCreateResult mirrors DeviceAuthCreateResult.java.
type DeviceAuthCreateResult struct {
	DeviceSessionID  string   `json:"deviceSessionId"`
	DeviceCode       string   `json:"deviceCode"`
	AuthorizationURL string   `json:"authorizationUrl"`
	ExpiresAt        *APITime `json:"expiresAt"`
	Scopes           []string `json:"scopes"`
	// Site is the site host the server accepted for the request's site; empty
	// when no site was requested or the server predates the field.
	Site string `json:"site"`
	// UserCode is the short code (e.g. BKTW-QZHM) the user must type on the
	// browser authorization page. It is never part of AuthorizationURL, so a
	// forwarded link alone cannot be used to authorize this CLI.
	UserCode string `json:"userCode"`
}

// DeviceAuthPollResult mirrors DeviceAuthPollResult.java. accessToken and
// refreshToken are non-null only on the single credentialsIssuedNow response.
type DeviceAuthPollResult struct {
	DeviceSessionID      string   `json:"deviceSessionId"`
	Status               string   `json:"status"`
	ExpiresAt            *APITime `json:"expiresAt"`
	AuthorizedAt         *APITime `json:"authorizedAt"`
	AccessToken          string   `json:"accessToken"`
	AccessTokenExpiresAt *APITime `json:"accessTokenExpiresAt"`
	RefreshToken         string   `json:"refreshToken"`
	CredentialsIssuedNow bool     `json:"credentialsIssuedNow"`
}

// TokenRefreshResult mirrors TokenRefreshResult.java.
type TokenRefreshResult struct {
	SessionID             string   `json:"sessionId"`
	AccessToken           string   `json:"accessToken"`
	AccessTokenExpiresAt  *APITime `json:"accessTokenExpiresAt"`
	RefreshToken          string   `json:"refreshToken"`
	RefreshTokenExpiresAt *APITime `json:"refreshTokenExpiresAt"`
}

// ImageTaskCreateResult mirrors UserIntegrationTaskFacade.ImageTaskCreateResult.
// The task payload is passed through verbatim so int64 IDs survive untouched.
type ImageTaskCreateResult struct {
	Task       json.RawMessage `json:"task"`
	Historical bool            `json:"historical"`
}

// RequestStatus mirrors IntegrationImageTaskRequestStatusDTO.java.
type RequestStatus struct {
	RequestID    *int64   `json:"requestId"`
	Status       string   `json:"status"`
	AttemptNo    *int     `json:"attemptNo"`
	ResourceType *string  `json:"resourceType"`
	ResourceID   *string  `json:"resourceId"`
	ErrorCode    *string  `json:"errorCode"`
	ErrorMessage *string  `json:"errorMessage"`
	Retryable    *bool    `json:"retryable"`
	RetryAfterAt *APITime `json:"retryAfterAt"`
	CompletedAt  *APITime `json:"completedAt"`
}
