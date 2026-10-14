package repository

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUserAgentModelPreferenceRepositoryIsolatesAndClears(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	ddl, err := os.ReadFile("../../../migrations/sqlite/000037_user_agent_model_preferences.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(ddl)).Error)
	repo := NewUserAgentModelPreferenceRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Set(ctx, "user-a", 1, "builtin-quick-answer", "model-a"))
	require.NoError(t, repo.Set(ctx, "user-b", 1, "builtin-quick-answer", "model-b"))
	require.NoError(t, repo.Set(ctx, "user-a", 2, "builtin-quick-answer", "model-tenant-2"))

	got, err := repo.Get(ctx, "user-a", 1, "builtin-quick-answer")
	require.NoError(t, err)
	require.Equal(t, "model-a", got)
	require.NoError(t, repo.Set(ctx, "user-a", 1, "builtin-quick-answer", "model-a-new"))
	got, err = repo.Get(ctx, "user-a", 1, "builtin-quick-answer")
	require.NoError(t, err)
	require.Equal(t, "model-a-new", got)

	require.NoError(t, repo.Clear(ctx, "user-a", 1, "builtin-quick-answer"))
	got, err = repo.Get(ctx, "user-a", 1, "builtin-quick-answer")
	require.NoError(t, err)
	require.Empty(t, got)
	got, err = repo.Get(ctx, "user-b", 1, "builtin-quick-answer")
	require.NoError(t, err)
	require.Equal(t, "model-b", got)
	got, err = repo.Get(ctx, "user-a", 2, "builtin-quick-answer")
	require.NoError(t, err)
	require.Equal(t, "model-tenant-2", got)
}
