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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type chatGPTAccountsStub struct {
	owner string
	fail  bool
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
func (s *chatGPTAccountsStub) Models(context.Context, string) ([]chatgpt.Model, error) {
	return nil, nil
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
