package types

import "time"

// UserAgentModelPreference is a user's model selection for a public built-in
// agent. It deliberately does not modify the tenant-wide agent configuration.
type UserAgentModelPreference struct {
	UserID    string    `json:"user_id" gorm:"type:varchar(36);primaryKey"`
	TenantID  uint64    `json:"tenant_id" gorm:"primaryKey"`
	AgentID   string    `json:"agent_id" gorm:"type:varchar(64);primaryKey"`
	ModelID   string    `json:"model_id" gorm:"type:varchar(64);not null"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (UserAgentModelPreference) TableName() string { return "user_agent_model_preferences" }
