package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/chatgpt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type chatGPTAccountsStub struct {
	owner  string
	fail   bool
	models []chatgpt.Model
}

func (s *chatGPTAccountsStub) Start(_ context.Context, owner string) (chatgpt.StartResult, error) {
	s.owner = owner
	if s.fail {
		return chatgpt.StartResult{}, errors.New("private-token-and-path")
	}
	return chatgpt.StartResult{AuthorizationURL: "https://auth.openai.com/api/accounts/authorize", AttemptID: "attempt"}, nil
}
func (s *chatGPTAccountsStub) Status(context.Context, string) (chatgpt.Status, error) {
	return chatgpt.Status{Connected: true, Account: &chatgpt.AccountStatus{EmailHint: "a***@example.com", PlanEnabled: true}}, nil
}
func (s *chatGPTAccountsStub) Models(_ context.Context, owner string) ([]chatgpt.Model, error) {
	s.owner = owner
	return s.models, nil
}
func (s *chatGPTAccountsStub) Disconnect(_ context.Context, owner string) error {
	s.owner = owner
	return nil
}

func TestChatGPTAuthorizeUsesSessionOwnerAndHidesProviderErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &chatGPTAccountsStub{}
	h := &ChatGPTHandler{accounts: stub}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.UserIDContextKey, "alice"))
	})
	r.POST("/authorize", h.Authorize)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(`{"user_id":"bob"}`)))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "alice", stub.owner)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	stub.fail = true
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/authorize", nil))
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.NotContains(t, w.Body.String(), "private-token-and-path")
}
func TestChatGPTBindingRequiresAuthenticatedWebPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &chatGPTAccountsStub{}
	h := &ChatGPTHandler{accounts: stub}
	r := gin.New()
	r.POST("/authorize", h.Authorize)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/authorize", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Empty(t, stub.owner)
}

type chatGPTHandlerModelRepo struct {
	interfaces.ModelRepository
	rows    []*types.Model
	creates []*types.Model
}

func (r *chatGPTHandlerModelRepo) List(_ context.Context, _ uint64, modelType types.ModelType, _ types.ModelSource) ([]*types.Model, error) {
	var rows []*types.Model
	for _, row := range r.rows {
		if modelType == "" || row.Type == modelType {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (r *chatGPTHandlerModelRepo) Create(_ context.Context, model *types.Model) error {
	r.creates = append(r.creates, model)
	r.rows = append(r.rows, model)
	return nil
}

func chatGPTHandlerContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.UserIDContextKey, "alice")
		ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(7))
		c.Request = c.Request.WithContext(ctx)
	}
}

func personalModelRow(id, owner, slug string, modelType types.ModelType) *types.Model {
	return &types.Model{ID: id, Name: slug, Type: modelType,
		Parameters: types.ModelParameters{ExtraConfig: map[string]string{
			"chatgpt_plan": "true", "chatgpt_owner": owner,
		}}}
}

func TestChatGPTModelsFiltersByRequestedCapabilityAndEnabledType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &chatGPTAccountsStub{models: []chatgpt.Model{
		{Slug: "vision", DisplayName: "Vision", SupportsVision: true},
		{Slug: "text", DisplayName: "Text"},
	}}
	repo := &chatGPTHandlerModelRepo{rows: []*types.Model{
		personalModelRow("chat-id", "alice", "vision", types.ModelTypeKnowledgeQA),
		personalModelRow("vision-id", "alice", "vision", types.ModelTypeVLLM),
		personalModelRow("other-id", "bob", "vision", types.ModelTypeVLLM),
	}}
	h := &ChatGPTHandler{accounts: stub, models: repo}
	r := gin.New()
	r.Use(chatGPTHandlerContext())
	r.GET("/models", h.Models)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/models", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"slug":"vision"`)
	require.Contains(t, w.Body.String(), `"slug":"text"`)
	require.Contains(t, w.Body.String(), `"id":"chat-id"`)
	require.Contains(t, w.Body.String(), `"supports_vision":true`)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/models?model_type=VLLM", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"id":"vision-id"`)
	require.Contains(t, w.Body.String(), `"slug":"vision"`)
	require.NotContains(t, w.Body.String(), `"slug":"text"`)
	require.NotContains(t, w.Body.String(), "other-id")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/models?model_type=Embedding", nil))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChatGPTSelectDefaultsToChatAndAcceptsOnlyVisionAlternative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &chatGPTAccountsStub{models: []chatgpt.Model{{Slug: "vision", DisplayName: "Vision", SupportsVision: true}}}
	repo := &chatGPTHandlerModelRepo{}
	h := &ChatGPTHandler{accounts: stub, models: repo}
	r := gin.New()
	r.Use(chatGPTHandlerContext())
	r.POST("/models/select", h.SelectModel)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/models/select", strings.NewReader(`{"slug":"vision"}`)))
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, repo.creates, 1)
	require.Equal(t, types.ModelTypeKnowledgeQA, repo.creates[0].Type)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/models/select", strings.NewReader(`{"slug":"vision","model_type":"VLLM"}`)))
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, repo.creates, 2)
	require.Equal(t, types.ModelTypeVLLM, repo.creates[1].Type)
	require.NotEqual(t, repo.creates[0].ID, repo.creates[1].ID)

	for _, unsupported := range []string{"Embedding", "Rerank", "ASR"} {
		w = httptest.NewRecorder()
		body := `{"slug":"vision","model_type":"` + unsupported + `"}`
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/models/select", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	require.Len(t, repo.creates, 2)
}
