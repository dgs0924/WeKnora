package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDecodeWikiPendingRowsPartitionsInitiators(t *testing.T) {
	makeRow := func(id int64, kid, user string) *types.TaskPendingOp {
		payload, err := json.Marshal(WikiPendingOp{Op: WikiOpIngest, KnowledgeID: kid, Initiator: types.TaskInitiator{UserID: user, Role: types.TenantRoleAdmin}})
		if err != nil {
			t.Fatal(err)
		}
		return &types.TaskPendingOp{ID: id, Payload: payload}
	}
	s := &wikiIngestService{}
	ops, selected, released := s.decodePendingRowsForInitiator(context.Background(), []*types.TaskPendingOp{
		makeRow(1, "a", "alice"), makeRow(2, "b", "bob"), makeRow(3, "c", "alice"),
	})
	if len(ops) != 2 || len(selected) != 2 || len(released) != 1 {
		t.Fatalf("ops=%#v selected=%v released=%v", ops, selected, released)
	}
	for _, op := range ops {
		if op.Initiator.UserID != "alice" {
			t.Fatalf("mixed actor leaked: %#v", op.Initiator)
		}
	}
}

func TestDecodeWikiPendingRowsKeepsLegacyActorlessSeparate(t *testing.T) {
	legacy, _ := json.Marshal(WikiPendingOp{Op: WikiOpIngest, KnowledgeID: "legacy"})
	modern, _ := json.Marshal(WikiPendingOp{Op: WikiOpIngest, KnowledgeID: "modern", Initiator: types.TaskInitiator{UserID: "admin", Role: types.TenantRoleAdmin}})
	s := &wikiIngestService{}
	ops, selected, released := s.decodePendingRowsForInitiator(context.Background(), []*types.TaskPendingOp{{ID: 1, Payload: legacy}, {ID: 2, Payload: modern}})
	if len(ops) != 1 || ops[0].Initiator.UserID != "admin" || len(selected) != 1 || selected[0] != 2 || len(released) != 1 || released[0] != 1 {
		t.Fatalf("legacy must not inherit admin: ops=%#v selected=%v released=%v", ops, selected, released)
	}
}

func TestDecodeWikiPendingRowsNeverTrimsSameDocumentOtherActor(t *testing.T) {
	makeRow := func(id int64, user string) *types.TaskPendingOp {
		payload, _ := json.Marshal(WikiPendingOp{Op: WikiOpIngest, KnowledgeID: "same-doc", Initiator: types.TaskInitiator{UserID: user, Role: types.TenantRoleAdmin}})
		return &types.TaskPendingOp{ID: id, Payload: payload}
	}
	s := &wikiIngestService{}
	ops, selected, released := s.decodePendingRowsForInitiator(context.Background(), []*types.TaskPendingOp{makeRow(1, "alice"), makeRow(2, "bob")})
	if len(ops) != 1 || ops[0].Initiator.UserID != "alice" || len(selected) != 1 || selected[0] != 1 || len(released) != 1 || released[0] != 2 {
		t.Fatalf("ops=%#v selected=%v released=%v", ops, selected, released)
	}
}

func TestWikiFinalizeRowsPartitionInitiators(t *testing.T) {
	makeRow := func(id int64, user string) *types.TaskPendingOp {
		payload, _ := json.Marshal(wikiFinalizeRow{Slug: "s", Initiator: types.TaskInitiator{UserID: user, Role: types.TenantRoleAdmin}})
		return &types.TaskPendingOp{ID: id, Payload: payload}
	}
	rows, actor := selectWikiFinalizeRowsByInitiator([]*types.TaskPendingOp{makeRow(1, "alice"), makeRow(2, "bob")})
	if len(rows) != 1 || actor.UserID != "alice" {
		t.Fatalf("rows=%#v actor=%#v", rows, actor)
	}
}

func TestWikiInitiatorKeySeparatesAPIKeyAndRole(t *testing.T) {
	base := types.TaskInitiator{UserID: "alice", Role: types.TenantRoleAdmin}
	if wikiInitiatorKey(base) == wikiInitiatorKey(types.TaskInitiator{UserID: "alice", Role: types.TenantRoleAdmin, APIKeyID: 7, APIKeyName: "automation"}) {
		t.Fatal("API-key initiated work must not share a web-user cohort")
	}
	if wikiInitiatorKey(base) == wikiInitiatorKey(types.TaskInitiator{UserID: "alice", Role: types.TenantRoleViewer}) {
		t.Fatal("role snapshot must remain part of the cohort")
	}
}

func TestWithWikiInitiatorDoesNotInheritLegacyAdmin(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.UserIDContextKey, "admin")
	ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleAdmin)
	clean := withWikiInitiator(ctx, types.TaskInitiator{})
	if user, ok := types.UserIDFromContext(clean); ok || user != "" {
		t.Fatalf("legacy inherited user %q", user)
	}
	withUser := withWikiInitiator(ctx, types.TaskInitiator{UserID: "alice", Role: types.TenantRoleViewer})
	if user, _ := types.UserIDFromContext(withUser); user != "alice" {
		t.Fatalf("user=%q", user)
	}
	if role := types.TenantRoleFromContext(withUser); role != types.TenantRoleViewer {
		t.Fatalf("role=%q", role)
	}
}
