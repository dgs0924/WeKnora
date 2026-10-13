package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Tencent/WeKnora/internal/chatgpt"
	"github.com/Tencent/WeKnora/internal/codex"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	"github.com/Tencent/WeKnora/internal/models/utils/ollama"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const codexPlanKey = "codex_plan"
const codexOwnerKey = "codex_owner"

// CodexConnection is intentionally deployment scoped: the owner is selected
// by trusted process configuration, never by an HTTP request or model row.
// An empty owner disables this optional integration completely.
type CodexConnection struct {
	Runtime *codex.Runtime
	Owner   string
	Err     error
}

func NewCodexConnectionFromEnvironment() (*CodexConnection, error) {
	owner := strings.TrimSpace(os.Getenv("WEKNORA_CODEX_OWNER_USER_ID"))
	if owner == "" {
		return &CodexConnection{}, nil
	}
	timeout := 2 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("WEKNORA_CODEX_TIMEOUT_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 1 || seconds > 600 {
			return nil, fmt.Errorf("invalid WEKNORA_CODEX_TIMEOUT_SECONDS")
		}
		timeout = time.Duration(seconds) * time.Second
	}
	maxOutput := 4 << 20
	if raw := strings.TrimSpace(os.Getenv("WEKNORA_CODEX_MAX_OUTPUT_BYTES")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1024 || value > 64<<20 {
			return nil, fmt.Errorf("invalid WEKNORA_CODEX_MAX_OUTPUT_BYTES")
		}
		maxOutput = value
	}
	maxConcurrent := 1
	if raw := strings.TrimSpace(os.Getenv("WEKNORA_CODEX_MAX_CONCURRENT")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 8 {
			return nil, fmt.Errorf("invalid WEKNORA_CODEX_MAX_CONCURRENT")
		}
		maxConcurrent = value
	}
	runtime, err := codex.New(codex.Config{
		Binary:         strings.TrimSpace(os.Getenv("WEKNORA_CODEX_BINARY")),
		Home:           strings.TrimSpace(os.Getenv("WEKNORA_CODEX_HOME")),
		WorkDir:        strings.TrimSpace(os.Getenv("WEKNORA_CODEX_WORKDIR")),
		Timeout:        timeout,
		MaxOutputBytes: maxOutput,
		MaxConcurrent:  maxConcurrent,
	})
	if err != nil {
		return &CodexConnection{Owner: owner, Err: err}, nil
	}
	return &CodexConnection{Runtime: runtime, Owner: owner}, nil
}

// CodexUser admits only the configured web-session user. A background worker
// may use it only after restoring a trusted TaskInitiator for that same user;
// API-key and actorless contexts are rejected.
func CodexUser(ctx context.Context, owner string) (string, error) {
	uid, err := ChatGPTUser(ctx)
	if err != nil || owner == "" || uid != owner {
		return "", apperrors.NewForbiddenError("Codex plan requires the configured personal web account")
	}
	return uid, nil
}

func isCodexModel(m *types.Model) bool {
	return m != nil && m.Parameters.ExtraConfig[codexPlanKey] == "true"
}

func ownsCodexModel(ctx context.Context, m *types.Model) bool {
	if !isCodexModel(m) || m.ManagedBy != "codex" || m.IsBuiltin {
		return false
	}
	configuredOwner := strings.TrimSpace(os.Getenv("WEKNORA_CODEX_OWNER_USER_ID"))
	if configuredOwner == "" || configuredOwner != m.Parameters.ExtraConfig[codexOwnerKey] {
		return false
	}
	uid, err := ChatGPTUser(ctx)
	return err == nil && uid == configuredOwner
}

// canUseCodexModel admits the configured deployment owner and explicitly
// granted active platform users only. It is rechecked for every request so
// revocation takes effect before the next upstream request.
func canUseCodexModel(ctx context.Context, m *types.Model, configuredOwner string, grants interfaces.CodexGrantRepository, users interfaces.UserRepository) bool {
	if configuredOwner == "" {
		configuredOwner = strings.TrimSpace(os.Getenv("WEKNORA_CODEX_OWNER_USER_ID"))
	}
	if !isCodexModel(m) || m.ManagedBy != "codex" || m.IsBuiltin || (m.Type != types.ModelTypeKnowledgeQA && m.Type != types.ModelTypeVLLM) || m.Status != types.ModelStatusActive || configuredOwner == "" || m.Parameters.ExtraConfig[codexOwnerKey] != configuredOwner {
		return false
	}
	if m.Type == types.ModelTypeVLLM && !m.Parameters.SupportsVision {
		return false
	}
	uid, err := ChatGPTUser(ctx)
	if err != nil {
		return false
	}
	if users == nil || grants == nil {
		return false
	}
	owner, err := users.GetUserByID(ctx, configuredOwner)
	if err != nil || owner == nil || !owner.IsActive {
		return false
	}
	grantee, err := users.GetUserByID(ctx, uid)
	if err != nil || grantee == nil || !grantee.IsActive {
		return false
	}
	if uid == configuredOwner {
		return true
	}
	granted, err := grants.GetGrantedCodexModel(ctx, configuredOwner, uid, m.ID)
	return err == nil && granted != nil && granted.ID == m.ID
}

func NewModelServiceWithChatGPTAndCodex(repo interfaces.ModelRepository, kb interfaces.KnowledgeBaseRepository,
	agents interfaces.CustomAgentRepository, ol *ollama.OllamaService, pool embedding.EmbedderPooler,
	tenants interfaces.TenantService, accounts *chatgpt.Manager, connection *CodexConnection,
	users interfaces.UserRepository,
) interfaces.ModelService {
	s := NewModelService(repo, kb, agents, ol, pool, tenants).(*modelService)
	s.chatGPT = accounts
	if connection != nil {
		s.codex = connection.Runtime
		s.codexOwner = connection.Owner
	}
	s.codexUsers = users
	return s
}

// EnableCodexModel persists only a model reference. The logged-in Codex CLI
// state remains outside the database and cannot be forged from model fields.
func EnableCodexModel(ctx context.Context, connection *CodexConnection, repo interfaces.ModelRepository, slug string) (*types.Model, error) {
	return EnableCodexModelForType(ctx, connection, repo, slug, types.ModelTypeKnowledgeQA)
}
func EnableCodexModelForType(ctx context.Context, connection *CodexConnection, repo interfaces.ModelRepository, slug string, modelType types.ModelType) (*types.Model, error) {
	if connection == nil || connection.Runtime == nil {
		return nil, apperrors.NewForbiddenError("Codex plan is not enabled on this server")
	}
	uid, err := CodexUser(ctx, connection.Owner)
	if err != nil {
		return nil, err
	}
	if modelType != types.ModelTypeKnowledgeQA && modelType != types.ModelTypeVLLM {
		return nil, apperrors.NewBadRequestError("Codex plan supports only chat and vision models")
	}
	status, err := connection.Runtime.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !status.LoggedIn {
		return nil, apperrors.NewForbiddenError("Codex CLI is not signed in")
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, apperrors.NewBadRequestError("select an available Codex model")
	}
	models, err := connection.Runtime.Models(ctx)
	if err != nil {
		return nil, err
	}
	var selected *codex.Model
	for i := range models {
		if models[i].ID == slug || models[i].Name == slug {
			selected = &models[i]
			break
		}
	}
	if selected == nil {
		return nil, apperrors.NewBadRequestError("select an available Codex model")
	}
	supportsVision := false
	for _, modality := range selected.InputModalities {
		if modality == "image" {
			supportsVision = true
		}
	}
	if modelType == types.ModelTypeVLLM && !supportsVision {
		return nil, apperrors.NewBadRequestError("selected Codex model does not support image input")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	name := selected.Name
	if name == "" {
		name = selected.ID
	}
	id := codexModelID(tenantID, uid, name, modelType)
	rows, err := repo.List(ctx, tenantID, modelType, "")
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if isCodexModel(row) && ownsCodexModel(ctx, row) && (row.ID == id || (modelType == types.ModelTypeKnowledgeQA && row.Name == name)) {
			return row, nil
		}
	}
	display := selected.DisplayName
	if display == "" {
		display = name
	}
	// A removed model is soft-deleted. A fresh ID allows the owner to enable
	// it again without colliding with the historical row's primary key.
	input := []string{"text"}
	if supportsVision {
		input = append(input, "image")
	}
	m := &types.Model{ID: id, TenantID: tenantID, Name: name, DisplayName: "Codex · " + display,
		Type: modelType, Source: types.ModelSourceRemote, Status: types.ModelStatusActive,
		Description: "Uses the server's configured Codex account for the designated personal user", ManagedBy: "codex",
		Parameters: types.ModelParameters{Provider: "openai", SupportsVision: supportsVision, ExtraConfig: map[string]string{codexPlanKey: "true", codexOwnerKey: uid},
			Spec: &types.ModelSpecOverride{API: string(api.APIOpenAIResponses), Input: input}},
	}
	if err := repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func codexModelID(tenantID uint64, owner, slug string, modelType types.ModelType) string {
	seed := fmt.Sprintf("weknora:codex:%d:%s:%s", tenantID, owner, slug)
	if modelType != types.ModelTypeKnowledgeQA {
		seed += ":" + string(modelType)
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(seed)).String()
}

type personalCodexChat struct {
	runtime         *codex.Runtime
	owner, name, id string
	repo            interfaces.ModelRepository
	users           interfaces.UserRepository
}

func (c *personalCodexChat) GetModelName() string { return c.name }
func (c *personalCodexChat) GetModelID() string   { return c.id }
func (c *personalCodexChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Chat(ctx, messages, opts)
}
func (c *personalCodexChat) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.ChatStream(ctx, messages, opts)
}
func (c *personalCodexChat) client(ctx context.Context) (chat.Chat, error) {
	if c.runtime == nil {
		return nil, apperrors.NewForbiddenError("Codex model belongs to another account")
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 || c.repo == nil {
		return nil, apperrors.NewForbiddenError("Codex model belongs to another account")
	}
	model, err := c.authorize(ctx)
	if err != nil {
		return nil, err
	}
	return c.runtime.ChatAuthorized(model.Name, model.ID, func(callCtx context.Context) error { _, err := c.authorize(callCtx); return err }), nil
}

// authorize re-reads the model and ACL. Runtime invokes it again after its
// single-request gate is acquired, closing the revoke-while-queued window.
func (c *personalCodexChat) authorize(ctx context.Context) (*types.Model, error) {
	if c.runtime == nil {
		return nil, apperrors.NewForbiddenError("Codex model belongs to another account")
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 || c.repo == nil {
		return nil, apperrors.NewForbiddenError("Codex model belongs to another account")
	}
	grants, ok := c.repo.(interfaces.CodexGrantRepository)
	if !ok {
		return nil, apperrors.NewForbiddenError("Codex model belongs to another account")
	}
	uid := codexContextUserID(ctx)
	var model *types.Model
	var err error
	if uid == c.owner {
		model, err = c.repo.GetByID(ctx, tenantID, c.id)
	} else {
		model, err = grants.GetGrantedCodexModel(ctx, c.owner, uid, c.id)
	}
	if err != nil || model == nil || model.Status != types.ModelStatusActive || !canUseCodexModel(ctx, model, c.owner, grants, c.users) {
		return nil, apperrors.NewForbiddenError("Codex model belongs to another account")
	}
	return model, nil
}

func codexContextUserID(ctx context.Context) string {
	uid, _ := types.UserIDFromContext(ctx)
	return uid
}
