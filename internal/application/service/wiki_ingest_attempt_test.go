package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The op records the parse attempt whose finalizing counter owns its slot,
// so a consumer can tell an op that outlived a reparse from a current one.
func TestNewWikiIngestPendingOpRecordsAttempt(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"attempt from context", withAttempt(context.Background(), 5), 5},
		{"no attempt", context.Background(), 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pendingOp, err := newWikiIngestPendingOp(test.ctx, 7, "kb-1", "knowledge-1")
			require.NoError(t, err)
			var op WikiPendingOp
			require.NoError(t, json.Unmarshal(pendingOp.Payload, &op))
			assert.Equal(t, test.want, op.Attempt)
		})
	}
}

// An op from a superseded attempt must not drain: reparse zeroed that
// attempt's counter, so the drain would release a slot of the new run and
// promote it before its own enrichment finished.
func TestFinalizeWikiSubtaskSkipsSupersededAttempt(t *testing.T) {
	tests := []struct {
		name       string
		attempt    int
		wantDrains int
	}{
		{name: "superseded attempt", attempt: 1},
		{name: "current attempt", attempt: 2, wantDrains: 1},
		{name: "op queued before attempts were recorded", attempt: 0, wantDrains: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &flakyFinalizeRepo{}
			svc := &wikiIngestService{knowledgeRepo: repo, spanTracker: newAttemptTracker(2)}

			svc.finalizeWikiSubtask(context.Background(), "k-1", test.attempt)

			assert.Equal(t, test.wantDrains, repo.successes)
		})
	}
}

// Model failures consume only the selected actor cohort's retry budget.
func TestWikiIngestModelFailureSettlesOnlySelectedActor(t *testing.T) {
	for _, modelErr := range []error{ErrModelNotFound, errors.New("base URL rejected")} {
		for _, exhausted := range []bool{false, true} {
			pending := newWikiActorFailureRepo(t, 1)
			if exhausted {
				pending.incrCount = wikiMaxFailRetries + 1
			}
			knowledge := &flakyFinalizeRepo{}
			svc := &wikiIngestService{
				kbService:    &wikiGuardKBService{kb: &types.KnowledgeBase{ID: "kb-1", IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}, WikiConfig: &types.WikiConfig{SynthesisModelID: "m-1"}}},
				modelService: &wikiUnavailableModelService{err: modelErr}, pendingRepo: pending, knowledgeRepo: knowledge,
			}
			payload, _ := json.Marshal(WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "kb-1"})
			err := svc.ProcessWikiIngest(context.Background(), asynq.NewTask(types.TypeWikiIngest, payload))
			require.ErrorIs(t, err, modelErr)
			require.Equal(t, []int64{1}, pending.incremented)
			if exhausted {
				require.Equal(t, []int64{1}, pending.deleted)
				require.Equal(t, 1, knowledge.successes)
			} else {
				require.Empty(t, pending.deleted)
				require.Equal(t, 0, knowledge.successes)
			}
		}
	}
}

type wikiActorFailureRepo struct {
	wikiPendingRepoForCleanupTest
	rows                 []*types.TaskPendingOp
	incremented, deleted []int64
}

func newWikiActorFailureRepo(t *testing.T, count int) *wikiActorFailureRepo {
	t.Helper()
	r := &wikiActorFailureRepo{}
	r.incrCount = count
	for i, user := range []string{"alice", "bob"} {
		body, err := json.Marshal(WikiPendingOp{Op: WikiOpIngest, KnowledgeID: user, Initiator: types.TaskInitiator{UserID: user, Role: types.TenantRoleAdmin}})
		require.NoError(t, err)
		r.rows = append(r.rows, &types.TaskPendingOp{ID: int64(i + 1), DedupKey: user, Payload: body})
	}
	return r
}
func (r *wikiActorFailureRepo) PeekBatch(context.Context, string, string, string, int) ([]*types.TaskPendingOp, error) {
	return r.rows, nil
}
func (r *wikiActorFailureRepo) IncrFailCount(_ context.Context, id int64) (int, error) {
	r.incremented = append(r.incremented, id)
	return r.incrCount, nil
}
func (r *wikiActorFailureRepo) DeleteByIDs(_ context.Context, ids []int64) error {
	r.deleted = append(r.deleted, ids...)
	return nil
}

type panickingKnowledgeService struct {
	interfaces.KnowledgeService
}

func (panickingKnowledgeService) GetKnowledgeByIDOnly(context.Context, string) (*types.Knowledge, error) {
	panic("unexpected nil row")
}

// errgroup does not recover panics: one escaping a map worker crashed the
// process before the batch settled its claims, and the op hit the same panic
// on every claim. As an error it goes through the fail_count budget instead.
func TestMapOneDocumentRecoveredTurnsPanicIntoError(t *testing.T) {
	svc := &wikiIngestService{knowledgeSvc: panickingKnowledgeService{}}

	result, updates, err := svc.mapOneDocumentRecovered(context.Background(), nil,
		WikiIngestPayload{TenantID: 7, KnowledgeBaseID: "kb-1"},
		WikiPendingOp{Op: WikiOpIngest, KnowledgeID: "k-1"}, nil)

	require.ErrorContains(t, err, "panicked")
	assert.Nil(t, result)
	assert.Nil(t, updates)
}
