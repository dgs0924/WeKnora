package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// modelRepository implements the model repository interface
type modelRepository struct {
	db *gorm.DB
}

// ReplaceCodexGrantsForUser atomically replaces one user's global Codex ACL.
func (r *modelRepository) ReplaceCodexGrantsForUser(ctx context.Context, owner, userID, grantedBy string, modelIDs []string) error {
	selected := make(map[string]struct{}, len(modelIDs))
	for _, id := range modelIDs {
		selected[id] = struct{}{}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []*types.Model
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("managed_by = ? AND is_builtin = ? AND type = ? AND status = ?", "codex", false, types.ModelTypeKnowledgeQA, types.ModelStatusActive).Find(&rows).Error; err != nil {
			return err
		}
		found := make(map[string]bool, len(selected))
		for _, row := range rows {
			if !isPersistedCodexModel(row, owner) {
				continue
			}
			_, chosen := selected[row.ID]
			if chosen {
				found[row.ID] = true
			}
		}
		if len(found) != len(selected) {
			return errors.New("one or more Codex models are unavailable")
		}
		if err := tx.Where("user_id = ? AND owner_user_id = ?", userID, owner).Delete(&types.CodexModelGrant{}).Error; err != nil {
			return err
		}
		for id := range selected {
			if err := tx.Create(&types.CodexModelGrant{ModelID: id, UserID: userID, OwnerUserID: owner, GrantedBy: grantedBy}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func isPersistedCodexModel(m *types.Model, owner string) bool {
	return m != nil && m.ManagedBy == "codex" && !m.IsBuiltin && m.Type == types.ModelTypeKnowledgeQA && m.Status == types.ModelStatusActive && m.Parameters.ExtraConfig["codex_plan"] == "true" && m.Parameters.ExtraConfig["codex_owner"] == owner
}

func (r *modelRepository) ListCodexModelsForOwner(ctx context.Context, owner string) ([]*types.Model, error) {
	var rows []*types.Model
	err := r.db.WithContext(ctx).Where("managed_by = ? AND is_builtin = ? AND type = ? AND status = ?", "codex", false, types.ModelTypeKnowledgeQA, types.ModelStatusActive).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*types.Model, 0)
	for _, row := range rows {
		if isPersistedCodexModel(row, owner) {
			out = append(out, row)
		}
	}
	return out, nil
}
func (r *modelRepository) ListGrantedCodexModels(ctx context.Context, owner, userID string) ([]*types.Model, error) {
	var rows []*types.Model
	err := r.db.WithContext(ctx).Joins("JOIN codex_model_grants g ON g.model_id = models.id").Where("g.user_id = ? AND g.owner_user_id = ? AND g.granted_by = ?", userID, owner, owner).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*types.Model, 0)
	for _, row := range rows {
		if isPersistedCodexModel(row, owner) {
			out = append(out, row)
		}
	}
	return out, nil
}
func (r *modelRepository) GetGrantedCodexModel(ctx context.Context, owner, userID, modelID string) (*types.Model, error) {
	var row types.Model
	err := r.db.WithContext(ctx).Joins("JOIN codex_model_grants g ON g.model_id = models.id").Where("models.id = ? AND g.user_id = ? AND g.owner_user_id = ? AND g.granted_by = ?", modelID, userID, owner, owner).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !isPersistedCodexModel(&row, owner) {
		return nil, nil
	}
	return &row, nil
}
func (r *modelRepository) ListCodexGrants(ctx context.Context, owner string) ([]*types.CodexModelGrant, error) {
	var rows []*types.CodexModelGrant
	err := r.db.WithContext(ctx).Where("owner_user_id = ? AND granted_by = ?", owner, owner).Find(&rows).Error
	return rows, err
}

// NewModelRepository creates a new model repository
func NewModelRepository(db *gorm.DB) interfaces.ModelRepository {
	return &modelRepository{db: db}
}

// Create creates a new model
func (r *modelRepository) Create(ctx context.Context, m *types.Model) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// GetByID retrieves a model by ID
func (r *modelRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.Model, error) {
	var m types.Model
	if err := r.db.WithContext(ctx).Where("id = ?", id).Where(
		"(tenant_id = ? OR is_builtin = true)", tenantID,
	).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// List lists models with optional filtering
func (r *modelRepository) List(
	ctx context.Context, tenantID uint64, modelType types.ModelType, source types.ModelSource,
) ([]*types.Model, error) {
	var models []*types.Model
	query := r.db.WithContext(ctx).Where(
		"(tenant_id = ? OR is_builtin = true)", tenantID,
	)

	if modelType != "" {
		query = query.Where("type = ?", modelType)
	}

	if source != "" {
		query = query.Where("source = ?", source)
	}

	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}

	return models, nil
}

// Update updates a model
func (r *modelRepository) Update(ctx context.Context, m *types.Model) error {
	// Use Select to explicitly update all fields, including zero values like false
	return r.db.WithContext(ctx).Debug().Model(&types.Model{}).Where(
		"id = ? AND tenant_id = ?", m.ID, m.TenantID,
	).Select("*").Updates(m).Error
}

// Delete deletes a model
func (r *modelRepository) Delete(ctx context.Context, tenantID uint64, id string) error {
	return r.db.WithContext(ctx).Where(
		"id = ? AND tenant_id = ?", id, tenantID,
	).Delete(&types.Model{}).Error
}

// ClearDefaultByType clears the default flag for all models of a specific type
// This is a batch operation that updates all matching records in one query
func (r *modelRepository) ClearDefaultByType(
	ctx context.Context,
	tenantID uint,
	modelType types.ModelType,
	excludeID string,
) error {
	query := r.db.WithContext(ctx).Model(&types.Model{}).Where(
		"tenant_id = ? AND type = ? AND is_default = ?", tenantID, modelType, true,
	)

	// If excludeID is provided, exclude that model from the update
	if excludeID != "" {
		query = query.Where("id != ?", excludeID)
	}

	// Batch update: set is_default to false for all matching records
	return query.Update("is_default", false).Error
}
