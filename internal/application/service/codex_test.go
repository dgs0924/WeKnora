package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/codex"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func codexTestModel(owner string) *types.Model {
	return &types.Model{ID: "codex-personal", Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{ExtraConfig: map[string]string{codexPlanKey: "true", codexOwnerKey: owner}}}
}

func TestCodexModelsAreVisibleOnlyToConfiguredOwner(t *testing.T) {
	t.Setenv("WEKNORA_CODEX_OWNER_USER_ID", "alice")
	mine, other := codexTestModel("alice"), codexTestModel("bob")
	s := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{mine, other}}}
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
	svc := &modelService{repo: &chatGPTListRepo{rows: []*types.Model{row}}, codex: runtime, codexOwner: "alice"}
	resolved, err := svc.GetChatModel(chatGPTTestContext("alice"), row.ID)
	require.NoError(t, err)
	require.IsType(t, &personalCodexChat{}, resolved)
	_, err = svc.GetChatModel(chatGPTTestContext("bob"), row.ID)
	require.Error(t, err)
}
