package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/chatgpt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
	"testing"
)

func chatGPTTestContext(uid string) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	return context.WithValue(ctx, types.UserIDContextKey, uid)
}
func chatGPTTestModel(owner string) *types.Model {
	return &types.Model{ID: "personal", Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{ExtraConfig: map[string]string{chatGPTPlanKey: "true", chatGPTOwnerKey: owner}}}
}

type chatGPTListRepo struct {
	interfaces.ModelRepository
	rows []*types.Model
}

func (r *chatGPTListRepo) List(context.Context, uint64, types.ModelType, types.ModelSource) ([]*types.Model, error) {
	return r.rows, nil
}
func (r *chatGPTListRepo) GetByID(context.Context, uint64, string) (*types.Model, error) {
	return r.rows[0], nil
}

type countingChatGPTTokens struct{ calls int }

func (c *countingChatGPTTokens) AccessToken(context.Context, string) (string, error) {
	c.calls++
	return "never-send", nil
}

func TestChatGPTModelsArePersonalWithinSharedWorkspace(t *testing.T) {
	mine, other, shared := chatGPTTestModel("alice"), chatGPTTestModel("bob"), &types.Model{ID: "shared"}
	s := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{mine, other, shared}}}
	rows, err := s.ListModels(chatGPTTestContext("alice"))
	require.NoError(t, err)
	require.Equal(t, []*types.Model{mine, shared}, rows)
	s.repo = &chatGPTListRepo{rows: []*types.Model{other}}
	_, err = s.GetChatModel(chatGPTTestContext("alice"), "personal")
	require.Error(t, err)
	_, err = s.GetModelByID(chatGPTTestContext("alice"), "personal")
	require.Error(t, err)
}
func TestChatGPTCallsCannotReuseOwnersClientAsOtherPrincipal(t *testing.T) {
	tokens := &countingChatGPTTokens{}
	c := &personalChatGPTChat{accounts: tokens, owner: "alice", name: "model"}
	for _, ctx := range []context.Context{
		context.Background(), chatGPTTestContext("bob"),
		types.WithPrincipal(chatGPTTestContext("alice"), types.Principal{Type: types.PrincipalEmbedChannel, ID: "alice"}),
	} {
		_, err := c.Chat(ctx, nil, nil)
		require.Error(t, err)
		_, err = c.ChatStream(ctx, nil, nil)
		require.Error(t, err)
	}
	require.Zero(t, tokens.calls, "denied identities must never read the owner's credentials")
}
func TestChatGPTModelCannotBeConvertedToSharedOrHaveOwnerRewritten(t *testing.T) {
	s := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{chatGPTTestModel("alice")}}}
	err := s.UpdateModel(chatGPTTestContext("alice"), &types.Model{ID: "personal"})
	require.Error(t, err)
	err = s.CreateModel(chatGPTTestContext("alice"), chatGPTTestModel("bob"))
	require.Error(t, err)
}

type chatGPTCatalogueStub struct{ owner string }

func (s *chatGPTCatalogueStub) Models(_ context.Context, owner string) ([]chatgpt.Model, error) {
	s.owner = owner
	return []chatgpt.Model{{Slug: "available", DisplayName: "Available"}}, nil
}

type chatGPTCreateRepo struct {
	chatGPTListRepo
	creates int
}

func (r *chatGPTCreateRepo) Create(_ context.Context, m *types.Model) error {
	r.creates++
	r.rows = append(r.rows, m)
	return nil
}

func TestEnableChatGPTModelRequiresAccountCatalogAndStoresNoCredentials(t *testing.T) {
	account := &chatGPTCatalogueStub{}
	repo := &chatGPTCreateRepo{}
	ctx := chatGPTTestContext("alice")
	_, err := EnableChatGPTModel(ctx, account, repo, "not-available")
	require.Error(t, err)
	require.Zero(t, repo.creates)
	m, err := EnableChatGPTModel(ctx, account, repo, "available")
	require.NoError(t, err)
	require.Equal(t, "alice", account.owner)
	require.Equal(t, "alice", m.Parameters.ExtraConfig[chatGPTOwnerKey])
	require.Equal(t, uint64(7), m.TenantID)
	require.False(t, m.IsBuiltin)
	require.False(t, m.IsDefault)
	require.Empty(t, m.Parameters.APIKey)
	require.Empty(t, m.Parameters.AppSecret)
	require.Empty(t, m.Parameters.CustomHeaders)
	second, err := EnableChatGPTModel(ctx, account, repo, "available")
	require.NoError(t, err)
	require.Equal(t, m.ID, second.ID)
	require.Equal(t, 1, repo.creates)
	_, err = EnableChatGPTModel(context.Background(), account, repo, "available")
	require.Error(t, err)
	require.Equal(t, 1, repo.creates)
}
