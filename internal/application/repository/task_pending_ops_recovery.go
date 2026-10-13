package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecoverLegacyWikiOps assigns a real caller only to unclaimed legacy rows
// whose payload has no human or API-key initiator. It deliberately does not
// touch counters, document state, or already-attributed work.
func (r *taskPendingOpsRepository) RecoverLegacyWikiOps(ctx context.Context, tenantID uint64, kbID string, initiator types.TaskInitiator) (int, int, error) {
	if initiator.UserID == "" || initiator.APIKeyID != 0 || initiator.APIKeyName != "" {
		return 0, 0, fmt.Errorf("real user required")
	}
	recovered, skipped := 0, 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var kb types.KnowledgeBase
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", kbID, tenantID).First(&kb).Error; err != nil {
			return err
		}
		if !kb.IsWikiEnabled() {
			return fmt.Errorf("wiki disabled")
		}
		var rows []*types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND scope = ? AND scope_id = ? AND ((task_type = ? AND op IN ?) OR (task_type = ? AND op IN ?))", tenantID, "knowledge_base", kbID, "wiki:ingest", []string{"ingest", "retract"}, "wiki:finalize", []string{"slug", "change", "folder_prune"}).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.ClaimedAt != nil {
				skipped++
				continue
			}
			var payload map[string]json.RawMessage
			if json.Unmarshal(row.Payload, &payload) != nil || payload == nil {
				skipped++
				continue
			}
			var old types.TaskInitiator
			if raw := payload["initiator"]; len(raw) > 0 {
				if json.Unmarshal(raw, &old) != nil {
					skipped++
					continue
				}
			}
			if (old.UserID != "" || old.APIKeyID != 0 || old.APIKeyName != "") && old != initiator {
				skipped++
				continue
			}
			if row.TaskType == "wiki:ingest" {
				var kid string
				if json.Unmarshal(payload["knowledge_id"], &kid) != nil || kid == "" || kid != row.DedupKey {
					skipped++
					continue
				}
				var knowledge types.Knowledge
				query := tx.Unscoped().Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", kid, tenantID, kbID)
				if row.Op == "ingest" {
					query = query.Where("deleted_at IS NULL AND parse_status = ? AND pending_subtasks_count > 0", types.ParseStatusFinalizing)
				}
				if err := query.First(&knowledge).Error; err != nil {
					if err == gorm.ErrRecordNotFound {
						skipped++
						continue
					}
					return err
				}
				if row.Op == "ingest" {
					var attempt int
					if raw := payload["attempt"]; len(raw) > 0 {
						if json.Unmarshal(raw, &attempt) != nil {
							skipped++
							continue
						}
					}
					var latest int
					if err := tx.Model(&types.KnowledgeProcessingSpan{}).Where("knowledge_id = ?", kid).Select("COALESCE(MAX(attempt),0)").Scan(&latest).Error; err != nil {
						return err
					}
					if attempt != latest {
						skipped++
						continue
					}
				}
			}
			encoded, err := json.Marshal(initiator)
			if err != nil {
				return err
			}
			payload["initiator"] = encoded
			body, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if err := tx.Model(&types.TaskPendingOp{}).Where("id = ? AND claimed_at IS NULL", row.ID).Updates(map[string]interface{}{"payload": body, "fail_count": 0}).Error; err != nil {
				return err
			}
			recovered++
		}

		// Rebuild missing Wiki content from already processed chunks only. Locking
		// the KB serializes repeated recovery requests without touching parse counters.
		var pages []types.WikiPage
		if err := tx.Select("source_refs").Where("knowledge_base_id = ? AND tenant_id = ?", kbID, tenantID).Find(&pages).Error; err != nil {
			return err
		}
		represented := map[string]bool{}
		for _, page := range pages {
			for _, id := range page.SourceKnowledgeIDs() {
				represented[id] = true
			}
		}
		for _, row := range rows {
			if row.TaskType == "wiki:ingest" && row.Op == "ingest" {
				represented[row.DedupKey] = true
			}
		}
		var docs []types.Knowledge
		if err := tx.Where("knowledge_base_id = ? AND tenant_id = ? AND deleted_at IS NULL AND parse_status = ?", kbID, tenantID, types.ParseStatusCompleted).Find(&docs).Error; err != nil {
			return err
		}
		for _, doc := range docs {
			if represented[doc.ID] {
				continue
			}
			var chunks int64
			if err := tx.Model(&types.Chunk{}).Where("knowledge_id = ? AND deleted_at IS NULL", doc.ID).Count(&chunks).Error; err != nil {
				return err
			}
			if chunks == 0 {
				continue
			}
			var attempt int
			if err := tx.Model(&types.KnowledgeProcessingSpan{}).Where("knowledge_id = ?", doc.ID).Select("COALESCE(MAX(attempt),0)").Scan(&attempt).Error; err != nil {
				return err
			}
			body, err := json.Marshal(map[string]interface{}{"op": "ingest", "knowledge_id": doc.ID, "attempt": attempt, "initiator": initiator, "language": types.LanguageFromContextOrDefault(ctx)})
			if err != nil {
				return err
			}
			if err := tx.Create(&types.TaskPendingOp{TenantID: tenantID, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: kbID, Op: "ingest", DedupKey: doc.ID, Payload: body, EnqueuedAt: time.Now()}).Error; err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, skipped, err
}
