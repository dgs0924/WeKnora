package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexHandlerRepo struct {
	interfaces.ModelRepository
	models       []*types.Model
	grants       []*types.CodexModelGrant
	replaceCalls int
	lastIDs      []string
}

func (r *codexHandlerRepo) ReplaceCodexGrantsForUser(_ context.Context, _, _, _ string, ids []string) error {
	r.replaceCalls++
	r.lastIDs = append([]string(nil), ids...)
	return nil
}
func (r *codexHandlerRepo) ListCodexModelsForOwner(context.Context, string) ([]*types.Model, error) {
	return r.models, nil
}
func (r *codexHandlerRepo) ListGrantedCodexModels(context.Context, string, string) ([]*types.Model, error) {
	return nil, nil
}
func (r *codexHandlerRepo) GetGrantedCodexModel(context.Context, string, string, string) (*types.Model, error) {
	return nil, nil
}
func (r *codexHandlerRepo) ListCodexGrants(context.Context, string) ([]*types.CodexModelGrant, error) {
	return r.grants, nil
}

type codexHandlerUsers struct {
	interfaces.UserRepository
	users map[string]*types.User
}

func (u *codexHandlerUsers) GetUserByID(_ context.Context, id string) (*types.User, error) {
	return u.users[id], nil
}

func codexHandlerContext(req *http.Request, uid string, admin bool, apiKey bool) *http.Request {
	ctx := context.WithValue(req.Context(), types.UserIDContextKey, uid)
	ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: uid})
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, admin)
	if apiKey {
		ctx = types.WithAuditAPIKey(ctx, types.AuditAPIKey{ID: 1, Name: "key"})
	}
	return req.WithContext(ctx)
}

func TestCodexStatusHidesServerConnectionFromOtherUsers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CodexHandler{connection: &service.CodexConnection{Owner: "alice"}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "bob")
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "bob"})
		c.Request = c.Request.WithContext(ctx)
	})
	r.GET("/status", h.Status)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":false`)
	require.NotContains(t, w.Body.String(), "email")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestCodexOwnerSeesConfigurationFailureWithoutAccountDisclosure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &CodexHandler{connection: &service.CodexConnection{Owner: "alice"}}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "alice")
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalWebUser, ID: "alice"})
		c.Request = c.Request.WithContext(ctx)
	})
	r.GET("/status", h.Status)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"connection_error":"unavailable"`)
	require.Contains(t, w.Body.String(), `"manage_grants":false`)
	require.NotContains(t, w.Body.String(), "email")
}

func TestCodexGrantManagementAuthorizationAndEmptyACL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &codexHandlerRepo{models: []*types.Model{{ID: "m1", Name: "gpt", DisplayName: "Codex · GPT"}}}
	users := &codexHandlerUsers{users: map[string]*types.User{"target": {ID: "target", IsActive: true}}}
	h := &CodexHandler{connection: &service.CodexConnection{Owner: "owner"}, models: repo, users: users}
	for _, tc := range []struct {
		name, uid  string
		admin, api bool
		code       int
	}{
		{"owner-admin", "owner", true, false, http.StatusOK},
		{"owner-not-admin", "owner", false, false, http.StatusForbidden},
		{"other-admin", "other", true, false, http.StatusUnauthorized},
		{"api-key", "owner", true, true, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Request = codexHandlerContext(c.Request, tc.uid, tc.admin, tc.api) })
			r.GET("/grants", h.Grants)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/grants", nil))
			require.Equal(t, tc.code, w.Code)
			if tc.code == http.StatusOK {
				require.Contains(t, w.Body.String(), `"user_ids":[]`)
			}
		})
	}
}

func TestCodexGrantReplaceRejectsMissingOrNullAndAllowsExplicitEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &codexHandlerRepo{}
	users := &codexHandlerUsers{users: map[string]*types.User{"target": {ID: "target", IsActive: true}}}
	h := &CodexHandler{connection: &service.CodexConnection{Owner: "owner"}, models: repo, users: users}
	for _, body := range []string{"{}", `{"model_ids":null}`} {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Request = codexHandlerContext(c.Request, "owner", true, false) })
		r.PUT("/grants/users/:userID", h.ReplaceUserGrants)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/grants/users/target", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, repo.replaceCalls)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Request = codexHandlerContext(c.Request, "owner", true, false) })
	r.PUT("/grants/users/:userID", h.ReplaceUserGrants)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/grants/users/target", strings.NewReader(`{"model_ids":[]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, repo.replaceCalls)
	require.Empty(t, repo.lastIDs)
}
