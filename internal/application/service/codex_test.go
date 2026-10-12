package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/codex"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type codexGrantTestRepo struct {
	interfaces.ModelRepository
	allowed *types.Model
}

func (*codexGrantTestRepo) GetByID(context.Context, uint64, string) (*types.Model, error) {
	return nil, nil
}

func (r *codexGrantTestRepo) GetGrantedCodexModel(_ context.Context, _, _, id string) (*types.Model, error) {
	if r.allowed != nil && r.allowed.ID == id {
		return r.allowed, nil
	}
	return nil, nil
}
func (*codexGrantTestRepo) ReplaceCodexGrantsForUser(context.Context, string, string, string, []string) error {
	return nil
}
func (*codexGrantTestRepo) ListCodexModelsForOwner(context.Context, string) ([]*types.Model, error) {
	return nil, nil
}
func (*codexGrantTestRepo) ListGrantedCodexModels(context.Context, string, string) ([]*types.Model, error) {
	return nil, nil
}
func (*codexGrantTestRepo) ListCodexGrants(context.Context, string) ([]*types.CodexModelGrant, error) {
	return nil, nil
}

type codexUserTestRepo struct {
	interfaces.UserRepository
	rows map[string]*types.User
}

func (r *codexUserTestRepo) GetUserByID(_ context.Context, id string) (*types.User, error) {
	return r.rows[id], nil
}

func codexGrantContext(uid string) context.Context { return chatGPTTestContext(uid) }

func codexTestModel(owner string) *types.Model {
	return &types.Model{ID: "codex-personal", ManagedBy: "codex", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{ExtraConfig: map[string]string{codexPlanKey: "true", codexOwnerKey: owner}}}
}

func TestCodexModelsAreVisibleOnlyToConfiguredOwner(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "alice")
	mine, other := codexTestModel("alice"), codexTestModel("bob")
	s := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{mine, other}}, codexOwner: "alice", codexUsers: &codexUserTestRepo{rows: map[string]*types.User{"alice": {ID: "alice", IsActive: true}, "bob": {ID: "bob", IsActive: true}}}}
	rows, err := s.ListModels(chatGPTTestContext("alice"))
	require.NoError(t, err)
	require.Equal(t, []*types.Model{mine}, rows)
	rows, err = s.ListModels(chatGPTTestContext("bob"))
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestCodexRejectsAPIKeysAndForgedModelRows(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "alice")
	apiCtx := types.WithAuditAPIKey(chatGPTTestContext("alice"), types.AuditAPIKey{ID: 9, Name: "tenant-key"})
	_, err := CodexUser(apiCtx, "alice")
	require.Error(t, err)

	s := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{codexTestModel("alice")}}}
	require.Error(t, s.CreateModel(chatGPTTestContext("alice"), codexTestModel("alice")), "request models may not forge a Codex owner")
	require.Error(t, s.UpdateModel(chatGPTTestContext("alice"), &types.Model{ID: "codex-personal"}), "stored personal plan rows may not be rewritten")
	require.Error(t, s.DeleteModel(chatGPTTestContext("bob"), "codex-personal"), "another workspace member may not delete the owner's model")
	require.Error(t, s.DeleteModel(apiCtx, "codex-personal"), "API keys may not delete personal Codex models")
}

func TestCodexTrustedTaskInitiatorCanUseConfiguredOwner(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "alice")
	ctx := types.TaskInitiator{UserID: "alice", Role: types.TenantRoleAdmin}.Apply(context.Background())
	_, err := CodexUser(ctx, "alice")
	require.NoError(t, err)
	_, err = CodexUser(context.Background(), "alice")
	require.Error(t, err, "legacy background work without an initiator must not use Codex")
}

func TestCodexChatResolutionChecksConfiguredOwnerBeforeRuntimeUse(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "alice")
	runtime, err := codex.New(codex.Config{Binary: "/bin/true", Home: "/tmp", WorkDir: "/tmp"})
	require.NoError(t, err)
	row := codexTestModel("alice")
	row.Name = "gpt-test"
	row.Type = types.ModelTypeKnowledgeQA
	svc := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{row}}, codex: runtime, codexOwner: "alice", codexUsers: &codexUserTestRepo{rows: map[string]*types.User{"alice": {ID: "alice", IsActive: true}, "bob": {ID: "bob", IsActive: true}}}}
	resolved, err := svc.GetChatModel(chatGPTTestContext("alice"), row.ID)
	require.NoError(t, err)
	require.IsType(t, &personalCodexChat{}, resolved)
	_, err = svc.GetChatModel(chatGPTTestContext("bob"), row.ID)
	require.Error(t, err)
}

func TestCodexGlobalGrantAccessAndRevocation(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "owner")
	model := codexTestModel("owner")
	model.ID = "allowed"
	model.TenantID = 99
	model.Name = "gpt-test"
	model.Type = types.ModelTypeKnowledgeQA
	grants := &codexGrantTestRepo{allowed: model}
	users := &codexUserTestRepo{rows: map[string]*types.User{"owner": {ID: "owner", IsActive: true}, "alice": {ID: "alice", IsActive: true}, "disabled": {ID: "disabled", IsActive: false}}}
	require.True(t, canUseCodexModel(codexGrantContext("alice"), model, "owner", grants, users))
	require.False(t, canUseCodexModel(codexGrantContext("alice"), codexTestModel("owner"), "owner", grants, users), "other model must not inherit a grant")
	require.False(t, canUseCodexModel(codexGrantContext("disabled"), model, "owner", grants, users))
	users.rows["owner"].IsActive = false
	require.False(t, canUseCodexModel(codexGrantContext("alice"), model, "owner", grants, users), "disabled owner revokes all grants")
	users.rows["owner"].IsActive = true
	runtime, err := codex.New(codex.Config{Binary: "/bin/true", Home: "/tmp", WorkDir: "/tmp"})
	require.NoError(t, err)
	// The model belongs to tenant 99 while Alice's request is tenant 7. The
	// normal tenant lookup misses, then the Codex-only ACL lookup admits it.
	svc := &modelService{repo: grants, codex: runtime, codexOwner: "owner", codexUsers: users}
	resolved, err := svc.GetChatModel(codexGrantContext("alice"), model.ID)
	require.NoError(t, err)
	require.IsType(t, &personalCodexChat{}, resolved)
	client := &personalCodexChat{runtime: runtime, owner: "owner", id: model.ID, name: model.Name, repo: grants, users: users}
	_, err = client.client(codexGrantContext("alice"))
	require.NoError(t, err)
	grants.allowed = nil
	_, err = client.client(codexGrantContext("alice"))
	require.Error(t, err, "cached model client must re-check a revoked grant")
}

func TestCodexVLMRequiresSeparateVisionRowAndCurrentGrant(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "owner")
	vision := codexTestModel("owner")
	vision.ID, vision.Name, vision.TenantID = "vision", "gpt-vision", 99
	vision.Type, vision.Parameters.SupportsVision = types.ModelTypeVLLM, true
	grants := &codexGrantTestRepo{allowed: vision}
	users := &codexUserTestRepo{rows: map[string]*types.User{"owner": {ID: "owner", IsActive: true}, "alice": {ID: "alice", IsActive: true}}}
	runtime, err := codex.New(codex.Config{Binary: "/bin/true", Home: "/tmp", WorkDir: "/tmp"})
	require.NoError(t, err)
	svc := &modelService{repo: grants, codex: runtime, codexOwner: "owner", codexUsers: users}
	resolved, err := svc.GetVLMModel(codexGrantContext("alice"), vision.ID)
	require.NoError(t, err)
	require.IsType(t, &personalChatGPTVLM{}, resolved)
	// A text row with the same provider connection is never a vision model.
	text := codexTestModel("owner")
	text.ID = "text"
	text.Parameters.SupportsVision = false
	grants.allowed = text
	_, err = svc.GetVLMModel(codexGrantContext("alice"), text.ID)
	require.Error(t, err)
	vision.Parameters.SupportsVision = false
	grants.allowed = vision
	_, err = svc.GetVLMModel(codexGrantContext("alice"), vision.ID)
	require.Error(t, err)
	vision.Parameters.SupportsVision = true
	// Revocation is observed by a newly resolved VLM and its nested chat.
	grants.allowed = nil
	_, err = svc.GetVLMModel(codexGrantContext("alice"), vision.ID)
	require.Error(t, err)
}

func TestCodexVisionModelIDIsIndependentFromLegacyChatID(t *testing.T) {
	chatID := codexModelID(7, "owner", "gpt-test", types.ModelTypeKnowledgeQA)
	visionID := codexModelID(7, "owner", "gpt-test", types.ModelTypeVLLM)
	require.NotEqual(t, chatID, visionID)
	require.Equal(t, chatID, codexModelID(7, "owner", "gpt-test", types.ModelTypeKnowledgeQA))
}
