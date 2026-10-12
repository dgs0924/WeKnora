package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/chatgpt"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
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

type chatGPTCapabilityCatalogue struct{}

func (*chatGPTCapabilityCatalogue) Models(context.Context, string) ([]chatgpt.Model, error) {
	return []chatgpt.Model{
		{Slug: "vision", DisplayName: "Vision", SupportsVision: true},
		{Slug: "text", DisplayName: "Text"},
	}, nil
}

func TestEnableChatGPTModelKeepsChatIDAndSeparatesVisionType(t *testing.T) {
	repo := &chatGPTCreateRepo{}
	ctx := chatGPTTestContext("alice")
	chatModel, err := EnableChatGPTModelForType(ctx, &chatGPTCapabilityCatalogue{}, repo, "vision", types.ModelTypeKnowledgeQA)
	require.NoError(t, err)
	require.Equal(t, types.ModelTypeKnowledgeQA, chatModel.Type)
	require.True(t, chatModel.Parameters.SupportsVision)
	require.Equal(t, []string{"text", "image"}, chatModel.Parameters.Spec.Input)

	// This exact ID seed shipped with the original chat-only integration and
	// must remain stable when vision rows are added.
	existing, err := EnableChatGPTModel(ctx, &chatGPTCapabilityCatalogue{}, repo, "vision")
	require.NoError(t, err)
	require.Equal(t, chatModel.ID, existing.ID)

	visionModel, err := EnableChatGPTModelForType(ctx, &chatGPTCapabilityCatalogue{}, repo, "vision", types.ModelTypeVLLM)
	require.NoError(t, err)
	require.Equal(t, types.ModelTypeVLLM, visionModel.Type)
	require.NotEqual(t, chatModel.ID, visionModel.ID)
	require.True(t, visionModel.Parameters.SupportsVision)
	require.Empty(t, visionModel.Parameters.APIKey)
	require.Equal(t, 2, repo.creates)

	_, err = EnableChatGPTModelForType(ctx, &chatGPTCapabilityCatalogue{}, repo, "text", types.ModelTypeVLLM)
	require.Error(t, err)
	_, err = EnableChatGPTModelForType(ctx, &chatGPTCapabilityCatalogue{}, repo, "vision", types.ModelTypeEmbedding)
	require.Error(t, err)
	require.Equal(t, 2, repo.creates)
}

type recordingVisionChat struct {
	messages []chat.Message
	calls    int
}

func (c *recordingVisionChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	c.calls++
	c.messages = messages
	return &types.ChatResponse{Content: "described"}, nil
}
func (*recordingVisionChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}
func (*recordingVisionChat) GetModelName() string { return "vision" }
func (*recordingVisionChat) GetModelID() string   { return "vision-id" }

func TestPersonalChatGPTVisionConvertsImagesAndRejectsInvalidInput(t *testing.T) {
	inner := &recordingVisionChat{}
	model := &personalChatGPTVLM{chat: inner}
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 16))
	content, err := model.Predict(t.Context(), [][]byte{png}, "read this")
	require.NoError(t, err)
	require.Equal(t, "described", content)
	require.Equal(t, 1, inner.calls)
	require.Len(t, inner.messages, 1)
	require.Len(t, inner.messages[0].MultiContent, 2)
	require.Equal(t, "text", inner.messages[0].MultiContent[0].Type)
	require.Equal(t, "read this", inner.messages[0].MultiContent[0].Text)
	image := inner.messages[0].MultiContent[1]
	require.Equal(t, "image_url", image.Type)
	require.NotNil(t, image.ImageURL)
	require.True(t, strings.HasPrefix(image.ImageURL.URL, "data:image/png;base64,"))

	_, err = model.Predict(t.Context(), [][]byte{[]byte("plain text")}, "read this")
	require.Error(t, err)
	require.Equal(t, 1, inner.calls, "invalid image must be rejected before any provider call")
}

func TestGetChatGPTVisionModelChecksTypeOwnerAndRefreshesCredentials(t *testing.T) {
	tokens := &countingChatGPTTokens{}
	row := chatGPTTestModel("alice")
	row.Name = "vision"
	row.Type = types.ModelTypeVLLM
	row.Parameters.SupportsVision = true
	svc := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{row}}, chatGPT: tokens}

	resolved, err := svc.GetVLMModel(chatGPTTestContext("alice"), row.ID)
	require.NoError(t, err)
	adapter := resolved.(*personalChatGPTVLM)
	personal := adapter.chat.(*personalChatGPTChat)
	_, err = personal.client(chatGPTTestContext("alice"))
	require.NoError(t, err)
	_, err = personal.client(chatGPTTestContext("alice"))
	require.NoError(t, err)
	require.Equal(t, 2, tokens.calls, "each use must resolve the current OAuth access token")

	_, err = svc.GetVLMModel(chatGPTTestContext("bob"), row.ID)
	require.Error(t, err)
	require.Equal(t, 2, tokens.calls, "another user must not read the owner's credentials")
	_, err = svc.GetChatModel(chatGPTTestContext("alice"), row.ID)
	require.Error(t, err, "a VLLM row must not be used through the chat-model selector")

	row.Type = types.ModelTypeKnowledgeQA
	row.Parameters.SupportsVision = false
	_, err = svc.GetVLMModel(chatGPTTestContext("alice"), row.ID)
	require.Error(t, err, "a chat-only row must not be used as a VLLM")
}
