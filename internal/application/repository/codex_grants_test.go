package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func codexGrantRepository(t *testing.T) (*modelRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.User{}, &types.Model{}, &types.CodexModelGrant{}))
	return NewModelRepository(db).(*modelRepository), db
}

func codexGrantModel(id string) *types.Model {
	return &types.Model{ID: id, TenantID: 7, Name: id, ManagedBy: "codex", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{ExtraConfig: map[string]string{"codex_plan": "true", "codex_owner": "owner"}}}
}

func TestReplaceCodexGrantsForUserIsAtomicAndPreservesOtherUsers(t *testing.T) {
	repo, db := codexGrantRepository(t)
	ctx := context.Background()
	for _, user := range []*types.User{{ID: "owner", Username: "owner", Email: "owner@example.com", PasswordHash: "x", IsActive: true}, {ID: "alice", Username: "alice", Email: "alice@example.com", PasswordHash: "x", IsActive: true}, {ID: "bob", Username: "bob", Email: "bob@example.com", PasswordHash: "x", IsActive: true}} {
		require.NoError(t, db.Create(user).Error)
	}
	for _, model := range []*types.Model{codexGrantModel("one"), codexGrantModel("two")} {
		require.NoError(t, db.Create(model).Error)
	}
	plain := &types.Model{ID: "plain", TenantID: 7, Name: "plain", Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}
	require.NoError(t, db.Create(plain).Error)
	require.NoError(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "alice", "owner", []string{"one"}))
	require.NoError(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "bob", "owner", []string{"two"}))

	// The generic repository remains tenant-scoped. The only cross-tenant
	// bridge is the explicit Codex ACL query.
	ordinary, err := repo.GetByID(ctx, 8, "one")
	require.NoError(t, err)
	require.Nil(t, ordinary)
	granted, err := repo.GetGrantedCodexModel(ctx, "owner", "alice", "one")
	require.NoError(t, err)
	require.NotNil(t, granted)
	// A normal model cannot be smuggled into the ACL, and the failed PUT
	// leaves Alice's existing model grant intact.
	require.Error(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "alice", "owner", []string{"plain"}))
	granted, _ = repo.GetGrantedCodexModel(ctx, "owner", "alice", "one")
	require.NotNil(t, granted)

	// One invalid selection must roll the entire replacement back: Alice keeps
	// the old grant and Bob's independent grant is unchanged.
	require.Error(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "alice", "owner", []string{"two", "missing"}))
	got, err := repo.GetGrantedCodexModel(ctx, "owner", "alice", "one")
	require.NoError(t, err)
	require.NotNil(t, got)
	got, err = repo.GetGrantedCodexModel(ctx, "owner", "alice", "two")
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = repo.GetGrantedCodexModel(ctx, "owner", "bob", "two")
	require.NoError(t, err)
	require.NotNil(t, got)

	// A valid replacement revokes the old model for Alice without touching Bob.
	require.NoError(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "alice", "owner", []string{"two"}))
	got, _ = repo.GetGrantedCodexModel(ctx, "owner", "alice", "one")
	require.Nil(t, got)
	got, _ = repo.GetGrantedCodexModel(ctx, "owner", "alice", "two")
	require.NotNil(t, got)
	got, _ = repo.GetGrantedCodexModel(ctx, "owner", "bob", "two")
	require.NotNil(t, got)
}

func TestCodexGrantRejectsInactiveOrBuiltinModels(t *testing.T) {
	repo, db := codexGrantRepository(t)
	ctx := context.Background()
	for _, user := range []*types.User{{ID: "owner", Username: "owner2", Email: "owner2@example.com", PasswordHash: "x", IsActive: true}, {ID: "alice", Username: "alice2", Email: "alice2@example.com", PasswordHash: "x", IsActive: true}} {
		require.NoError(t, db.Create(user).Error)
	}
	inactive := codexGrantModel("inactive")
	inactive.Status = types.ModelStatusDownloadFailed
	builtin := codexGrantModel("builtin")
	builtin.IsBuiltin = true
	require.NoError(t, db.Create(inactive).Error)
	require.NoError(t, db.Create(builtin).Error)
	require.Error(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "alice", "owner", []string{"inactive"}))
	require.Error(t, repo.ReplaceCodexGrantsForUser(ctx, "owner", "alice", "owner", []string{"builtin"}))
}
