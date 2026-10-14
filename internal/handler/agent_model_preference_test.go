package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type preferenceHandlerRepo struct {
	user, agent, model string
	tenant             uint64
	writes, clears     int
}

func (r *preferenceHandlerRepo) Get(_ context.Context, user string, tenant uint64, agent string) (string, error) {
	r.user = user
	r.tenant = tenant
	r.agent = agent
	return r.model, nil
}
func (r *preferenceHandlerRepo) Set(_ context.Context, user string, tenant uint64, agent, model string) error {
	r.user = user
	r.tenant = tenant
	r.agent = agent
	r.model = model
	r.writes++
	return nil
}
func (r *preferenceHandlerRepo) Clear(_ context.Context, user string, tenant uint64, agent string) error {
	r.user = user
	r.tenant = tenant
	r.agent = agent
	r.model = ""
	r.clears++
	return nil
}

type preferenceHandlerModels struct {
	interfaces.ModelService
	model     *types.Model
	denied    error
	authCalls int
}

func (m *preferenceHandlerModels) GetModelByID(context.Context, string) (*types.Model, error) {
	return m.model, nil
}
func (m *preferenceHandlerModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	m.authCalls++
	return nil, m.denied
}
func preferenceTestContext(body, agent string, ctx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("PUT", "/", strings.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: agent}}
	c.Set(types.TenantIDContextKey.String(), uint64(7))
	return c, rec
}
func TestAgentModelPreferenceOnlyWritesCurrentUser(t *testing.T) {
	repo := &preferenceHandlerRepo{}
	models := &preferenceHandlerModels{model: &types.Model{ID: "allowed", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}}
	h := &CustomAgentHandler{preferences: repo, modelService: models}
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "alice")
	c, rec := preferenceTestContext(`{"model_id":"allowed","user_id":"bob","tenant_id":99}`, types.GetBuiltinAgentIDs()[0], ctx)
	h.PutModelPreference(c)
	require.Empty(t, c.Errors)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "alice", repo.user)
	require.EqualValues(t, 7, repo.tenant)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, 1, models.authCalls)
	c, rec = preferenceTestContext(`{"model_id":""}`, types.GetBuiltinAgentIDs()[0], ctx)
	h.PutModelPreference(c)
	require.Empty(t, c.Errors)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, 1, repo.clears)
}
func TestAgentModelPreferenceRejectsUnauthorizedSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		user   string
		agent  string
		api    bool
		model  *types.Model
		denied error
	}{
		{name: "actorless", agent: types.GetBuiltinAgentIDs()[0]},
		{name: "api key", user: "alice", agent: types.GetBuiltinAgentIDs()[0], api: true},
		{name: "custom agent", user: "alice", agent: "custom-agent"},
		{name: "internal fixer", user: "alice", agent: types.BuiltinWikiFixerID},
		{name: "missing model", user: "alice", agent: types.GetBuiltinAgentIDs()[0]},
		{name: "wrong type", user: "alice", agent: types.GetBuiltinAgentIDs()[0], model: &types.Model{Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive}},
		{name: "inactive", user: "alice", agent: types.GetBuiltinAgentIDs()[0], model: &types.Model{Type: types.ModelTypeKnowledgeQA}},
		{name: "revoked", user: "alice", agent: types.GetBuiltinAgentIDs()[0], model: &types.Model{Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}, denied: errors.New("revoked")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &preferenceHandlerRepo{}
			h := &CustomAgentHandler{preferences: repo, modelService: &preferenceHandlerModels{model: tc.model, denied: tc.denied}}
			ctx := context.WithValue(context.Background(), types.UserIDContextKey, tc.user)
			if tc.api {
				ctx = types.WithAuditAPIKey(ctx, types.AuditAPIKey{ID: 1})
			}
			c, _ := preferenceTestContext(`{"model_id":"model"}`, tc.agent, ctx)
			h.PutModelPreference(c)
			require.NotEmpty(t, c.Errors)
			require.Zero(t, repo.writes)
			require.Zero(t, repo.clears)
		})
	}
}
