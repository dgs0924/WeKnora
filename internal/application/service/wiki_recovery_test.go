package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type recoveryPendingFake struct {
	interfaces.TaskPendingOpsRepository
	calls int
}

func (r *recoveryPendingFake) RecoverLegacyWikiOps(context.Context, uint64, string, types.TaskInitiator) (int, int, error) {
	r.calls++
	return 2, 1, nil
}

func TestWikiRecoveryRejectsMissingIdentityAndModelBeforeMutation(t *testing.T) {
	repo := &recoveryPendingFake{}
	svc := &WikiRecoveryService{pending: repo}
	_, err := svc.Resume(context.Background(), "kb")
	require.Error(t, err)
	require.Zero(t, repo.calls)
	api := types.TaskInitiator{UserID: "alice", APIKeyID: 4}.Apply(context.Background())
	_, err = svc.Resume(api, "kb")
	require.Error(t, err)
	require.Zero(t, repo.calls)
	svc.kb = &wikiLookupKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 7, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}, WikiConfig: &types.WikiConfig{SynthesisModelID: "m"}}}
	denied := errors.New("model permission denied")
	svc.models = &wikiUnavailableModelService{err: denied}
	ctx := types.TaskInitiator{UserID: "alice", Role: types.TenantRoleAdmin}.Apply(context.Background())
	_, err = svc.Resume(ctx, "kb")
	require.ErrorIs(t, err, denied)
	require.Zero(t, repo.calls)
}

func TestWikiRecoveryRearmsAfterEnqueueFailure(t *testing.T) {
	repo := &recoveryPendingFake{}
	queue := &wikiEnqueueFailureTaskQueue{wikiErr: errors.New("redis unavailable")}
	svc := &WikiRecoveryService{pending: repo, tasks: queue,
		kb:     &wikiLookupKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 7, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}, SummaryModelID: "m"}},
		models: &wikiUnavailableModelService{}}
	ctx := types.TaskInitiator{UserID: "alice", Role: types.TenantRoleAdmin}.Apply(context.Background())
	_, err := svc.Resume(ctx, "kb")
	require.Error(t, err)
	require.Equal(t, 1, repo.calls)
	queue.wikiErr = nil
	result, err := svc.Resume(ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, 2, result.RecoveredCount)
	require.Equal(t, 1, result.SkippedCount)
	require.Equal(t, []string{types.TypeWikiIngest, types.TypeWikiIngest, types.TypeWikiFinalize}, queue.taskTypes)
}
