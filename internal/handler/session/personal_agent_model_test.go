package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type personalPreferenceStub struct {
	interfaces.UserAgentModelPreferenceRepository
	modelID  string
	userID   string
	tenantID uint64
	agentID  string
	err      error
}

func (s *personalPreferenceStub) Get(_ context.Context, userID string, tenantID uint64, agentID string) (string, error) {
	s.userID, s.tenantID, s.agentID = userID, tenantID, agentID
	return s.modelID, s.err
}

type personalModelStub struct {
	interfaces.ModelService
	model     *types.Model
	modelErr  error
	chatErr   error
	requested string
}

func (s *personalModelStub) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	s.requested = id
	return s.model, s.modelErr
}
func (s *personalModelStub) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	s.requested = id
	return nil, s.chatErr
}

func personalCtx() context.Context {
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "member")
	return context.WithValue(ctx, types.TenantIDContextKey, uint64(9))
}
func personalBuiltin() *types.CustomAgent {
	return &types.CustomAgent{ID: types.BuiltinQuickAnswerID, TenantID: 9, IsBuiltin: true, Config: types.CustomAgentConfig{ModelID: "shared"}}
}
func activeChatModel(id string) *types.Model {
	return &types.Model{ID: id, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}
}

func TestApplyPersonalAgentModelClonesAndHonorsExplicitOverride(t *testing.T) {
	prefs := &personalPreferenceStub{modelID: "personal"}
	models := &personalModelStub{model: activeChatModel("personal")}
	h := &Handler{modelPreferences: prefs, modelService: models}
	original := personalBuiltin()
	got, override, err := h.applyPersonalAgentModel(personalCtx(), original, 9, 0, false, "request-model")
	require.NoError(t, err)
	require.NotSame(t, original, got)
	require.Equal(t, "personal", got.Config.ModelID)
	require.Equal(t, "shared", original.Config.ModelID)
	require.Equal(t, "request-model", override)
	require.Equal(t, "member", prefs.userID)
	require.Equal(t, uint64(9), prefs.tenantID)
}

func TestApplyPersonalAgentModelRejectsRevokedPreference(t *testing.T) {
	prefs := &personalPreferenceStub{modelID: "personal"}
	models := &personalModelStub{model: activeChatModel("personal"), chatErr: errors.New("grant revoked")}
	h := &Handler{modelPreferences: prefs, modelService: models}
	got, _, err := h.applyPersonalAgentModel(personalCtx(), personalBuiltin(), 9, 0, false, "")
	require.Nil(t, got)
	require.ErrorContains(t, err, "no longer authorized")
}

func TestApplyPersonalAgentModelSkipsAPIsAndNonPublicOrOtherTenant(t *testing.T) {
	prefs := &personalPreferenceStub{modelID: "personal"}
	models := &personalModelStub{model: activeChatModel("personal")}
	h := &Handler{modelPreferences: prefs, modelService: models}
	ctx := types.WithTenantAPIKeyScope(personalCtx(), types.TenantAPIKeyScope{KeyID: 1})
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		agent  *types.CustomAgent
		source uint64
	}{
		{"api key", ctx, personalBuiltin(), 0},
		{"internal builtin", personalCtx(), &types.CustomAgent{ID: types.BuiltinWikiFixerID, TenantID: 9, IsBuiltin: true, Config: types.CustomAgentConfig{ModelID: "shared"}}, 0},
		{"other tenant", personalCtx(), personalBuiltin(), 77},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := h.applyPersonalAgentModel(tc.ctx, tc.agent, 9, tc.source, false, "")
			require.NoError(t, err)
			require.Same(t, tc.agent, got)
		})
	}
}
