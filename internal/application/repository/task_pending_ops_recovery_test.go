package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecoverWikiOpsScopedIdempotentAndPreservesCounters(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TaskPendingOp{}))
	for _, ddl := range []string{
		`CREATE TABLE knowledge_bases(id TEXT PRIMARY KEY,tenant_id INTEGER,indexing_strategy TEXT,deleted_at DATETIME)`,
		`CREATE TABLE knowledges(id TEXT PRIMARY KEY,tenant_id INTEGER,knowledge_base_id TEXT,parse_status TEXT,pending_subtasks_count INTEGER,deleted_at DATETIME)`,
		`CREATE TABLE knowledge_processing_spans(knowledge_id TEXT,attempt INTEGER)`,
		`CREATE TABLE wiki_pages(knowledge_base_id TEXT,tenant_id INTEGER,source_refs TEXT,deleted_at DATETIME)`,
		`CREATE TABLE chunks(knowledge_id TEXT,deleted_at DATETIME)`,
		`INSERT INTO knowledge_bases VALUES('kb',7,CAST('{"wiki_enabled":true}' AS BLOB),NULL)`,
		`INSERT INTO knowledges VALUES('doc',7,'kb','finalizing',1,NULL),('done',7,'kb','completed',0,NULL),('gone',7,'kb','deleting',0,'2026-01-01')`,
		`INSERT INTO knowledge_processing_spans VALUES('doc',2),('done',1)`,
		`INSERT INTO chunks VALUES('done',NULL)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
	actor := types.TaskInitiator{UserID: "alice", Role: types.TenantRoleAdmin}
	makeRow := func(id int64, tenant uint64, kb, op, kid string, who types.TaskInitiator, attempt int, claimed bool) {
		body, _ := json.Marshal(map[string]interface{}{"op": op, "knowledge_id": kid, "initiator": who, "attempt": attempt, "unknown": "keep"})
		row := types.TaskPendingOp{ID: id, TenantID: tenant, TaskType: "wiki:ingest", Scope: "knowledge_base", ScopeID: kb, Op: op, DedupKey: kid, Payload: body}
		if claimed {
			now := time.Now()
			row.ClaimedAt = &now
		}
		require.NoError(t, db.Create(&row).Error)
	}
	makeRow(1, 7, "kb", "ingest", "doc", types.TaskInitiator{}, 2, false)
	makeRow(2, 7, "kb", "retract", "gone", types.TaskInitiator{}, 0, false)
	makeRow(3, 7, "kb", "ingest", "doc", types.TaskInitiator{UserID: "bob"}, 2, false)
	makeRow(4, 7, "kb", "ingest", "doc", types.TaskInitiator{}, 2, true)
	makeRow(5, 8, "kb", "ingest", "doc", types.TaskInitiator{}, 2, false)
	makeRow(6, 7, "elsewhere", "ingest", "doc", types.TaskInitiator{}, 2, false)
	makeRow(7, 7, "kb", "ingest", "doc", types.TaskInitiator{}, 1, false)
	repo := &taskPendingOpsRepository{db: db}
	recovered, skipped, err := repo.RecoverLegacyWikiOps(context.Background(), 7, "kb", actor)
	require.NoError(t, err)
	require.Equal(t, 3, recovered)
	require.Equal(t, 3, skipped)
	var rows []types.TaskPendingOp
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 8)
	for _, index := range []int{0, 1, 7} {
		var body struct{ Initiator types.TaskInitiator }
		require.NoError(t, json.Unmarshal(rows[index].Payload, &body))
		require.Equal(t, actor, body.Initiator)
	}
	for _, index := range []int{3, 4, 5, 6} {
		var body struct{ Initiator types.TaskInitiator }
		require.NoError(t, json.Unmarshal(rows[index].Payload, &body))
		require.Empty(t, body.Initiator.UserID)
	}
	require.Contains(t, string(rows[0].Payload), `"unknown":"keep"`)
	var count int
	require.NoError(t, db.Table("knowledges").Select("pending_subtasks_count").Where("id = ?", "doc").Scan(&count).Error)
	require.Equal(t, 1, count)
	_, _, err = repo.RecoverLegacyWikiOps(context.Background(), 7, "kb", actor)
	require.NoError(t, err)
	var total int64
	require.NoError(t, db.Model(&types.TaskPendingOp{}).Count(&total).Error)
	require.EqualValues(t, 8, total)
	_, _, err = repo.RecoverLegacyWikiOps(context.Background(), 7, "kb", types.TaskInitiator{APIKeyID: 1})
	require.Error(t, err)
}
