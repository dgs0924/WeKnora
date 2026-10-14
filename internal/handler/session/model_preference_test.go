package session

import (
	"context"
	"errors"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

type personalModelRepo struct {
	interfaces.UserAgentModelPreferenceRepository
	calls int
}

func (r *personalModelRepo) Get(_ context.Context, user string, tenant uint64, agent string) (string, error) {
	r.calls++
	return "personal", nil
}

type personalModels struct {
	interfaces.ModelService
	denied error
}

func (m *personalModels) GetModelByID(context.Context, string) (*types.Model, error) {
	return &types.Model{ID: "personal", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}, nil
}
func (m *personalModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return nil, m.denied
}
func TestPersonalAgentModelUsesIsolatedCopyAndExplicitPriority(t *testing.T) {
	repo := &personalModelRepo{}
	models := &personalModels{}
	h := &Handler{modelPreferences: repo, modelService: models}
	original := &types.CustomAgent{ID: types.GetBuiltinAgentIDs()[0], TenantID: 7, Config: types.CustomAgentConfig{ModelID: "shared"}}
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "alice")
	got, model, err := h.applyPersonalAgentModel(ctx, original, 7, 0, false, "")
	require.NoError(t, err)
	require.NotSame(t, original, got)
	require.Equal(t, "personal", got.Config.ModelID)
	require.Equal(t, "personal", model)
	require.Equal(t, "shared", original.Config.ModelID)
	_, model, err = h.applyPersonalAgentModel(ctx, original, 7, 0, false, "explicit")
	require.NoError(t, err)
	require.Equal(t, "explicit", model)
	models.denied = errors.New("grant revoked")
	_, _, err = h.applyPersonalAgentModel(ctx, original, 7, 0, false, "")
	require.Error(t, err)
	require.Equal(t, "shared", original.Config.ModelID)
}
func TestPersonalAgentModelNeverAppliesAcrossSecurityScopes(t *testing.T) {
	user := context.WithValue(context.Background(), types.UserIDContextKey, "alice")
	key := types.WithAuditAPIKey(user, types.AuditAPIKey{ID: 1})
	for _, tc := range []struct {
		name           string
		ctx            context.Context
		agent          string
		tenant, source uint64
		shared         bool
	}{
		{"actorless", context.Background(), types.GetBuiltinAgentIDs()[0], 7, 0, false},
		{"api key", key, types.GetBuiltinAgentIDs()[0], 7, 0, false},
		{"cross workspace", user, types.GetBuiltinAgentIDs()[0], 8, 0, false},
		{"shared agent", user, types.GetBuiltinAgentIDs()[0], 7, 8, true},
		{"custom", user, "custom-agent", 7, 0, false},
		{"internal", user, types.BuiltinWikiFixerID, 7, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &personalModelRepo{}
			h := &Handler{modelPreferences: r}
			a := &types.CustomAgent{ID: tc.agent, TenantID: 7}
			got, model, err := h.applyPersonalAgentModel(tc.ctx, a, tc.tenant, tc.source, tc.shared, "explicit")
			require.NoError(t, err)
			require.Same(t, a, got)
			require.Equal(t, "explicit", model)
			require.Zero(t, r.calls)
		})
	}
}
