package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/chatgpt"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/openairesponses"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const chatGPTPlanKey = "chatgpt_plan"
const chatGPTOwnerKey = "chatgpt_owner"

// ChatGPTUser derives the owner from the authenticated web principal, never
// from a request parameter or a shared workspace's model configuration.
func ChatGPTUser(ctx context.Context) (string, error) {
	uid, ok := types.UserIDFromContext(ctx)
	p, valid := types.PrincipalFromContext(ctx)
	_, apiKey := types.AuditAPIKeyFromContext(ctx)
	if !ok || !valid || apiKey || p.Type != types.PrincipalWebUser || p.ID != uid {
		return "", apperrors.NewForbiddenError("ChatGPT plan requires an authenticated personal account")
	}
	return uid, nil
}

func isChatGPTModel(m *types.Model) bool {
	return m != nil && m.Parameters.ExtraConfig[chatGPTPlanKey] == "true"
}

// isPersonalPlanModel identifies provider-managed plan references. These rows
// never contain customer credentials and cannot be created, copied, edited or
// shared through the ordinary model APIs.
func isPersonalPlanModel(m *types.Model) bool {
	return isChatGPTModel(m) || isCodexModel(m)
}

func ownsChatGPTModel(ctx context.Context, m *types.Model) bool {
	uid, err := ChatGPTUser(ctx)
	return err == nil && uid == m.Parameters.ExtraConfig[chatGPTOwnerKey] && !m.IsBuiltin
}

func ownsPersonalPlanModel(ctx context.Context, m *types.Model) bool {
	if isChatGPTModel(m) {
		return ownsChatGPTModel(ctx, m)
	}
	return ownsCodexModel(ctx, m)
}

// NewModelServiceWithChatGPT leaves the existing constructor usable by tests
// and wires the optional account runtime into the application container.
func NewModelServiceWithChatGPT(repo interfaces.ModelRepository, kb interfaces.KnowledgeBaseRepository,
	agents interfaces.CustomAgentRepository, ol *ollama.OllamaService, pool embedding.EmbedderPooler,
	tenants interfaces.TenantService, accounts *chatgpt.Manager,
) interfaces.ModelService {
	s := NewModelService(repo, kb, agents, ol, pool, tenants).(*modelService)
	s.chatGPT = accounts
	return s
}

// EnableChatGPTModel creates a credential-free personal model reference in the
// active workspace. Only the account's current visible catalog can be selected.
type chatGPTModelCatalogue interface {
	Models(context.Context, string) ([]chatgpt.Model, error)
}

func EnableChatGPTModel(ctx context.Context, accounts chatGPTModelCatalogue, repo interfaces.ModelRepository, slug string) (*types.Model, error) {
	return EnableChatGPTModelForType(ctx, accounts, repo, slug, types.ModelTypeKnowledgeQA)
}

// EnableChatGPTModelForType creates an independent personal row for each
// supported use. Keeping the historical KnowledgeQA ID seed unchanged avoids
// breaking existing selections, while VLLM gets its own stable ID.
func EnableChatGPTModelForType(ctx context.Context, accounts chatGPTModelCatalogue, repo interfaces.ModelRepository,
	slug string, modelType types.ModelType,
) (*types.Model, error) {
	uid, err := ChatGPTUser(ctx)
	if err != nil {
		return nil, err
	}
	if modelType != types.ModelTypeKnowledgeQA && modelType != types.ModelTypeVLLM {
		return nil, apperrors.NewBadRequestError("ChatGPT plan supports only chat and vision models")
	}
	slug = strings.TrimSpace(slug)
	models, err := accounts.Models(ctx, uid)
	if err != nil {
		return nil, err
	}
	var selected *chatgpt.Model
	for i := range models {
		if models[i].Slug == slug {
			selected = &models[i]
			break
		}
	}
	if selected == nil {
		return nil, apperrors.NewBadRequestError("select an available ChatGPT model")
	}
	if modelType == types.ModelTypeVLLM && !selected.SupportsVision {
		return nil, apperrors.NewBadRequestError("selected ChatGPT model does not support image input")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	idSeed := fmt.Sprintf("weknora:chatgpt:%d:%s:%s", tenantID, uid, slug)
	if modelType != types.ModelTypeKnowledgeQA {
		idSeed += ":" + string(modelType)
	}
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(idSeed)).String()
	rows, err := repo.List(ctx, tenantID, modelType, "")
	if err != nil {
		return nil, err
	}
	for _, m := range rows {
		if m.ID == id && m.Type == modelType && isChatGPTModel(m) && ownsChatGPTModel(ctx, m) {
			return m, nil
		}
	}
	input := []string{"text"}
	if selected.SupportsVision {
		input = append(input, "image")
	}
	m := &types.Model{ID: id, TenantID: tenantID, Name: slug, DisplayName: "ChatGPT · " + selected.DisplayName,
		Type: modelType, Source: types.ModelSourceRemote, Status: types.ModelStatusActive,
		Description: "Uses the owner's authorized ChatGPT plan for interactive requests", ManagedBy: "chatgpt",
		Parameters: types.ModelParameters{Provider: "openai", BaseURL: "https://api.openai.com/v1",
			SupportsVision: selected.SupportsVision,
			ExtraConfig:    map[string]string{chatGPTPlanKey: "true", chatGPTOwnerKey: uid},
			Spec:           &types.ModelSpecOverride{API: string(api.APIOpenAIResponses), Input: input}},
	}
	if err := repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// personalChatGPTChat resolves a fresh access token at each call, so cached
// clients cannot bypass sign-out or reuse another member's authorization.
type chatGPTTokenSource interface {
	AccessToken(context.Context, string) (string, error)
}

type personalChatGPTChat struct {
	accounts        chatGPTTokenSource
	owner, name, id string
}

func (c *personalChatGPTChat) GetModelName() string { return c.name }
func (c *personalChatGPTChat) GetModelID() string   { return c.id }
func (c *personalChatGPTChat) client(ctx context.Context) (chat.Chat, error) {
	uid, err := ChatGPTUser(ctx)
	if err != nil || uid != c.owner {
		return nil, apperrors.NewForbiddenError("ChatGPT model belongs to another account")
	}
	if c.accounts == nil {
		return nil, fmt.Errorf("ChatGPT connection is unavailable")
	}
	token, err := c.accounts.AccessToken(ctx, uid)
	if err != nil {
		return nil, err
	}
	return openairesponses.New(openairesponses.Config{ChatGPTPlan: true, Settings: api.DefaultOpenAIResponses(), Reasoning: true, Endpoint: api.Endpoint{
		BaseURL: "https://api.openai.com/v1", Model: c.name, ModelID: c.id, Auth: api.BearerAuth(token),
	}}), nil
}
func (c *personalChatGPTChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Chat(ctx, messages, opts)
}
func (c *personalChatGPTChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.ChatStream(ctx, messages, opts)
}
