package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userAgentModelPreferenceRepository struct{ db *gorm.DB }

func NewUserAgentModelPreferenceRepository(db *gorm.DB) interfaces.UserAgentModelPreferenceRepository {
	return &userAgentModelPreferenceRepository{db: db}
}

func (r *userAgentModelPreferenceRepository) Get(ctx context.Context, userID string, tenantID uint64, agentID string) (string, error) {
	var row types.UserAgentModelPreference
	err := r.db.WithContext(ctx).Where("user_id = ? AND tenant_id = ? AND agent_id = ?", userID, tenantID, agentID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return row.ModelID, err
}

func (r *userAgentModelPreferenceRepository) Set(ctx context.Context, userID string, tenantID uint64, agentID, modelID string) error {
	row := &types.UserAgentModelPreference{UserID: userID, TenantID: tenantID, AgentID: agentID, ModelID: modelID}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "tenant_id"}, {Name: "agent_id"}}, DoUpdates: clause.AssignmentColumns([]string{"model_id", "updated_at"})}).Create(row).Error
}

func (r *userAgentModelPreferenceRepository) Clear(ctx context.Context, userID string, tenantID uint64, agentID string) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND tenant_id = ? AND agent_id = ?", userID, tenantID, agentID).Delete(&types.UserAgentModelPreference{}).Error
}
