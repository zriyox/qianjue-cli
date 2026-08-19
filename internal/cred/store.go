// Package cred stores CLI credentials in the operating-system credential
// store (macOS Keychain / Windows Credential Manager / Linux Secret Service).
// There is deliberately no plaintext-file fallback: when the system store is
// unavailable the CLI must fail safely (cli-contract.md §26).
package cred

import (
	"errors"
	"fmt"
	"time"
)

// ServiceName is the fixed credential-store service (cli-contract.md §8).
const ServiceName = "com.zriyo.qianjue.cli"

// Credential types stored in Record.CredentialType.
const (
	TypeDeviceFlow = "DEVICE_FLOW"
	TypePAT        = "PAT"
)

// DeviceFlowAccount is the store account for Device Flow credentials.
func DeviceFlowAccount(profile string) string { return profile + ":device-flow" }

// DeviceFlowStagingAccount holds the not-yet-committed rotation record.
func DeviceFlowStagingAccount(profile string) string { return profile + ":device-flow.staging" }

// PATAccount is the store account for an imported Personal Access Token.
func PATAccount(profile string) string { return profile + ":pat" }

// ErrNotFound is returned when no record exists for the account.
var ErrNotFound = errors.New("credential not found")

// Record is the JSON payload stored as the credential-store secret.
type Record struct {
	CredentialType       string    `json:"credentialType"` // TypeDeviceFlow | TypePAT
	AccessToken          string    `json:"accessToken"`
	AccessTokenExpiresAt time.Time `json:"accessTokenExpiresAt,omitempty"`
	RefreshToken         string    `json:"refreshToken,omitempty"`
	SessionExpiresAt     time.Time `json:"sessionExpiresAt,omitempty"`
	SessionID            string    `json:"sessionId,omitempty"`
	Scopes               []string  `json:"scopes,omitempty"`
}

// Validate rejects structurally broken records before they are persisted.
func (r *Record) Validate() error {
	switch r.CredentialType {
	case TypeDeviceFlow:
		if r.AccessToken == "" || r.RefreshToken == "" {
			return fmt.Errorf("device flow record 缺少 access/refresh token")
		}
	case TypePAT:
		if r.AccessToken == "" {
			return fmt.Errorf("PAT record 缺少 token")
		}
	default:
		return fmt.Errorf("未知 credentialType %q", r.CredentialType)
	}
	return nil
}

// Store abstracts the system credential store for testability.
type Store interface {
	Get(account string) (*Record, error)
	Set(account string, r *Record) error
	Delete(account string) error // deleting a missing record is not an error
}
