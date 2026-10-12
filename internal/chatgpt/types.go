// Package chatgpt implements the local Sign in with ChatGPT token-sharing flow.
//
// It deliberately owns its credential store and never discovers or imports
// credentials from Codex or another application.
package chatgpt

import (
	"errors"
	"time"
)

var (
	ErrNotConnected            = errors.New("ChatGPT is not connected")
	ErrAuthorizationFailed     = errors.New("ChatGPT authorization failed")
	ErrReauthenticationNeeded  = errors.New("ChatGPT authorization must be renewed")
	ErrInvalidLocalUser        = errors.New("invalid local user")
	ErrTokenSharingUnavailable = errors.New("ChatGPT token sharing was not granted")
)

type StartResult struct {
	AuthorizationURL string    `json:"authorization_url"`
	AttemptID        string    `json:"attempt_id"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type AttemptStatus string

const (
	AttemptPending   AttemptStatus = "pending"
	AttemptSucceeded AttemptStatus = "succeeded"
	AttemptFailed    AttemptStatus = "failed"
	AttemptExpired   AttemptStatus = "expired"
	AttemptCancelled AttemptStatus = "cancelled"
)

type PendingStatus struct {
	AttemptID string        `json:"attempt_id"`
	Status    AttemptStatus `json:"status"`
	ExpiresAt time.Time     `json:"expires_at"`
	Error     string        `json:"error,omitempty"`
}

type AccountStatus struct {
	SubjectHint string    `json:"subject_hint,omitempty"`
	EmailHint   string    `json:"email,omitempty"`
	Plan        string    `json:"plan,omitempty"`
	PlanEnabled bool      `json:"plan_enabled"`
	ExpiresAt   time.Time `json:"expires_at"`
	ConnectedAt time.Time `json:"connected_at"`
}

type Status struct {
	Connected bool           `json:"connected"`
	Account   *AccountStatus `json:"account,omitempty"`
	Pending   *PendingStatus `json:"pending,omitempty"`
}

type Model struct {
	Slug           string `json:"slug"`
	DisplayName    string `json:"display_name"`
	SupportsVision bool   `json:"supports_vision"`
}
