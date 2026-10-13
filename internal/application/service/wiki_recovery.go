package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
)

type WikiRecoveryService struct {
	kb      interfaces.KnowledgeBaseService
	models  interfaces.ModelService
	pending interfaces.TaskPendingOpsRepository
	tasks   interfaces.TaskEnqueuer
	audit   interfaces.AuditLogService
}

func NewWikiRecoveryService(kb interfaces.KnowledgeBaseService, models interfaces.ModelService, pending interfaces.TaskPendingOpsRepository, tasks interfaces.TaskEnqueuer, audit interfaces.AuditLogService) *WikiRecoveryService {
	return &WikiRecoveryService{kb: kb, models: models, pending: pending, tasks: tasks, audit: audit}
}

type WikiRecoveryResult struct {
	RecoveredCount int `json:"recovered_count"`
	SkippedCount   int `json:"skipped_count"`
}

func (s *WikiRecoveryService) Resume(ctx context.Context, kbID string) (*WikiRecoveryResult, error) {
	actor := types.TaskInitiatorFromContext(ctx)
	_, apiScope := types.TenantAPIKeyScopeFromContext(ctx)
	_, apiAudit := types.AuditAPIKeyFromContext(ctx)
	if actor.UserID == "" || actor.APIKeyID != 0 || apiScope || apiAudit {
		return nil, fmt.Errorf("请使用已登录的个人账号恢复 Wiki 生成")
	}
	kb, err := s.kb.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}
	if kb == nil || !kb.IsWikiEnabled() {
		return nil, fmt.Errorf("请先启用 Wiki 并配置生成模型")
	}
	modelID := kb.SummaryModelID
	if kb.WikiConfig != nil && kb.WikiConfig.SynthesisModelID != "" {
		modelID = kb.WikiConfig.SynthesisModelID
	}
	if modelID == "" {
		return nil, fmt.Errorf("请先配置 Wiki 生成模型")
	}
	if _, err := s.models.GetChatModel(ctx, modelID); err != nil {
		return nil, err
	}
	repo, ok := s.pending.(interfaces.TaskPendingOpsWikiRecovery)
	if !ok || s.tasks == nil {
		return nil, fmt.Errorf("Wiki 恢复服务不可用")
	}
	recovered, skipped, err := repo.RecoverLegacyWikiOps(ctx, kb.TenantID, kb.ID, actor)
	if err != nil {
		return nil, err
	}
	RecordWikiContentActivity(ctx, s.audit, kb.TenantID, kb.ID, map[string]int{"recovered": recovered, "skipped": skipped})
	// Always rearm: a previous transaction may have succeeded while enqueue failed.
	if err := enqueueWikiIngestTrigger(ctx, s.tasks, kb.TenantID, kb.ID); err != nil {
		return nil, err
	}
	body, _ := json.Marshal(WikiIngestPayload{TenantID: kb.TenantID, KnowledgeBaseID: kb.ID, Language: types.LanguageFromContextOrDefault(ctx)})
	_, err = s.tasks.Enqueue(asynq.NewTask(types.TypeWikiFinalize, body, asynq.Queue(types.QueueWiki), asynq.MaxRetry(wikiIngestMaxRetry), asynq.Timeout(30*time.Minute), asynq.ProcessIn(wikiFinalizeDelay)))
	if err != nil {
		return nil, err
	}
	return &WikiRecoveryResult{RecoveredCount: recovered, SkippedCount: skipped}, nil
}
